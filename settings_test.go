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
