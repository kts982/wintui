package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// withAppSettings runs body with appSettings replaced and restored on exit.
// Cleanup tests mutate auto-scan and enabled-targets globally; this keeps
// each test hermetic without exporting a setter.
func withAppSettings(t *testing.T, replacement Settings, body func()) {
	t.Helper()
	original := appSettings
	t.Cleanup(func() { appSettings = original })
	appSettings = replacement
	body()
}

// updateExpectingScreen casts the result of cleanupScreen.update through the
// screen interface back into a cleanupScreen for further assertions.
func updateExpectingScreen(t *testing.T, s cleanupScreen, msg tea.Msg) (cleanupScreen, tea.Cmd) {
	t.Helper()
	out, cmd := s.update(msg)
	cs, ok := out.(cleanupScreen)
	if !ok {
		t.Fatalf("update returned %T, want cleanupScreen", out)
	}
	return cs, cmd
}

// ── Construction + visibility ─────────────────────────────────────────

func TestCleanupScreenInitialStateIsReady(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		if s.state != cleanupReady {
			t.Errorf("initial state = %v, want cleanupReady", s.state)
		}
		if len(s.visible) == 0 {
			t.Error("visible should contain at least the always-on Core Temp targets")
		}
	})
}

func TestCleanupScreenSeedsCheckedFromSettings(t *testing.T) {
	settings := DefaultSettings()
	settings.CleanupEnabledTargets = []string{"go_build", "yarn_cache"}
	withAppSettings(t, settings, func() {
		s := newCleanupScreen()
		// Default-checked rows are always seeded on (regardless of persisted list).
		if !s.checked["user_temp"] {
			t.Error("default-checked user_temp should be seeded on")
		}
		// Opt-in rows are seeded only when present in the registry visible set.
		// They might be detect-if-present absent on this machine — we only
		// assert that the seeding logic *would* mark them on if visible.
		for _, idx := range s.visible {
			def := s.targets[idx]
			if def.id == "go_build" && !s.checked["go_build"] {
				t.Error("persisted opt-in go_build should be seeded on when visible")
			}
		}
	})
}

// ── Auto-scan dispatch ────────────────────────────────────────────────

func TestCleanupBeginAutoScanOffQueuesNothing(t *testing.T) {
	settings := DefaultSettings()
	settings.CleanupAutoScan = CleanupAutoScanOff
	withAppSettings(t, settings, func() {
		s := newCleanupScreen()
		cmd := s.beginAutoScan()
		if cmd != nil {
			t.Errorf("beginAutoScan(off) = %v, want nil", cmd)
		}
		if len(s.inflight) != 0 {
			t.Errorf("off should not register any inflight scans, got %d", len(s.inflight))
		}
	})
}

func TestCleanupBeginAutoScanSafeSkipsDeveloper(t *testing.T) {
	settings := DefaultSettings() // CleanupAutoScan = "" → safe
	withAppSettings(t, settings, func() {
		s := newCleanupScreen()
		_ = s.beginAutoScan()
		// No Developer-group target should appear in s.inflight.
		for id := range s.inflight {
			def, _ := cleanupTargetByID(id)
			if def.group == cleanupGroupDeveloper {
				t.Errorf("safe mode queued developer target %q", id)
			}
		}
		// And at least one Core Temp scan should have been queued.
		coreQueued := false
		for id := range s.inflight {
			def, _ := cleanupTargetByID(id)
			if def.group == cleanupGroupCoreTemp {
				coreQueued = true
				break
			}
		}
		if !coreQueued {
			t.Error("safe mode should queue at least one core_temp scan")
		}
	})
}

func TestCleanupBeginAutoScanAllQueuesEveryVisible(t *testing.T) {
	settings := DefaultSettings()
	settings.CleanupAutoScan = CleanupAutoScanAll
	withAppSettings(t, settings, func() {
		s := newCleanupScreen()
		_ = s.beginAutoScan()
		if len(s.inflight) != len(s.visible) {
			t.Errorf("all mode queued %d scans, want %d (every visible target)",
				len(s.inflight), len(s.visible))
		}
	})
}

// ── Scan result lifecycle ─────────────────────────────────────────────

func TestCleanupScannedMsgPopulatesResults(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		_ = s.beginAutoScan()

		// Pick any inflight id and feign its completion.
		var someID string
		for id := range s.inflight {
			someID = id
			break
		}
		if someID == "" {
			t.Fatal("expected at least one inflight scan in safe mode")
		}

		s, _ = updateExpectingScreen(t, s, cleanupTargetScannedMsg{
			id:     someID,
			result: cleanupTargetResult{id: someID, sizeBytes: 1234, files: 4},
		})
		if _, still := s.inflight[someID]; still {
			t.Error("scanned id should be removed from inflight")
		}
		if r := s.results[someID]; r.sizeBytes != 1234 || r.files != 4 {
			t.Errorf("results not populated, got %#v", r)
		}
	})
}

