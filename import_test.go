package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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

func TestLoadImportFileDetectsCanonicalPackageInstalledAsRawIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wintui_packages_test.json")

	packages := []struct {
		Name    string `json:"name"`
		ID      string `json:"id"`
		Version string `json:"version"`
		Source  string `json:"source,omitempty"`
	}{
		{"Notepad++", "Notepad++.Notepad++", "8.6.4", "winget"},
	}

	data, err := json.MarshalIndent(packages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	installed := []Package{
		{Name: "Notepad++", ID: "MSIX\\NotepadPlusPlus_1.0.0.0_neutral__gabc1234", Version: "1.0.0.0"},
	}

	pkgs, err := loadImportFile(path, installed)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if !pkgs[0].Installed {
		t.Fatal("expected canonical package to be marked installed when a raw identity duplicate is present")
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

func TestResolveImportSource(t *testing.T) {
	tests := []struct {
		name string
		pkg  importPkg
		want string
	}{
		{
			name: "explicit winget source",
			pkg:  importPkg{ID: "Mozilla.Firefox", Source: "winget"},
			want: "winget",
		},
		{
			name: "explicit msstore source",
			pkg:  importPkg{ID: "9NBLGGH5R558", Source: "msstore"},
			want: "msstore",
		},
		{
			name: "old canonical winget export defaults to winget",
			pkg:  importPkg{ID: "Mozilla.Firefox"},
			want: "winget",
		},
		{
			name: "old store export infers msstore from product id",
			pkg:  importPkg{ID: "9NBLGGH5R558"},
			want: "msstore",
		},
		{
			name: "raw identity stays source-less",
			pkg:  importPkg{ID: "MSIX\\Some.Package_hash1234567890"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveImportSource(tt.pkg); got != tt.want {
				t.Fatalf("resolveImportSource(%+v) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestImportLoadedDefaultsToActionablePackages(t *testing.T) {
	m := newImportModel()
	m.active = true
	m.state = importScanning

	next, _, handled := m.update(importLoadedMsg{
		packages: []importPkg{
			{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0"},
			{Name: "Git", ID: "Git.Git", Version: "2.45", Installed: true},
			{Name: "Raw App", ID: "MSIX\\Raw.App_hash123", Version: "1.0", NonCanonical: true},
		},
	}, nil)
	if !handled {
		t.Fatal("expected importLoadedMsg to be handled")
	}

	if next.state != importReview {
		t.Fatalf("state = %v, want %v", next.state, importReview)
	}
	if next.showAll {
		t.Fatal("expected review to default to actionable packages")
	}
	if got := next.visiblePackageIndices(); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("visiblePackageIndices() = %v, want [0]", got)
	}
	if got := next.selectedCount(); got != 1 {
		t.Fatalf("selectedCount() = %d, want 1", got)
	}
}

func TestImportReviewToggleSkippedShowsAllEntries(t *testing.T) {
	m := newImportModel()
	m.active = true
	m.state = importReview
	m.packages = []importPkg{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0"},
		{Name: "Git", ID: "Git.Git", Version: "2.45", Installed: true},
		{Name: "Raw App", ID: "MSIX\\Raw.App_hash123", Version: "1.0", NonCanonical: true},
	}
	m.selected[0] = true

	next, _, handled := m.update(keyMsg("v"), nil)
	if !handled {
		t.Fatal("expected show-skipped toggle to be handled")
	}
	if !next.showAll {
		t.Fatal("expected showAll to be enabled after pressing v")
	}
	if got := next.visiblePackageIndices(); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("visiblePackageIndices() = %v, want [0 1 2]", got)
	}
}

func TestImportReviewShowsEmptyActionableSummary(t *testing.T) {
	m := newImportModel()
	m.state = importReview
	m.packages = []importPkg{
		{Name: "Git", ID: "Git.Git", Installed: true},
		{Name: "Raw App", ID: "MSIX\\Raw.App_hash123", NonCanonical: true},
	}

	got := m.view(120, 24)
	if !strings.Contains(got, "Nothing to install from this file.") {
		t.Fatalf("view() = %q, want empty actionable summary", got)
	}
	if !strings.Contains(got, "Press v to show skipped entries") {
		t.Fatalf("view() = %q, want skipped toggle hint", got)
	}
}
