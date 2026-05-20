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
// Hex values for named palettes come from each theme's published spec
// where the theme ships a light/dark pair. For palettes that are
// primarily dark, the light variant is a contrast-matched companion
// palette using the same color family so the theme remains readable on
// light terminals without forcing ThemeBackground.
// Background fields are set so users who toggle the opt-in
// ThemeBackground setting get the canonical immersive look for the
// chosen theme.

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

// ── WinTUI Midnight ────────────────────────────────────────────────
//
// App-native theme: close enough to the default palette to keep the
// WinTUI identity, but calmer and more balanced for daily use.

var wintuiMidnight = Palette{
	Accent:     lipgloss.Color("#ff6fae"), // rose
	Secondary:  lipgloss.Color("#8ea0ff"), // blue-violet
	State:      lipgloss.Color("#5eead4"), // mint-cyan
	Dim:        lipgloss.Color("#5b6372"), // cool chrome
	Subtle:     lipgloss.Color("#9aa4b5"), // secondary data
	Bright:     lipgloss.Color("#e7eaf0"), // soft white
	Success:    lipgloss.Color("#8bd17c"), // green
	Danger:     lipgloss.Color("#ff6b6b"), // coral red
	Warning:    lipgloss.Color("#f6c177"), // amber
	Override:   lipgloss.Color("#f6c177"), // amber
	Background: lipgloss.Color("#111318"), // deep charcoal
	Foreground: lipgloss.Color("#e7eaf0"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#ff6fae"),
		lipgloss.Color("#5eead4"),
	},
}

var wintuiDaybreak = Palette{
	Accent:     lipgloss.Color("#c02667"), // deep rose
	Secondary:  lipgloss.Color("#4f46e5"), // indigo
	State:      lipgloss.Color("#0f766e"), // teal
	Dim:        lipgloss.Color("#a5adba"), // light chrome
	Subtle:     lipgloss.Color("#647083"), // secondary data
	Bright:     lipgloss.Color("#20242d"), // ink
	Success:    lipgloss.Color("#367a2c"), // green
	Danger:     lipgloss.Color("#c93636"), // coral red, darkened
	Warning:    lipgloss.Color("#9f6800"), // amber, darkened
	Override:   lipgloss.Color("#9f6800"), // amber, darkened
	Background: lipgloss.Color("#f6f7fb"), // cool off-white
	Foreground: lipgloss.Color("#20242d"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#c02667"),
		lipgloss.Color("#0f766e"),
	},
}

// ── Catppuccin ─────────────────────────────────────────────────────

var catppuccinMocha = Palette{
	Accent:     lipgloss.Color("#f5c2e7"), // pink
	Secondary:  lipgloss.Color("#cba6f7"), // mauve
	State:      lipgloss.Color("#94e2d5"), // teal
	Dim:        lipgloss.Color("#6c7086"), // overlay0
	Subtle:     lipgloss.Color("#a6adc8"), // subtext0
	Bright:     lipgloss.Color("#cdd6f4"), // text
	Success:    lipgloss.Color("#a6e3a1"), // green
	Danger:     lipgloss.Color("#f38ba8"), // red
	Warning:    lipgloss.Color("#f9e2af"), // yellow
	Override:   lipgloss.Color("#fab387"), // peach
	Background: lipgloss.Color("#1e1e2e"), // base
	Foreground: lipgloss.Color("#cdd6f4"), // text
	LogoStops: [2]color.Color{
		lipgloss.Color("#f5c2e7"),
		lipgloss.Color("#89b4fa"), // blue endpoint for the wave
	},
}

var catppuccinLatte = Palette{
	Accent:     lipgloss.Color("#ea76cb"), // pink
	Secondary:  lipgloss.Color("#8839ef"), // mauve
	State:      lipgloss.Color("#179299"), // teal
	Dim:        lipgloss.Color("#9ca0b0"), // overlay0
	Subtle:     lipgloss.Color("#6c6f85"), // subtext0
	Bright:     lipgloss.Color("#4c4f69"), // text
	Success:    lipgloss.Color("#40a02b"), // green
	Danger:     lipgloss.Color("#d20f39"), // red
	Warning:    lipgloss.Color("#df8e1d"), // yellow
	Override:   lipgloss.Color("#fe640b"), // peach
	Background: lipgloss.Color("#eff1f5"), // base
	Foreground: lipgloss.Color("#4c4f69"), // text
	LogoStops: [2]color.Color{
		lipgloss.Color("#ea76cb"),
		lipgloss.Color("#1e66f5"),
	},
}

