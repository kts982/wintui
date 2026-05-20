package main

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
)

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
