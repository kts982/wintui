package main

import (
	"strings"
	"testing"
	"time"
)

func TestModalTransitionReviewToRunning(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{
			pkg:         Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.0", Available: "2.1"},
			upgradeable: true,
			installed:   "2.0",
			available:   "2.1",
		},
	}
	ws.selected[ws.items[0].key()] = true

	next, _ := ws.beginAction(retryOpUpgrade)
	got := next.(workspaceScreen)

	if got.modal == nil {
		t.Fatal("modal = nil after beginAction")
	}
	if got.modal.phase != execPhaseReview {
		t.Fatalf("modal.phase = %d, want execPhaseReview (%d)", got.modal.phase, execPhaseReview)
	}

	// Press enter to transition from review to running.
	next, _ = got.update(keyMsg("enter"))
	got = next.(workspaceScreen)

	if got.state != workspaceExecuting {
		t.Fatalf("state = %d, want workspaceExecuting (%d)", got.state, workspaceExecuting)
	}
	if got.modal == nil {
		t.Fatal("modal = nil after enter")
	}
	if got.modal.phase != execPhaseRunning {
		t.Fatalf("modal.phase = %d, want execPhaseRunning (%d)", got.modal.phase, execPhaseRunning)
	}
}

func TestModalEscCancellationMarksPendingAsSkipped(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting

	items := []batchItem{
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{Name: "A", ID: "A.A", Source: "winget"}, upgradeable: true},
			status: batchDone,
		},
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{Name: "B", ID: "B.B", Source: "winget"}, upgradeable: true},
			status: batchRunning,
		},
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{Name: "C", ID: "C.C", Source: "winget"}, upgradeable: true},
			status: batchQueued,
		},
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseRunning
	ws.modal = &m

	next, _ := ws.update(keyMsg("esc"))
	got := next.(workspaceScreen)

	if got.modal == nil {
		t.Fatal("modal = nil after esc")
	}
	if got.modal.phase != execPhaseComplete {
		t.Fatalf("modal.phase = %d, want execPhaseComplete (%d)", got.modal.phase, execPhaseComplete)
	}

	third := got.modal.items[2]
	if third.status != batchFailed {
		t.Fatalf("third item status = %d, want batchFailed (%d)", third.status, batchFailed)
	}
	if third.err == nil {
		t.Fatal("third item err = nil, want error containing 'cancelled'")
	}
	if !strings.Contains(third.err.Error(), "cancelled") {
		t.Fatalf("third item err = %q, want it to contain 'cancelled'", third.err.Error())
	}
}

