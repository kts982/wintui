package main

import (
	"bytes"
	"strings"
	"testing"
)

// withSavedSettings runs fn with appSettings restored afterward. SaveSettings
// writes to disk, but TestMain snapshots and restores settings.json, so the
// on-disk file is safe; this just protects the in-memory global.
func withSavedSettings(fn func()) {
	saved := appSettings
	defer func() { appSettings = saved }()
	fn()
}

func TestRunThemeListShowsAllThemesAndActiveMarker(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = "nord"
		var buf bytes.Buffer
		if err := runTheme("", true, &buf); err != nil {
			t.Fatalf("runTheme list: %v", err)
		}
		out := buf.String()
		for _, id := range themeOrder {
			if !strings.Contains(out, themes[id].Label) {
				t.Errorf("theme list missing label %q for %q\n%s", themes[id].Label, id, out)
			}
		}
		if !strings.Contains(out, "(active)") {
			t.Errorf("theme list missing active marker:\n%s", out)
		}
		if !strings.Contains(out, "active: Nord") {
			t.Errorf("theme list header should name active theme:\n%s", out)
		}
	})
}

func TestRunThemeShowActive(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = "" // default slot
		var buf bytes.Buffer
		if err := runTheme("", false, &buf); err != nil {
			t.Fatalf("runTheme show: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Theme:") || !strings.Contains(out, lookupTheme("default").Label) {
			t.Errorf("expected active default theme in output:\n%s", out)
		}
		if !strings.Contains(out, "Background:") {
			t.Errorf("expected background line:\n%s", out)
		}
	})
}

func TestRunThemeSetPersistsSelection(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = ""
		var buf bytes.Buffer
		if err := runTheme("nord", false, &buf); err != nil {
			t.Fatalf("runTheme set: %v", err)
		}
		if appSettings.Theme != "nord" {
			t.Errorf("appSettings.Theme = %q, want \"nord\"", appSettings.Theme)
		}
		if !strings.Contains(buf.String(), "Theme set to Nord") {
			t.Errorf("expected confirmation, got: %q", buf.String())
		}
	})
}

func TestRunThemeSetDefaultPersistsEmpty(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = "nord"
		var buf bytes.Buffer
		if err := runTheme("default", false, &buf); err != nil {
			t.Fatalf("runTheme set default: %v", err)
		}
		// "default" normalizes to the canonical empty "I haven't picked" value,
		// matching the settings-UI behavior.
		if appSettings.Theme != "" {
			t.Errorf("appSettings.Theme = %q, want \"\" for default", appSettings.Theme)
		}
	})
}

func TestRunThemeUnknownReturnsError(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = "nord"
		var buf bytes.Buffer
		err := runTheme("not-a-theme", false, &buf)
		if err == nil {
			t.Fatal("expected error for unknown theme, got nil")
		}
		if appSettings.Theme != "nord" {
			t.Errorf("unknown theme should not mutate settings; got %q", appSettings.Theme)
		}
	})
}

func TestRunThemeSetIsCaseInsensitive(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = ""
		var buf bytes.Buffer
		if err := runTheme("Nord", false, &buf); err != nil {
			t.Fatalf("runTheme set mixed-case: %v", err)
		}
		if appSettings.Theme != "nord" {
			t.Errorf("appSettings.Theme = %q, want \"nord\"", appSettings.Theme)
		}
	})
}

func TestCheckThemeSummaryIsInfoRow(t *testing.T) {
	withSavedSettings(func() {
		appSettings.Theme = "dracula"
		appSettings.ThemeBackground = ThemeBackgroundTheme
		c := checkThemeSummary()
		if c.Check != "Theme" || c.Status != "INFO" {
			t.Errorf("checkThemeSummary = %+v, want Check=Theme Status=INFO", c)
		}
		if !strings.Contains(c.Details, lookupTheme("dracula").Label) {
			t.Errorf("details should name the theme: %q", c.Details)
		}
		if !strings.Contains(c.Details, "background: theme") {
			t.Errorf("details should report background mode: %q", c.Details)
		}
	})
}