// ── Nord ───────────────────────────────────────────────────────────

var nord = Palette{
	Accent:     lipgloss.Color("#88c0d0"), // frost light blue
	Secondary:  lipgloss.Color("#81a1c1"), // frost mid blue
	State:      lipgloss.Color("#8fbcbb"), // frost teal
	Dim:        lipgloss.Color("#4c566a"), // polar night light
	Subtle:     lipgloss.Color("#d8dee9"), // snow storm dim
	Bright:     lipgloss.Color("#eceff4"), // snow storm bright
	Success:    lipgloss.Color("#a3be8c"), // aurora green
	Danger:     lipgloss.Color("#bf616a"), // aurora red
	Warning:    lipgloss.Color("#ebcb8b"), // aurora yellow
	Override:   lipgloss.Color("#d08770"), // aurora orange
	Background: lipgloss.Color("#2e3440"), // polar night base
	Foreground: lipgloss.Color("#eceff4"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#88c0d0"),
		lipgloss.Color("#5e81ac"), // deeper frost blue
	},
}

var nordLight = Palette{
	Accent:     lipgloss.Color("#5e81ac"), // frost deep blue
	Secondary:  lipgloss.Color("#81a1c1"), // frost mid blue
	State:      lipgloss.Color("#4f8f8b"), // frost teal, darkened for light bg
	Dim:        lipgloss.Color("#a7afbd"), // snow storm chrome
	Subtle:     lipgloss.Color("#4c566a"), // polar night light
	Bright:     lipgloss.Color("#2e3440"), // polar night base
	Success:    lipgloss.Color("#5f7f45"), // aurora green, darkened
	Danger:     lipgloss.Color("#a9444f"), // aurora red, darkened
	Warning:    lipgloss.Color("#9a6f20"), // aurora yellow, darkened
	Override:   lipgloss.Color("#a45f40"), // aurora orange, darkened
	Background: lipgloss.Color("#eceff4"), // snow storm bright
	Foreground: lipgloss.Color("#2e3440"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#5e81ac"),
		lipgloss.Color("#88c0d0"),
	},
}

// ── Dracula ────────────────────────────────────────────────────────

var dracula = Palette{
	Accent:     lipgloss.Color("#ff79c6"), // pink
	Secondary:  lipgloss.Color("#bd93f9"), // purple
	State:      lipgloss.Color("#8be9fd"), // cyan
	Dim:        lipgloss.Color("#6272a4"), // comment
	Subtle:     lipgloss.Color("#bfbfd1"), // muted text
	Bright:     lipgloss.Color("#f8f8f2"), // foreground
	Success:    lipgloss.Color("#50fa7b"), // green
	Danger:     lipgloss.Color("#ff5555"), // red
	Warning:    lipgloss.Color("#f1fa8c"), // yellow
	Override:   lipgloss.Color("#ffb86c"), // orange
	Background: lipgloss.Color("#282a36"),
	Foreground: lipgloss.Color("#f8f8f2"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#ff79c6"),
		lipgloss.Color("#bd93f9"),
	},
}

var draculaLight = Palette{
	Accent:     lipgloss.Color("#c2187a"), // pink, darkened for light bg
	Secondary:  lipgloss.Color("#6d42b2"), // purple, darkened
	State:      lipgloss.Color("#007c91"), // cyan, darkened
	Dim:        lipgloss.Color("#9aa0bf"), // soft comment tone
	Subtle:     lipgloss.Color("#6272a4"), // comment
	Bright:     lipgloss.Color("#282a36"), // dark foreground
	Success:    lipgloss.Color("#168a35"), // green, darkened
	Danger:     lipgloss.Color("#c92c2c"), // red, darkened
	Warning:    lipgloss.Color("#8f7600"), // yellow, darkened
	Override:   lipgloss.Color("#b55f00"), // orange, darkened
	Background: lipgloss.Color("#f8f8f2"), // Dracula foreground used as light base
	Foreground: lipgloss.Color("#282a36"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#c2187a"),
		lipgloss.Color("#6d42b2"),
	},
}