func TestModalDismissalStartsBackgroundRefresh(t *testing.T) {
	originalCache := cache
	originalSettings := appSettings
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	appSettings = DefaultSettings()
	t.Cleanup(func() {
		cache = originalCache
		appSettings = originalSettings
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting
	ws.items = []workspaceItem{
		{pkg: Package{Name: "A", ID: "A.A", Source: "winget"}, installed: "1.0"},
	}

	items := []batchItem{
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{Name: "A", ID: "A.A", Source: "winget"}, upgradeable: true},
			status: batchDone,
		},
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, cmd := ws.update(keyMsg("enter"))
	got := next.(workspaceScreen)

	if got.modal != nil {
		t.Fatal("modal should be nil after dismissal")
	}
	if got.state != workspaceReady {
		t.Fatalf("state = %d, want workspaceReady (%d)", got.state, workspaceReady)
	}
	if !got.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if cmd == nil {
		t.Fatal("cmd = nil, want background refresh command")
	}
}

func TestBuildItemsSeparatesUpgradeableFromInstalled(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	t.Cleanup(func() { appSettings = original })

	installed := []Package{
		{Name: "A", ID: "A.A", Source: "winget", Version: "1.0", Available: "2.0"},
		{Name: "B", ID: "B.B", Source: "winget", Version: "1.0"},
		{Name: "C", ID: "C.C", Source: "winget", Version: "1.0"},
	}
	upgradeable := []Package{
		{Name: "A", ID: "A.A", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	items, hiddenCount := buildItems(installed, upgradeable)

	if hiddenCount != 0 {
		t.Fatalf("hiddenCount = %d, want 0", hiddenCount)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(items))
	}

	// A should be first (upgradeable).
	if items[0].pkg.ID != "A.A" {
		t.Fatalf("items[0].pkg.ID = %q, want A.A", items[0].pkg.ID)
	}
	if !items[0].upgradeable {
		t.Fatal("items[0].upgradeable = false, want true")
	}
	if items[0].available != "2.0" {
		t.Fatalf("items[0].available = %q, want 2.0", items[0].available)
	}

	// B and C should follow (not upgradeable).
	if items[1].pkg.ID != "B.B" {
		t.Fatalf("items[1].pkg.ID = %q, want B.B", items[1].pkg.ID)
	}
	if items[1].upgradeable {
		t.Fatal("items[1].upgradeable = true, want false")
	}
	if items[2].pkg.ID != "C.C" {
		t.Fatalf("items[2].pkg.ID = %q, want C.C", items[2].pkg.ID)
	}
	if items[2].upgradeable {
		t.Fatal("items[2].upgradeable = true, want false")
	}
}

func TestBuildItemsFiltersIgnoredUpgrades(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("A.A", "winget"): {Ignore: true},
	}
	t.Cleanup(func() { appSettings = original })

	installed := []Package{
		{Name: "A", ID: "A.A", Source: "winget", Version: "1.0"},
		{Name: "B", ID: "B.B", Source: "winget", Version: "1.0"},
	}
	upgradeable := []Package{
		{Name: "A", ID: "A.A", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	items, hiddenCount := buildItems(installed, upgradeable)

	if hiddenCount != 1 {
		t.Fatalf("hiddenCount = %d, want 1", hiddenCount)
	}

	// A should NOT appear as upgradeable.
	for _, item := range items {
		if item.pkg.ID == "A.A" && item.upgradeable {
			t.Fatal("expected ignored package A.A to not appear as upgradeable")
		}
	}

	// A should still be in the installed section.
	foundA := false
	for _, item := range items {
		if item.pkg.ID == "A.A" {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Fatal("expected A.A to still appear in installed items")
	}

	// B should be present.
	foundB := false
	for _, item := range items {
		if item.pkg.ID == "B.B" {
			foundB = true
			break
		}
	}
	if !foundB {
		t.Fatal("expected B.B to appear in items")
	}
}

func TestIncrementalUpdateUpgradeRemovesFromUpgradeable(t *testing.T) {
	originalCache := cache
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() { cache = originalCache })

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{
			pkg:         Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.0", Available: "2.1"},
			upgradeable: true,
			installed:   "2.0",
			available:   "2.1",
		},
	}

	msg := incrementalUpdateMsg{
		action: retryOpUpgrade,
		pkg:    Package{Name: "Git", ID: "Git.Git", Source: "winget"},
		result: []Package{
			{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.1"},
		},
	}

	ws.applyIncrementalUpdate(msg)

	if len(ws.items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(ws.items))
	}
	item := ws.items[0]
	if item.upgradeable {
		t.Fatal("item.upgradeable = true, want false after upgrade")
	}
	if item.installed != "2.1" {
		t.Fatalf("item.installed = %q, want 2.1", item.installed)
	}
	if item.available != "" {
		t.Fatalf("item.available = %q, want empty", item.available)
	}
	if item.pkg.Version != "2.1" {
		t.Fatalf("item.pkg.Version = %q, want 2.1", item.pkg.Version)
	}
}

func TestIncrementalUpdateUninstallRemovesItem(t *testing.T) {
	originalCache := cache
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() { cache = originalCache })

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{pkg: Package{Name: "A", ID: "A.A", Source: "winget", Version: "1.0"}, installed: "1.0"},
		{pkg: Package{Name: "B", ID: "B.B", Source: "winget", Version: "1.0"}, installed: "1.0"},
	}

	msg := incrementalUpdateMsg{
		action: retryOpUninstall,
		pkg:    Package{Name: "A", ID: "A.A", Source: "winget"},
		result: []Package{}, // empty = confirmed removed
	}

	ws.applyIncrementalUpdate(msg)

	if len(ws.items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(ws.items))
	}
	if ws.items[0].pkg.ID != "B.B" {
		t.Fatalf("remaining item ID = %q, want B.B", ws.items[0].pkg.ID)
	}
}

