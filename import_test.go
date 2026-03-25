package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsNonCanonical(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		// Canonical winget IDs
		{"Mozilla.Firefox", false},
		{"Microsoft.VisualStudioCode", false},
		{"Git.Git", false},
		{"Notepad++.Notepad++", false},

		// MSIX paths
		{"MSIX\\Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", true},
		{"MSIX/Something.Else_abc123", true},

		// GUIDs
		{"{6F320B93-EE3C-4826-85E0-ADF79F8D4C61}", true},
		{"{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}", true},

		// Package family names (underscore + 13+ char hash)
		{"Microsoft.WindowsTerminal_8wekyb3d8bbwe", true},
		{"AppName_1234567890abc", true},

		// Short suffix after underscore (not a family name)
		{"Some_App", false},
		{"My_Tool_v2", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := isNonCanonical(tt.id)
			if got != tt.want {
				t.Errorf("isNonCanonical(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestLoadImportFile(t *testing.T) {
	// Create a temporary export file
	dir := t.TempDir()
	path := filepath.Join(dir, "wintui_packages_test.json")

	packages := []struct {
		Name    string `json:"name"`
		ID      string `json:"id"`
		Version string `json:"version"`
		Source  string `json:"source,omitempty"`
	}{
		{"Firefox", "Mozilla.Firefox", "128.0", "winget"},
		{"VS Code", "Microsoft.VisualStudioCode", "1.90", "winget"},
		{"Some MSIX App", "MSIX\\SomePublisher.App_hash123", "1.0", ""},
		{"Unknown App", "{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}", "2.0", ""},
		{"Git", "Git.Git", "2.45", "winget"},
	}

	data, err := json.MarshalIndent(packages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate installed packages
	installed := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Version: "2.45", Source: "winget"},
	}

	pkgs, err := loadImportFile(path, installed)
	if err != nil {
		t.Fatal(err)
	}

	if len(pkgs) != 5 {
		t.Fatalf("expected 5 packages, got %d", len(pkgs))
	}

	// Firefox — already installed
	if !pkgs[0].Installed {
		t.Error("Firefox should be marked as installed")
	}
	if pkgs[0].NonCanonical {
		t.Error("Firefox should not be non-canonical")
	}

	// VS Code — not installed, canonical
	if pkgs[1].Installed {
		t.Error("VS Code should not be marked as installed")
	}
	if pkgs[1].NonCanonical {
		t.Error("VS Code should not be non-canonical")
	}

	// MSIX app — non-canonical
	if !pkgs[2].NonCanonical {
		t.Error("MSIX app should be non-canonical")
	}

	// GUID app — non-canonical
	if !pkgs[3].NonCanonical {
		t.Error("GUID app should be non-canonical")
	}

	// Git — already installed
	if !pkgs[4].Installed {
		t.Error("Git should be marked as installed")
	}
}

func TestLoadImportFileBackwardCompatible(t *testing.T) {
	// Test loading an old export file without source field
	dir := t.TempDir()
	path := filepath.Join(dir, "wintui_packages_old.json")

	content := `[
  {"name": "Firefox", "id": "Mozilla.Firefox", "version": "128.0"},
  {"name": "VS Code", "id": "Microsoft.VisualStudioCode", "version": "1.90"}
]`

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pkgs, err := loadImportFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}
	if pkgs[0].Source != "" {
		t.Errorf("expected empty source for old format, got %q", pkgs[0].Source)
	}
	if pkgs[0].Name != "Firefox" {
		t.Errorf("expected Firefox, got %q", pkgs[0].Name)
	}
}
