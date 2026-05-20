package main

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// snapshotAppSettings snapshots the package-level appSettings and
// restores it on test cleanup. Required for any test that calls a
// path which mutates appSettings (e.g. settingsScreen.cycleForward /
// cycleBackward, the "r" reset key) — without this, a non-default
// Theme/Scope/etc. set during one test will leak into every test
// that runs afterward. Distinct from cleanup_test.go's withAppSettings
// (which also *replaces* the settings); this helper only snapshots.
func snapshotAppSettings(t *testing.T) {
	t.Helper()
	prev := appSettings
	t.Cleanup(func() { appSettings = prev })
}

// withTheme snapshots the active theme state and restores it on test
// cleanup so tests that mutate package globals don't leak into each
// other. Use this in any test that calls setActiveTheme.
func withTheme(t *testing.T, id string, bgIsDark bool) {
	t.Helper()
	prev := activePalette
	setActiveTheme(id, bgIsDark)
	t.Cleanup(func() {
		activePalette = prev
		// Re-bind the globals from the snapshotted palette. Cheaper
		// than calling setActiveTheme again because we already know
		// the palette is well-formed.
		accent = prev.Accent
		secondary = prev.Secondary
		state = prev.State
		dim = prev.Dim
		subtle = prev.Subtle
		bright = prev.Bright
		success = prev.Success
		danger = prev.Danger
		warning = prev.Warning
		override = prev.Override
		rebuildThemeStyles()
	})
}

func TestSetActiveThemeUnknownFallsBackToDefault(t *testing.T) {
	withTheme(t, "default", true) // snapshot+restore

	setActiveTheme("this-theme-does-not-exist", true)

	// The default Dark palette's Accent is ANSI 212. If unknown fell
	// back correctly, accent should equal that.
	want := defaultDark.Accent
	if accent != want {
		t.Errorf("unknown theme did not fall back to default: accent = %v, want %v", accent, want)
	}
}

func TestSetActiveThemeRebindsGlobals(t *testing.T) {
	withTheme(t, "default", true)

	// Sanity check Dark variant.
	if accent != defaultDark.Accent {
		t.Errorf("dark accent = %v, want %v", accent, defaultDark.Accent)
	}
	if bright != defaultDark.Bright {
		t.Errorf("dark bright = %v, want %v", bright, defaultDark.Bright)
	}

	// Flip to Light.
	setActiveTheme("default", false)
	if accent != defaultLight.Accent {
		t.Errorf("light accent = %v, want %v", accent, defaultLight.Accent)
	}
	if bright != defaultLight.Bright {
		t.Errorf("light bright = %v, want %v", bright, defaultLight.Bright)
	}
}

func TestSetActiveThemeWithoutLightVariantUsesDark(t *testing.T) {
	withTheme(t, "default", true)

	// Register a theme that only defines Dark. With bgIsDark=false
	// we should still get the Dark palette since there's no Light
	// variant to fall back to.
	const key = "__darkonly_test__"
	themes[key] = Theme{
		ID:    key,
		Label: "darkonly",
		Dark: Palette{
			Accent:    lipgloss.Color("196"),
			Secondary: lipgloss.Color("99"),
			State:     lipgloss.Color("86"),
			Dim:       lipgloss.Color("240"),
			Subtle:    lipgloss.Color("247"),
			Bright:    lipgloss.Color("231"),
			Success:   lipgloss.Color("78"),
			Danger:    lipgloss.Color("196"),
			Warning:   lipgloss.Color("220"),
			Override:  lipgloss.Color("222"),
			LogoStops: [2]color.Color{
				lipgloss.Color("196"),
				lipgloss.Color("226"),
			},
		},
		// Light is left zero — should fall back to Dark.
	}
	t.Cleanup(func() { delete(themes, key) })

	setActiveTheme(key, false /* bgIsDark */)
	want := themes[key].Dark.Accent
	if accent != want {
		t.Errorf("theme without Light variant did not use Dark: accent = %v, want %v", accent, want)
	}
}

func TestRebuildThemeStylesPopulatesCaches(t *testing.T) {
	withTheme(t, "default", true)

	// itemStyle, sectionTitleStyle, cursorStr, tabBoxActive,
	// tabBoxInactive, and logoGradient should all be non-zero after
	// setActiveTheme. If any are left at zero value, a future
	// developer added a cached style without updating
	// rebuildThemeStyles.
	if cursorStr == "" {
		t.Error("cursorStr is empty after setActiveTheme")
	}
	if len(logoGradient) == 0 {
		t.Error("logoGradient is empty after setActiveTheme")
	}
	// Rendering through itemStyle should not produce an empty string
	// even for a single character.
	if itemStyle.Render("x") == "" {
		t.Error("itemStyle renders empty output")
	}
}

