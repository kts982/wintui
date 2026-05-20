package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// ── Theme architecture ─────────────────────────────────────────────
//
// A Palette holds every semantic color the UI uses. setActiveTheme
// re-binds the package-level color vars below from the active palette
// and calls rebuildThemeStyles, which re-derives every cached
// lipgloss.Style and pre-rendered string. The ~100 inline call sites
// across the codebase that read these vars (lipgloss.NewStyle().
// Foreground(accent)…) re-resolve them every frame and pick up
// changes automatically.
//
// Model-held styles (spinners, text inputs, viewports, progress bars,
// help model styles) live inside screen structs and are NOT touched
// by setActiveTheme. The themeAware interface in app.go handles those
// — each screen returns an updated copy via applyTheme() screen, and
// app.rethemeScreens walks the screens map and swaps the values.
//
// Palette principle (carries over from v2.7.x):
//
//	accent (pink)         — primary navigation + structure: tabs, borders,
//	                        cursor, headers, key hints. "Where are you?".
//	secondary (lavender)  — labels and section titles.
//	state (mint-cyan)     — your intent or current state: staged
//	                        checkboxes, AUTO policy badges, version
//	                        values, upgrade-available chips.
//	                        "What have you set?".
//	override (warm yellow) — per-package rule indicator (gear ⚙).
//	success/danger/warning — pass/fail/warn semantics.
//	dim/subtle/bright     — chrome / secondary data / primary text tiers.

type Palette struct {
	Accent, Secondary, State           color.Color
	Dim, Subtle, Bright                color.Color
	Success, Danger, Warning, Override color.Color

	// Optional opt-in OSC 10/11 colors (terminal fg/bg). Zero value =
	// don't touch the terminal's foreground/background.
	Background, Foreground color.Color

	// LogoStops are the two endpoint colors for the animated header
	// gradient. rebuildThemeStyles expands them into logoGradient via
	// lipgloss.Blend1D.
	LogoStops [2]color.Color
}

// Theme bundles light/dark variants of a named palette. Light may be
// zero — in which case setActiveTheme falls back to Dark regardless of
// the detected terminal background.
type Theme struct {
	ID, Label string
	Dark      Palette
	Light     Palette
}

// ── Curated palette set ───────────────────────────────────────────
//
// PR A ships only the default theme with proper Dark + Light variants.
// PR B will add Catppuccin / Nord / Dracula / Tokyo Night / Monochrome.

var defaultDark = Palette{
	Accent:    lipgloss.Color("212"), // pink
	Secondary: lipgloss.Color("99"),  // lavender
	State:     lipgloss.Color("86"),  // mint-cyan
	Dim:       lipgloss.Color("240"), // dark grey — chrome
	Subtle:    lipgloss.Color("247"), // mid grey — secondary data
	Bright:    lipgloss.Color("252"), // near-white — primary text
	Success:   lipgloss.Color("78"),  // green
	Danger:    lipgloss.Color("196"), // red
	Warning:   lipgloss.Color("220"), // yellow
	Override:  lipgloss.Color("222"), // warm yellow — per-package rule
	LogoStops: [2]color.Color{
		lipgloss.Color("212"), // bright pink
		lipgloss.Color("141"), // periwinkle
	},
}

// defaultLight uses deeper, more saturated hues so accent/structure
// remains legible on a white-ish terminal. Bright flips to near-black
// so primary text reads correctly, dim/subtle invert their grey tiers.
var defaultLight = Palette{
	Accent:    lipgloss.Color("162"), // deep pink
	Secondary: lipgloss.Color("62"),  // deep lavender/purple
	State:     lipgloss.Color("30"),  // deep teal
	Dim:       lipgloss.Color("245"), // light grey — chrome
	Subtle:    lipgloss.Color("240"), // mid grey — secondary data
	Bright:    lipgloss.Color("232"), // near-black — primary text
	Success:   lipgloss.Color("28"),  // forest green
	Danger:    lipgloss.Color("160"), // deep red
	Warning:   lipgloss.Color("172"), // deep amber
	Override:  lipgloss.Color("130"), // deep tan/orange
	LogoStops: [2]color.Color{
		lipgloss.Color("162"), // deep pink
		lipgloss.Color("98"),  // medium purple
	},
}

var themes = map[string]Theme{
	"default": {
		ID:    "default",
		Label: "Sweet Pink",
		Dark:  defaultDark,
		Light: defaultLight,
	},
}

// lookupTheme returns the named theme, or the default theme when the
// name is unknown. Used by setActiveTheme so a hand-edited settings
// file with a garbage value can't crash startup.
func lookupTheme(id string) Theme {
	if t, ok := themes[id]; ok {
		return t
	}
	return themes["default"]
}

