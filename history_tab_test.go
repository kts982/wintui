package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// withHistoryLoadFn swaps the shared load seam (also used by `wintui history`)
// and restores it on cleanup so no test reads the real %APPDATA% file.
func withHistoryLoadFn(t *testing.T, fn func() (historyEnvelope, error)) {
	t.Helper()
	original := historyLoadFn
	t.Cleanup(func() { historyLoadFn = original })
	historyLoadFn = fn
}

// updateExpectingHistory casts the result of historyScreen.update through the
// screen interface back into a historyScreen for further assertions.
func updateExpectingHistory(t *testing.T, s historyScreen, msg tea.Msg) (historyScreen, tea.Cmd) {
	t.Helper()
	out, cmd := s.update(msg)
	hs, ok := out.(historyScreen)
	if !ok {
		t.Fatalf("update returned %T, want historyScreen", out)
	}
	return hs, cmd
}

// historyTestEnv builds a 3-batch envelope: oldest install (ok), middle
// upgrade (1 ok + 1 failed), newest uninstall (ok). Timestamps ascend with
// index, matching the append-order invariant of the writer.
func historyTestEnv() historyEnvelope {
	base := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	return historyEnvelope{
		Version: historyEnvelopeVersion,
		Records: []historyRecord{
			{
				ID: "b-oldest", Timestamp: base, Trigger: historyTriggerTUI, Action: historyActionInstall,
				Items: []historyItem{
					{ID: "Vendor.Alpha", Name: "Alpha", Action: historyActionInstall, ToVersion: "1.0", Status: historyStatusOK},
				},
				Summary: historySummary{Total: 1, OK: 1},
			},
			{
				ID: "b-middle", Timestamp: base.Add(24 * time.Hour), Trigger: historyTriggerCLIAuto, Action: historyActionUpgrade,
				Items: []historyItem{
					{ID: "Vendor.Alpha", Name: "Alpha", Action: historyActionUpgrade, FromVersion: "1.0", ToVersion: "2.0", Status: historyStatusOK},
					{ID: "Vendor.Beta", Name: "Beta", Action: historyActionUpgrade, FromVersion: "3.0", ToVersion: "3.1", Status: historyStatusError, Error: "installer hash mismatch"},
				},
				Summary: historySummary{Total: 2, OK: 1, Failed: 1},
			},
			{
				ID: "b-newest", Timestamp: base.Add(48 * time.Hour), Trigger: historyTriggerTUI, Action: historyActionUninstall,
				Items: []historyItem{
					{ID: "Vendor.Gamma", Name: "Gamma", Action: historyActionUninstall, FromVersion: "0.9", Status: historyStatusOK},
				},
				Summary: historySummary{Total: 1, OK: 1},
			},
		},
	}
}

func loadedHistoryScreen(t *testing.T, env historyEnvelope) historyScreen {
	t.Helper()
	s := newHistoryScreen()
	s, _ = updateExpectingHistory(t, s, historyLoadedMsg{env: env})
	return s
}

// ── Loading + ordering ────────────────────────────────────────────────

func TestHistoryScreenListsNewestFirst(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	if !s.loaded {
		t.Fatal("loaded should be true after historyLoadedMsg")
	}
	if len(s.visible) != 3 {
		t.Fatalf("visible = %d rows, want 3", len(s.visible))
	}
	first, _ := s.focusedRecord()
	if first.ID != "b-newest" {
		t.Errorf("first row = %q, want b-newest (newest-first)", first.ID)
	}
}

func TestHistoryScreenInitLoadsThroughSeam(t *testing.T) {
	called := false
	withHistoryLoadFn(t, func() (historyEnvelope, error) {
		called = true
		return historyTestEnv(), nil
	})
	s := newHistoryScreen()
	cmd := s.init()
	if cmd == nil {
		t.Fatal("init should return a load cmd")
	}
	msg := cmd()
	if !called {
		t.Error("init cmd should call historyLoadFn")
	}
	loaded, ok := msg.(historyLoadedMsg)
	if !ok {
		t.Fatalf("init cmd returned %T, want historyLoadedMsg", msg)
	}
	if len(loaded.env.Records) != 3 {
		t.Errorf("loaded %d records, want 3", len(loaded.env.Records))
	}
}

func TestHistoryScreenFocusTriggersReload(t *testing.T) {
	withHistoryLoadFn(t, func() (historyEnvelope, error) { return historyTestEnv(), nil })
	s := loadedHistoryScreen(t, historyTestEnv())
	_, cmd := updateExpectingHistory(t, s, screenFocusMsg{})
	if cmd == nil {
		t.Fatal("screenFocusMsg should return a reload cmd")
	}
	if _, ok := cmd().(historyLoadedMsg); !ok {
		t.Error("focus reload cmd should produce a historyLoadedMsg")
	}
}

// ── Failed-only filter ────────────────────────────────────────────────