func TestAllScreensImplementThemeAware(t *testing.T) {
	// Every screen reachable via createScreen must implement
	// themeAware so app.rethemeScreens can refresh model-held
	// styles. A new screen added later that doesn't will trip this
	// at test time, not in production.
	for _, id := range []screenID{
		screenWorkspace,
		screenCleanup,
		screenHealthcheck,
		screenSettings,
	} {
		s := createScreen(id)
		if _, ok := s.(themeAware); !ok {
			t.Errorf("screen id %d (%T) does not implement themeAware", id, s)
		}
	}
}

func TestLookupThemeFallback(t *testing.T) {
	if got := lookupTheme(""); got.ID != "default" {
		t.Errorf("lookupTheme(\"\") = %q, want default", got.ID)
	}
	if got := lookupTheme("garbage"); got.ID != "default" {
		t.Errorf("lookupTheme(garbage) = %q, want default", got.ID)
	}
	if got := lookupTheme("default"); got.ID != "default" {
		t.Errorf("lookupTheme(default) = %q, want default", got.ID)
	}
}

// TestAllCuratedThemesApplyWithoutPanic exercises every theme ID in
// themeOrder against setActiveTheme. Guards against a partially-
// initialized Palette (e.g. forgot to set LogoStops) crashing the
// caller — Blend1D and the various lipgloss.Style chains would
// panic or render garbage on nil colors.
func TestAllCuratedThemesApplyWithoutPanic(t *testing.T) {
	withTheme(t, "default", true)

	for _, id := range themeOrder {
		t.Run(id, func(t *testing.T) {
			setActiveTheme(id, true)
			if activePalette.Accent == nil {
				t.Errorf("theme %q has nil Accent after apply", id)
			}
			if len(logoGradient) == 0 {
				t.Errorf("theme %q produced empty logoGradient", id)
			}
			// Render something through every cached style to catch
			// nil-color panics; we don't assert on the output.
			_ = itemStyle.Render("x")
			_ = stateStyle.Render("y")
			_ = sectionTitleStyle.Render("z")
		})
	}
}

// TestThemeSettingDefCoversCuratedSet asserts the picker exposes every
// curated theme. Equivalent to "themeOrder vs themeSettingDef.choices"
// drift detection — if someone adds a palette to theme.go but forgets
// the picker mapping, this trips.
func TestThemeSettingDefCoversCuratedSet(t *testing.T) {
	if len(themeSettingDef.choices) != len(themeOrder) {
		t.Errorf("themeSettingDef has %d choices, themeOrder has %d",
			len(themeSettingDef.choices), len(themeOrder))
	}
	for _, choice := range themeSettingDef.choices {
		// Normalize the empty-string default-slot back to "default"
		// before lookup, matching normalizeTheme's contract.
		id := choice
		if id == "" {
			id = "default"
		}
		if _, ok := themes[id]; !ok {
			t.Errorf("themeSettingDef choice %q has no themes entry", choice)
		}
	}
}

func TestAllCuratedThemesHaveLightVariants(t *testing.T) {
	withTheme(t, "default", true)

	for _, id := range themeOrder {
		t.Run(id, func(t *testing.T) {
			theme := lookupTheme(id)
			if !hasLightVariant(theme.Light) {
				t.Fatalf("theme %q does not define a light variant", id)
			}
			setActiveTheme(id, false)
			if activePalette.Accent != theme.Light.Accent {
				t.Fatalf("light background applied accent %v, want %v", activePalette.Accent, theme.Light.Accent)
			}
		})
	}
}

