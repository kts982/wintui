package main

import "charm.land/lipgloss/v2"

// ── Colour palette ─────────────────────────────────────────────────

// Palette principle:
//
//	accent (pink)     — primary navigation + structure: tabs, borders, cursor,
//	                    headers, key hints. Answers "where are you?".
//	secondary (lavender) — labels and section titles.
//	state (mint-cyan) — your intent or current state: staged checkboxes,
//	                    AUTO policy badges, version values, upgrade-available
//	                    chips. Answers "what have you set / what changed?".
//	override (warm yellow) — per-package rule indicator (gear ⚙).
//	success/danger/warning — pass/fail/warn semantics.
var (
	accent    = lipgloss.Color("212") // pink
	secondary = lipgloss.Color("99")  // lavender
	state     = lipgloss.Color("86")  // mint-cyan — "your intent / current state"
	dim       = lipgloss.Color("240") // grey
	bright    = lipgloss.Color("252") // near-white
	success   = lipgloss.Color("78")  // green
	danger    = lipgloss.Color("196") // red
	warning   = lipgloss.Color("220") // yellow
	override  = lipgloss.Color("222") // warm yellow — per-package rule indicator
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

	// stateStyle highlights "your intent or current state" — version numbers,
	// upgrade-available chips, AUTO policy badges, staged checkmarks.
	stateStyle = lipgloss.NewStyle().Foreground(state)
	// urlStyle marks an actionable link in the detail panel.
	urlStyle = lipgloss.NewStyle().Foreground(secondary).Underline(true)
	// chipStyle is the dim brackets used for identity-only chips like source.
	chipStyle = lipgloss.NewStyle().Foreground(dim)
)

// ── Helpers ────────────────────────────────────────────────────────

func checkbox(checked bool) string {
	if checked {
		// Staged is INTENT — paint it with the state-accent so the user can
		// scan their staging at a glance against the structural pink.
		return lipgloss.NewStyle().Foreground(state).Bold(true).Render("[✓]")
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
