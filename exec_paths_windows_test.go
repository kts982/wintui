package main

import (
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
