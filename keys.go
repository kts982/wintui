package main

import "github.com/charmbracelet/bubbles/key"

// ── Shared key bindings ────────────────────────────────────────────

var (
	keyUp = key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	)
	keyDown = key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	)
	keyEnter = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	)
	keyEsc = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	)
	keyEscCancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	)
	keyRefresh = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	)
	keyFilter = key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	)
	keyDetails = key.NewBinding(
		key.WithKeys("i", "d"),
		key.WithHelp("i", "details"),
	)
	keyToggle = key.NewBinding(
		key.WithKeys(" ", "x"),
		key.WithHelp("space", "toggle"),
	)
	keyToggleAll = key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "all"),
	)
	keyConfirmY = key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y/n", "confirm"),
	)
	keyExport = key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "export"),
	)
	keySave = key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "save"),
	)
	keyReset = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "reset defaults"),
	)
	keyCycle = key.NewBinding(
		key.WithKeys("left", "right", "enter"),
		key.WithHelp("←→", "cycle"),
	)
	keyTabs = key.NewBinding(
		key.WithKeys("1", "2", "3", "4", "5", "6"),
		key.WithHelp("1-6", "tabs"),
	)
	keyScroll = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "scroll"),
	)
	keySearch = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "search"),
	)
)
