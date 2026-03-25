package main

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
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
	case tea.KeyMsg:
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
	a, cmd = a.switchTab(2) // Install
	if cmd == nil {
		t.Fatal("expected init command when first switching to Install tab")
	}

	for _, r := range "firefox" {
		model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		a = model.(app)
	}

	installBefore, ok := a.activeScreen().(installScreen)
	if !ok {
		t.Fatal("active screen is not installScreen")
	}
	if got := installBefore.input.Value(); got != "firefox" {
		t.Fatalf("install input before switch = %q, want %q", got, "firefox")
	}

	a, _ = a.switchTab(1) // Installed
	a, _ = a.switchTab(2) // Install again

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
			screenUpgrade: stubScreen{},
			screenInstall: stubScreen{},
		},
	}

	var cmd tea.Cmd
	a, cmd = a.updateScreen(screenUpgrade, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd == nil {
		t.Fatal("expected command from stub screen")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("wrapped command produced %T, want tea.BatchMsg", msg)
	}

	a.activeTab = 2 // Install tab becomes active while upgrade commands continue in background

	for _, sub := range batch {
		model, _ := a.Update(sub())
		a = model.(app)
	}

	upgradeScreen, ok := a.screens[screenUpgrade].(stubScreen)
	if !ok {
		t.Fatal("upgrade screen missing or wrong type")
	}
	if got, want := upgradeScreen.log, []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade screen log = %#v, want %#v", got, want)
	}

	installScreen, ok := a.screens[screenInstall].(stubScreen)
	if !ok {
		t.Fatal("install screen missing or wrong type")
	}
	if len(installScreen.log) != 0 {
		t.Fatalf("install screen unexpectedly received background updates: %#v", installScreen.log)
	}
}
