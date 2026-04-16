package main

import (
	"os"
	"regexp"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// TestMain backs up the user's real settings and cache files before any tests
// run, and restores them afterwards. This prevents tests that call
// persistSettings or cache.saveToDiskLocked from overwriting real user data.
func TestMain(m *testing.M) {
	settingsPath := configPath()
	cachePath := diskCachePath()

	settingsBackup, settingsErr := os.ReadFile(settingsPath)
	cacheBackup, cacheErr := os.ReadFile(cachePath)

	code := m.Run()

	if settingsErr == nil {
		os.WriteFile(settingsPath, settingsBackup, 0644)
	}
	if cacheErr == nil {
		os.WriteFile(cachePath, cacheBackup, 0644)
	}
	os.Exit(code)
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
	case "ctrl+,":
		return tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl}
	case "ctrl+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}
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
