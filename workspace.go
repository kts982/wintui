package main

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
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
	workspaceConfirm
	workspaceExecuting
	workspaceDone
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
	state            workspaceState
	width            int
	height           int
	layout           layout
	items            []workspaceItem // grouped: upgradeable first, then installed
	cursor           int
	selected         map[string]bool   // keyed by packageSourceKey
	selectedVersions map[string]string // custom version picks per package
	spinner          spinner.Model
	summary          summaryPanel
	filter           listFilter
	detail           detailPanel
	exec             executionLog
	progress         progressBar
	pendingAction    string // "upgrade" or "uninstall"
	pendingItems     []workspaceItem
	batchCurrent     int
	batchTotal       int
	batchErrs        []error
	batchOutputs     []string
	err              error
	ctx              context.Context
	cancel           context.CancelFunc
}

func newWorkspaceScreen() workspaceScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	return workspaceScreen{
		state:            workspaceLoading,
		width:            80,
		height:           24,
		layout:           computeLayout(80, 24, true),
		selected:         make(map[string]bool),
		selectedVersions: make(map[string]string),
		spinner:          sp,
		summary:          newSummaryPanel(),
		filter:           newListFilter(),
		detail:           newDetailPanel(),
		exec:             newExecutionLog(),
		progress:         newProgressBar(50),
		ctx:              ctx,
		cancel:           cancel,
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
			merged := pkg
			merged.Available = upPkg.Available
			updates = append(updates, workspaceItem{
				pkg:         merged,
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
		s.detail = s.detail.withWindowSize(msg.Width, msg.Height)
		return s, nil

	case tea.KeyPressMsg:
		// Detail panel intercepts keys when visible.
		if s.detail.visible() {
			return s.updateDetail(msg)
		}

		// Confirm state.
		if s.state == workspaceConfirm {
			switch msg.String() {
			case "enter":
				return s.startBatch()
			case "esc":
				s.state = workspaceReady
				s.pendingItems = nil
				s.pendingAction = ""
				return s, nil
			}
			return s, nil
		}

		// Done state.
		if s.state == workspaceDone {
			cmd, handled := s.exec.doneUpdate(msg)
			if handled {
				return s, cmd
			}
			if msg.String() == "r" {
				cache.invalidate()
				s.state = workspaceLoading
				s.items = nil
				s.cursor = 0
				s.err = nil
				s.exec.reset()
				return s, s.init()
			}
			return s, nil
		}

		// Executing state — only allow cancel.
		if s.state == workspaceExecuting {
			if msg.String() == "esc" {
				if s.cancel != nil {
					s.cancel()
				}
			}
			cmd, handled := s.exec.update(msg)
			if handled {
				return s, cmd
			}
			return s, nil
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
		case "space":
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
		case "u":
			return s.beginAction("upgrade")
		case "x", "delete":
			return s.beginAction("uninstall")
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
		if s.detail.visible() {
			updated, cmd, _ := s.detail.update(msg)
			s.detail = updated
			return s, cmd
		}
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

	case detailVersionSelectedMsg:
		if msg.version != "" {
			if s.selectedVersions == nil {
				s.selectedVersions = make(map[string]string)
			}
			s.selectedVersions[packageSourceKey(msg.pkgID, msg.source)] = msg.version
		} else {
			delete(s.selectedVersions, packageSourceKey(msg.pkgID, msg.source))
		}
		return s, nil

	case startWorkspaceBatchMsg:
		if s.state == workspaceExecuting {
			return s.processNextBatchItem()
		}

	case streamMsg:
		if s.state == workspaceExecuting {
			s.exec.appendLine(string(msg))
			return s, nil
		}

	case streamDoneMsg:
		if s.state == workspaceExecuting {
			s.batchOutputs = append(s.batchOutputs, s.exec.currentOutput())
			s.batchErrs = append(s.batchErrs, msg.err)
			s.batchCurrent++
			if s.batchTotal > 0 {
				s.progress.percent = float64(s.batchCurrent) / float64(s.batchTotal)
			}
			return s.processNextBatchItem()
		}

	case progressTickMsg:
		if s.state == workspaceExecuting {
			var cmd tea.Cmd
			s.progress, cmd = s.progress.update(msg)
			return s, cmd
		}

	case progress.FrameMsg:
		if s.state == workspaceExecuting {
			var cmd tea.Cmd
			s.progress, cmd = s.progress.update(msg)
			return s, cmd
		}
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

// ── Actions ───────────────────────────────────────────────────────

func (s workspaceScreen) beginAction(action string) (screen, tea.Cmd) {
	// Collect selected items that match the action.
	var targets []workspaceItem
	for _, item := range s.items {
		if !s.selected[item.key()] {
			continue
		}
		if action == "upgrade" && !item.upgradeable {
			continue
		}
		targets = append(targets, item)
	}

	// If nothing selected, act on the focused item.
	if len(targets) == 0 {
		items := s.filteredItems()
		if s.cursor < len(items) {
			item := items[s.cursor]
			if action == "upgrade" && !item.upgradeable {
				return s, nil
			}
			targets = []workspaceItem{item}
		}
	}

	if len(targets) == 0 {
		return s, nil
	}

	s.pendingAction = action
	s.pendingItems = targets
	s.state = workspaceConfirm
	return s, nil
}

func (s workspaceScreen) confirmModalView(width, height int) string {
	count := len(s.pendingItems)
	verb := s.pendingAction
	title := strings.ToUpper(verb[:1]) + verb[1:]
	var body string
	if count == 1 {
		body = fmt.Sprintf("%s %s (%s)?", title, s.pendingItems[0].pkg.Name, s.pendingItems[0].pkg.ID)
	} else {
		body = fmt.Sprintf("%s %d selected packages?", title, count)
	}
	bg := s.viewReady(width, height)
	return renderConfirmModal(bg, width, height, confirmModal{
		title:       title + " Packages",
		body:        []string{body},
		confirmVerb: verb,
	})
}

type startWorkspaceBatchMsg struct{}

func (s workspaceScreen) startBatch() (screen, tea.Cmd) {
	s.state = workspaceExecuting
	s.batchTotal = len(s.pendingItems)
	s.batchCurrent = 0
	s.batchErrs = make([]error, 0, s.batchTotal)
	s.batchOutputs = make([]string, 0, s.batchTotal)
	s.exec.reset()
	s.progress, _ = s.progress.start()
	return s, tea.Batch(
		tickProgress(),
		func() tea.Msg { return startWorkspaceBatchMsg{} },
	)
}

func (s workspaceScreen) processNextBatchItem() (screen, tea.Cmd) {
	if s.batchCurrent >= s.batchTotal {
		return s.finishBatch()
	}

	item := s.pendingItems[s.batchCurrent]
	s.exec.appendSection(fmt.Sprintf("== %s (%s) ==", item.pkg.Name, item.pkg.ID))

	var args []string
	var outChan <-chan string
	var errChan <-chan error

	if s.pendingAction == "upgrade" {
		version := s.selectedVersions[item.key()]
		args, outChan, errChan = upgradePackageStreamCtx(s.ctx, item.pkg.ID, item.pkg.Source, version)
	} else {
		args, outChan, errChan = uninstallPackageStreamCtx(s.ctx, item.pkg)
	}
	_ = args

	return s, awaitStream(nil, outChan, errChan)
}

func (s workspaceScreen) finishBatch() (screen, tea.Cmd) {
	s.state = workspaceDone
	s.progress = s.progress.stop()
	s.exec.setDoneExpanded(true)
	s.exec.setDoneSize(s.width, s.height, 18)
	s.selected = make(map[string]bool)
	cache.invalidate()
	return s, func() tea.Msg { return packageDataChangedMsg{origin: screenWorkspace} }
}

// ── View ──────────────────────────────────────────────────────────

func (s workspaceScreen) view(width, height int) string {
	// Detail overlay takes over the full content area.
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Package Details") + "\n\n" +
			s.detail.view(width, height-2)
	}

	switch s.state {
	case workspaceLoading:
		return s.viewLoading()
	case workspaceEmpty:
		return "  " + helpStyle.Render("No packages found. Press r to refresh.") + "\n"
	case workspaceReady:
		return s.viewReady(width, height)
	case workspaceConfirm:
		return s.confirmModalView(width, height)
	case workspaceExecuting:
		return s.viewExecuting(width, height)
	case workspaceDone:
		return s.viewDone(width, height)
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

func (s workspaceScreen) viewExecuting(width, height int) string {
	var b strings.Builder
	action := strings.ToUpper(s.pendingAction[:1]) + s.pendingAction[1:]
	b.WriteString("  " + sectionTitleStyle.Render(action+" Packages") + "\n")
	if s.batchTotal > 0 {
		b.WriteString(fmt.Sprintf("  %s %d/%d\n", action+"ing", s.batchCurrent+1, s.batchTotal))
	}
	b.WriteString("  " + s.progress.view() + "\n\n")
	b.WriteString(s.exec.view(width, height))
	return b.String()
}

func (s workspaceScreen) viewDone(width, height int) string {
	var b strings.Builder
	action := strings.ToUpper(s.pendingAction[:1]) + s.pendingAction[1:]

	// Count successes and failures.
	successCount := 0
	failCount := 0
	for _, err := range s.batchErrs {
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}

	if failCount == 0 {
		b.WriteString("  " + successStyle.Render(fmt.Sprintf("%s complete: %d succeeded", action, successCount)) + "\n\n")
	} else {
		b.WriteString("  " + warnStyle.Render(fmt.Sprintf("%s finished: %d succeeded, %d failed", action, successCount, failCount)) + "\n\n")
	}

	// Per-package results.
	for i, item := range s.pendingItems {
		if i < len(s.batchErrs) {
			if s.batchErrs[i] == nil {
				b.WriteString("  " + successStyle.Render("✓ ") + item.pkg.Name + "\n")
			} else {
				b.WriteString("  " + errorStyle.Render("✗ ") + item.pkg.Name + "\n")
				b.WriteString("    " + helpStyle.Render(s.batchErrs[i].Error()) + "\n")
			}
		}
	}

	if logView := s.exec.doneView(width, height, 18); logView != "" {
		b.WriteString("\n" + logView + "\n")
	}
	b.WriteString("\n  " + helpStyle.Render("Press r to refresh or tab to switch screens") + "\n")
	return b.String()
}

// ── Help keys ─────────────────────────────────────────────────────

func (s workspaceScreen) helpKeys() []key.Binding {
	if s.detail.visible() {
		return s.detail.helpKeys()
	}
	switch s.state {
	case workspaceConfirm:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", s.pendingAction)),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	case workspaceExecuting:
		return s.exec.helpKeys()
	case workspaceDone:
		return s.exec.doneHelpKeys()
	case workspaceReady:
		// fall through
	default:
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
