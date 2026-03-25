package main

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Screen identifiers ─────────────────────────────────────────────

type screenID int

const (
	screenMenu screenID = iota // kept for compat, maps to tab 0
	screenUpgrade
	screenInstall
	screenPackages
	screenCleanup
	screenHealthcheck
	screenSettings
)

// ── Tab definitions ────────────────────────────────────────────────

type tabDef struct {
	label string
	id    screenID
}

var tabs = []tabDef{
	{"Upgrade", screenUpgrade},
	{"Installed", screenPackages},
	{"Install", screenInstall},
	{"Cleanup", screenCleanup},
	{"Health", screenHealthcheck},
	{"Settings", screenSettings},
}

// ── Shared messages ────────────────────────────────────────────────

type switchScreenMsg screenID

type packagesLoadedMsg struct {
	packages []Package
	err      error
}

type commandDoneMsg struct {
	output string
	err    error
}

type streamMsg string

type streamDoneMsg struct {
	err error
}

type filesScannedMsg struct {
	files []string
	err   error
}

type screenCmdMsg struct {
	target screenID
	msg    tea.Msg
}

// ── Screen interface ───────────────────────────────────────────────

type screen interface {
	init() tea.Cmd
	update(msg tea.Msg) (screen, tea.Cmd)
	view(width, height int) string
	helpKeys() []key.Binding // keybindings for the help bar
}

// ── ASCII art header ───────────────────────────────────────────────

var asciiLogo = []string{
	`██╗    ██╗ ██╗ ███╗   ██╗ ████████╗ ██╗   ██╗ ██╗`,
	`██║    ██║ ██║ ████╗  ██║ ╚══██╔══╝ ██║   ██║ ██║`,
	`██║ █╗ ██║ ██║ ██╔██╗ ██║    ██║    ██║   ██║ ██║`,
	`██║███╗██║ ██║ ██║╚██╗██║    ██║    ██║   ██║ ██║`,
	`╚███╔███╔╝ ██║ ██║ ╚████║    ██║    ╚██████╔╝ ██║`,
	` ╚══╝╚══╝  ╚═╝ ╚═╝  ╚═══╝    ╚═╝     ╚═════╝  ╚═╝`,
}

var logoGradient = []lipgloss.Color{
	lipgloss.Color("212"), // bright pink
	lipgloss.Color("211"), // pink
	lipgloss.Color("206"), // salmon
	lipgloss.Color("170"), // magenta
	lipgloss.Color("134"), // purple
	lipgloss.Color("99"),  // lavender
	lipgloss.Color("105"), // light purple
	lipgloss.Color("141"), // periwinkle
}

// ── App model ──────────────────────────────────────────────────────

type logoTickMsg struct{}

// logoRow holds a spring-animated color offset for one logo line.
type logoRow struct {
	pos float64 // current color offset (fractional index into gradient)
	vel float64 // current velocity
}

type app struct {
	activeTab  int
	screens    map[screenID]screen
	help       help.Model
	width      int
	height     int
	quitting   bool
	logoRows   []logoRow
	logoSpring harmonica.Spring
	logoTime   float64 // accumulated time for wave target
}

func newApp(retryReq *retryRequest) app {
	h := help.New()
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(accent)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(dim)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	rows := make([]logoRow, len(asciiLogo))
	for i := range rows {
		rows[i].pos = float64(i) // start each row at a different offset
	}

	a := app{
		activeTab:  0,
		screens:    make(map[screenID]screen),
		help:       h,
		width:      80,
		height:     24,
		logoRows:   rows,
		logoSpring: harmonica.NewSpring(harmonica.FPS(15), 2.0, 0.8),
	}
	if retryReq != nil {
		a.activeTab = tabForRetry(*retryReq)
		switch retryReq.Op {
		case retryOpInstall:
			a.screens[tabs[a.activeTab].id] = newInstallScreenWithRetry(*retryReq)
		case retryOpUpgrade:
			a.screens[tabs[a.activeTab].id] = newUpgradeScreenWithRetry(*retryReq)
		case retryOpUninstall:
			a.screens[tabs[a.activeTab].id] = newPackagesScreenWithRetry(*retryReq)
		default:
			a.screens[tabs[a.activeTab].id] = createScreen(tabs[a.activeTab].id)
		}
	} else {
		a.screens[tabs[0].id] = createScreen(tabs[0].id)
	}
	return a
}

