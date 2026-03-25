package main

import "testing"

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
		{"9NBLGGH5R558", false}, // msstore product ID (short, no special prefix)

		// MSIX paths
		{"MSIX\\Microsoft.DesktopAppInstaller_8wekyb3d8bbwe", true},
		{"MSIX\\NotepadPlusPlus_1.0.0.0_neutral__gabc1234", true},
		{"MSIX/Something.Else_abc123", true},

		// GUIDs
		{"{6F320B93-EE3C-4826-85E0-ADF79F8D4C61}", true},
		{"{11111111-2222-3333-4444-555555555555}", true},

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

func TestIdentityKind(t *testing.T) {
	tests := []struct {
		pkg  Package
		want string
	}{
		{Package{ID: "Mozilla.Firefox", Source: "winget"}, "winget"},
		{Package{ID: "9NBLGGH5R558", Source: "msstore"}, "msstore"},
		{Package{ID: "MSIX\\Some.Package_hash", Source: ""}, "system"},
		{Package{ID: "{GUID-HERE-1234}", Source: ""}, "system"},
		{Package{ID: "SomeApp_1234567890abc", Source: ""}, "system"},
		{Package{ID: "UnknownApp", Source: ""}, "other"},
	}

	for _, tt := range tests {
		t.Run(tt.pkg.ID, func(t *testing.T) {
			got := identityKind(tt.pkg)
			if got != tt.want {
				t.Errorf("identityKind(%+v) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestDeduplicatePackages(t *testing.T) {
	pkgs := []Package{
		{Name: "Notepad++", ID: "Notepad++.Notepad++", Version: "8.6.4", Source: "winget"},
		{Name: "Notepad++", ID: "MSIX\\NotepadPlusPlus_1.0.0.0_neutral__gabc1234", Version: "1.0.0.0"},
		{Name: "Microsoft To Do", ID: "9NBLGGH5R558", Version: "Unknown", Source: "msstore"},
		{Name: "Contoso Legacy Tool", ID: "{11111111-2222-3333-4444-555555555555}", Version: "2.4.1"},
		{Name: "Git", ID: "Git.Git", Version: "2.45", Source: "winget"},
	}

	result := deduplicatePackages(pkgs)

	// Notepad++ MSIX entry should be removed (canonical winget entry exists)
	if len(result) != 4 {
		t.Fatalf("expected 4 packages after dedup, got %d", len(result))
	}

	// Verify remaining packages
	ids := make([]string, len(result))
	for i, p := range result {
		ids[i] = p.ID
	}

	expected := []string{
		"Notepad++.Notepad++",
		"9NBLGGH5R558",
		"{11111111-2222-3333-4444-555555555555}",
		"Git.Git",
	}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("result[%d].ID = %q, want %q", i, ids[i], want)
		}
	}
}

func TestDeduplicatePackagesNoCanonicalsKept(t *testing.T) {
	// When there is no canonical entry, keep the non-canonical one
	pkgs := []Package{
		{Name: "Some App", ID: "MSIX\\SomeApp_hash1234567890", Version: "1.0"},
	}

	result := deduplicatePackages(pkgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 package when no canonical exists, got %d", len(result))
	}
	if result[0].ID != "MSIX\\SomeApp_hash1234567890" {
		t.Errorf("expected non-canonical package to be kept, got %q", result[0].ID)
	}
}

func TestDeduplicatePackagesPreservesOrder(t *testing.T) {
	pkgs := []Package{
		{Name: "Z App", ID: "Z.App", Version: "1.0", Source: "winget"},
		{Name: "A App", ID: "A.App", Version: "2.0", Source: "winget"},
		{Name: "M App", ID: "M.App", Version: "3.0", Source: "winget"},
	}

	result := deduplicatePackages(pkgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(result))
	}
	if result[0].ID != "Z.App" || result[1].ID != "A.App" || result[2].ID != "M.App" {
		t.Error("deduplication changed order of non-duplicate entries")
	}
}

func TestPackageSummaryWithSystem(t *testing.T) {
	pkgs := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Source: "winget"},
		{Name: "To Do", ID: "9NBLGGH5R558", Source: "msstore"},
		{Name: "Legacy", ID: "{GUID-1234-5678}", Source: ""},
		{Name: "Unknown", ID: "SomeApp", Source: ""},
	}

	summary := packageSummary(pkgs)

	// Should include "system" category
	if !contains(summary, "2 winget") {
		t.Errorf("summary missing winget count: %s", summary)
	}
	if !contains(summary, "1 msstore") {
		t.Errorf("summary missing msstore count: %s", summary)
	}
	if !contains(summary, "1 system") {
		t.Errorf("summary missing system count: %s", summary)
	}
	if !contains(summary, "1 other") {
		t.Errorf("summary missing other count: %s", summary)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
