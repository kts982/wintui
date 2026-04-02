package main

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func bindingHelps(bindings []key.Binding) []key.Help {
	helps := make([]key.Help, len(bindings))
	for i, binding := range bindings {
		helps[i] = binding.Help()
	}
	return helps
}

func shortHelp(width int, bindings []key.Binding) string {
	h := help.New()
	h.SetWidth(width)
	return h.ShortHelpView(bindings)
}

func keyMsg(keyName string) tea.KeyPressMsg {
	switch keyName {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+,":
		return tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl}
	default:
		r := []rune(keyName)[0]
		return tea.KeyPressMsg{Code: r, Text: string(r)}
	}
}

func TestInstallDetailHelpOverridesScreenHelp(t *testing.T) {
	s := newInstallScreen()
	s.detail.state = detailReady

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyOpen.Help(),
		keyEscOrLeft.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install detail help = %#v, want %#v", got, want)
	}
}

func TestInstallSearchingEscCancelsBackToInput(t *testing.T) {
	s := newInstallScreen()
	s.state = installSearching
	called := false
	s.cancel = func() { called = true }

	next, cmd := s.update(keyMsg("esc"))
	if cmd == nil {
		t.Fatal("expected blink command when returning to input")
	}

	got := next.(installScreen)
	if !called {
		t.Fatal("expected cancel to be invoked")
	}
	if got.state != installInput {
		t.Fatalf("state = %v, want %v", got.state, installInput)
	}
}

func TestInstallDoneHelpUsesSearchAgain(t *testing.T) {
	s := newInstallScreen()
	s.state = installDone

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keySearchAgain.Help(),
		keyEsc.Help(),
		keyTabs.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install done help = %#v, want %#v", got, want)
	}
}

func TestInstallConfirmHelpUsesModalBindings(t *testing.T) {
	s := newInstallScreen()
	s.state = installConfirm

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyConfirm.Help(),
		keyCancel.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install confirm help = %#v, want %#v", got, want)
	}
}

func TestInstallInputEscClearsQueryLocally(t *testing.T) {
	s := newInstallScreen()
	s.state = installInput
	s.input.SetValue("firefox")

	next, cmd := s.update(keyMsg("esc"))
	if cmd == nil {
		t.Fatal("expected blink command after clearing input")
	}

	got := next.(installScreen)
	if got.input.Value() != "" {
		t.Fatalf("input = %q, want empty", got.input.Value())
	}
}

func TestPackagesReadyEscClearsSelectionLocally(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesReady
	s.packages = []Package{{Name: "Git", ID: "Git.Git", Version: "1.0", Source: "winget"}}
	s.selected["Git.Git"] = true
	s.rebuildTable()

	next, cmd := s.update(keyMsg("esc"))
	if cmd != nil {
		t.Fatalf("expected no tab switch command, got %#v", cmd())
	}

	got := next.(packagesScreen)
	if got.selectedCount() != 0 {
		t.Fatalf("selectedCount() = %d, want 0", got.selectedCount())
	}
}

func TestCleanupEmptyRefreshStartsReload(t *testing.T) {
	s := newCleanupScreen()
	s.state = cleanupEmpty

	next, cmd := s.update(keyMsg("r"))
	if cmd == nil {
		t.Fatal("expected reload command")
	}

	got := next.(cleanupScreen)
	if got.state != cleanupLoading {
		t.Fatalf("state = %v, want %v", got.state, cleanupLoading)
	}
}

func TestCleanupReadyHelpMatchesBulkAction(t *testing.T) {
	s := newCleanupScreen()
	s.state = cleanupReady

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyUp.Help(),
		keyDown.Help(),
		keyCleanAll.Help(),
		keyRefresh.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup help = %#v, want %#v", got, want)
	}
}

func TestHealthcheckReadyHelpIncludesRefreshAndTabs(t *testing.T) {
	s := newHealthcheckScreen()
	s.state = hcReady

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyRefresh.Help(),
		keyTabs.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("health help = %#v, want %#v", got, want)
	}
}

