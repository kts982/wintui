package main

import (
	"slices"
	"testing"
)

func TestSettingChoiceLabelUsesPerSettingDisplay(t *testing.T) {
	tests := []struct {
		def  settingDef
		val  string
		want string
	}{
		{settingDefs[1], "", "default"},
		{settingDefs[2], "", "auto"},
		{settingDefs[3], "", "all"},
	}

	for _, tt := range tests {
		if got := tt.def.choiceLabel(tt.val); got != tt.want {
			t.Fatalf("choiceLabel(%q) = %q, want %q", tt.val, got, tt.want)
		}
	}
}

func TestBuildUninstallArgsIncludesInteractiveMode(t *testing.T) {
	s := DefaultSettings()
	s.InstallMode = "interactive"
	got := s.BuildUninstallArgs(false)
	if len(got) != 1 || got[0] != "--interactive" {
		t.Fatalf("BuildUninstallArgs(false) = %#v, want [\"--interactive\"]", got)
	}
}

func TestBuildUninstallArgsIncludesSilentMode(t *testing.T) {
	s := DefaultSettings()
	s.InstallMode = ModeSilent
	got := s.BuildUninstallArgs(false)
	if len(got) != 1 || got[0] != "--silent" {
		t.Fatalf("BuildUninstallArgs(false) = %#v, want [\"--silent\"]", got)
	}
}

// TestUninstallCommandArgsIncludesSilentMode locks the full uninstall command
// chain — uninstallCommandArgs must surface --silent when Action Mode is set
// to silent in settings. Regression guard for the wire between settings and
// the rendered winget command.
func TestUninstallCommandArgsIncludesSilentMode(t *testing.T) {
	orig := appSettings
	defer func() { appSettings = orig }()
	appSettings = DefaultSettings()
	appSettings.InstallMode = ModeSilent
	appSettings.Source = "winget"

	got := uninstallCommandArgs(Package{ID: "Mozilla.Firefox", Source: "winget"}, false, false)
	want := []string{"uninstall", "--id", "Mozilla.Firefox", "--exact", "--silent"}
	if !slices.Equal(got, want) {
		t.Fatalf("uninstallCommandArgs() = %#v, want %#v", got, want)
	}
}

// TestSettingsResetKeepsPackageRules: "R" on the Settings screen resets the
// global settings it edits, never the per-package rules or cleanup opt-ins
// edited elsewhere. Dropping the rules made held packages reappear on the
// next Packages refresh (the reload is skipped while the screen is dirty),
// and a following "s" would have erased them on disk.
func TestSettingsResetKeepsPackageRules(t *testing.T) {
	setupConfigTest(t)
	t.Cleanup(func() { settingsScreenDirty = false })
	if err := updateSettings(func(s *Settings) {
		s.InstallMode = ModeSilent
		s.setOverride("Ollama.Ollama", "winget", PackageOverride{UpdatePolicy: PolicyHold})
		s.CleanupEnabledTargets = []string{"go_build"}
	}); err != nil {
		t.Fatal(err)
	}

	sc := newSettingsScreen()
	// Plain r is refresh on every other tab; on Settings it must do nothing.
	if _, _ = sc.update(keyMsg("r")); currentSettings().InstallMode != ModeSilent {
		t.Fatal(`plain "r" changed settings; reset is bound to "R"`)
	}
	next, _ := sc.update(keyMsg("R"))
	got := currentSettings()
	if got.InstallMode != DefaultSettings().InstallMode {
		t.Errorf("reset kept InstallMode %q; want the default", got.InstallMode)
	}
	if got.updatePolicy("Ollama.Ollama", "winget", "0.40.2") != PolicyHold {
		t.Error("reset dropped the package hold rule")
	}
	if !slices.Equal(got.CleanupEnabledTargets, []string{"go_build"}) {
		t.Errorf("reset dropped cleanup opt-ins: %v", got.CleanupEnabledTargets)
	}
	if !next.(settingsScreen).dirty {
		t.Error("reset of a non-default setting must leave the screen dirty until saved")
	}
	_, hidden := buildItems(nil, []Package{{ID: "Ollama.Ollama", Source: "winget", Version: "0.34.4", Available: "0.40.2"}})
	if hidden != 1 {
		t.Errorf("held package not hidden after reset: hidden=%d", hidden)
	}
}