func TestCleanupScreenBlurCancelsInflightScans(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		_ = s.beginAutoScan()
		if len(s.inflight) == 0 {
			t.Fatal("test precondition: at least one scan should be queued")
		}
		s, _ = updateExpectingScreen(t, s, screenBlurMsg{})
		if len(s.inflight) != 0 {
			t.Errorf("blur should cancel + clear inflight, still %d", len(s.inflight))
		}
	})
}

// Regression: scans that finish after cancel/rescan must not overwrite
// results from a fresh scan cycle. The generation token in
// cleanupTargetScannedMsg.gen is the guard.
func TestCleanupScannedMsgFromStaleGenerationIsIgnored(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		_ = s.beginAutoScan()

		// Pick any inflight id, capture current gen, then cancel everything.
		var id string
		for k := range s.inflight {
			id = k
			break
		}
		if id == "" {
			t.Skip("no inflight scan to test against")
		}
		staleGen := s.scanGen
		s.cancelAllScans()
		if s.scanGen == staleGen {
			t.Fatal("cancelAllScans should bump scanGen")
		}

		// A late-arriving message stamped with the old generation must
		// not populate results.
		s, _ = updateExpectingScreen(t, s, cleanupTargetScannedMsg{
			id:     id,
			gen:    staleGen,
			result: cleanupTargetResult{id: id, sizeBytes: 999},
		})
		if _, populated := s.results[id]; populated {
			t.Errorf("stale-gen message should be dropped, but results[%q] = %v", id, s.results[id])
		}
	})
}

// Regression: rescan re-registers the same id while a prior goroutine
// might still be in flight. The new scan must not be polluted by the old
// one's result.
func TestCleanupRescanDropsLatePriorGenerationResults(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		_ = s.beginAutoScan()

		var id string
		for k := range s.inflight {
			id = k
			break
		}
		if id == "" {
			t.Skip("no inflight scan to test against")
		}

		oldGen := s.scanGen
		out, _ := s.rescanAll()
		s = out.(cleanupScreen)

		// Old goroutine's result lands. It should be ignored — the new
		// scan cycle's generation is current.
		s, _ = updateExpectingScreen(t, s, cleanupTargetScannedMsg{
			id:     id,
			gen:    oldGen,
			result: cleanupTargetResult{id: id, sizeBytes: 999},
		})
		if r, ok := s.results[id]; ok && r.sizeBytes == 999 {
			t.Errorf("rescan should not be polluted by stale-gen result; got %v", r)
		}
	})
}

func TestCleanupScreenBlurDoesNotCancelDuringExecute(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupExecuting
		// Manually plant a fake inflight to verify it isn't touched.
		_ = s.startScan("user_temp")
		preLen := len(s.inflight)

		s, _ = updateExpectingScreen(t, s, screenBlurMsg{})
		if len(s.inflight) != preLen {
			t.Errorf("execute-state blur should not cancel scans (got len %d, want %d)",
				len(s.inflight), preLen)
		}
	})
}

// ── Selection toggling ────────────────────────────────────────────────

func TestCleanupSpaceTogglesFocusedRow(t *testing.T) {
	settings := DefaultSettings()
	withAppSettings(t, settings, func() {
		s := newCleanupScreen()
		// Move cursor to a non-default-checked target (Developer or GPU).
		// If none visible (unlikely on a dev box but possible), skip.
		var optInIdx = -1
		for i, regIdx := range s.visible {
			if !s.targets[regIdx].defaultChecked {
				optInIdx = i
				break
			}
		}
		if optInIdx < 0 {
			t.Skip("no opt-in target visible on this machine")
		}
		s.cursor = optInIdx
		def := s.targets[s.visible[optInIdx]]

		s, _ = updateExpectingScreen(t, s, tea.KeyPressMsg{Code: ' '})
		if !s.checked[def.id] {
			t.Errorf("space did not check %q", def.id)
		}
		if !appSettings.cleanupTargetEnabled(def) {
			t.Errorf("toggle should have persisted to appSettings")
		}

		// Toggle off again.
		s, _ = updateExpectingScreen(t, s, tea.KeyPressMsg{Code: ' '})
		if s.checked[def.id] {
			t.Errorf("second space did not un-check %q", def.id)
		}
		if appSettings.cleanupTargetEnabled(def) {
			t.Errorf("toggle-off should have removed persisted opt-in")
		}
	})
}

// ── Confirm gate ─────────────────────────────────────────────────────

func TestCleanupConfirmGateBlocksWhenNoCheckedWithSize(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		// Default-checked rows are checked but have no scan results yet, so
		// checkedWithSizeIDs() returns nothing → enter must not transition.
		s, _ = updateExpectingScreen(t, s, tea.KeyPressMsg{Code: tea.KeyEnter})
		if s.state != cleanupReady {
			t.Errorf("enter without scan results should stay in ready, got %v", s.state)
		}
	})
}

func TestCleanupConfirmGatePassesWithSizedCheck(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		// Inject a result for any default-checked target.
		var seedID string
		for _, idx := range s.visible {
			def := s.targets[idx]
			if def.defaultChecked {
				seedID = def.id
				break
			}
		}
		if seedID == "" {
			t.Skip("no default-checked target visible")
		}
		s.results[seedID] = cleanupTargetResult{id: seedID, sizeBytes: 1024}

		s, _ = updateExpectingScreen(t, s, tea.KeyPressMsg{Code: tea.KeyEnter})
		if s.state != cleanupConfirming {
			t.Errorf("enter with sized check should transition to confirming, got %v", s.state)
		}
	})
}

