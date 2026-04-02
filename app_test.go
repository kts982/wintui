package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type stubStateMsg string

type stubScreen struct {
	log []string
}

func (s stubScreen) init() tea.Cmd { return nil }

func (s stubScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case stubStateMsg:
		s.log = append(s.log, string(msg))
		return s, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "b":
			return s, tea.Batch(
				func() tea.Msg { return stubStateMsg("first") },
				func() tea.Msg { return stubStateMsg("second") },
			)
		}
	}
	return s, nil
}

func (s stubScreen) view(width, height int) string { return "" }

func (s stubScreen) helpKeys() []key.Binding { return nil }

func TestInstallScreenStatePreservedAcrossTabSwitches(t *testing.T) {
	a := newApp(nil)

	var cmd tea.Cmd
	a, cmd = a.switchTab(1) // Install
	if cmd == nil {
		t.Fatal("expected init command when first switching to Install tab")
	}

	for _, r := range "firefox" {
		model, _ := a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		a = model.(app)
	}

	installBefore, ok := a.activeScreen().(installScreen)
	if !ok {
		t.Fatal("active screen is not installScreen")
	}
	if got := installBefore.input.Value(); got != "firefox" {
		t.Fatalf("install input before switch = %q, want %q", got, "firefox")
	}

	a, _ = a.switchTab(0) // Packages
	a, _ = a.switchTab(1) // Install again

	installAfter, ok := a.activeScreen().(installScreen)
	if !ok {
		t.Fatal("active screen after switch is not installScreen")
	}
	if got := installAfter.input.Value(); got != "firefox" {
		t.Fatalf("install input after switch = %q, want %q", got, "firefox")
	}
}

func TestBackgroundScreenCommandsStayOwnedByOriginatingScreen(t *testing.T) {
	a := app{
		activeTab: 0,
		screens: map[screenID]screen{
			screenWorkspace: stubScreen{},
			screenInstall:   stubScreen{},
		},
	}

	var cmd tea.Cmd
	a, cmd = a.updateScreen(screenWorkspace, tea.KeyPressMsg{Code: 'b'})
	if cmd == nil {
		t.Fatal("expected command from stub screen")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("wrapped command produced %T, want tea.BatchMsg", msg)
	}

	a.activeTab = 1 // Install tab becomes active while workspace commands continue in background

	for _, sub := range batch {
		model, _ := a.Update(sub())
		a = model.(app)
	}

	wsScreen, ok := a.screens[screenWorkspace].(stubScreen)
	if !ok {
		t.Fatal("workspace screen missing or wrong type")
	}
	if got, want := wsScreen.log, []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace screen log = %#v, want %#v", got, want)
	}

	instScreen, ok := a.screens[screenInstall].(stubScreen)
	if !ok {
		t.Fatal("install screen missing or wrong type")
	}
	if len(instScreen.log) != 0 {
		t.Fatalf("install screen unexpectedly received background updates: %#v", instScreen.log)
	}
}

func TestPackageDataChangedReloadsInactiveDataScreens(t *testing.T) {
	a := app{
		activeTab: 1, // Install
		screens: map[screenID]screen{
			screenWorkspace: stubScreen{},
			screenInstall:   stubScreen{},
		},
	}

	_, cmd := a.Update(packageDataChangedMsg{origin: screenInstall})
	if cmd == nil {
		t.Fatal("expected reload command when affected tabs are inactive")
	}
	if _, ok := a.screens[screenWorkspace].(workspaceScreen); !ok {
		t.Fatalf("inactive workspace screen was not recreated: %T", a.screens[screenWorkspace])
	}
}

func TestPackageDataChangedUsesSequentialRefresh(t *testing.T) {
	a := app{
		activeTab: 1, // Install
		screens: map[screenID]screen{
			screenWorkspace: stubScreen{},
			screenInstall:   stubScreen{},
		},
	}

	_, cmd := a.Update(packageDataChangedMsg{origin: screenInstall})
	if cmd == nil {
		t.Fatal("expected refresh command")
	}

	msg := cmd()
	if got := fmt.Sprintf("%T", msg); got != "tea.sequenceMsg" {
		t.Fatalf("refresh command type = %s, want tea.sequenceMsg", got)
	}
}

func TestRenderLogoUsesCompactHeaderOnShortTerminals(t *testing.T) {
	a := newApp(nil)
	a.width = 100
	a.height = 24

	got := a.renderLogo()
	if !strings.Contains(got, "WinTUI") {
		t.Fatalf("renderLogo() = %q, want compact title", got)
	}
	if strings.Contains(got, asciiLogo[0]) {
		t.Fatalf("renderLogo() = %q, did not expect full ASCII logo in compact mode", got)
	}
}

func TestRenderLogoUsesFullAsciiLogoOnLargeTerminals(t *testing.T) {
	a := newApp(nil)
	a.width = 140
	a.height = 40

	got := a.renderLogo()
	if !strings.Contains(got, asciiLogo[0]) {
		t.Fatalf("renderLogo() = %q, want full ASCII logo", got)
	}
}

func TestSwitchTabAppliesCurrentWindowSizeToNewInstallScreen(t *testing.T) {
	a := newApp(nil)
	a.width = 140
	a.height = 40

	var cmd tea.Cmd
	a, cmd = a.switchTab(1) // Install
	if cmd == nil {
		t.Fatal("expected init command when first switching to Install tab")
	}

	_, ok := a.activeScreen().(installScreen)
	if !ok {
		t.Fatalf("active screen = %T, want installScreen", a.activeScreen())
	}
}

func TestConfirmModalBlocksGlobalTabSwitchShortcuts(t *testing.T) {
	a := app{
		activeTab: 1, // Install
		screens: map[screenID]screen{
			screenInstall: installScreen{state: installConfirm},
		},
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	got := model.(app)

	if got.activeTab != 1 {
		t.Fatalf("activeTab = %d, want %d", got.activeTab, 1)
	}
}

func TestDetailOverlayBlocksGlobalTabSwitchShortcuts(t *testing.T) {
	a := app{
		activeTab: 1, // Install
		screens: map[screenID]screen{
			screenInstall: installScreen{
				detail: detailPanel{state: detailReady},
			},
		},
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	got := model.(app)

	if got.activeTab != 1 {
		t.Fatalf("activeTab = %d, want %d", got.activeTab, 1)
	}
}

func TestStartRetryMsgTriggersActionImmediately(t *testing.T) {
	req := retryRequest{
		Op: retryOpInstall,
		ID: "Test.App",
	}
	a := newApp(&req)

	cmd := a.Init()
	if cmd == nil {
		t.Fatalf("Expected Init to return a batch command, got nil")
	}

	msg := startRetryMsg{req: req}
	s := newInstallScreen()
	newS, _ := s.update(msg)

	if newS.(installScreen).state != installExecuting {
		t.Fatalf("Expected state to be installExecuting, got %v", newS.(installScreen).state)
	}
}
