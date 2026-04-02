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
	keyApply = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "apply"),
	)
	keyUpgradeSelected = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "upgrade"),
	)
	keyInstallSelected = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "install"),
	)
	keyEsc = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
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
	keyFilter = key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	)
	keyFilterEdit = key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "edit"),
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
	keyConfirm = key.NewBinding(
		key.WithKeys("enter", "y"),
		key.WithHelp("enter/y", "confirm"),
	)
	keyCancel = key.NewBinding(
		key.WithKeys("esc", "n"),
		key.WithHelp("esc/n", "cancel"),
	)
	keyExport = key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "export"),
	)
	keyRetryElevated = key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "retry elevated"),
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
		key.WithKeys("up", "down", "pgup", "pgdown"),
		key.WithHelp("↑↓/pg", "scroll"),
	)
	keyFollow = key.NewBinding(
		key.WithKeys("f", "end"),
		key.WithHelp("f/end", "follow"),
	)
	keyLog = key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "log"),
	)
	keySearch = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "search"),
	)
	keyImport = key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "import"),
	)
	keyUpgradeAll = key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "upgrade all"),
	)
	keyCleanAll = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "clean all"),
	)
	keySearchAgain = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "search again"),
	)
	keyShowSkipped = key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "show skipped"),
	)
	keyFocusInstallable = key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "focus installable"),
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