// ── Execute chain ────────────────────────────────────────────────────

func TestCleanupExecutingChainAdvancesIndex(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupExecuting
		// Real registry IDs so dispatchNextDelete returns a real (deferred)
		// cmd instead of taking the unknown-id skip path.
		s.execQueue = []string{"user_temp", "crash_dumps", "wer_reports"}
		s.execResults = map[string]cleanupTargetResult{}

		s, _ = updateExpectingScreen(t, s, cleanupTargetDeletedMsg{
			id:     "user_temp",
			result: cleanupTargetResult{id: "user_temp", freedBytes: 100},
		})
		if s.execIdx != 1 {
			t.Errorf("execIdx after first delete = %d, want 1", s.execIdx)
		}
		if s.state != cleanupExecuting {
			t.Errorf("state after partial = %v, want still executing", s.state)
		}
	})
}

func TestCleanupExecutingFinishesToDone(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupExecuting
		s.execQueue = []string{"only"}
		s.execResults = map[string]cleanupTargetResult{}

		s, _ = updateExpectingScreen(t, s, cleanupTargetDeletedMsg{
			id:     "only",
			result: cleanupTargetResult{id: "only", freedBytes: 100},
		})
		if s.state != cleanupDone {
			t.Errorf("state = %v, want cleanupDone", s.state)
		}
	})
}

func TestCleanupExecutingFinishesToPartialFailure(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupExecuting
		s.execQueue = []string{"only"}
		s.execResults = map[string]cleanupTargetResult{}

		s, _ = updateExpectingScreen(t, s, cleanupTargetDeletedMsg{
			id:     "only",
			result: cleanupTargetResult{id: "only", failed: 1},
		})
		if s.state != cleanupPartialFailure {
			t.Errorf("state = %v, want cleanupPartialFailure", s.state)
		}
	})
}

// ── Modal blocking ───────────────────────────────────────────────────

func TestCleanupBlocksGlobalShortcutsOnlyDuringExecute(t *testing.T) {
	cases := []struct {
		state cleanupState
		want  bool
	}{
		{cleanupReady, false},
		{cleanupConfirming, false},
		{cleanupExecuting, true},
		{cleanupDone, false},
		{cleanupPartialFailure, false},
	}
	for _, tc := range cases {
		s := cleanupScreen{state: tc.state}
		if got := s.blocksGlobalShortcuts(); got != tc.want {
			t.Errorf("state %v: blocksGlobalShortcuts() = %v, want %v", tc.state, got, tc.want)
		}
	}
}

// ── View smoke tests ─────────────────────────────────────────────────

func TestCleanupReadyViewMentionsTargetsAndCount(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		out := s.view(120, 40)
		if !strings.Contains(out, "Cleanup Targets") {
			t.Errorf("ready view should include header; got:\n%s", out)
		}
	})
}

func TestCleanupDoneViewListsResults(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupDone
		s.execResults = map[string]cleanupTargetResult{
			"user_temp": {id: "user_temp", freedBytes: 5 * 1024 * 1024},
		}
		out := s.view(120, 40)
		if !strings.Contains(out, "5.0 MB") {
			t.Errorf("done view should show freed size, got:\n%s", out)
		}
		if !strings.Contains(out, "User temp directory") {
			t.Errorf("done view should label the target, got:\n%s", out)
		}
	})
}

func TestCleanupPartialFailureMentionsAdminRetry(t *testing.T) {
	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupPartialFailure
		s.execResults = map[string]cleanupTargetResult{
			"windows_temp": {
				id:      "windows_temp",
				failed:  3,
				skipped: cleanupSkipNotElevated,
			},
		}
		out := s.view(120, 40)
		if !strings.Contains(out, "Ctrl+E") {
			t.Errorf("partial-failure view should mention Ctrl+E retry, got:\n%s", out)
		}
	})
}

func TestCleanupConfirmViewMentionsAVNoticeWhenAdminTargetIncluded(t *testing.T) {
	// The AV-FP notice in viewConfirm only renders when admin targets are
	// queued AND the current process isn't already elevated. GitHub
	// Actions Windows runners run as Administrator by default, so we pin
	// isElevated to false to exercise the non-elevated render path
	// regardless of where the test runs.
	origIsElevated := isElevated
	t.Cleanup(func() { isElevated = origIsElevated })
	isElevated = func() bool { return false }

	withAppSettings(t, DefaultSettings(), func() {
		s := newCleanupScreen()
		s.state = cleanupConfirming
		s.checked["windows_temp"] = true
		s.results["windows_temp"] = cleanupTargetResult{id: "windows_temp", sizeBytes: 1024}
		out := s.view(120, 40)
		if !strings.Contains(out, "antivirus") {
			t.Errorf("confirm view with admin target should include AV-FP notice, got:\n%s", out)
		}
	})
}
