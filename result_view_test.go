package main

import (
	"errors"
	"strings"
	"testing"
)

func TestUpgradeDoneViewShowsSummaryAndHint(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeDone
	s.batchTotal = 2
	s.batchIDs = []string{"Git.Git", "Mozilla.Firefox"}
	s.batchErrs = []error{nil, errors.New("boom")}
	s.batchOutputs = []string{"", "installer failed with exit code 1"}

	got := s.view(120, 24)
	if !strings.Contains(got, "Upgrade finished: 1 succeeded, 1 failed") {
		t.Fatalf("view() = %q, want upgrade result summary", got)
	}
	if !strings.Contains(got, "Press r to rescan or tab to switch screens") {
		t.Fatalf("view() = %q, want next-step hint", got)
	}
}

func TestCleanupDoneViewShowsNextStepHint(t *testing.T) {
	s := newCleanupScreen()
	s.state = cleanupDone
	s.deleted = 3
	s.failed = 1

	got := s.view(120, 24)
	if !strings.Contains(got, "Deleted 3 item(s).") {
		t.Fatalf("view() = %q, want cleanup summary", got)
	}
	if !strings.Contains(got, "Press r to scan again or tab to switch screens") {
		t.Fatalf("view() = %q, want next-step hint", got)
	}
}

func TestImportDoneViewShowsReturnHint(t *testing.T) {
	m := newImportModel()
	m.state = importDone
	m.batchTotal = 2
	m.batchIDs = []string{"Git.Git", "Mozilla.Firefox"}
	m.batchErrs = []error{nil, nil}

	got := m.view(120, 24)
	if !strings.Contains(got, "2 package(s) installed from this export.") {
		t.Fatalf("view() = %q, want import summary", got)
	}
	if !strings.Contains(got, "Press enter or esc to return to Installed packages") {
		t.Fatalf("view() = %q, want return hint", got)
	}
}

func TestHealthReadyViewShowsRecommendationAndRerunHints(t *testing.T) {
	s := newHealthcheckScreen()
	s.state = hcReady
	s.report = healthReport{
		Hostname:      "devbox",
		OS:            "Windows 11",
		OverallStatus: "WARN",
		Sections: []healthSection{
			{
				Title: "Essentials",
				Checks: []healthCheck{
					{
						Check:          "winget",
						Status:         "WARN",
						Details:        "missing",
						Recommendation: "Install App Installer from Microsoft Store.",
					},
				},
			},
		},
	}
	s.report.Counts.Warn = 1
	s.report.Counts.Total = 1

	got := s.view(120, 24)
	if !strings.Contains(got, "1 recommendation(s) listed at the end of the report.") {
		t.Fatalf("view() = %q, want recommendation hint", got)
	}
	if !strings.Contains(got, "Press r to rerun checks or tab to switch screens") {
		t.Fatalf("view() = %q, want rerun hint", got)
	}
}