// ── Tokyo Night ───────────────────────────────────────────────────

var tokyoNight = Palette{
	Accent:     lipgloss.Color("#bb9af7"), // magenta-purple
	Secondary:  lipgloss.Color("#7aa2f7"), // blue
	State:      lipgloss.Color("#7dcfff"), // cyan
	Dim:        lipgloss.Color("#565f89"), // muted blue-grey
	Subtle:     lipgloss.Color("#a9b1d6"), // mid foreground
	Bright:     lipgloss.Color("#c0caf5"), // foreground
	Success:    lipgloss.Color("#9ece6a"), // green
	Danger:     lipgloss.Color("#f7768e"), // red
	Warning:    lipgloss.Color("#e0af68"), // yellow-orange
	Override:   lipgloss.Color("#ff9e64"), // orange
	Background: lipgloss.Color("#1a1b26"),
	Foreground: lipgloss.Color("#c0caf5"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#bb9af7"),
		lipgloss.Color("#7dcfff"),
	},
}

var tokyoNightDay = Palette{
	Accent:     lipgloss.Color("#9854f1"), // magenta
	Secondary:  lipgloss.Color("#2e7de9"), // blue
	State:      lipgloss.Color("#007197"), // cyan
	Dim:        lipgloss.Color("#a8aecb"), // light chrome
	Subtle:     lipgloss.Color("#6172b0"), // muted foreground
	Bright:     lipgloss.Color("#3760bf"), // foreground
	Success:    lipgloss.Color("#587539"), // green
	Danger:     lipgloss.Color("#f52a65"), // red
	Warning:    lipgloss.Color("#8c6c3e"), // yellow
	Override:   lipgloss.Color("#b15c00"), // orange
	Background: lipgloss.Color("#e1e2e7"), // day bg
	Foreground: lipgloss.Color("#3760bf"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#9854f1"),
		lipgloss.Color("#007197"),
	},
}

// ── Ember ─────────────────────────────────────────────────────────
//
// Warm earth-tones — the lane every other curated theme leans away
// from (Sweet Pink, WinTUI Midnight, Catppuccin, Dracula, and Tokyo
// Night all sit on the cool/pink/magenta side; Nord is muted cool;
// Mono is achromatic). Ember fills the Solarized/Gruvbox-adjacent
// warm-amber/deep-green role without cloning either.
//
// Identity:
//   Dusk — glowing coals in a deep mahogany dark.
//   Dawn — morning light through autumn leaves; cream paper, burnt-
//          sienna ink, forest sage.
//
// State (the "your intent" channel) leans sage rather than the cool
// cyan/teal the other warm themes would normally use — the warm-vs-
// cool tension keeps staged checkmarks and AUTO badges legible
// against the warm structural amber.

var emberDusk = Palette{
	Accent:     lipgloss.Color("#e89a4f"), // warm amber — primary structure
	Secondary:  lipgloss.Color("#c4956c"), // muted gold — labels
	State:      lipgloss.Color("#8fa66e"), // sage — intent / state
	Dim:        lipgloss.Color("#5a4a3a"), // warm chrome
	Subtle:     lipgloss.Color("#a08977"), // warm light grey
	Bright:     lipgloss.Color("#f5e6d3"), // cream — primary text
	Success:    lipgloss.Color("#88a86b"), // sage
	Danger:     lipgloss.Color("#d9603f"), // terracotta
	Warning:    lipgloss.Color("#e8b86e"), // honey
	Override:   lipgloss.Color("#c97b3c"), // copper
	Background: lipgloss.Color("#1a1410"), // deep mahogany
	Foreground: lipgloss.Color("#f5e6d3"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#e89a4f"), // amber
		lipgloss.Color("#c97b3c"), // copper
	},
}

