package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatInstallResults(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		output   string
		want     string
		notWant  string
	}{
		{
			name:   "success",
			err:    nil,
			output: "Successfully installed",
			want:   "installed successfully",
		},
		{
			name:   "winget_error",
			err:    errors.New("package not found (0x8a150011)"),
			output: "",
			want:   "package not found",
		},
		{
			name:   "generic_error",
			err:    errors.New("some error"),
			output: "",
			want:   "some error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newInstallScreen()
			s.state = installDone
			s.err = tt.err
			s.output = tt.output
			s.packages = []Package{{Name: "App", ID: "App.ID"}}
			
			got := s.view(100, 20)
			if !strings.Contains(got, tt.want) {
				t.Errorf("view() = %q, want %q", got, tt.want)
			}
			if tt.notWant != "" && strings.Contains(got, tt.notWant) {
				t.Errorf("view() = %q, did not want %q", got, tt.notWant)
			}
		})
	}
}

func TestFormatUpgradeResults(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeDone
	s.batchTotal = 1
	s.batchName = "Edge"
	s.batchIDs = []string{"Microsoft.Edge"}
	s.batchVersions = []string{"1.0.0"}
	s.batchErrs = []error{nil}
	s.batchOutputs = []string{"Successfully upgraded"}
	
	got := s.view(100, 20)
	if !strings.Contains(got, "upgraded successfully") {
		t.Errorf("view() = %q, want 'upgraded successfully'", got)
	}
}

func TestFormatUninstallResults(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesDone
	s.batchTotal = 1
	s.batchPackages = []Package{{Name: "Firefox", ID: "Mozilla.Firefox"}}
	s.batchErrs = []error{nil}
	s.batchOutputs = []string{"Successfully uninstalled"}
	
	got := s.view(100, 20)
	if !strings.Contains(got, "uninstalled successfully") {
		t.Errorf("view() = %q, want 'uninstalled successfully'", got)
	}
}

func TestFormatUninstallResultsPrefersDecodedError(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesDone
	s.batchTotal = 1
	s.batchPackages = []Package{{Name: "Firefox", ID: "Mozilla.Firefox"}}
	s.batchErrs = []error{errors.New("installer requires administrator privileges (try running as admin) (0x80073d28)")}
	s.batchOutputs = []string{"Uninstall failed with exit code: 0x80073d28"}
	s.err = s.batchErrs[0]
	s.output = s.batchOutputs[0]

	got := s.view(120, 24)
	if !strings.Contains(got, "installer requires administrator privileges") {
		t.Fatalf("view() = %q, want decoded friendly error", got)
	}
	if strings.Contains(got, "exit code: 0x80073d28") && !strings.Contains(got, "installer requires administrator privileges") {
		t.Fatalf("view() = %q, did not want raw exit line to override decoded error", got)
	}
}

func TestInstallDoneViewShowsCollapsedLogPreview(t *testing.T) {
	s := newInstallScreen()
	s.state = installDone
	s.packages = []Package{{Name: "Notepad++", ID: "Notepad++.Notepad++", Source: "winget"}}
	s.exec.appendSection("== Notepad++ ==")
	s.exec.appendLine("Successfully installed")
	s.exec.setDoneExpanded(false)

	got := s.view(100, 20)
	if !strings.Contains(got, "Log preview — press l to expand") {
		t.Errorf("view() = %q, want log preview hint", got)
	}
}
