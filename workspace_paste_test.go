package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Bracketed paste arrives as tea.PasteMsg, not key presses. These tests pin
// the routing added for it: the focused text input receives the content, and
// foreground surfaces without text inputs swallow the paste.

func TestPasteIntoActiveFilter(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.filter = ws.filter.activate()
	ws.cursor = 3

	next, _ := ws.update(tea.PasteMsg{Content: "PowerToys"})
	got := next.(workspaceScreen)

	if got.filter.query != "PowerToys" {
		t.Fatalf("filter.query = %q, want %q", got.filter.query, "PowerToys")
	}
	if got.filter.input.Value() != "PowerToys" {
		t.Fatalf("filter.input.Value() = %q, want %q", got.filter.input.Value(), "PowerToys")
	}
	if got.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (reset when the filter query changes)", got.cursor)
	}
}

func TestPasteAppendsAtFilterCursor(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.filter = ws.filter.activate()

	next, _ := ws.update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	next, _ = next.(workspaceScreen).update(tea.PasteMsg{Content: "ower"})
	got := next.(workspaceScreen)

	if got.filter.query != "power" {
		t.Fatalf("filter.query = %q, want %q (typed prefix + pasted suffix)", got.filter.query, "power")
	}
}

func TestPasteMultilineIsSanitized(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.filter = ws.filter.activate()

	next, _ := ws.update(tea.PasteMsg{Content: "Mozilla\n.Firefox"})
	got := next.(workspaceScreen)

	if strings.Contains(got.filter.query, "\n") {
		t.Fatalf("filter.query = %q, must not contain a raw newline", got.filter.query)
	}
}

func TestPasteIntoActiveSearch(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.searchActive = true
	ws.searchInput.Focus()

	next, _ := ws.update(tea.PasteMsg{Content: "Mozilla.Firefox"})
	got := next.(workspaceScreen)

	if got.searchInput.Value() != "Mozilla.Firefox" {
		t.Fatalf("searchInput.Value() = %q, want %q", got.searchInput.Value(), "Mozilla.Firefox")
	}
	if !got.searchActive {
		t.Fatal("searchActive flipped off by paste; want unchanged")
	}
}

func TestPasteWithNoActiveInputIsNoop(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.cursor = 2

	next, _ := ws.update(tea.PasteMsg{Content: "PowerToys"})
	got := next.(workspaceScreen)

	if got.filter.query != "" {
		t.Fatalf("filter.query = %q, want empty (no input focused)", got.filter.query)
	}
	if got.searchInput.Value() != "" {
		t.Fatalf("searchInput.Value() = %q, want empty (no input focused)", got.searchInput.Value())
	}
	if got.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (unchanged)", got.cursor)
	}
}

func TestPasteWhileModalIsDropped(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	// A modal owns the foreground even if the filter was left active behind it.
	ws.filter = ws.filter.activate()
	ws.modal = &execModal{}

	next, _ := ws.update(tea.PasteMsg{Content: "PowerToys"})
	got := next.(workspaceScreen)

	if got.filter.query != "" {
		t.Fatalf("filter.query = %q, want empty (modal owns the foreground)", got.filter.query)
	}
}
