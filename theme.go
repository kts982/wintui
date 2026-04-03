package main

import "charm.land/lipgloss/v2"

// ── Colour palette ─────────────────────────────────────────────────

var (
	accent    = lipgloss.Color("212") // pink
	secondary = lipgloss.Color("99")  // lavender
	dim       = lipgloss.Color("240") // grey
	bright    = lipgloss.Color("252") // near-white
	success   = lipgloss.Color("78")  // green
	danger    = lipgloss.Color("196") // red
	warning   = lipgloss.Color("220") // yellow
)

// ── Menu / list items ──────────────────────────────────────────────

var (
	itemStyle = lipgloss.NewStyle().
			Foreground(bright)

	itemActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	itemDescStyle = lipgloss.NewStyle().
			Foreground(dim)

	cursorStr      = lipgloss.NewStyle().Foreground(accent).Bold(true).Render("▸ ")
	cursorBlankStr = "  "
)

// ── Section titles ─────────────────────────────────────────────────

var sectionTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(secondary).
	MarginBottom(1)

// ── Status / feedback ──────────────────────────────────────────────

var (
	infoStyle    = lipgloss.NewStyle().Foreground(secondary)
	successStyle = lipgloss.NewStyle().Foreground(success).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(danger).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(warning)
	helpStyle    = lipgloss.NewStyle().Foreground(dim)
)

// ── Helpers ────────────────────────────────────────────────────────

func checkbox(checked bool) string {
	if checked {
		return lipgloss.NewStyle().Foreground(accent).Bold(true).Render("[✓]")
	}
	return lipgloss.NewStyle().Foreground(dim).Render("[ ]")
}

func indentBlock(block string, spaces int) string {
	return lipgloss.NewStyle().MarginLeft(spaces).Render(block)
}

func useCompactHeaderForSize(width, height int) bool {
	return height < 32 || width < 110
}

func contentAreaHeightForWindow(width, height int, hasHelp bool) int {
	chromeHeight := 11 // full logo (6) + subtitle (1) + bordered tabs (3) + padding (1)
	if useCompactHeaderForSize(width, height) {
		chromeHeight = 5 // compact title (1) + bordered tabs (3) + padding (1)
	}
	helpHeight := 0
	if hasHelp {
		helpHeight = 1
	}
	contentHeight := height - chromeHeight - helpHeight - 1
	if contentHeight < 1 {
		return 1
	}
	return contentHeight
}