func TestHistoryScreenFailedOnlyFiltersBatches(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, keyMsg("f"))
	if len(s.visible) != 1 {
		t.Fatalf("failed-only visible = %d, want 1", len(s.visible))
	}
	rec, _ := s.focusedRecord()
	if rec.ID != "b-middle" {
		t.Errorf("failed-only row = %q, want b-middle", rec.ID)
	}
	// Toggle off restores all rows.
	s, _ = updateExpectingHistory(t, s, keyMsg("f"))
	if len(s.visible) != 3 {
		t.Errorf("after toggle-off visible = %d, want 3", len(s.visible))
	}
}

func TestHistoryScreenEscClearsFilterInListMode(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, keyMsg("f"))
	s, _ = updateExpectingHistory(t, s, keyMsg("esc"))
	if s.failedOnly {
		t.Error("esc in list mode should clear the failed-only filter")
	}
	if len(s.visible) != 3 {
		t.Errorf("visible = %d after esc, want 3", len(s.visible))
	}
}

func TestHistoryScreenFilterClampsCursor(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, keyMsg("down"))
	s, _ = updateExpectingHistory(t, s, keyMsg("down"))
	if s.cursor != 2 {
		t.Fatalf("cursor = %d after two downs, want 2", s.cursor)
	}
	s, _ = updateExpectingHistory(t, s, keyMsg("f")) // 1 row remains
	if s.cursor != 0 {
		t.Errorf("cursor = %d after filter shrunk list, want clamped 0", s.cursor)
	}
}

func TestHistoryScreenFailedOnlyFiltersItemsInBatchMode(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, keyMsg("down")) // b-middle
	s, _ = updateExpectingHistory(t, s, keyMsg("enter"))
	if s.mode != historyModeBatch {
		t.Fatal("enter should drill into the focused batch")
	}
	if len(s.visibleItems) != 2 {
		t.Fatalf("visibleItems = %d, want 2", len(s.visibleItems))
	}
	s, _ = updateExpectingHistory(t, s, keyMsg("f"))
	if len(s.visibleItems) != 1 {
		t.Fatalf("failed-only visibleItems = %d, want 1", len(s.visibleItems))
	}
	it, _ := s.focusedItem()
	if it.ID != "Vendor.Beta" {
		t.Errorf("failed-only item = %q, want Vendor.Beta", it.ID)
	}
}

// ── Drill-down navigation ─────────────────────────────────────────────

func TestHistoryScreenDrillDownAndBack(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, keyMsg("enter"))
	if s.mode != historyModeBatch {
		t.Fatal("enter should switch to batch mode")
	}
	batch, ok := s.drilledBatch()
	if !ok || batch.ID != "b-newest" {
		t.Errorf("drilled batch = %q, want b-newest", batch.ID)
	}
	s, _ = updateExpectingHistory(t, s, keyMsg("esc"))
	if s.mode != historyModeBatches {
		t.Error("esc should return to the batch list")
	}
}

func TestHistoryScreenReloadPreservesDrilledBatchByID(t *testing.T) {
	env := historyTestEnv()
	s := loadedHistoryScreen(t, env)
	s, _ = updateExpectingHistory(t, s, keyMsg("down")) // b-middle
	s, _ = updateExpectingHistory(t, s, keyMsg("enter"))

	// Reload with a new record appended: indices shift, ID must re-resolve.
	grown := historyTestEnv()
	grown.Records = append(grown.Records, historyRecord{
		ID: "b-brand-new", Timestamp: time.Now().UTC(), Trigger: historyTriggerTUI,
		Action: historyActionInstall, Summary: historySummary{Total: 1, OK: 1},
		Items: []historyItem{{ID: "Vendor.Delta", Action: historyActionInstall, Status: historyStatusOK}},
	})
	s, _ = updateExpectingHistory(t, s, historyLoadedMsg{env: grown})
	if s.mode != historyModeBatch {
		t.Fatal("drill-down should survive a reload")
	}
	batch, _ := s.drilledBatch()
	if batch.ID != "b-middle" {
		t.Errorf("drilled batch after reload = %q, want b-middle", batch.ID)
	}

	// Reload where the drilled batch was trimmed: fall back to the list.
	s, _ = updateExpectingHistory(t, s, historyLoadedMsg{env: historyEnvelope{Version: historyEnvelopeVersion}})
	if s.mode != historyModeBatches {
		t.Error("vanished batch should drop back to the batch list")
	}
}

// ── Timeline ──────────────────────────────────────────────────────────

func TestHistoryScreenPackageTimelineNewestFirstCaseInsensitive(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	entries := s.packageTimeline("vendor.alpha")
	if len(entries) != 2 {
		t.Fatalf("timeline entries = %d, want 2 (install + upgrade)", len(entries))
	}
	if entries[0].item.Action != historyActionUpgrade || entries[1].item.Action != historyActionInstall {
		t.Errorf("timeline order = [%s, %s], want [upgrade, install] (newest first)",
			entries[0].item.Action, entries[1].item.Action)
	}
	if entries[0].batch != "b-middle" {
		t.Errorf("newest entry batch = %q, want b-middle", entries[0].batch)
	}
}

// ── States: empty / corrupt / future ──────────────────────────────────

