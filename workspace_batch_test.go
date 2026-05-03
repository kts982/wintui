package main

import (
	"os/exec"
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

func TestLaunchAutoUpdateCountdownStartsForAutoPolicy(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("A.A", "winget"): {UpdatePolicy: PolicyAuto},
	}
	t.Cleanup(func() { appSettings = original })

	ws := newWorkspaceScreen()
	msg := workspaceDataMsg{
		installed: []Package{
			{Name: "A", ID: "A.A", Source: "winget", Version: "1.0"},
		},
		upgradeable: []Package{
			{Name: "A", ID: "A.A", Source: "winget", Version: "1.0", Available: "2.0"},
		},
	}

	next, cmd := ws.update(msg)
	got := next.(workspaceScreen)
	if cmd == nil {
		t.Fatal("cmd = nil, want countdown tick command")
	}
	if !got.launchAutoChecked {
		t.Fatal("launchAutoChecked = false, want true")
	}
	if got.modal == nil {
		t.Fatal("modal = nil, want auto-update countdown modal")
	}
	if got.modal.phase != execPhaseCountdown {
		t.Fatalf("modal.phase = %d, want execPhaseCountdown", got.modal.phase)
	}
	if got.modal.countdown != launchAutoUpdateCountdownSeconds {
		t.Fatalf("countdown = %d, want %d", got.modal.countdown, launchAutoUpdateCountdownSeconds)
	}
	if len(got.modal.items) != 1 || got.modal.items[0].item.pkg.ID != "A.A" {
		t.Fatalf("modal items = %#v, want A.A", got.modal.items)
	}
}

func TestLaunchAutoUpdateWaitsForFreshData(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("A.A", "winget"): {UpdatePolicy: PolicyAuto},
	}
	t.Cleanup(func() { appSettings = original })

	ws := newWorkspaceScreen()
	msg := workspaceDataMsg{
		installed: []Package{
			{Name: "A", ID: "A.A", Source: "winget", Version: "1.0"},
		},
		upgradeable: []Package{
			{Name: "A", ID: "A.A", Source: "winget", Version: "1.0", Available: "2.0"},
		},
		fromDisk: true,
		savedAt:  time.Now().Add(-time.Hour),
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)
	if got.launchAutoChecked {
		t.Fatal("launchAutoChecked = true, want false until fresh data arrives")
	}
	if got.modal != nil {
		t.Fatalf("modal = %#v, want nil for stale disk data", got.modal)
	}
	if !got.refreshing {
		t.Fatal("refreshing = false, want background refresh")
	}
}

func TestAutoUpdateCountdownCanStartOrCancel(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceConfirm
	items := []batchItem{{
		action: retryOpUpgrade,
		item: workspaceItem{
			pkg:         Package{Name: "A", ID: "A.A", Source: "winget", Version: "1.0", Available: "2.0"},
			upgradeable: true,
		},
		status: batchQueued,
	}}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseCountdown
	m.countdown = 1
	ws.modal = &m

	next, cmd := ws.update(autoUpdateCountdownTickMsg{})
	started := next.(workspaceScreen)
	if started.state != workspaceExecuting {
		t.Fatalf("state after countdown = %d, want workspaceExecuting", started.state)
	}
	if started.modal == nil || started.modal.phase != execPhaseRunning {
		t.Fatalf("modal after countdown = %#v, want running", started.modal)
	}
	if cmd == nil {
		t.Fatal("cmd = nil, want batch start command")
	}

	ws = newWorkspaceScreen()
	ws.state = workspaceConfirm
	m = newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseCountdown
	m.countdown = launchAutoUpdateCountdownSeconds
	ws.modal = &m
	next, _ = ws.update(keyMsg("esc"))
	cancelled := next.(workspaceScreen)
	if cancelled.modal != nil {
		t.Fatalf("modal after cancel = %#v, want nil", cancelled.modal)
	}
	if cancelled.state != workspaceEmpty {
		t.Fatalf("state after cancel = %d, want workspaceEmpty with no items", cancelled.state)
	}
}