func TestSettingsThemeRoundTrip(t *testing.T) {
	withTheme(t, "default", true)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"default empty stays empty", "", ""},
		{"explicit default normalizes to empty", "default", ""},
		{"wintui preserved", "wintui", "wintui"},
		{"catppuccin preserved", "catppuccin", "catppuccin"},
		{"nord preserved", "nord", "nord"},
		{"ember preserved", "ember", "ember"},
		{"garbage falls back to empty (= default)", "this-doesnt-exist", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultSettings()
			s.setValue("theme", tc.in)
			if got := s.getValue("theme"); got != tc.want {
				t.Errorf("setValue(%q) → getValue = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSettingsThemeBackgroundRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"theme", "theme"},
		{"garbage", ""},
	}
	for _, tc := range cases {
		s := DefaultSettings()
		s.setValue("theme_background", tc.in)
		if got := s.getValue("theme_background"); got != tc.want {
			t.Errorf("setValue(%q) → getValue = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSettingsCycleEmitsThemeChangedMsg asserts that flipping the
// theme or theme_background settings emits themeChangedMsg so the
// app picks up the change. Without this signal, model-held styles
// (spinners, viewports, progress bars) would stay stuck on the old
// palette until the next screen recreation.
func TestSettingsCycleEmitsThemeChangedMsg(t *testing.T) {
	withTheme(t, "default", true)
	snapshotAppSettings(t) // cycleForward mutates appSettings via setValue

	// Find the cursor index of the "theme" row.
	themeIdx := -1
	for i, def := range settingDefs {
		if def.key == "theme" {
			themeIdx = i
			break
		}
	}
	if themeIdx < 0 {
		t.Fatal("theme row not present in settingDefs")
	}

	s := newSettingsScreen()
	s.cursor = themeIdx
	cmd := s.cycleForward()
	if cmd == nil {
		t.Fatal("cycling theme returned nil cmd; expected themeChangedMsg producer")
	}
	if _, ok := cmd().(themeChangedMsg); !ok {
		t.Errorf("cycling theme produced %T, want themeChangedMsg", cmd())
	}
}

// TestSettingsCycleOnNonThemeKeyReturnsNil asserts the themeChangedMsg
// emission is gated on the key — flipping an unrelated setting must
// not blast a retheme cmd onto the bus.
func TestSettingsCycleOnNonThemeKeyReturnsNil(t *testing.T) {
	withTheme(t, "default", true)
	snapshotAppSettings(t)

	scopeIdx := -1
	for i, def := range settingDefs {
		if def.key == "scope" {
			scopeIdx = i
			break
		}
	}
	if scopeIdx < 0 {
		t.Fatal("scope row not present in settingDefs")
	}

	s := newSettingsScreen()
	s.cursor = scopeIdx
	if cmd := s.cycleForward(); cmd != nil {
		t.Errorf("cycling non-theme key returned non-nil cmd: %T", cmd())
	}
}

// TestWrapScreenCmdPassesThemeChangedMsg asserts the wrapScreenCmd
// passthrough at the bottom of its switch case list: a screen
// emitting themeChangedMsg must reach the app handler intact, not
// get re-wrapped into screenCmdMsg{target: id} and routed back to
// the originating screen.
func TestWrapScreenCmdPassesThemeChangedMsg(t *testing.T) {
	a := app{}
	inner := func() tea.Msg { return themeChangedMsg{} }
	wrapped := a.wrapScreenCmd(screenSettings, inner)
	if wrapped == nil {
		t.Fatal("wrapScreenCmd returned nil for a non-nil inner cmd")
	}
	got := wrapped()
	if _, ok := got.(themeChangedMsg); !ok {
		t.Errorf("wrapScreenCmd rewrapped themeChangedMsg as %T; want passthrough", got)
	}
}

// TestAppViewBackgroundColorOptIn asserts ThemeBackground gating:
// nil bg when "terminal" (default), set bg only when the user opts
// into "theme" AND the active palette has a Background.
func TestAppViewBackgroundColorOptIn(t *testing.T) {
	withTheme(t, "wintui", true) // wintui palette ships a Background
	snapshotAppSettings(t)

	a := newApp(nil)
	a.width = 80
	a.height = 24

	// 1. Default: ThemeBackground = "" → no background set.
	appSettings.ThemeBackground = ""
	v := a.View()
	if v.BackgroundColor != nil {
		t.Errorf("ThemeBackground=terminal set BackgroundColor=%v; want nil", v.BackgroundColor)
	}

	// 2. Opt-in: ThemeBackground = "theme" → palette Background applied.
	appSettings.ThemeBackground = ThemeBackgroundTheme
	v = a.View()
	if v.BackgroundColor != activePalette.Background {
		t.Errorf("ThemeBackground=theme set BackgroundColor=%v; want %v", v.BackgroundColor, activePalette.Background)
	}
	if v.ForegroundColor != activePalette.Foreground {
		t.Errorf("ThemeBackground=theme set ForegroundColor=%v; want %v", v.ForegroundColor, activePalette.Foreground)
	}

	// 3. Opt-in but palette has no Background → still nil (the default
	//    Sweet Pink theme leaves Background unset).
	withTheme(t, "default", true)
	appSettings.ThemeBackground = ThemeBackgroundTheme
	v = a.View()
	if v.BackgroundColor != nil {
		t.Errorf("default theme + ThemeBackground=theme set BackgroundColor=%v; want nil (default has no Background)", v.BackgroundColor)
	}
}
