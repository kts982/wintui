package main

import "testing"

func TestComputeLayoutShowsDetailOnWideTerminal(t *testing.T) {
	l := computeLayout(120, 40)
	if !l.hasDetail {
		t.Fatal("expected detail panel on 120-wide terminal")
	}
	if l.detail.W < minDetailWidth || l.detail.W > maxDetailWidth {
		t.Fatalf("detail.W = %d, want between %d and %d", l.detail.W, minDetailWidth, maxDetailWidth)
	}
	if l.list.W+l.detail.W+1 != l.contentWidth {
		t.Fatalf("list.W(%d) + detail.W(%d) + 1 != contentWidth(%d)", l.list.W, l.detail.W, l.contentWidth)
	}
	if l.detail.X != l.list.W+1 {
		t.Fatalf("detail.X = %d, want list.W+1 = %d", l.detail.X, l.list.W+1)
	}
}

func TestComputeLayoutHidesDetailOnNarrowTerminal(t *testing.T) {
	l := computeLayout(80, 30)
	if l.hasDetail {
		t.Fatal("expected no detail panel on 80-wide terminal")
	}
	if l.detail.W != 0 {
		t.Fatalf("detail.W = %d, want 0", l.detail.W)
	}
	if l.list.W != l.contentWidth {
		t.Fatalf("list.W = %d, want contentWidth %d", l.list.W, l.contentWidth)
	}
}

func TestComputeLayoutThresholdBoundary(t *testing.T) {
	below := computeLayout(splitPanelThreshold-1, 30)
	if below.hasDetail {
		t.Fatal("expected no detail panel below threshold")
	}
	at := computeLayout(splitPanelThreshold, 30)
	if !at.hasDetail {
		t.Fatal("expected detail panel at threshold")
	}
}

func TestLayoutWithLogExpanded(t *testing.T) {
	l := computeLayout(120, 40)
	baseH := l.list.H

	withLog := l.withLogExpanded(10)
	if withLog.log.H == 0 {
		t.Fatal("expected non-zero log height")
	}
	if withLog.list.H >= baseH {
		t.Fatalf("list.H with log (%d) should be less than without (%d)", withLog.list.H, baseH)
	}
	if withLog.list.H+withLog.log.H != l.contentHeight {
		t.Fatalf("list.H(%d) + log.H(%d) != contentHeight(%d)", withLog.list.H, withLog.log.H, l.contentHeight)
	}
	if withLog.log.Y != withLog.list.H {
		t.Fatalf("log.Y = %d, want list.H = %d", withLog.log.Y, withLog.list.H)
	}
}

func TestLayoutWithLogExpandedTinyTerminal(t *testing.T) {
	l := computeLayout(80, 8)

	withLog := l.withLogExpanded(20)
	// Panels should never go negative.
	if withLog.list.H < 0 {
		t.Fatalf("list.H = %d, should never be negative", withLog.list.H)
	}
	if withLog.log.H < 0 {
		t.Fatalf("log.H = %d, should never be negative", withLog.log.H)
	}
	// At minimum, list keeps 3 rows.
	if withLog.list.H < 3 && withLog.log.H > 0 {
		t.Fatalf("list.H = %d with log.H = %d, list should keep at least 3 rows", withLog.list.H, withLog.log.H)
	}
}

func TestLayoutWithLogExpandedZeroLines(t *testing.T) {
	l := computeLayout(120, 40)
	same := l.withLogExpanded(0)
	if same.log.H != 0 {
		t.Fatalf("log.H = %d, want 0 for zero log lines", same.log.H)
	}
	if same.list.H != l.list.H {
		t.Fatalf("list.H changed from %d to %d for zero log lines", l.list.H, same.list.H)
	}
}

func TestLayoutMaxVisibleItems(t *testing.T) {
	l := computeLayout(120, 40)
	items := l.maxVisibleItems(2)
	if items < 1 {
		t.Fatalf("maxVisibleItems = %d, want >= 1", items)
	}
	if items > l.list.H {
		t.Fatalf("maxVisibleItems = %d, exceeds list.H %d", items, l.list.H)
	}
}

func TestLayoutMaxVisibleItemsTinyTerminal(t *testing.T) {
	l := computeLayout(80, 5)
	items := l.maxVisibleItems(4)
	if items < 1 {
		t.Fatalf("maxVisibleItems = %d, want >= 1 even on tiny terminal", items)
	}
}

func TestContentAreaHeightForWindowFeedsComputeLayoutWithoutDoubleSubtraction(t *testing.T) {
	contentH := contentAreaHeightForWindow(132, 43, true)
	l := computeLayout(132, contentH)

	if l.contentHeight != contentH {
		t.Fatalf("contentHeight = %d, want %d", l.contentHeight, contentH)
	}
	if l.list.H != contentH {
		t.Fatalf("list.H = %d, want %d", l.list.H, contentH)
	}
}
