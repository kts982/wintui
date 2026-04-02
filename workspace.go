package main

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"

	tea "charm.land/bubbletea/v2"
)

// ── Workspace: merged Upgrade + Installed screen ──────────────────

type workspaceState int

const (
	workspaceLoading workspaceState = iota
	workspaceReady
	workspaceEmpty
)

// workspaceItem is a unified list entry for both upgradeable and installed packages.
type workspaceItem struct {
	pkg         Package
	upgradeable bool
	installed   string // installed version (same as pkg.Version for installed-only)
	available   string // available version (empty if up-to-date)
}

func (w workspaceItem) key() string {
	return packageSourceKey(w.pkg.ID, w.pkg.Source)
}

type workspaceScreen struct {
	state    workspaceState
	width    int
	height   int
	layout   layout
	items    []workspaceItem // grouped: upgradeable first, then installed
	cursor   int
	selected map[string]bool // keyed by packageSourceKey
	spinner  spinner.Model
	summary  summaryPanel
	filter   listFilter
	detail   detailPanel
	ctx      context.Context
	cancel   context.CancelFunc
}

func newWorkspaceScreen() workspaceScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	return workspaceScreen{
		state:    workspaceLoading,
		width:    80,
		height:   24,
		layout:   computeLayout(80, 24, true),
		selected: make(map[string]bool),
		spinner:  sp,
		summary:  newSummaryPanel(),
		filter:   newListFilter(),
		detail:   newDetailPanel(),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// workspaceDataMsg carries both installed and upgradeable data.
type workspaceDataMsg struct {
	installed   []Package
	upgradeable []Package
	err         error
}

func (s workspaceScreen) init() tea.Cmd {
	// Try cache first.
	inst, instOK := cache.getInstalled()
	upgr, upgrOK := cache.getUpgradeable()
	if instOK && upgrOK {
		return func() tea.Msg {
			return workspaceDataMsg{installed: inst, upgradeable: upgr}
		}
	}
	return tea.Batch(s.spinner.Tick, func() tea.Msg {
		installed, err := getInstalledCtx(s.ctx)
		if err != nil {
			return workspaceDataMsg{err: err}
		}
		cache.setInstalled(installed)
		upgradeable, err := getUpgradeableCtx(s.ctx)
		if err != nil {
			// Installed data is still valid even if upgrade check fails.
			return workspaceDataMsg{installed: installed, err: err}
		}
		cache.setUpgradeable(upgradeable)
		return workspaceDataMsg{installed: installed, upgradeable: upgradeable}
	})
}

// buildItems merges installed and upgradeable into a grouped list.
func buildItems(installed, upgradeable []Package) []workspaceItem {
	// Build upgrade lookup.
	upgradeMap := make(map[string]Package, len(upgradeable))
	for _, pkg := range upgradeable {
		upgradeMap[packageSourceKey(pkg.ID, pkg.Source)] = pkg
	}

	var updates []workspaceItem
	var rest []workspaceItem
	seen := make(map[string]bool, len(installed))

	for _, pkg := range installed {
		k := packageSourceKey(pkg.ID, pkg.Source)
		seen[k] = true
		if upPkg, ok := upgradeMap[k]; ok {
			updates = append(updates, workspaceItem{
				pkg:         pkg,
				upgradeable: true,
				installed:   pkg.Version,
				available:   upPkg.Available,
			})
		} else {
			rest = append(rest, workspaceItem{
				pkg:       pkg,
				installed: pkg.Version,
			})
		}
	}

	// Include upgradeable packages not in installed list (edge case).
	for _, pkg := range upgradeable {
		k := packageSourceKey(pkg.ID, pkg.Source)
		if !seen[k] {
			updates = append(updates, workspaceItem{
				pkg:         pkg,
				upgradeable: true,
				installed:   pkg.Version,
				available:   pkg.Available,
			})
		}
	}

	// Updates first, then installed.
	return append(updates, rest...)
}

// countUpgradeable returns how many items are upgradeable.
func countUpgradeable(items []workspaceItem) int {
	n := 0
	for _, item := range items {
		if item.upgradeable {
			n++
		} else {
			break // upgradeable are always first
		}
	}
	return n
}

func (s workspaceScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.layout = computeLayout(msg.Width, msg.Height, true)
		s.summary.setSize(s.layout.detail.W, s.layout.detail.H)
		return s, nil

	case tea.KeyPressMsg:
		// Detail panel intercepts keys when visible.
		if s.detail.visible() {
			return s.updateDetail(msg)
		}

		// Filter input.
		if s.filter.active {
			return s.updateFilter(msg)
		}

		switch msg.String() {
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
				return s, s.focusSummary()
			}
		case "down", "j":
			if s.cursor < len(s.filteredItems())-1 {
				s.cursor++
				return s, s.focusSummary()
			}
		case " ":
			items := s.filteredItems()
			if s.cursor < len(items) {
				k := items[s.cursor].key()
				s.selected[k] = !s.selected[k]
				if !s.selected[k] {
					delete(s.selected, k)
				}
			}
			return s, nil
		case "enter":
			return s.openDetail()
		case "/":
			s.filter = s.filter.activate()
			return s, s.filter.input.Focus()
		case "r":
			if s.state == workspaceReady || s.state == workspaceEmpty {
				cache.invalidate()
				s.state = workspaceLoading
				s.items = nil
				s.cursor = 0
				return s, s.init()
			}
		case "a":
			s.selectAllUpgradeable()
			return s, nil
		}

	case workspaceDataMsg:
		if msg.err != nil && msg.installed == nil {
			s.state = workspaceEmpty
			return s, nil
		}
		s.items = buildItems(msg.installed, msg.upgradeable)
		if len(s.items) == 0 {
			s.state = workspaceEmpty
		} else {
			s.state = workspaceReady
			s.cursor = 0
		}
		return s, s.focusSummary()

	case spinner.TickMsg:
		if s.state == workspaceLoading {
			var cmd tea.Cmd
			s.spinner, cmd = s.spinner.Update(msg)
			return s, cmd
		}

	// Summary panel messages.
	case summaryFetchTickMsg, summaryDetailMsg:
		cmd := s.summary.update(msg)
		return s, cmd

	// Detail panel messages — routed through detail's own update.
	case packageDetailMsg, packageVersionsMsg:
		updated, cmd, _ := s.detail.update(msg)
		s.detail = updated
		return s, cmd
	}

	return s, nil
}