func TestBackgroundRefreshPreservesCursorKey(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	t.Cleanup(func() { appSettings = original })

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{pkg: Package{Name: "A", ID: "A.A", Source: "winget", Version: "1.0"}, installed: "1.0"},
		{pkg: Package{Name: "B", ID: "B.B", Source: "winget", Version: "1.0"}, installed: "1.0"},
		{pkg: Package{Name: "C", ID: "C.C", Source: "winget", Version: "1.0"}, installed: "1.0"},
	}
	ws.cursor = 1 // on B

	// Background refresh returns reordered data: C, B, A.
	msg := backgroundRefreshMsg{
		installed: []Package{
			{Name: "C", ID: "C.C", Source: "winget", Version: "1.0"},
			{Name: "B", ID: "B.B", Source: "winget", Version: "1.0"},
			{Name: "A", ID: "A.A", Source: "winget", Version: "1.0"},
		},
		upgradeable: []Package{},
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.cursor < 0 || got.cursor >= len(got.items) {
		t.Fatalf("cursor = %d out of range [0, %d)", got.cursor, len(got.items))
	}

	cursorItem := got.items[got.cursor]
	if cursorItem.pkg.ID != "B.B" {
		t.Fatalf("cursor item ID = %q, want B.B (cursor should follow the item by key)", cursorItem.pkg.ID)
	}

	// Verify cursor actually moved (B is no longer at index 1 if order changed).
	bIndex := -1
	for i, item := range got.items {
		if item.pkg.ID == "B.B" {
			bIndex = i
			break
		}
	}
	if bIndex == -1 {
		t.Fatal("B.B not found in items after refresh")
	}
	if got.cursor != bIndex {
		t.Fatalf("cursor = %d, want %d (index of B.B)", got.cursor, bIndex)
	}

}

func TestCtrlERetriesBlockedByProcessWithoutForcingElevation(t *testing.T) {
	forceNotElevated(t)

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting

	items := []batchItem{
		{
			action:        retryOpUninstall,
			item:          workspaceItem{pkg: Package{Name: "Comet", ID: "Perplexity.Comet", Source: "winget"}},
			status:        batchFailed,
			err:           assertErr("exit status 0x8a150066"),
			output:        "exit status 0x8a150066",
			blockedByProc: true,
			allVersions:   true,
		},
	}
	m := newExecModal(retryOpUninstall, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, _ := ws.update(keyMsg("ctrl+e"))
	got := next.(workspaceScreen)

	if got.modal == nil {
		t.Fatal("modal = nil after ctrl+e retry")
	}
	if got.modal.phase != execPhaseRunning {
		t.Fatalf("modal.phase = %d, want execPhaseRunning", got.modal.phase)
	}
	if got.modal.forceElevated {
		t.Fatal("forceElevated = true; blocked-by-process-only retries must not force UAC")
	}
	if len(got.modal.items) != 1 || got.modal.items[0].item.pkg.ID != "Perplexity.Comet" {
		t.Fatalf("retry items = %#v, want only Perplexity.Comet", got.modal.items)
	}
	if got.state != workspaceExecuting {
		t.Fatalf("state = %d, want workspaceExecuting", got.state)
	}
}

func TestCtrlEForcesElevationWhenElevationCandidatesPresent(t *testing.T) {
	forceNotElevated(t)

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting

	items := []batchItem{
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{ID: "Admin.Tool", Source: "winget"}},
			status: batchFailed,
			err:    assertErr("package requires administrator privileges (0x8a150056)"),
			output: "0x8a150056",
		},
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, _ := ws.update(keyMsg("ctrl+e"))
	got := next.(workspaceScreen)

	if got.modal == nil {
		t.Fatal("modal = nil after ctrl+e retry")
	}
	if !got.modal.forceElevated {
		t.Fatal("forceElevated = false; elevation-candidate retries should force elevation")
	}
}

