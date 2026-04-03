package main

import "charm.land/bubbles/v2/key"

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
	keyEscOrLeft = key.NewBinding(
		key.WithKeys("esc", "left"),
		key.WithHelp("←/esc", "back"),
	)
	keyEscCancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	)
	keyEscClear = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "clear"),
	)
	keyRefresh = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
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
		key.WithKeys("1", "2", "3", "4"),
		key.WithHelp("1-4", "tabs"),
	)
	keyScroll = key.NewBinding(
		key.WithKeys("up", "down", "pgup", "pgdown"),
		key.WithHelp("↑↓/pg", "scroll"),
	)
	keyOpen = key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open link"),
	)
	keyVersion = key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "versions"),
	)
	keyUseLatest = key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "latest"),
	)
)
