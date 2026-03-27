package main

import (
	"strings"
	"testing"
)

func TestInstallDoneViewShowsPackageSummaryAndHint(t *testing.T) {
	s := newInstallScreen()
	s.state = installDone
	s.packages = []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0", Source: "winget"},
	}

	got := s.view(120, 24)
	if !strings.Contains(got, "Firefox installed successfully") {
		t.Fatalf("view() = %q, want package success summary", got)
	}
	if !strings.Contains(got, "Mozilla.Firefox") {
		t.Fatalf("view() = %q, want package metadata", got)
	}
	if !strings.Contains(got, "Press r to search again or esc to leave") {
		t.Fatalf("view() = %q, want next-step hint", got)
	}
}

func TestInstallConfirmViewShowsSelectedTargetVersion(t *testing.T) {
	s := newInstallScreen()
	s.state = installConfirm
	s.packages = []Package{
		{Name: "Notepad++", ID: "Notepad++.Notepad++", Version: "8.9.3", Source: "winget"},
	}
	s.selectedVersions[packageSourceKey("Notepad++.Notepad++", "winget")] = "8.9.2"

	got := s.view(120, 24)
	if !strings.Contains(got, "Install ") ||
		!strings.Contains(got, "Notepad++.Notepad++) version ") ||
		!strings.Contains(got, "8.9.2") {
		t.Fatalf("view() = %q, want explicit target version in confirm text", got)
	}
}