func TestHistoryScreenEmptyState(t *testing.T) {
	s := loadedHistoryScreen(t, historyEnvelope{Version: historyEnvelopeVersion})
	view := stripANSI(s.view(120, 30))
	if !strings.Contains(view, "No action history yet") {
		t.Errorf("empty view should carry the no-history message, got:\n%s", view)
	}
}

func TestHistoryScreenLoadErrorState(t *testing.T) {
	s := newHistoryScreen()
	loadErr := errors.New("history file is unreadable (corrupt JSON): C:\\x\\history.json")
	s, _ = updateExpectingHistory(t, s, historyLoadedMsg{err: loadErr})
	view := stripANSI(s.view(120, 30))
	if !strings.Contains(view, "History unavailable") || !strings.Contains(view, "corrupt JSON") {
		t.Errorf("error view should surface the reader error, got:\n%s", view)
	}
	if !strings.Contains(view, "Press r to retry") {
		t.Error("error view should hint at r-to-retry")
	}
	helps := bindingHelps(s.helpKeys())
	if len(helps) != 1 || helps[0].Key != "r" {
		t.Errorf("error-state helpKeys = %v, want just refresh", helps)
	}
	// A successful retry recovers.
	s, _ = updateExpectingHistory(t, s, historyLoadedMsg{env: historyTestEnv()})
	if s.loadErr != nil {
		t.Error("successful reload should clear loadErr")
	}
}

func TestHistoryScreenFutureVersionErrorSurfaces(t *testing.T) {
	// The reader classifies future-version files as errors; the screen renders
	// whatever loadHistory returns. Simulate through the seam end-to-end.
	withHistoryLoadFn(t, func() (historyEnvelope, error) {
		return historyEnvelope{}, errors.New("unsupported history format v9 (this WinTUI reads up to v1; upgrade WinTUI)")
	})
	s := newHistoryScreen()
	msg := s.init()()
	s, _ = updateExpectingHistory(t, s, msg)
	view := stripANSI(s.view(120, 30))
	if !strings.Contains(view, "unsupported history format v9") {
		t.Errorf("future-version error should surface in the view, got:\n%s", view)
	}
}

// ── View smoke ────────────────────────────────────────────────────────

func TestHistoryScreenViewBatchesRendersCoreFields(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, tea.WindowSizeMsg{Width: 120, Height: 40})
	view := stripANSI(s.view(120, 38))
	for _, want := range []string{"Action History", "Batches", "uninstall", "Gamma", "[tui]", "Batch details"} {
		if !strings.Contains(view, want) {
			t.Errorf("batches view missing %q", want)
		}
	}
}

func TestHistoryScreenViewBatchRendersItemDetailAndTimeline(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	s, _ = updateExpectingHistory(t, s, keyMsg("down")) // b-middle
	s, _ = updateExpectingHistory(t, s, keyMsg("enter"))
	s, _ = updateExpectingHistory(t, s, keyMsg("down")) // Vendor.Beta (failed)
	view := stripANSI(s.view(120, 38))
	for _, want := range []string{"Vendor.Beta", "installer hash mismatch", "Timeline", "3.0 → 3.1"} {
		if !strings.Contains(view, want) {
			t.Errorf("batch view missing %q, got:\n%s", want, view)
		}
	}
}

func TestHistoryScreenNarrowViewOmitsDetailPane(t *testing.T) {
	s := loadedHistoryScreen(t, historyTestEnv())
	view := stripANSI(s.view(80, 30)) // below splitPanelThreshold
	if strings.Contains(view, "Batch details") {
		t.Error("narrow view should not render the detail pane")
	}
	if !strings.Contains(view, "Batches") {
		t.Error("narrow view should still render the batch list")
	}
}

// ── App wiring ────────────────────────────────────────────────────────

func TestHistoryTabRegistered(t *testing.T) {
	found := false
	for _, tab := range tabs {
		if tab.id == screenHistory {
			found = true
			if tab.label != "History" {
				t.Errorf("history tab label = %q, want History", tab.label)
			}
		}
	}
	if !found {
		t.Fatal("screenHistory missing from tabs")
	}
	if _, ok := createScreen(screenHistory).(historyScreen); !ok {
		t.Error("createScreen(screenHistory) should return a historyScreen")
	}
}

func TestSwitchTabSendsFocusToExistingScreen(t *testing.T) {
	withHistoryLoadFn(t, func() (historyEnvelope, error) { return historyTestEnv(), nil })
	a := app{
		activeTab: 0,
		screens: map[screenID]screen{
			screenWorkspace: newWorkspaceScreen(),
			screenHistory:   loadedHistoryScreen(t, historyTestEnv()),
		},
		width:  120,
		height: 40,
	}
	historyIdx := -1
	for i, tab := range tabs {
		if tab.id == screenHistory {
			historyIdx = i
		}
	}
	next, cmd := a.switchTab(historyIdx)
	if next.activeTab != historyIdx {
		t.Fatalf("activeTab = %d, want %d", next.activeTab, historyIdx)
	}
	if cmd == nil {
		t.Error("switching to an existing history screen should batch a focus reload cmd")
	}
}