func TestCtrlERetriesMixedBatchForcesElevationAndIncludesBoth(t *testing.T) {
	forceNotElevated(t)

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting

	items := []batchItem{
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{ID: "Admin.Tool", Source: "winget"}},
			status: batchFailed,
			err:    assertErr("package requires administrator privileges (0x8a150056)"),
			output: "0x8a150056",
		},
		{
			action:        retryOpUninstall,
			item:          workspaceItem{pkg: Package{ID: "Perplexity.Comet", Source: "winget"}},
			status:        batchFailed,
			err:           assertErr("exit status 0x8a150066"),
			output:        "exit status 0x8a150066",
			blockedByProc: true,
		},
	}
	m := newExecModal(retryOpApply, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, _ := ws.update(keyMsg("ctrl+e"))
	got := next.(workspaceScreen)

	if got.modal == nil {
		t.Fatal("modal = nil after ctrl+e retry")
	}
	if !got.modal.forceElevated {
		t.Fatal("forceElevated = false; mixed retries with elevation candidates should force elevation")
	}
	if len(got.modal.items) != 2 {
		t.Fatalf("retry items = %d, want 2 (both elevation and blocked-by-process items)", len(got.modal.items))
	}
	gotIDs := map[string]bool{
		got.modal.items[0].item.pkg.ID: true,
		got.modal.items[1].item.pkg.ID: true,
	}
	if !gotIDs["Admin.Tool"] || !gotIDs["Perplexity.Comet"] {
		t.Fatalf("retry items IDs = %v, want both Admin.Tool and Perplexity.Comet", gotIDs)
	}
}

