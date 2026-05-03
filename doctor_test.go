package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildDoctorOutputVerdicts(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		wantVerd string
		wantSum  string
		wantExit int
	}{
		{"all pass", []string{"PASS", "PASS"}, "OK", "OK", 0},
		{"info does not affect verdict", []string{"PASS", "INFO", "INFO"}, "OK", "OK", 0},
		{"single warn", []string{"PASS", "WARN"}, "WARN", "WARN: 1 issue", 1},
		{"two warns", []string{"WARN", "PASS", "WARN"}, "WARN", "WARN: 2 issues", 1},
		{"single fail", []string{"FAIL", "PASS"}, "FAIL", "FAIL: 1 issue", 2},
		{"fail dominates warn", []string{"FAIL", "WARN", "WARN"}, "FAIL", "FAIL: 1 issue", 2},
		{"three fails", []string{"FAIL", "FAIL", "FAIL"}, "FAIL", "FAIL: 3 issues", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := healthReport{}
			for i, s := range tt.statuses {
				report.Checks = append(report.Checks, healthCheck{
					Check:   "row" + string(rune('A'+i)),
					Status:  s,
					Details: "x",
				})
			}
			got := buildDoctorOutput(report)
			if got.Verdict != tt.wantVerd {
				t.Errorf("Verdict = %q, want %q", got.Verdict, tt.wantVerd)
			}
			if got.Summary != tt.wantSum {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSum)
			}
			if got.ExitCode != tt.wantExit {
				t.Errorf("ExitCode = %d, want %d", got.ExitCode, tt.wantExit)
			}
		})
	}
}

func TestRunDoctorPlainTextWritesVerdictAndSetsExitCode(t *testing.T) {
	origSettings := appSettings
	origExit := cliExitCode
	t.Cleanup(func() {
		appSettings = origSettings
		cliExitCode = origExit
	})
	appSettings = DefaultSettings()
	cliExitCode = 0

	var buf bytes.Buffer
	if err := runDoctor(doctorOptions{}, &buf); err != nil {
		t.Fatalf("runDoctor err = %v", err)
	}

	first := strings.SplitN(buf.String(), "\n", 2)[0]
	if first != "OK" && !strings.HasPrefix(first, "WARN:") && !strings.HasPrefix(first, "FAIL:") {
		t.Fatalf("first line %q is not a recognized verdict", first)
	}
	if (first == "OK" && cliExitCode != 0) ||
		(strings.HasPrefix(first, "WARN:") && cliExitCode != 1) ||
		(strings.HasPrefix(first, "FAIL:") && cliExitCode != 2) {
		t.Fatalf("verdict %q does not match exit code %d", first, cliExitCode)
	}
}

func TestRunDoctorJSONIncludesVerdictAndCounts(t *testing.T) {
	origSettings := appSettings
	origExit := cliExitCode
	t.Cleanup(func() {
		appSettings = origSettings
		cliExitCode = origExit
	})
	appSettings = DefaultSettings()
	cliExitCode = 0

	var buf bytes.Buffer
	if err := runDoctor(doctorOptions{JSON: true}, &buf); err != nil {
		t.Fatalf("runDoctor err = %v", err)
	}

	var got doctorOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, buf.String())
	}
	switch got.Verdict {
	case "OK", "WARN", "FAIL":
	default:
		t.Fatalf("Verdict = %q, want one of OK/WARN/FAIL", got.Verdict)
	}
	if got.ExitCode != cliExitCode {
		t.Errorf("JSON exit_code (%d) does not match cliExitCode (%d)", got.ExitCode, cliExitCode)
	}
	if len(got.Checks) == 0 {
		t.Error("JSON checks slice is empty; expected slim default rows")
	}

	want := got.Counts.Pass + got.Counts.Warn + got.Counts.Fail + got.Counts.Info
	if want != len(got.Checks) {
		t.Errorf("counts (%d) do not sum to checks (%d)", want, len(got.Checks))
	}
}

func TestRunDoctorVerboseAppendsTable(t *testing.T) {
	origSettings := appSettings
	origExit := cliExitCode
	t.Cleanup(func() {
		appSettings = origSettings
		cliExitCode = origExit
	})
	appSettings = DefaultSettings()
	cliExitCode = 0

	var plain, verbose bytes.Buffer
	_ = runDoctor(doctorOptions{}, &plain)
	_ = runDoctor(doctorOptions{Verbose: true}, &verbose)

	if !strings.Contains(verbose.String(), "STATUS") || !strings.Contains(verbose.String(), "WinTUI") {
		t.Fatalf("verbose output missing table header or WinTUI row:\n%s", verbose.String())
	}
	if len(verbose.String()) <= len(plain.String()) {
		t.Fatalf("verbose output should be longer than plain; got %d <= %d", len(verbose.String()), len(plain.String()))
	}
}

func TestPluralIssues(t *testing.T) {
	if got := pluralIssues(1); got != "1 issue" {
		t.Errorf("pluralIssues(1) = %q, want %q", got, "1 issue")
	}
	if got := pluralIssues(2); got != "2 issues" {
		t.Errorf("pluralIssues(2) = %q, want %q", got, "2 issues")
	}
	if got := pluralIssues(0); got != "0 issues" {
		t.Errorf("pluralIssues(0) = %q, want %q", got, "0 issues")
	}
}