func TestLaunchAutoUpdateDefersSelfPackageOnly(t *testing.T) {
	original := appSettings
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey(selfPackageID, "winget"): {UpdatePolicy: PolicyAuto},
	}
	currentExecutablePath = func() (string, error) {
		return `C:\Users\test\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	t.Cleanup(func() {
		appSettings = original
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
	})

	ws := newWorkspaceScreen()
	msg := workspaceDataMsg{
		installed: []Package{
			{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.4.0"},
		},
		upgradeable: []Package{
			{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.4.0", Available: "2.5.0"},
		},
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)
	if got.modal != nil {
		t.Fatalf("modal = %#v, want nil when only self-package is Auto", got.modal)
	}
	if !got.selfAutoDeferred {
		t.Fatal("selfAutoDeferred = false, want true")
	}
}

func TestLaunchAutoUpdateDefersSelfWithOtherAutoPackages(t *testing.T) {
	original := appSettings
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey(selfPackageID, "winget"): {UpdatePolicy: PolicyAuto},
		packageRuleKey("Other.Pkg", "winget"):   {UpdatePolicy: PolicyAuto},
	}
	currentExecutablePath = func() (string, error) {
		return `C:\Users\test\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	t.Cleanup(func() {
		appSettings = original
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
	})

	ws := newWorkspaceScreen()
	msg := workspaceDataMsg{
		installed: []Package{
			{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.4.0"},
			{Name: "Other", ID: "Other.Pkg", Source: "winget", Version: "1.0"},
		},
		upgradeable: []Package{
			{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.4.0", Available: "2.5.0"},
			{Name: "Other", ID: "Other.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
		},
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)
	if got.modal == nil {
		t.Fatal("modal = nil, want countdown modal for non-self Auto package")
	}
	if !got.selfAutoDeferred || !got.modal.selfAutoDeferred {
		t.Fatalf("selfAutoDeferred screen=%v modal=%v, want both true", got.selfAutoDeferred, got.modal.selfAutoDeferred)
	}
	if len(got.modal.items) != 1 || got.modal.items[0].item.pkg.ID != "Other.Pkg" {
		t.Fatalf("modal items = %#v, want only Other.Pkg", got.modal.items)
	}
}

func TestSelfAutoDeferredBannerDismissesOnEsc(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.selfAutoDeferred = true

	next, _ := ws.update(keyMsg("esc"))
	got := next.(workspaceScreen)
	if got.selfAutoDeferred {
		t.Fatal("selfAutoDeferred = true after esc, want cleared")
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

// Regression: with a single upgrade (e.g. only Firefox), after the upgrade
// completes the cursor must not stay on the just-upgraded package. It should
// land on the top of whatever section is now first — the next upgrade if any
// remain, otherwise the top installed item.
func TestIncrementalUpdateUpgradeMovesItemToEndOfList(t *testing.T) {
	originalCache := cache
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() { cache = originalCache })

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{
			pkg:         Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "2.0", Available: "2.1"},
			upgradeable: true,
			installed:   "2.0",
			available:   "2.1",
		},
		{pkg: Package{Name: "Notepad++", ID: "Notepad.Notepad", Source: "winget", Version: "8.5"}, installed: "8.5"},
		{pkg: Package{Name: "VLC", ID: "VideoLAN.VLC", Source: "winget", Version: "3.0"}, installed: "3.0"},
	}

	msg := incrementalUpdateMsg{
		action: retryOpUpgrade,
		pkg:    Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"},
		result: []Package{{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "2.1"}},
	}
	ws.applyIncrementalUpdate(msg)

	if len(ws.items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(ws.items))
	}
	if ws.items[len(ws.items)-1].pkg.ID != "Mozilla.Firefox" {
		t.Fatalf("last item = %q, want Mozilla.Firefox to be moved to the end", ws.items[len(ws.items)-1].pkg.ID)
	}
	if ws.items[0].pkg.ID != "Notepad.Notepad" {
		t.Fatalf("first item = %q, want Notepad.Notepad (the just-upgraded Firefox should not still be at the top)", ws.items[0].pkg.ID)
	}
	if ws.items[len(ws.items)-1].upgradeable {
		t.Fatal("upgraded item still upgradeable, want false")
	}
}

// Regression: dismissing the post-batch modal must reset the cursor to the
// top of the list (top upgrade if any, else top installed). Combined with
// the move-to-end behavior above, this ensures the user lands on a useful
// row instead of the package they just upgraded.
func TestDismissModalAndRefreshResetsCursorToTop(t *testing.T) {
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
	// Simulate state after applyIncrementalUpdate has moved the just-upgraded
	// Firefox to the end of s.items.
	ws.items = []workspaceItem{
		{pkg: Package{Name: "Notepad++", ID: "Notepad.Notepad", Source: "winget", Version: "8.5"}, installed: "8.5"},
		{pkg: Package{Name: "VLC", ID: "VideoLAN.VLC", Source: "winget", Version: "3.0"}, installed: "3.0"},
		{pkg: Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "2.1"}, installed: "2.1"},
	}
	ws.cursor = 2 // was on Firefox before dismiss

	items := []batchItem{
		{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"}, upgradeable: true},
			status: batchDone,
		},
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	ws.modal = &m

	next, _ := ws.update(keyMsg("enter"))
	got := next.(workspaceScreen)

	if got.modal != nil {
		t.Fatal("modal should be nil after dismissal")
	}
	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (top of list)", got.cursor)
	}
	q, sr, up, ins := got.displayItems()
	all := append(append(append([]workspaceItem{}, q...), sr...), up...)
	all = append(all, ins...)
	if len(all) == 0 || all[0].pkg.ID == "Mozilla.Firefox" {
		t.Fatalf("displayed[0] = %q, want anything other than the just-upgraded Mozilla.Firefox", all[0].pkg.ID)
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

func TestEnterSchedulesPendingSelfUpgradeHandoffWithoutAdmin(t *testing.T) {
	forceNotElevated(t)

	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origCacheDir := userCacheDirPath
	origStartHost := startSelfUpdateHost
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	cacheDir := t.TempDir()
	userCacheDirPath = func() (string, error) { return cacheDir, nil }
	started := false
	startSelfUpdateHost = func(cmd *exec.Cmd) error {
		started = true
		return nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
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

	next, cmd := ws.update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("cmd = nil, want self-upgrade handoff command")
	}
	got := next.(workspaceScreen)
	if got.modal == nil {
		t.Fatal("expected pending self-upgrade modal before handoff confirmation")
	}

	msg := cmd()
	scheduledMsg, ok := msg.(selfUpgradeScheduledMsg)
	if !ok {
		t.Fatalf("cmd() msg = %T, want selfUpgradeScheduledMsg", msg)
	}
	if scheduledMsg.err != nil {
		t.Fatalf("selfUpgradeScheduledMsg.err = %v", scheduledMsg.err)
	}
	if !started {
		t.Fatal("startSelfUpdateHost was not called")
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
