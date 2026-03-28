package main

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestUpgradeDoneViewShowsSummaryAndHint(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeDone
	s.batchTotal = 2
	s.batchIDs = []string{"Git.Git", "Mozilla.Firefox"}
	s.batchErrs = []error{nil, errors.New("boom")}
	s.batchOutputs = []string{"", "installer failed with exit code 1"}
	s.exec.appendSection("== Mozilla.Firefox ==")
	s.exec.appendLine("Installer failed with exit code: 1603")
	s.exec.setDoneExpanded(true)

	got := s.view(120, 24)
	if !strings.Contains(got, "Upgrade finished: 1 succeeded, 1 failed") {
		t.Fatalf("view() = %q, want upgrade result summary", got)
	}
	if !strings.Contains(got, "Execution log — press l to hide") {
		t.Fatalf("view() = %q, want expandable execution log", got)
	}
	if !strings.Contains(got, "Press r to rescan or tab to switch screens") {
		t.Fatalf("view() = %q, want next-step hint", got)
	}
}

func TestUpgradeExecutingViewShowsLogViewport(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeExecuting
	s.batchTotal = 2
	s.batchCurrent = 0
	s.batchName = "Mozilla.Firefox"
	s.exec.appendSection("== Mozilla Firefox ==")
	s.exec.appendLine("Downloading installer")

	got := s.view(120, 24)
	if !strings.Contains(got, "Upgrading 1 of 2: Mozilla.Firefox") {
		t.Fatalf("view() = %q, want active upgrade progress header", got)
	}
	if !strings.Contains(got, "Downloading installer") {
		t.Fatalf("view() = %q, want streamed log content", got)
	}
	if !strings.Contains(got, "┌") {
		t.Fatalf("view() = %q, want bordered log viewport", got)
	}
}

func TestUpgradeConfirmViewShowsSelectedTargetVersion(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeConfirming
	s.action = "selected"
	s.packages = []Package{
		{Name: "Neovim", ID: "Neovim.Neovim", Version: "0.11.5", Available: "0.11.6", Source: "winget"},
	}
	s.selected[0] = true
	s.selectedVersions[packageSourceKey("Neovim.Neovim", "winget")] = "0.11.4"

	got := s.view(120, 24)
	if !strings.Contains(got, "Neovim.Neovim") ||
		!strings.Contains(got, "Target version:") ||
		!strings.Contains(got, "0.11.4") {
		t.Fatalf("view() = %q, want explicit target version in confirm text", got)
	}
}

func TestUpgradeConfirmViewUsesModalLayout(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeConfirming
	s.action = "selected"
	s.packages = []Package{
		{Name: "Neovim", ID: "Neovim.Neovim", Version: "0.11.5", Available: "0.11.6", Source: "winget"},
	}
	s.selected[0] = true

	got := stripANSI(s.view(120, 24))
	if !strings.Contains(got, "╭") ||
		!strings.Contains(got, "Upgrade Package") ||
		!strings.Contains(got, "enter upgrade") {
		t.Fatalf("view() = %q, want centered modal card and action hint", got)
	}
}

func TestPackagesUninstallingViewShowsLogViewport(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesUninstalling
	s.batchTotal = 2
	s.batchCurrent = 0
	s.batchName = "Mozilla Firefox"
	s.exec.appendSection("== Mozilla Firefox ==")
	s.exec.appendLine("Removing package files")

	got := s.view(120, 24)
	if !strings.Contains(got, "Uninstalling 1 of 2: Mozilla Firefox") {
		t.Fatalf("view() = %q, want active uninstall progress header", got)
	}
	if !strings.Contains(got, "Removing package files") {
		t.Fatalf("view() = %q, want streamed uninstall log content", got)
	}
	if !strings.Contains(got, "┌") {
		t.Fatalf("view() = %q, want bordered log viewport", got)
	}
}

func TestPackagesDoneViewShowsSummaryAndHint(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesDone
	s.batchTotal = 2
	s.batchPackages = []Package{
		{Name: "Mozilla Firefox", ID: "Mozilla.Firefox"},
		{Name: "Neovim", ID: "Neovim.Neovim"},
	}
	s.batchErrs = []error{nil, errors.New("boom")}
	s.batchOutputs = []string{"", "installer failed with exit code 1"}
	s.output = formatUninstallResults(s.batchPackages, s.batchErrs, s.batchOutputs)
	s.exec.appendSection("== Neovim ==")
	s.exec.appendLine("Installer failed with exit code: 1603")
	s.exec.setDoneExpanded(false)

	got := s.view(120, 24)
	if !strings.Contains(got, "Uninstall finished: 1 succeeded, 1 failed") {
		t.Fatalf("view() = %q, want uninstall result summary", got)
	}
	if !strings.Contains(got, "Log preview — press l to expand") {
		t.Fatalf("view() = %q, want collapsed log preview", got)
	}
	if !strings.Contains(got, "Press r to reload or tab to switch screens") {
		t.Fatalf("view() = %q, want next-step hint", got)
	}
}

