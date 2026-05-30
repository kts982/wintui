package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func wheel(b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: b}
}

func TestWorkspaceWheelMovesCursor(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()

	ws.cursor = 0
	next, _ := ws.update(wheel(tea.MouseWheelDown))
	if got := next.(workspaceScreen).cursor; got != 1 {
		t.Fatalf("after wheel down cursor = %d, want 1", got)
	}

	ws.cursor = 1
	next, _ = ws.update(wheel(tea.MouseWheelUp))
	if got := next.(workspaceScreen).cursor; got != 0 {
		t.Fatalf("after wheel up cursor = %d, want 0", got)
	}
}

func TestWorkspaceWheelClampsAtBounds(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = threeInstalledItems()

	ws.cursor = 0
	next, _ := ws.update(wheel(tea.MouseWheelUp))
	if got := next.(workspaceScreen).cursor; got != 0 {
		t.Fatalf("wheel up at top cursor = %d, want 0 (clamped)", got)
	}

	ws.cursor = 2 // last of three items
	next, _ = ws.update(wheel(tea.MouseWheelDown))
	if got := next.(workspaceScreen).cursor; got != 2 {
		t.Fatalf("wheel down at bottom cursor = %d, want 2 (clamped)", got)
	}
}

func TestDetailWheelScrolls(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.windowWidth = 80
	p.windowHeight = 20
	p.detail = PackageDetail{
		Name:         "Test Pkg",
		ID:           "Test.Pkg",
		Version:      "1.0",
		Source:       "winget",
		ReleaseNotes: strings.Repeat("Release notes line. ", 80),
	}
	if p.maxScroll() == 0 {
		t.Skip("content fits the window; nothing to scroll")
	}

	next, _, handled := p.update(wheel(tea.MouseWheelDown))
	if !handled {
		t.Fatal("expected wheel down to be handled")
	}
	if next.scroll != 1 {
		t.Fatalf("after wheel down scroll = %d, want 1", next.scroll)
	}

	up, _, _ := next.update(wheel(tea.MouseWheelUp))
	if up.scroll != 0 {
		t.Fatalf("after wheel up scroll = %d, want 0", up.scroll)
	}
}

func TestCleanupWheelMovesCursorButNotWhileExecuting(t *testing.T) {
	s := newCleanupScreen()
	if len(s.visible) < 2 {
		t.Skip("need at least two visible cleanup targets")
	}

	s.cursor = 0
	next, _ := s.update(wheel(tea.MouseWheelDown))
	if got := next.(cleanupScreen).cursor; got != 1 {
		t.Fatalf("after wheel down cursor = %d, want 1", got)
	}

	// The execute modal owns the foreground — the wheel must be ignored.
	s.state = cleanupExecuting
	s.cursor = 0
	next, _ = s.update(wheel(tea.MouseWheelDown))
	if got := next.(cleanupScreen).cursor; got != 0 {
		t.Fatalf("wheel during execute moved cursor to %d, want 0 (ignored)", got)
	}
}