func (s *workspaceScreen) focusSummary() tea.Cmd {
	items := s.filteredItems()
	if s.cursor >= 0 && s.cursor < len(items) {
		item := items[s.cursor]
		return s.summary.focus(&item.pkg, item.installed, item.available)
	}
	return s.summary.focus(nil, "", "")
}

func (s *workspaceScreen) selectAllUpgradeable() {
	for _, item := range s.items {
		if item.upgradeable {
			s.selected[item.key()] = true
		}
	}
}

func (s workspaceScreen) filteredItems() []workspaceItem {
	if s.filter.query == "" {
		return s.items
	}
	matches := s.filter.matchItems(s.items)
	return matches
}

func (s workspaceScreen) openDetail() (screen, tea.Cmd) {
	items := s.filteredItems()
	if s.cursor >= len(items) {
		return s, nil
	}
	item := items[s.cursor]
	var cmd tea.Cmd
	s.detail, cmd = s.detail.showWithVersion(item.pkg, "", true)
	return s, cmd
}

func (s workspaceScreen) updateDetail(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	updated, cmd, handled := s.detail.update(msg)
	s.detail = updated
	if handled {
		return s, cmd
	}
	return s, nil
}

func (s workspaceScreen) updateFilter(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.filter = s.filter.deactivate()
		s.cursor = 0
		return s, s.focusSummary()
	case "enter":
		s.filter = s.filter.apply()
		s.cursor = 0
		return s, s.focusSummary()
	default:
		var cmd tea.Cmd
		s.filter.input, cmd = s.filter.input.Update(msg)
		s.filter.query = s.filter.input.Value()
		s.cursor = 0
		return s, tea.Batch(cmd, s.focusSummary())
	}
}

// ── View ──────────────────────────────────────────────────────────