func logoTick() tea.Cmd {
	return tea.Tick(66*time.Millisecond, func(time.Time) tea.Msg { // ~15 FPS
		return logoTickMsg{}
	})
}

func (a app) Init() tea.Cmd {
	return tea.Batch(a.wrapScreenCmd(a.currentScreenID(), a.activeScreen().init()), logoTick())
}

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			a.quitting = true
			return a, tea.Quit
		case "q":
			// Don't quit if the active screen has text input (install/search)
			if !a.screenHasTextInput() {
				a.quitting = true
				return a, tea.Quit
			}
		// Number keys switch tabs
		case "tab":
			idx := (a.activeTab + 1) % len(tabs)
			return a.switchTab(idx)
		case "shift+tab":
			idx := (a.activeTab - 1 + len(tabs)) % len(tabs)
			return a.switchTab(idx)
		case "1", "2", "3", "4", "5", "6", "7":
			idx := int(msg.String()[0]-'0') - 1
			if idx >= 0 && idx < len(tabs) {
				return a.switchTab(idx)
			}
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// Check if click is on the tab bar row
			tabRow := lipgloss.Height(a.renderLogo()) // row right after logo
			if msg.Y == tabRow {
				if idx := a.tabHitTest(msg.X); idx >= 0 {
					return a.switchTab(idx)
				}
			}
		}

	case logoTickMsg:
		n := float64(len(logoGradient))
		a.logoTime += 0.05
		for i := range a.logoRows {
			// Each row targets a sine-wave offset, staggered by row index
			target := math.Mod(a.logoTime+float64(i)*1.0, n)
			a.logoRows[i].pos, a.logoRows[i].vel = a.logoSpring.Update(
				a.logoRows[i].pos, a.logoRows[i].vel, target,
			)
		}
		return a, logoTick()

	case screenCmdMsg:
		if msg.msg == nil {
			return a, nil
		}
		if switchMsg, ok := msg.msg.(switchScreenMsg); ok {
			return a.handleSwitchScreen(switchMsg)
		}
		return a.updateScreen(msg.target, msg.msg)

	case switchScreenMsg:
		return a.handleSwitchScreen(msg)
	}

	return a.updateScreen(a.currentScreenID(), msg)
}

func (a app) switchTab(idx int) (app, tea.Cmd) {
	if idx == a.activeTab {
		return a, nil
	}
	a.activeTab = idx
	id := tabs[idx].id
	if _, ok := a.screens[id]; ok {
		return a, nil
	}
	s := createScreen(id)
	a.screens[id] = s
	return a, a.wrapScreenCmd(id, s.init())
}

func (a app) View() string {
	if a.quitting {
		return ""
	}

	logo := a.renderLogo()
	tabBar := a.renderTabBar()

	// Build help bar from screen keybindings
	a.help.Width = a.width - 4
	helpBar := "  " + a.help.ShortHelpView(a.activeScreen().helpKeys())

	chrome := logo + tabBar + "\n"
	chromeHeight := lipgloss.Height(chrome)
	helpHeight := lipgloss.Height(helpBar)
	contentHeight := a.height - chromeHeight - helpHeight - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	content := a.activeScreen().view(a.width, contentHeight)

	// Assemble: chrome + content + help at bottom
	rendered := chrome + content + "\n" + helpBar
	renderedHeight := lipgloss.Height(rendered)
	if renderedHeight < a.height {
		// Insert padding before help bar to push it to the bottom
		pad := a.height - renderedHeight
		rendered = chrome + content + strings.Repeat("\n", pad+1) + helpBar
	}

	return rendered
}

// ── Logo rendering ─────────────────────────────────────────────────

