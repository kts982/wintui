package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkWingetMCP is environment-dependent, so this asserts only the stable
// contract: never panics, always a neutral INFO row with the expected name.
func TestCheckWingetMCPContract(t *testing.T) {
	c := checkWingetMCP()
	if c.Check != "winget MCP" {
		t.Errorf("Check = %q, want %q", c.Check, "winget MCP")
	}
	if c.Status != "INFO" {
		t.Errorf("Status = %q, want INFO (neutral ride-along, never PASS/WARN/FAIL)", c.Status)
	}
	if c.Details == "" {
		t.Error("Details should describe detected/not-found")
	}
}

// The winget-MCP row must be wired into the slim default check set (= the TUI
// Health tab). The doctor sum-invariant test would still pass if it were
// removed, so guard the wiring explicitly.
func TestWingetMCPInSlimDefault(t *testing.T) {
	report := runDoctorReport(false, false)
	for _, c := range report.Checks {
		if c.Check == "winget MCP" {
			if c.Status != "INFO" {
				t.Errorf("winget MCP row Status = %q, want INFO", c.Status)
			}
			return
		}
	}
	t.Error("slim default check set is missing the 'winget MCP' row")
}

// Regression: the MCP server's user-readable alias lives under
// %LOCALAPPDATA%\Microsoft\WindowsApps\Microsoft.DesktopAppInstaller_*\, NOT
// %ProgramFiles%\WindowsApps (which is access-restricted and gave a false
// "not found"). Detection must find it via the LocalAppData path.
func TestCheckWingetMCPDetectsLocalAppDataAlias(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LOCALAPPDATA", tmp)
	t.Setenv("ProgramFiles", filepath.Join(tmp, "no-programfiles")) // make the fallback miss

	dir := filepath.Join(tmp, "Microsoft", "WindowsApps", "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WindowsPackageManagerMCPServer.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	c := checkWingetMCP()
	if c.Status != "INFO" || !strings.Contains(c.Details, "detected") {
		t.Errorf("expected detected via the LocalAppData alias, got %+v", c)
	}
}

func TestCheckWingetMCPNotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LOCALAPPDATA", tmp)
	t.Setenv("ProgramFiles", tmp) // both empty -> nothing to find

	c := checkWingetMCP()
	if c.Status != "INFO" || !strings.Contains(c.Details, "not found") {
		t.Errorf("expected not found, got %+v", c)
	}
}