func (s workspaceScreen) view(width, height int) string {
	switch s.state {
	case workspaceLoading:
		return s.viewLoading()
	case workspaceEmpty:
		return "  " + helpStyle.Render("No packages found. Press r to refresh.") + "\n"
	case workspaceReady:
		return s.viewReady(width, height)
	}
	return ""
}

func (s workspaceScreen) viewLoading() string {
	return "  " + s.spinner.View() + " Loading packages...\n"
}

func (s workspaceScreen) viewReady(width, height int) string {
	l := computeLayout(width, height, true)
	items := s.filteredItems()
	nUpgradeable := countUpgradeable(items)

	// Render list panel.
	listView := s.renderList(items, nUpgradeable, l)

	if !l.hasDetail {
		return listView
	}

	// Render detail panel.
	sp := s.summary
	sp.setSize(l.detail.W, l.detail.H)
	detailView := sp.view()

	return lipgloss.JoinHorizontal(lipgloss.Top, listView, " ", detailView)
}

func (s workspaceScreen) renderList(items []workspaceItem, nUpgradeable int, l layout) string {
	var b strings.Builder
	maxVisible := l.maxVisibleItems(2) // 2 for section headers

	// Filter bar.
	if s.filter.active {
		b.WriteString("  " + s.filter.input.View() + "\n")
	} else if s.filter.query != "" {
		b.WriteString("  " + helpStyle.Render("Filter: "+s.filter.query+"  (/ edit • esc clear)") + "\n")
	}

	start, end := scrollWindow(s.cursor, len(items), maxVisible)
	visible := items[start:end]

	// Track whether we need section headers.
	inUpgradeable := start < nUpgradeable

	for i, item := range visible {
		globalIdx := start + i

		// Section header: transition from upgradeable to installed.
		if inUpgradeable && !item.upgradeable {
			inUpgradeable = false
			if globalIdx > 0 {
				b.WriteString("\n")
			}
			b.WriteString("  " + sectionTitleStyle.Render("Installed") + "\n")
		} else if globalIdx == start && item.upgradeable && start == 0 {
			b.WriteString("  " + sectionTitleStyle.Render("Updates Available") + "\n")
		} else if globalIdx == start && !item.upgradeable && start == 0 {
			b.WriteString("  " + sectionTitleStyle.Render("Installed") + "\n")
		}

		// Cursor + selection.
		cursor := cursorBlankStr
		if globalIdx == s.cursor {
			cursor = cursorStr
		}

		sel := checkbox(s.selected[item.key()])

		// Package row.
		name := item.pkg.Name
		row := cursor + sel + " " + s.renderItemText(item, l.list.W-6) // 6 = cursor+checkbox+spacing
		b.WriteString(row + "\n")
		_ = name
	}

	// Pad to fill list height.
	rendered := b.String()
	lines := strings.Count(rendered, "\n")
	for lines < l.list.H {
		rendered += "\n"
		lines++
	}

	return rendered
}

func (s workspaceScreen) renderItemText(item workspaceItem, maxWidth int) string {
	if item.upgradeable {
		name := itemActiveStyle.Render(item.pkg.Name)
		ver := helpStyle.Render(item.installed + " → " + item.available)
		return name + "  " + ver
	}
	name := itemStyle.Render(item.pkg.Name)
	ver := helpStyle.Render(item.installed)
	return name + "  " + ver
}

// ── Help keys ─────────────────────────────────────────────────────

func (s workspaceScreen) helpKeys() []key.Binding {
	if s.detail.visible() {
		return s.detail.helpKeys()
	}
	if s.state != workspaceReady {
		return nil
	}
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "select")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall")),
		key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all updates")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
	return bindings
}

// ── Filter support ────────────────────────────────────────────────

// matchItems filters workspace items using the fuzzy filter.
func (f listFilter) matchItems(items []workspaceItem) []workspaceItem {
	if f.query == "" {
		return items
	}
	strs := make([]string, len(items))
	for i, item := range items {
		strs[i] = item.pkg.Name + " " + item.pkg.ID
	}
	matches := fuzzy.Find(f.query, strs)
	result := make([]workspaceItem, 0, len(matches))
	for _, m := range matches {
		if m.Index < len(items) {
			result = append(result, items[m.Index])
		}
	}
	return result
}