// hasLightVariant reports whether p was meaningfully populated (the
// zero value means "no light variant; use Dark for both"). We probe
// Accent because every real palette sets it.
func hasLightVariant(p Palette) bool {
	return p.Accent != nil
}

// ── Active palette + bound color vars ──────────────────────────────

// activePalette is the palette currently rendered. Updated only by
// setActiveTheme.
var activePalette Palette

// These vars are rebound by setActiveTheme. ~100 inline
// lipgloss.NewStyle().Foreground(accent) call sites read them every
// frame and pick up changes automatically.
var (
	accent    color.Color
	secondary color.Color
	state     color.Color
	dim       color.Color
	subtle    color.Color
	bright    color.Color
	success   color.Color
	danger    color.Color
	warning   color.Color
	override  color.Color
)

// ── Cached styles + pre-rendered strings ──────────────────────────
//
// These capture color values at the moment .Foreground(accent) is
// called, so they go stale the instant the underlying var is
// rebound. rebuildThemeStyles re-derives all of them from the active
// palette. New entries here MUST be added to rebuildThemeStyles or
// they'll silently render with the previous theme's colors.

var (
	itemStyle         lipgloss.Style
	itemActiveStyle   lipgloss.Style
	itemDescStyle     lipgloss.Style
	sectionTitleStyle lipgloss.Style

	infoStyle    lipgloss.Style
	successStyle lipgloss.Style
	errorStyle   lipgloss.Style
	warnStyle    lipgloss.Style
	helpStyle    lipgloss.Style
	stateStyle   lipgloss.Style
	urlStyle     lipgloss.Style
	chipStyle    lipgloss.Style
	subtleStyle  lipgloss.Style

	cursorStr      string
	cursorBlankStr = "  "

	// Tab bar and header gradient — moved here from app.go so
	// rebuildThemeStyles owns the entire cache list.
	tabBoxActive   lipgloss.Style
	tabBoxInactive lipgloss.Style
	logoGradient   []color.Color
)

// setActiveTheme rebinds the color vars and rebuilds every cached
// style from the named palette. Called at startup, on
// tea.BackgroundColorMsg (to switch light/dark variant), and on
// settings theme change.
func setActiveTheme(id string, bgIsDark bool) {
	t := lookupTheme(id)
	p := t.Dark
	if !bgIsDark && hasLightVariant(t.Light) {
		p = t.Light
	}
	activePalette = p

	accent = p.Accent
	secondary = p.Secondary
	state = p.State
	dim = p.Dim
	subtle = p.Subtle
	bright = p.Bright
	success = p.Success
	danger = p.Danger
	warning = p.Warning
	override = p.Override

	rebuildThemeStyles()
}

// rebuildThemeStyles re-derives every package-cached lipgloss.Style
// and pre-rendered string from the current color vars. Keep this in
// sync with the var declarations above — anything declared as
// package state must be (re)assigned here.
func rebuildThemeStyles() {
	itemStyle = lipgloss.NewStyle().Foreground(bright)
	itemActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	itemDescStyle = lipgloss.NewStyle().Foreground(dim)

	sectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(secondary).MarginBottom(1)

	infoStyle = lipgloss.NewStyle().Foreground(secondary)
	successStyle = lipgloss.NewStyle().Foreground(success).Bold(true)
	errorStyle = lipgloss.NewStyle().Foreground(danger).Bold(true)
	warnStyle = lipgloss.NewStyle().Foreground(warning)
	helpStyle = lipgloss.NewStyle().Foreground(dim)
	stateStyle = lipgloss.NewStyle().Foreground(state)
	urlStyle = lipgloss.NewStyle().Foreground(secondary).Underline(true)
	chipStyle = lipgloss.NewStyle().Foreground(dim)
	subtleStyle = lipgloss.NewStyle().Foreground(subtle)

	cursorStr = lipgloss.NewStyle().Foreground(accent).Bold(true).Render("▸ ")

	tabBoxActive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Foreground(accent).
		Bold(true).
		Padding(0, 1)
	tabBoxInactive = lipgloss.NewStyle().
		Border(lipgloss.HiddenBorder()).
		Foreground(dim).
		Padding(0, 1)

	logoGradient = lipgloss.Blend1D(8, activePalette.LogoStops[0], activePalette.LogoStops[1])
}

// init ensures the vars are populated before any package-level
// initializer reads them. Without this, anything that does
// `var foo = lipgloss.NewStyle().Foreground(accent)` at the top of
// some other .go file would snapshot a nil color.
func init() {
	setActiveTheme("default", true)
}

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
