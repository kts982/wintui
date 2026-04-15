package main

import (
	"fmt"
	"testing"
	"time"
)

func threeInstalledItems() []workspaceItem {
	return []workspaceItem{
		{pkg: Package{Name: "Alpha", ID: "Test.Alpha", Source: "winget", Version: "1.0"}, installed: "1.0"},
		{pkg: Package{Name: "Bravo", ID: "Test.Bravo", Source: "winget", Version: "2.0"}, installed: "2.0"},
		{pkg: Package{Name: "Charlie", ID: "Test.Charlie", Source: "winget", Version: "3.0"}, installed: "3.0"},
	}
}

func TestCursorDownIncrements(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()
	ws.cursor = 0

	next, _ := ws.update(keyMsg("down"))
	got := next.(workspaceScreen)

	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.cursor)
	}
}

func TestCursorUpDecrements(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()
	ws.cursor = 2

	next, _ := ws.update(keyMsg("up"))
	got := next.(workspaceScreen)

	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", got.cursor)
	}
}

func TestCursorDownClampsAtEnd(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()
	ws.cursor = 2

	next, _ := ws.update(keyMsg("down"))
	got := next.(workspaceScreen)

	if got.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (clamped at end)", got.cursor)
	}
}

func TestCursorUpClampsAtZero(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()
	ws.cursor = 0

	next, _ := ws.update(keyMsg("up"))
	got := next.(workspaceScreen)

	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (clamped at zero)", got.cursor)
	}
}

func TestWorkspaceDataMsgTransitionsToReady(t *testing.T) {
	originalSettings := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = originalSettings
		cache = originalCache
	})

	ws := newWorkspaceScreen()
	// ws starts in workspaceLoading by default.

	msg := workspaceDataMsg{
		installed: []Package{
			{Name: "Alpha", ID: "Test.Alpha", Source: "winget", Version: "1.0"},
			{Name: "Bravo", ID: "Test.Bravo", Source: "winget", Version: "2.0"},
			{Name: "Charlie", ID: "Test.Charlie", Source: "winget", Version: "3.0"},
		},
		fromDisk: false,
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.state != workspaceReady {
		t.Fatalf("state = %d, want workspaceReady (%d)", got.state, workspaceReady)
	}
	if len(got.items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(got.items))
	}
	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", got.cursor)
	}
}

func TestWorkspaceDataMsgEmptyGoesToEmpty(t *testing.T) {
	originalSettings := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = originalSettings
		cache = originalCache
	})

	ws := newWorkspaceScreen()

	msg := workspaceDataMsg{
		installed:   nil,
		upgradeable: nil,
		err:         fmt.Errorf("winget not found"),
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.state != workspaceEmpty {
		t.Fatalf("state = %d, want workspaceEmpty (%d)", got.state, workspaceEmpty)
	}
}

func TestWorkspaceDataFromDiskSetsRefreshing(t *testing.T) {
	originalSettings := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = originalSettings
		cache = originalCache
	})

	ws := newWorkspaceScreen()
	savedAt := time.Now().Add(-5 * time.Minute)

	msg := workspaceDataMsg{
		installed: []Package{
			{Name: "Alpha", ID: "Test.Alpha", Source: "winget", Version: "1.0"},
		},
		fromDisk: true,
		savedAt:  savedAt,
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.state != workspaceReady {
		t.Fatalf("state = %d, want workspaceReady (%d)", got.state, workspaceReady)
	}
	if !got.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if got.cacheAge.IsZero() {
		t.Fatal("cacheAge is zero, want non-zero")
	}
}

func TestFilteredItemsReturnsAllWhenNoFilter(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{pkg: Package{Name: "Alpha", ID: "Test.Alpha", Source: "winget", Version: "1.0"}, installed: "1.0"},
		{pkg: Package{Name: "Bravo", ID: "Test.Bravo", Source: "winget", Version: "2.0"}, installed: "2.0"},
		{pkg: Package{Name: "Charlie", ID: "Test.Charlie", Source: "winget", Version: "3.0"}, installed: "3.0"},
		{pkg: Package{Name: "Delta", ID: "Test.Delta", Source: "winget", Version: "4.0"}, installed: "4.0"},
		{pkg: Package{Name: "Echo", ID: "Test.Echo", Source: "winget", Version: "5.0"}, installed: "5.0"},
	}
	// No filter query set (default).

	got := ws.filteredItems()

	if len(got) != 5 {
		t.Fatalf("len(filteredItems) = %d, want 5", len(got))
	}
}

func TestOpenDetailTransitionsToDetail(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()
	ws.cursor = 0

	next, _ := ws.openDetail()
	got := next.(workspaceScreen)

	if !got.detail.visible() {
		t.Fatal("detail.visible() = false, want true")
	}
	if got.detail.pkgID != "Test.Alpha" {
		t.Fatalf("detail.pkgID = %q, want %q", got.detail.pkgID, "Test.Alpha")
	}
}

func TestEscFromDetailReturnsToList(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()
	ws.cursor = 0

	// Open detail first.
	next, _ := ws.openDetail()
	ws = next.(workspaceScreen)

	if !ws.detail.visible() {
		t.Fatal("detail should be visible before esc")
	}

	// Dispatch esc.
	next, _ = ws.update(keyMsg("esc"))
	got := next.(workspaceScreen)

	if got.detail.visible() {
		t.Fatal("detail.visible() = true after esc, want false")
	}
}
