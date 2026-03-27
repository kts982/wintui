package main

import "testing"

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
