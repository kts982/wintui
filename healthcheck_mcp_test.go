package main

import "testing"

// checkWingetMCP is environment-dependent (the binary may or may not be present,
// and %ProgramFiles%\WindowsApps may be unreadable), so we assert only the
// stable contract: it never panics and always returns a neutral INFO row with
// the expected check name. The runDoctorReport sum-invariant test covers its
// inclusion in the slim default.
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
