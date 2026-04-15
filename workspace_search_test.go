package main

import (
	"fmt"
	"testing"
)

func TestToggleSelectionAddsSearchResultToQueue(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.searchResults = []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
	}
	// No queue, no installed items — cursor 0 is on the search result.
	ws.cursor = 0

	next, _ := ws.toggleSelection()
	got := next.(workspaceScreen)

	if len(got.installQueue) != 1 {
		t.Fatalf("len(installQueue) = %d, want 1", len(got.installQueue))
	}
	if got.installQueue[0].pkg.ID != "Mozilla.Firefox" {
		t.Fatalf("installQueue[0].pkg.ID = %q, want Mozilla.Firefox", got.installQueue[0].pkg.ID)
	}
	k := packageSourceKey("Mozilla.Firefox", "winget")
	if !got.installQueueMap[k] {
		t.Fatal("installQueueMap missing key for Mozilla.Firefox")
	}
}

func TestToggleSelectionRemovesFromQueue(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady

	item := workspaceItem{
		pkg:       Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
		available: "130.0",
	}
	ws.installQueue = []workspaceItem{item}
	ws.installQueueMap[item.key()] = true
	// Cursor 0 is on the queue item.
	ws.cursor = 0

	next, _ := ws.toggleSelection()
	got := next.(workspaceScreen)

	if len(got.installQueue) != 0 {
		t.Fatalf("len(installQueue) = %d, want 0", len(got.installQueue))
	}
	if len(got.installQueueMap) != 0 {
		t.Fatalf("len(installQueueMap) = %d, want 0", len(got.installQueueMap))
	}
}

func TestToggleSelectionOnInstalledTogglesSelected(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{
			pkg:       Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.44"},
			installed: "2.44",
		},
	}
	ws.cursor = 0

	// First toggle: select.
	next, _ := ws.toggleSelection()
	got := next.(workspaceScreen)

	k := packageSourceKey("Git.Git", "winget")
	if !got.selected[k] {
		t.Fatal("expected selected[Git.Git] = true after first toggle")
	}

	// Second toggle: deselect.
	next, _ = got.toggleSelection()
	got = next.(workspaceScreen)

	if got.selected[k] {
		t.Fatal("expected selected[Git.Git] = false after second toggle")
	}
}

func TestSearchResultsMsgUpdatesResults(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.searchLoading = true
	ws.searchQuery = "firefox"
	ws.cursor = 5 // arbitrary non-zero cursor

	msg := searchResultsMsg{
		query: "firefox",
		results: []Package{
			{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
			{Name: "Firefox ESR", ID: "Mozilla.Firefox.ESR", Source: "winget", Version: "115.0"},
		},
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.searchLoading {
		t.Fatal("searchLoading = true, want false")
	}
	if len(got.searchResults) != 2 {
		t.Fatalf("len(searchResults) = %d, want 2", len(got.searchResults))
	}
	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (reset after search results)", got.cursor)
	}
}

func TestSearchResultsMsgDiscardsStaleQuery(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.searchLoading = true
	ws.searchQuery = "git"

	msg := searchResultsMsg{
		query: "firefox",
		results: []Package{
			{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
		},
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.searchResults != nil {
		t.Fatalf("searchResults = %v, want nil (stale query should be discarded)", got.searchResults)
	}
}

func TestDisplayItemsOrdersQueueThenSearchThenUpgradeableThenInstalled(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady

	// Install queue.
	firefox := workspaceItem{
		pkg:       Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
		available: "130.0",
	}
	ws.installQueue = []workspaceItem{firefox}
	ws.installQueueMap[firefox.key()] = true

	// Search results.
	ws.searchResults = []Package{
		{Name: "Chrome", ID: "Google.Chrome", Source: "winget", Version: "125.0"},
	}

	// Installed items: one upgradeable, one installed-only.
	ws.items = []workspaceItem{
		{
			pkg:         Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.0", Available: "2.1"},
			upgradeable: true,
			installed:   "2.0",
			available:   "2.1",
		},
		{
			pkg:       Package{Name: "Node", ID: "OpenJS.NodeJS", Source: "winget", Version: "20.0"},
			installed: "20.0",
		},
	}

	queue, search, upgradeable, installed := ws.displayItems()

	if len(queue) != 1 || queue[0].pkg.ID != "Mozilla.Firefox" {
		t.Fatalf("queue: got %d items, want [Mozilla.Firefox]", len(queue))
	}
	if len(search) != 1 || search[0].pkg.ID != "Google.Chrome" {
		t.Fatalf("search: got %d items, want [Google.Chrome]", len(search))
	}
	if len(upgradeable) != 1 || upgradeable[0].pkg.ID != "Git.Git" {
		t.Fatalf("upgradeable: got %d items, want [Git.Git]", len(upgradeable))
	}
	if len(installed) != 1 || installed[0].pkg.ID != "OpenJS.NodeJS" {
		t.Fatalf("installed: got %d items, want [OpenJS.NodeJS]", len(installed))
	}
}

func TestSelectAllUpgradeableSelectsOnlyUpgradeable(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{
		{
			pkg:         Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.0", Available: "2.1"},
			upgradeable: true,
			installed:   "2.0",
			available:   "2.1",
		},
		{
			pkg:       Package{Name: "Node", ID: "OpenJS.NodeJS", Source: "winget", Version: "20.0"},
			installed: "20.0",
		},
		{
			pkg:         Package{Name: "Python", ID: "Python.Python.3", Source: "winget", Version: "3.11", Available: "3.12"},
			upgradeable: true,
			installed:   "3.11",
			available:   "3.12",
		},
	}

	ws.selectAllUpgradeable()

	gitKey := packageSourceKey("Git.Git", "winget")
	nodeKey := packageSourceKey("OpenJS.NodeJS", "winget")
	pythonKey := packageSourceKey("Python.Python.3", "winget")

	if !ws.selected[gitKey] {
		t.Fatal("expected Git to be selected (upgradeable)")
	}
	if !ws.selected[pythonKey] {
		t.Fatal("expected Python to be selected (upgradeable)")
	}
	if ws.selected[nodeKey] {
		t.Fatal("expected Node to NOT be selected (not upgradeable)")
	}
}

func TestSuccessfulSearchClearsPriorError(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.searchLoading = true
	ws.searchQuery = "firefox"
	ws.err = fmt.Errorf("previous search failed")

	msg := searchResultsMsg{
		query: "firefox",
		results: []Package{
			{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
		},
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)

	if got.err != nil {
		t.Fatalf("err = %v, want nil (successful search should clear prior error)", got.err)
	}
	if len(got.searchResults) != 1 {
		t.Fatalf("len(searchResults) = %d, want 1", len(got.searchResults))
	}
}