var emberDawn = Palette{
	Accent:     lipgloss.Color("#a85419"), // burnt sienna — readable on cream
	Secondary:  lipgloss.Color("#8a6a3f"), // warm bronze
	State:      lipgloss.Color("#4f6b3f"), // forest sage
	Dim:        lipgloss.Color("#b8a99b"), // mid warm grey
	Subtle:     lipgloss.Color("#6e5d4a"), // deeper warm grey
	Bright:     lipgloss.Color("#2c1f17"), // deep brown — near-black
	Success:    lipgloss.Color("#4f6b3f"), // forest sage
	Danger:     lipgloss.Color("#983827"), // deep terracotta
	Warning:    lipgloss.Color("#9a6418"), // deep honey
	Override:   lipgloss.Color("#7a3c14"), // deep copper
	Background: lipgloss.Color("#f4ead8"), // warm cream
	Foreground: lipgloss.Color("#2c1f17"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#a85419"), // burnt sienna
		lipgloss.Color("#7a3c14"), // deep copper
	},
}

// ── Monochrome ────────────────────────────────────────────────────
//
// High-contrast greyscale palette for accessibility. Bold weight on
// active items carries the structural emphasis that color usually
// provides. Success/Danger/Warning keep a *small* hue cue so status
// glyphs stay legible — pure greyscale would erase the pass/fail
// distinction entirely.

var monoDark = Palette{
	Accent:     lipgloss.Color("#ffffff"),
	Secondary:  lipgloss.Color("#cccccc"),
	State:      lipgloss.Color("#ffffff"),
	Dim:        lipgloss.Color("#666666"),
	Subtle:     lipgloss.Color("#999999"),
	Bright:     lipgloss.Color("#ffffff"),
	Success:    lipgloss.Color("#a3be8c"),
	Danger:     lipgloss.Color("#bf616a"),
	Warning:    lipgloss.Color("#ebcb8b"),
	Override:   lipgloss.Color("#ffffff"),
	Background: lipgloss.Color("#000000"),
	Foreground: lipgloss.Color("#ffffff"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#ffffff"),
		lipgloss.Color("#999999"),
	},
}

var monoLight = Palette{
	Accent:     lipgloss.Color("#000000"),
	Secondary:  lipgloss.Color("#333333"),
	State:      lipgloss.Color("#000000"),
	Dim:        lipgloss.Color("#999999"),
	Subtle:     lipgloss.Color("#666666"),
	Bright:     lipgloss.Color("#000000"),
	Success:    lipgloss.Color("#28702b"),
	Danger:     lipgloss.Color("#a91527"),
	Warning:    lipgloss.Color("#a06914"),
	Override:   lipgloss.Color("#000000"),
	Background: lipgloss.Color("#ffffff"),
	Foreground: lipgloss.Color("#000000"),
	LogoStops: [2]color.Color{
		lipgloss.Color("#000000"),
		lipgloss.Color("#555555"),
	},
}

var themes = map[string]Theme{
	"default": {
		ID:    "default",
		Label: "Sweet Pink",
		Dark:  defaultDark,
		Light: defaultLight,
	},
	"wintui": {
		ID:    "wintui",
		Label: "WinTUI Midnight",
		Dark:  wintuiMidnight,
		Light: wintuiDaybreak,
	},
	"catppuccin": {
		ID:    "catppuccin",
		Label: "Catppuccin",
		Dark:  catppuccinMocha,
		Light: catppuccinLatte,
	},
	"nord": {
		ID:    "nord",
		Label: "Nord",
		Dark:  nord,
		Light: nordLight,
	},
	"dracula": {
		ID:    "dracula",
		Label: "Dracula",
		Dark:  dracula,
		Light: draculaLight,
	},
	"tokyonight": {
		ID:    "tokyonight",
		Label: "Tokyo Night",
		Dark:  tokyoNight,
		Light: tokyoNightDay,
	},
	"ember": {
		ID:    "ember",
		Label: "Ember",
		Dark:  emberDusk,
		Light: emberDawn,
	},
	"mono": {
		ID:    "mono",
		Label: "Monochrome",
		Dark:  monoDark,
		Light: monoLight,
	},
}

// themeOrder is the user-facing cycle order. Defined separately from
// the themes map because Go map iteration is unordered — the picker
// needs a deterministic next/prev.
var themeOrder = []string{
	"default",
	"wintui",
	"catppuccin",
	"nord",
	"dracula",
	"tokyonight",
	"ember",
	"mono",
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
