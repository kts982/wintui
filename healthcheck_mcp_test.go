package main

import "testing"

// checkWingetMCP is environment-dependent (the binary may or may not be present,
// and %ProgramFiles%\WindowsApps may be unreadable), so we assert only the
// stable contract: it never panics and always returns a neutral INFO row with
// the expected check name.
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

// The winget-MCP row must actually be wired into the slim default check set (=
// the TUI Health tab). The doctor sum-invariant test would still pass if the row
// were removed, so guard the wiring explicitly.
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
