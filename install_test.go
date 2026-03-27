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
