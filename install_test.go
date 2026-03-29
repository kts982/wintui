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
	if !strings.Contains(got, "Notepad++.Notepad++") ||
		!strings.Contains(got, "Target version:") ||
		!strings.Contains(got, "8.9.2") {
		t.Fatalf("view() = %q, want explicit target version in confirm text", got)
	}
}

func TestInstallDetailOverlayConsumesEnter(t *testing.T) {
	s := newInstallScreen()
	s.state = installResults
	s.packages = []Package{
		{Name: "Neovim", ID: "Neovim.Neovim", Version: "0.11.6", Source: "winget"},
	}
	s.detail.state = detailReady
	s.detail.pkgID = "Neovim.Neovim"
	s.detail.source = "winget"
	s.detail.detail = PackageDetail{Name: "Neovim", ID: "Neovim.Neovim"}

	next, cmd := s.update(keyMsg("enter"))
	got := next.(installScreen)

	if cmd != nil {
		t.Fatalf("update() returned unexpected cmd while detail overlay was open")
	}
	if got.state != installResults {
		t.Fatalf("state = %v, want installResults", got.state)
	}
	if !got.detail.visible() {
		t.Fatal("detail overlay closed unexpectedly")
	}
}

func TestInstallDoneViewShowsSoftElevationRetryHint(t *testing.T) {
	s := newInstallScreen()
	s.state = installDone
	s.err = assertErr("installer failed with a fatal error (1603)")
	s.output = "Install failed with exit code: 1603"
	s.packages = []Package{
		{Name: "Neovim", ID: "Neovim.Neovim", Source: "winget"},
	}

	got := s.view(120, 24)
	if !strings.Contains(got, "Retrying elevated may install machine-wide instead of per-user for some packages.") {
		t.Fatalf("view() = %q, want soft elevation warning", got)
	}
	if !strings.Contains(got, "Press Ctrl+e to relaunch elevated and retry the failed package.") {
		t.Fatalf("view() = %q, want retry hint", got)
	}
}
