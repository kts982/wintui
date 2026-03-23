package main

import "github.com/charmbracelet/lipgloss"

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

// ── Header ─────────────────────────────────────────────────────────

var (
	logoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(accent).
			Padding(0, 1)

	headerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accent).
				MarginLeft(1)

	headerBarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 1).
			MarginBottom(1)
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

// ── Tab bar ────────────────────────────────────────────────────────

var (
	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			Underline(true)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(dim)

	tabSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))
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
