package main

import (
	"reflect"
	"testing"

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

func keyMsg(keyName string) tea.KeyPressMsg {
	switch keyName {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
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
		keyUp.Help(),
		keyDown.Help(),
		keyOpen.Help(),
		keyEsc.Help(),
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

func TestPackagesReadyEscBacksToUpgrade(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesReady

	next, cmd := s.update(keyMsg("esc"))
	if cmd == nil {
		t.Fatal("expected back command")
	}

	got := next.(packagesScreen)
	if got.state != packagesReady {
		t.Fatalf("state = %v, want %v", got.state, packagesReady)
	}

	msg := cmd()
	if back, ok := msg.(switchScreenMsg); !ok || back != switchScreenMsg(screenUpgrade) {
		t.Fatalf("back cmd = %#v, want switchScreenMsg(screenUpgrade)", msg)
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
		keyEsc.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup help = %#v, want %#v", got, want)
	}
}

func TestHealthcheckReadyHelpIncludesRefreshAndBack(t *testing.T) {
	s := newHealthcheckScreen()
	s.state = hcReady

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyRefresh.Help(),
		keyEsc.Help(),
		keyTabs.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("health help = %#v, want %#v", got, want)
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
		keyUp.Help(),
		keyDown.Help(),
		keyFilterEdit.Help(),
		keyToggle.Help(),
		keyDetails.Help(),
		keyExport.Help(),
		keyImport.Help(),
		keyRefresh.Help(),
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
		keyUp.Help(),
		keyDown.Help(),
		keyFilterEdit.Help(),
		keyExport.Help(),
		keyImport.Help(),
		keyRefresh.Help(),
		keyEscClear.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packages empty filtered help = %#v, want %#v", got, want)
	}
}

func TestUpgradeFilteredHelpMatchesAvailableActions(t *testing.T) {
	s := newUpgradeScreen()
	s.state = upgradeSelecting
	s.packages = []Package{{Name: "GitHub CLI", ID: "GitHub.cli", Version: "1.0", Available: "1.1", Source: "winget"}}
	s.filter.query = "git"

	got := bindingHelps(s.helpKeys())
	want := []key.Help{
		keyUp.Help(),
		keyDown.Help(),
		keyFilterEdit.Help(),
		keyToggleAll.Help(),
		keyToggle.Help(),
		keyDetails.Help(),
		keyUpgradeAll.Help(),
		keyRefresh.Help(),
		keyEscClear.Help(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade filtered help = %#v, want %#v", got, want)
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
