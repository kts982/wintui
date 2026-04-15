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