func TestFormatUninstallResultsKeepsFriendlyDecodedError(t *testing.T) {
	pkgs := []Package{{Name: "Neovim", ID: "Neovim.Neovim"}}
	errs := []error{errors.New("installer failed with a fatal error (1603)")}
	outputs := []string{"Uninstall failed with exit code: 1603"}

	got := formatUninstallResults(pkgs, errs, outputs)
	if !strings.Contains(got, "installer failed with a fatal error (1603)") {
		t.Fatalf("formatUninstallResults() = %q, want decoded error to remain visible", got)
	}
	if strings.Contains(got, "Uninstall failed with exit code: 1603") {
		t.Fatalf("formatUninstallResults() = %q, did not want raw exit line to override decoded error", got)
	}
}

func TestPackagesDoneViewShowsSoftElevationRetryHintFor1603(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesDone
	s.batchTotal = 1
	s.batchPackages = []Package{{Name: "Neovim", ID: "Neovim.Neovim", Source: "winget"}}
	s.batchErrs = []error{errors.New("installer failed with a fatal error (1603)")}
	s.batchOutputs = []string{"Uninstall failed with exit code: 1603"}
	s.err = s.batchErrs[0]
	s.retryAction = &retryRequest{Op: retryOpUninstall, ID: "Neovim.Neovim", Name: "Neovim", Source: "winget"}

	got := s.view(120, 24)
	if !strings.Contains(got, "Retrying elevated may help remove packages blocked by permissions or services.") {
		t.Fatalf("view() = %q, want soft elevation retry hint", got)
	}
	if strings.Contains(got, "Some packages require administrator privileges.") {
		t.Fatalf("view() = %q, did not want hard admin requirement hint", got)
	}
}

func TestPackagesDoneViewOffersBatchRetryForFailedItemsOnly(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesDone
	s.batchTotal = 3
	s.batchPackages = []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"},
		{Name: "Neovim", ID: "Neovim.Neovim", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Source: "winget"},
	}
	s.batchErrs = []error{
		nil,
		errors.New("installer failed with a fatal error (1603)"),
		nil,
	}
	s.batchOutputs = []string{
		"",
		"Uninstall failed with exit code: 1603",
		"",
	}
	s.retryAction = newRetryRequest(retryOpUninstall, failedUninstallRetryItems(s.batchPackages, s.batchErrs, s.batchOutputs))

	got := s.view(120, 24)
	if !strings.Contains(got, "Press ctrl+e to retry failed items elevated") {
		t.Fatalf("view() = %q, want failed-items retry hint", got)
	}
}

func TestInstallDoneViewShowsCollapsedLogPreview(t *testing.T) {
	s := newInstallScreen()
	s.state = installDone
	s.packages = []Package{{Name: "Notepad++", ID: "Notepad++.Notepad++", Source: "winget"}}
	s.exec.appendSection("== Notepad++ ==")
	s.exec.appendLine("Successfully installed")
	s.exec.setDoneExpanded(false)

	got := s.view(120, 24)
	if !strings.Contains(got, "Notepad++ installed successfully") {
		t.Fatalf("view() = %q, want install success summary", got)
	}
	if !strings.Contains(got, "Log preview — press l to expand") {
		t.Fatalf("view() = %q, want collapsed log preview", got)
	}
}

func TestPackagesConfirmViewUsesModalLayout(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesConfirmUninstall
	s.packages = []Package{
		{Name: "Mozilla Firefox", ID: "Mozilla.Firefox"},
	}
	s.selected["Mozilla.Firefox"] = true
	s.rebuildTable()

	got := stripANSI(s.view(120, 24))
	if !strings.Contains(got, "╭") ||
		!strings.Contains(got, "Uninstall Packages?") ||
		!strings.Contains(got, "enter uninstall") {
		t.Fatalf("view() = %q, want centered modal card and action hint", got)
	}
}

func TestInstallConfirmViewUsesModalLayout(t *testing.T) {
	s := newInstallScreen()
	s.state = installConfirm
	s.packages = []Package{
		{Name: "Notepad++", ID: "Notepad++.Notepad++", Version: "8.9.3", Source: "winget"},
	}

	got := stripANSI(s.view(120, 24))
	if !strings.Contains(got, "╭") ||
		!strings.Contains(got, "Install Package?") ||
		!strings.Contains(got, "enter install") {
		t.Fatalf("view() = %q, want centered modal card and action hint", got)
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