func TestCtrlASchedulesAdminRelaunchForPendingSelfUpgrade(t *testing.T) {
	forceNotElevated(t)

	origExe := currentExecutablePath
	origRelaunch := relaunchAsAdminFunc
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	var (
		gotExe  string
		gotArgs []string
		gotShow int
	)
	relaunchAsAdminFunc = func(exe string, args []string, showCmd int) error {
		gotExe = exe
		gotArgs = append([]string(nil), args...)
		gotShow = showCmd
		return nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		relaunchAsAdminFunc = origRelaunch
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting
	item := workspaceItem{pkg: Package{Name: "WinTUI", ID: selfPackageID, Source: "winget"}}
	ws.selectedVersions[item.key()] = "2.4.0"

	items := []batchItem{{
		action: retryOpUpgrade,
		item:   item,
		status: batchPendingRestart,
	}}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, cmd := ws.update(keyMsg("ctrl+a"))
	if cmd == nil {
		t.Fatal("cmd = nil, want admin relaunch message command")
	}
	got := next.(workspaceScreen)
	if got.modal == nil || !got.modal.pendingSelfUpgradeRequiresAdmin() {
		t.Fatal("expected pending self-upgrade modal to remain active before relaunch confirmation")
	}

	msg := cmd()
	relaunchMsg, ok := msg.(selfUpgradeAdminRelaunchMsg)
	if !ok {
		t.Fatalf("cmd() msg = %T, want selfUpgradeAdminRelaunchMsg", msg)
	}
	if relaunchMsg.err != nil {
		t.Fatalf("selfUpgradeAdminRelaunchMsg.err = %v", relaunchMsg.err)
	}
	if gotExe == "" || !strings.Contains(strings.ToLower(gotExe), `winget\links\wintui.exe`) {
		t.Fatalf("relaunch exe = %q, want installed WinTUI path", gotExe)
	}
	if gotShow != swShowNormal {
		t.Fatalf("showCmd = %d, want %d", gotShow, swShowNormal)
	}
	wantPairs := [][]string{
		{"--retry-op", "upgrade"},
		{"--id", selfPackageID},
		{"--source", "winget"},
		{"--package-version", "2.4.0"},
	}
	for _, pair := range wantPairs {
		found := false
		for i := 0; i+1 < len(gotArgs); i++ {
			if gotArgs[i] == pair[0] && gotArgs[i+1] == pair[1] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("relaunch args = %#v, missing %q %q", gotArgs, pair[0], pair[1])
		}
	}
}

func TestCtrlAFailureShowsManualAdminCommand(t *testing.T) {
	forceNotElevated(t)

	origExe := currentExecutablePath
	origRelaunch := relaunchAsAdminFunc
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	relaunchAsAdminFunc = func(exe string, args []string, showCmd int) error {
		return assertErr("The operation was canceled by the user.")
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		relaunchAsAdminFunc = origRelaunch
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting
	item := workspaceItem{pkg: Package{Name: "WinTUI", ID: selfPackageID, Source: "winget"}}
	ws.selectedVersions[item.key()] = "2.4.0"

	items := []batchItem{{
		action: retryOpUpgrade,
		item:   item,
		status: batchPendingRestart,
	}}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, cmd := ws.update(keyMsg("ctrl+a"))
	if cmd == nil {
		t.Fatal("cmd = nil, want admin relaunch message command")
	}
	msg := cmd()
	got := next.(workspaceScreen)
	next, _ = got.update(msg)
	got = next.(workspaceScreen)

	if got.modal == nil || len(got.modal.items) != 1 {
		t.Fatalf("modal = %#v, want one pending item", got.modal)
	}
	output := got.modal.items[0].output
	for _, want := range []string{
		"Admin relaunch failed: The operation was canceled by the user.",
		"Open PowerShell as administrator and run:",
		"wintui '--retry-op' 'upgrade'",
		"'--retry-op' 'upgrade'",
		"'--package-version' '2.4.0'",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("modal output = %q, missing %q", output, want)
		}
	}
}

func TestExtractKeyLogLinesPreservesManualAdminFallback(t *testing.T) {
	output := strings.Join([]string{
		"Admin relaunch failed: The operation was canceled by the user.",
		"Open PowerShell as administrator and run:",
		"wintui '--retry-op' 'upgrade'",
	}, "\n")

	lines := extractKeyLogLines(output)
	want := []string{
		"Admin relaunch failed: The operation was canceled by the user.",
		"Open PowerShell as administrator and run:",
		"wintui '--retry-op' 'upgrade'",
	}
	if len(lines) != len(want) {
		t.Fatalf("extractKeyLogLines() = %#v, want %#v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("extractKeyLogLines()[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestWorkspaceDataMsgPreservesActiveModalState(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	t.Cleanup(func() { appSettings = original })

	ws := newWorkspaceScreen()
	ws.state = workspaceExecuting
	item := workspaceItem{pkg: Package{Name: "WinTUI", ID: selfPackageID, Source: "winget"}}
	items := []batchItem{{
		action: retryOpUpgrade,
		item:   item,
		status: batchPendingRestart,
	}}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, cmd := ws.update(workspaceDataMsg{
		installed: []Package{
			{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "0.0.1"},
		},
		upgradeable: []Package{},
	})
	if cmd != nil {
		t.Fatal("workspaceDataMsg should not replace the active modal flow with a new command")
	}

	got := next.(workspaceScreen)
	if got.state != workspaceExecuting {
		t.Fatalf("state = %d, want workspaceExecuting (%d)", got.state, workspaceExecuting)
	}
	if got.modal == nil || got.modal.phase != execPhaseComplete {
		t.Fatalf("modal = %#v, want active execPhaseComplete modal", got.modal)
	}
	if len(got.items) != 1 || got.items[0].pkg.ID != selfPackageID {
		t.Fatalf("items = %#v, want refreshed workspace data without losing modal", got.items)
	}
}