func (a app) renderLogo() string {
	n := len(logoGradient)
	var lines []string
	for i, line := range asciiLogo {
		// Convert spring position to a gradient index
		idx := int(math.Round(a.logoRows[i].pos))
		idx = ((idx % n) + n) % n // ensure positive modulo
		color := logoGradient[idx]
		styled := lipgloss.NewStyle().Foreground(color).Bold(true).Render(line)
		lines = append(lines, "  "+styled)
	}
	subtitle := lipgloss.NewStyle().Foreground(dim).Italic(true).Render("  Windows Package Manager")
	lines = append(lines, subtitle)
	return strings.Join(lines, "\n") + "\n"
}

// ── Tab bar rendering ──────────────────────────────────────────────

func (a app) renderTabBar() string {
	var parts []string
	for i, t := range tabs {
		num := string(rune('1' + i))
		if i == a.activeTab {
			numStr := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(num)
			labelStr := tabActiveStyle.Render(t.label)
			parts = append(parts, numStr+" "+labelStr)
		} else {
			numStr := lipgloss.NewStyle().Foreground(dim).Render(num)
			labelStr := tabInactiveStyle.Render(t.label)
			parts = append(parts, numStr+" "+labelStr)
		}
	}
	sep := tabSepStyle.Render(" │ ")
	bar := "  " + strings.Join(parts, sep)

	// Admin badge + hints right after tabs
	var adminBadge string
	if isElevated() {
		adminBadge = lipgloss.NewStyle().Foreground(success).Render("● admin")
	} else {
		adminBadge = lipgloss.NewStyle().Foreground(warning).Render("● user")
	}
	bar += "    " + adminBadge + helpStyle.Render("  tab cycle • q quit")

	return bar + "\n"
}

// tabHitTest returns the tab index at x position, or -1.
func (a app) tabHitTest(x int) int {
	pos := 2 // leading indent
	for i, t := range tabs {
		tabWidth := 2 + len(t.label) // "N label"
		if x >= pos && x < pos+tabWidth {
			return i
		}
		pos += tabWidth + 3 // " │ " separator
	}
	return -1
}

// screenHasTextInput returns true if the active screen has a text input field.
func (a app) screenHasTextInput() bool {
	switch s := a.activeScreen().(type) {
	case installScreen:
		return s.state == installInput
	default:
		return false
	}
}

func (a app) currentScreenID() screenID {
	return tabs[a.activeTab].id
}

func (a app) activeScreen() screen {
	return a.screens[a.currentScreenID()]
}

func (a app) handleSwitchScreen(msg switchScreenMsg) (app, tea.Cmd) {
	for i, t := range tabs {
		if t.id == screenID(msg) {
			return a.switchTab(i)
		}
	}

	id := screenID(msg)
	if _, ok := a.screens[id]; ok {
		return a, nil
	}
	s := createScreen(id)
	a.screens[id] = s
	return a, a.wrapScreenCmd(id, s.init())
}

func (a app) updateScreen(id screenID, msg tea.Msg) (app, tea.Cmd) {
	s, ok := a.screens[id]
	if !ok {
		s = createScreen(id)
		a.screens[id] = s
	}

	next, cmd := s.update(msg)
	a.screens[id] = next
	return a, a.wrapScreenCmd(id, cmd)
}

func (a app) wrapScreenCmd(id screenID, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}

	return func() tea.Msg {
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
			return nil
		case tea.BatchMsg:
			wrapped := make(tea.BatchMsg, 0, len(msg))
			for _, sub := range msg {
				if sub == nil {
					continue
				}
				wrapped = append(wrapped, a.wrapScreenCmd(id, sub))
			}
			if len(wrapped) == 0 {
				return nil
			}
			return wrapped
		case screenCmdMsg:
			return msg
		case switchScreenMsg:
			return msg
		default:
			return screenCmdMsg{target: id, msg: msg}
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────

func createScreen(id screenID) screen {
	switch id {
	case screenUpgrade:
		return newUpgradeScreen()
	case screenInstall:
		return newInstallScreen()
	case screenPackages:
		return newPackagesScreen()
	case screenCleanup:
		return newCleanupScreen()
	case screenHealthcheck:
		return newHealthcheckScreen()
	case screenSettings:
		return newSettingsScreen()
	default:
		return newUpgradeScreen()
	}
}