func TestHealthcheckPgDownAdvancesScroll(t *testing.T) {
	s := newHealthcheckScreen()
	s.state = hcReady
	s.width = 120
	s.height = 34
	s.report = healthReport{
		Sections: []healthSection{
			{Title: "One", Checks: []healthCheck{{Check: "A", Status: "PASS", Details: "ok"}}},
		},
	}

	next, _ := s.update(keyMsg("pgdown"))
	got := next.(healthcheckScreen)

	if got.scroll != 0 {
		t.Fatalf("scroll = %d, want 0 for short report", got.scroll)
	}
}

func TestHealthcheckScrollStopsAtBottom(t *testing.T) {
	s := newHealthcheckScreen()
	s.state = hcReady
	s.width = 120
	s.height = 34
	checks := make([]healthCheck, 0, 40)
	for i := 0; i < 40; i++ {
		checks = append(checks, healthCheck{
			Check:   "Check",
			Status:  "PASS",
			Details: "ok",
		})
	}
	s.report = healthReport{
		Sections: []healthSection{
			{Title: "Many", Checks: checks},
		},
	}

	for i := 0; i < 50; i++ {
		next, _ := s.update(keyMsg("down"))
		s = next.(healthcheckScreen)
	}

	maxScroll := s.maxScroll()
	if s.scroll != maxScroll {
		t.Fatalf("scroll = %d, want clamped maxScroll %d", s.scroll, maxScroll)
	}

	next, _ := s.update(keyMsg("up"))
	got := next.(healthcheckScreen)
	if got.scroll != max(0, maxScroll-1) {
		t.Fatalf("scroll after up = %d, want %d", got.scroll, max(0, maxScroll-1))
	}
}

func TestHealthcheckEscResetsScrollLocally(t *testing.T) {
	s := newHealthcheckScreen()
	s.state = hcReady
	s.scroll = 12

	next, cmd := s.update(keyMsg("esc"))
	if cmd != nil {
		t.Fatalf("expected no tab switch command, got %#v", cmd())
	}

	got := next.(healthcheckScreen)
	if got.scroll != 0 {
		t.Fatalf("scroll = %d, want 0", got.scroll)
	}
}

func TestPackagesFilteredHelpShowsClearInsteadOfBack(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesReady
	s.packages = []Package{{Name: "Git", ID: "Git.Git", Version: "1.0", Source: "winget"}}
	s.filter.query = "git"
	s.rebuildTable()

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyFilterEdit.Help(),
		keyToggle.Help(),
		keyDetails.Help(),
		keyEscClear.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages filtered help = %#v, want %#v", got, want)
	}
}

func TestPackagesFilteredEmptyHelpHidesRowActions(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesReady
	s.packages = []Package{{Name: "Git", ID: "Git.Git", Version: "1.0", Source: "winget"}}
	s.filter.query = "zzz"
	s.rebuildTable()

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyFilterEdit.Help(),
		keyEscClear.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages empty filtered help = %#v, want %#v", got, want)
	}
}

func TestPackagesDoneHelpIncludesRefreshAndTabs(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesDone

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyRefresh.Help(),
		keyTabs.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages done help = %#v, want %#v", got, want)
	}
}

func TestPackagesConfirmHelpUsesModalBindings(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesConfirmUninstall

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyConfirm.Help(),
		keyCancel.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages confirm help = %#v, want %#v", got, want)
	}
}

