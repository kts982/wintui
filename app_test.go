package main

import (
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

func TestBackgroundScreenCommandsStayOwnedByOriginatingScreen(t *testing.T) {
	a := app{
		activeTab: 0,
		screens: map[screenID]screen{
			screenWorkspace: stubScreen{},
			screenCleanup:   stubScreen{},
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

	a.activeTab = 1 // Cleanup tab active while workspace commands continue

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

	cleanScreen, ok := a.screens[screenCleanup].(stubScreen)
	if !ok {
		t.Fatal("cleanup screen missing or wrong type")
	}
	if len(cleanScreen.log) != 0 {
		t.Fatalf("cleanup screen unexpectedly received background updates: %#v", cleanScreen.log)
	}
}

func TestPackageDataChangedTriggersRefresh(t *testing.T) {
	a := app{
		activeTab: 1, // Cleanup
		screens: map[screenID]screen{
			screenWorkspace: stubScreen{},
			screenCleanup:   stubScreen{},
		},
	}

	_, cmd := a.Update(packageDataChangedMsg{origin: screenCleanup})
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	if _, ok := a.screens[screenWorkspace].(workspaceScreen); !ok {
		t.Fatalf("workspace screen was not recreated: %T", a.screens[screenWorkspace])
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

func TestConfirmModalBlocksGlobalTabSwitchShortcuts(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.state = workspaceConfirm
	ws.modal = &execModal{phase: execPhaseReview}
	a := app{
		activeTab: 0,
		screens: map[screenID]screen{
			screenWorkspace: ws,
		},
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	got := model.(app)

	if got.activeTab != 0 {
		t.Fatalf("activeTab = %d, want 0 (modal should block)", got.activeTab)
	}
}

func TestDetailOverlayBlocksGlobalTabSwitchShortcuts(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.detail = detailPanel{state: detailReady}
	a := app{
		activeTab: 0,
		screens: map[screenID]screen{
			screenWorkspace: ws,
		},
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	got := model.(app)

	if got.activeTab != 0 {
		t.Fatalf("activeTab = %d, want 0 (detail should block)", got.activeTab)
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
}
