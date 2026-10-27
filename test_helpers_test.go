package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// TestMain points %APPDATA% at a temporary directory before any test runs, so
// settings.json, cache.json and history.json resolve there (os.UserConfigDir)
// and no test reads or writes the user's real files — not even for the length
// of a run that crashes or is interrupted. %LOCALAPPDATA% stays real: winget's
// path validation depends on it.
func TestMain(m *testing.M) {
	appData, err := os.MkdirTemp("", "wintui-test-appdata-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("APPDATA", appData); err != nil {
		panic(err)
	}

	// Action-history write-points (finishBatch, the CLI upgrade loops) go through
	// historyRecordFn. Default it to a no-op so no test writes the real
	// history.json; write-point tests override it locally to capture records.
	historyRecordFn = func(historyRecord) (string, error) { return "", nil }

	// Portable evidence reads the real %LOCALAPPDATA%; pin it to "not
	// portable" so routing tests don't depend on what this machine has
	// installed. Policy tests override it locally.
	portableInstalled = func(string) bool { return false }

	code := m.Run()

	_ = os.RemoveAll(appData)
	os.Exit(code)
}

func TestProfileFilesResolveToTheScratchAppData(t *testing.T) {
	appData := os.Getenv("APPDATA")
	if !strings.HasPrefix(filepath.Base(appData), "wintui-test-appdata-") {
		t.Fatalf("APPDATA = %q, want TestMain's scratch dir", appData)
	}
	for name, path := range map[string]string{
		"settings": configPath(),
		"cache":    diskCachePath(),
		"history":  historyPath(),
	} {
		if !strings.HasPrefix(path, appData+string(filepath.Separator)) {
			t.Errorf("%s path = %q, want it under %q", name, path, appData)
		}
	}
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

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
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "ctrl+,":
		return tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	default:
		r := []rune(keyName)[0]
		return tea.KeyPressMsg{Code: r, Text: string(r)}
	}
}

func bindingHelps(bindings []key.Binding) []key.Help {
	helps := make([]key.Help, len(bindings))
	for i, binding := range bindings {
		helps[i] = binding.Help()
	}
	return helps
}

func stripANSI(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}