func TestUpgradeFilteredHelpMatchesAvailableActions(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeSelecting
	s.packages = []Package{{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"}}
	s.filter.query = "git"

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyFilterEdit.Help(),
		keyToggle.Help(),
		keyDetails.Help(),
		keyUpgradeAll.Help(),
		keyEscClear.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade filtered help = %#v, want %#v", got, want)
	}
}

func TestPackagesCompactHelpAvoidsEllipsis(t *testing.T) {
	s := newPackagesScreen()
	s.width = 120
	s.state = packagesReady
	s.packages = []Package{{Name: "Git", ID: "Git.Git", Version: "1.0", Source: "winget"}}
	s.filter.query = "git"
	s.rebuildTable()

	got := shortHelp(116, s.helpKeys())
	if strings.Contains(got, "…") {
		t.Fatalf("short help = %q, did not expect truncation", got)
	}
}

func TestUpgradeCompactHelpAvoidsEllipsis(t *testing.T) {
	s := newUpgradeScreen()
	s.width = 120
	s.state = upgradeSelecting
	s.packages = []Package{{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"}}
	s.filter.query = "git"

	got := shortHelp(116, s.helpKeys())
	if strings.Contains(got, "…") {
		t.Fatalf("short help = %q, did not expect truncation", got)
	}
}

func TestImportReviewSkippedOnlyHelpShowsRevealAction(t *testing.T) {
	m := newImportModel()
	m.state = importReview
	m.packages = []importPkg{
		{Name: "Git", ID: "Git.Git", Installed: true},
		{Name: "Raw App", ID: "MSIX\\Raw.App_hash123", NonCanonical: true},
	}

	got := bindingHelps(m.helpKeys())
	want := []key.Help{
		keyShowSkipped.Help(),
		keyEsc.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("import review help = %#v, want %#v", got, want)
	}
}

func TestImportReviewShowAllHelpUsesFocusInstallable(t *testing.T) {
	m := newImportModel()
	m.state = importReview
	m.showAll = true
	m.packages = []importPkg{
		{Name: "Firefox", ID: "Mozilla.Firefox"},
		{Name: "Git", ID: "Git.Git", Installed: true},
	}
	m.selected[0] = true

	got := bindingHelps(m.helpKeys())
	want := []key.Help{
		keyUp.Help(),
		keyDown.Help(),
		keyToggle.Help(),
		keyToggleAll.Help(),
		keyFocusInstallable.Help(),
		keyInstallSelected.Help(),
		keyEsc.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("import review show-all help = %#v, want %#v", got, want)
	}
}

func TestSettingsHelpUsesTabsInsteadOfEsc(t *testing.T) {
	s := newSettingsScreen()

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyUp.Help(),
		keyDown.Help(),
		keyCycle.Help(),
		keySave.Help(),
		keyReset.Help(),
		keyTabs.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settings help = %#v, want %#v", got, want)
	}
}

func TestUpgradePackagesLoadIntoSelectingState(t *testing.T) {
	s := newUpgradeScreen()

	next, _ := s.update(packagesLoadedMsg{
		packages: []Package{{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"}},
	})

	got := next.(upgradeScreen)
	if got.state != upgradeSelecting {
		t.Fatalf("state = %v, want %v", got.state, upgradeSelecting)
	}
}

func TestUpgradeSelectAllUsesFilteredPackages(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeSelecting
	s.packages = []Package{
		{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"},
		{Name: "Microsoft Edge", ID: "Microsoft.Edge", Version: "1.0", Available: "1.1", Source: "winget"},
	}
	s.filter.query = "hub"

	next, _ := s.update(keyMsg("a"))
	got := next.(upgradeScreen)

	if !got.selected[0] {
		t.Fatal("expected visible filtered package to be selected")
	}
	if got.selected[1] {
		t.Fatal("did not expect hidden package to be selected")
	}
}

func TestUpgradeEnterConfirmsSelectedPackages(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeSelecting
	s.packages = []Package{{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"}}
	s.selected[0] = true

	next, _ := s.update(keyMsg("enter"))
	got := next.(upgradeScreen)

	if got.state != upgradeConfirming {
		t.Fatalf("state = %v, want %v", got.state, upgradeConfirming)
	}
	if got.action != "selected" {
		t.Fatalf("action = %q, want %q", got.action, "selected")
	}
}

func TestUpgradeAllShortcutEntersConfirmation(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeSelecting
	s.packages = []Package{{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"}}

	next, _ := s.update(keyMsg("u"))
	got := next.(upgradeScreen)

	if got.state != upgradeConfirming {
		t.Fatalf("state = %v, want %v", got.state, upgradeConfirming)
	}
	if got.action != "all" {
		t.Fatalf("action = %q, want %q", got.action, "all")
	}
}

func TestUpgradeConfirmHelpUsesModalBindings(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeConfirming

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyConfirm.Help(),
		keyCancel.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade confirm help = %#v, want %#v", got, want)
	}
}
