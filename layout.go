package main

// rect describes a panel region within the terminal.
type rect struct {
	X, Y, W, H int
}

// layout computes panel rectangles from terminal size.
// Screens call computeLayout once per resize and use the
// resulting rects for rendering, scroll calculations, and hit testing.
type layout struct {
	// Terminal dimensions.
	termWidth  int
	termHeight int

	// Content area (below chrome, above help bar).
	contentWidth  int
	contentHeight int

	// Panel rectangles within the content area.
	list   rect // left panel (package list)
	detail rect // right panel (summary); zero-width when hidden

	// Bottom log panel (zero-height when collapsed).
	log rect

	// Whether the detail panel is visible.
	hasDetail bool

	// Whether to use compact header.
	compact bool
}

// splitPanelThreshold is the minimum terminal width to show the detail panel.
const splitPanelThreshold = 100

// detailPanelRatio is the fraction of content width given to the detail panel.
const detailPanelRatio = 0.35

// minDetailWidth is the narrowest the detail panel can be and still be useful.
const minDetailWidth = 30

// maxDetailWidth caps the detail panel so it doesn't dominate on wide terminals.
const maxDetailWidth = 50

// computeLayout calculates panel rectangles for a split-panel screen.
// termHeight is the usable content height below app chrome and above the help bar.
func computeLayout(termWidth, termHeight int) layout {
	l := layout{
		termWidth:  termWidth,
		termHeight: termHeight,
		compact:    useCompactHeaderForSize(termWidth, termHeight),
	}

	l.contentHeight = max(termHeight, 1)
	l.contentWidth = termWidth

	// Decide whether to show the detail panel.
	l.hasDetail = termWidth >= splitPanelThreshold

	if l.hasDetail {
		dw := min(max(int(float64(l.contentWidth)*detailPanelRatio), minDetailWidth), maxDetailWidth)
		lw := l.contentWidth - dw - 1 // 1 char gap
		l.list = rect{X: 0, Y: 0, W: lw, H: l.contentHeight}
		l.detail = rect{X: lw + 1, Y: 0, W: dw, H: l.contentHeight}
	} else {
		l.list = rect{X: 0, Y: 0, W: l.contentWidth, H: l.contentHeight}
		l.detail = rect{}
	}

	return l
}

// withLogExpanded returns a copy of the layout with height split between
// the list/detail panels and a bottom log panel.
func (l layout) withLogExpanded(logLines int) layout {
	if logLines == 0 || l.contentHeight <= 5 {
		return l
	}
	// Log takes up to 40% of content height, minimum 5 lines.
	// Ensure at least 3 rows remain for the list/detail panels.
	maxLog := max(l.contentHeight*2/5, 5)
	logH := min(logLines+2, maxLog) // +2 for border
	logH = max(min(logH, l.contentHeight-3), 0)

	out := l
	panelH := l.contentHeight - logH
	out.list.H = panelH
	out.detail.H = panelH
	out.log = rect{X: 0, Y: panelH, W: l.contentWidth, H: logH}
	return out
}

// maxVisibleItems returns how many list items fit in the list panel,
// accounting for section headers and padding.
func (l layout) maxVisibleItems(headerLines int) int {
	return max(l.list.H-headerLines-2, 1) // 2 for top/bottom padding
}
