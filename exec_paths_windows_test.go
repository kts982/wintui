package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPowershellExePathIsAbsoluteSystem32(t *testing.T) {
	path := powershellExePath()
	if !filepath.IsAbs(path) {
		t.Fatalf("powershellExePath() = %q, want absolute", path)
	}
	if !strings.HasSuffix(strings.ToLower(path), `\system32\windowspowershell\v1.0\powershell.exe`) {
		t.Fatalf("powershellExePath() = %q, want the System32 PowerShell 5.1 path", path)
	}
}

func TestWingetPathAllowed(t *testing.T) {
	const (
		localAppData = `C:\Users\someone\AppData\Local`
		programFiles = `C:\Program Files`
	)
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"user app execution alias", `C:\Users\someone\AppData\Local\Microsoft\WindowsApps\winget.exe`, true},
		{"user alias case-insensitive", `c:\users\someone\appdata\local\microsoft\windowsapps\WINGET.EXE`, true},
		{"packaged app store subdir", `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.24_x64__8wekyb3d8bbwe\winget.exe`, true},
		{"arbitrary PATH dir", `C:\evil\winget.exe`, false},
		{"name-prefix sibling", `C:\Users\someone\AppData\Local\Microsoft\WindowsAppsEvil\winget.exe`, false},
		{"root itself", `C:\Users\someone\AppData\Local\Microsoft\WindowsApps`, false},
		{"dotdot escape", `C:\Program Files\WindowsApps\..\..\evil\winget.exe`, false},
	}
	for _, tt := range tests {
		if got := wingetPathAllowed(tt.path, localAppData, programFiles); got != tt.want {
			t.Errorf("%s: wingetPathAllowed(%q) = %v, want %v", tt.name, tt.path, got, tt.want)
		}
	}

	if wingetPathAllowed(`C:\anything\winget.exe`, "", "") {
		t.Error("wingetPathAllowed must reject everything when no roots are known")
	}
}

func TestRankAppInstallerDirs(t *testing.T) {
	names := []string{
		"Microsoft.DesktopAppInstaller_1.29.380.0_x64__8wekyb3d8bbwe",
		"Microsoft.DesktopAppInstaller_1.30.10.0_arm64__8wekyb3d8bbwe",
		"Microsoft.DesktopAppInstaller_1.28.240.0_x64__8wekyb3d8bbwe",
		"Microsoft.DesktopAppInstaller_1.29.380.0_neutral_~_8wekyb3d8bbwe", // resource bundle
		"Microsoft.DesktopAppInstaller_1.31.0.0_x64__0000000000000",        // wrong publisher
		"Microsoft.DesktopAppInstaller_beta_x64__8wekyb3d8bbwe",            // unparseable version
		"Microsoft.WindowsTerminal_1.25.0.0_x64__8wekyb3d8bbwe",
	}
	got := rankAppInstallerDirs(names, "amd64")
	want := []string{
		"Microsoft.DesktopAppInstaller_1.29.380.0_x64__8wekyb3d8bbwe",
		"Microsoft.DesktopAppInstaller_1.28.240.0_x64__8wekyb3d8bbwe",
		"Microsoft.DesktopAppInstaller_1.30.10.0_arm64__8wekyb3d8bbwe",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rankAppInstallerDirs(amd64) = %q, want %q", got, want)
	}

	got = rankAppInstallerDirs(names, "arm64")
	if len(got) == 0 || got[0] != "Microsoft.DesktopAppInstaller_1.30.10.0_arm64__8wekyb3d8bbwe" {
		t.Fatalf("rankAppInstallerDirs(arm64) = %q, want the arm64 package first", got)
	}
}

func TestCompareDottedVersions(t *testing.T) {
	tests := []struct {
		a, b []int
		sign int
	}{
		{[]int{1, 29, 380, 0}, []int{1, 29, 380, 0}, 0},
		{[]int{1, 30}, []int{1, 29, 999}, 1},
		{[]int{1, 29}, []int{1, 29, 0, 1}, -1},
	}
	for _, tt := range tests {
		got := compareDottedVersions(tt.a, tt.b)
		if (got > 0) != (tt.sign > 0) || (got < 0) != (tt.sign < 0) {
			t.Errorf("compareDottedVersions(%v, %v) = %d, want sign %d", tt.a, tt.b, got, tt.sign)
		}
	}
}

func TestAppInstallerWingetPath(t *testing.T) {
	programFiles := t.TempDir()
	apps := filepath.Join(programFiles, "WindowsApps")
	newest := filepath.Join(apps, "Microsoft.DesktopAppInstaller_1.30.0.0_x64__8wekyb3d8bbwe")
	older := filepath.Join(apps, "Microsoft.DesktopAppInstaller_1.29.380.0_x64__8wekyb3d8bbwe")
	for _, dir := range []string{newest, older} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Only the older package actually carries winget.exe: the newest
	// directory without it must be skipped, not returned.
	if err := os.WriteFile(filepath.Join(older, "winget.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := appInstallerWingetPath(programFiles, "amd64")
	if !ok || got != filepath.Join(older, "winget.exe") {
		t.Fatalf("appInstallerWingetPath() = %q, %v; want %q", got, ok, filepath.Join(older, "winget.exe"))
	}
	if !wingetPathAllowed(got, "", programFiles) {
		t.Fatalf("fallback path %q must pass wingetPathAllowed", got)
	}

	if _, ok := appInstallerWingetPath(t.TempDir(), "amd64"); ok {
		t.Fatal("appInstallerWingetPath() found winget in a ProgramFiles without WindowsApps")
	}
	if _, ok := appInstallerWingetPath("", "amd64"); ok {
		t.Fatal("appInstallerWingetPath() must fail when ProgramFiles is unknown")
	}
}
