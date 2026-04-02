package main

import (
	"context"
	"fmt"
	"image/color"
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
	pendingAction    string // "upgrade" or "uninstall"
	batchItems       []batchItem
	batchIdx         int // index of the currently running item
	streamOut        <-chan string
	streamErr        <-chan error
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
				s.batchItems = nil
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
		case "enter", "right", "l":
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
		if s.state == workspaceLoading || s.state == workspaceExecuting {
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
			return s, awaitStream(nil, s.streamOut, s.streamErr)
		}

	case streamDoneMsg:
		if s.state == workspaceExecuting && s.batchIdx < len(s.batchItems) {
			s.batchItems[s.batchIdx].output = s.exec.currentOutput()
			if msg.err != nil {
				s.batchItems[s.batchIdx].status = batchFailed
				s.batchItems[s.batchIdx].err = msg.err
			} else {
				s.batchItems[s.batchIdx].status = batchDone
			}
			s.batchIdx++
			return s.processNextBatchItem()
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
	if !canFetchDetails(item.pkg.Source) {
		return s, nil // ARP packages don't have fetchable details
	}
	var cmd tea.Cmd
	s.detail, cmd = s.detail.showWithVersion(item.pkg, "", item.upgradeable)
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
	s.batchItems = make([]batchItem, len(targets))
	for i, t := range targets {
		s.batchItems[i] = batchItem{item: t, status: batchQueued}
	}
	s.state = workspaceConfirm
	return s, nil
}

func (s workspaceScreen) confirmModalView(width, height int) string {
	count := len(s.batchItems)
	verb := s.pendingAction
	title := strings.ToUpper(verb[:1]) + verb[1:]
	var body string
	if count == 1 {
		body = fmt.Sprintf("%s %s (%s)?", title, s.batchItems[0].item.pkg.Name, s.batchItems[0].item.pkg.ID)
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
	s.batchIdx = 0
	s.exec.reset()
	return s, tea.Batch(
		s.spinner.Tick,
		func() tea.Msg { return startWorkspaceBatchMsg{} },
	)
}

func (s workspaceScreen) processNextBatchItem() (screen, tea.Cmd) {
	if s.batchIdx >= len(s.batchItems) {
		return s.finishBatch()
	}

	s.batchItems[s.batchIdx].status = batchRunning
	item := s.batchItems[s.batchIdx].item
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
	s.streamOut = outChan
	s.streamErr = errChan

	return s, tea.Batch(s.spinner.Tick, awaitStream(nil, s.streamOut, s.streamErr))
}

func (s workspaceScreen) finishBatch() (screen, tea.Cmd) {
	s.state = workspaceDone
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
	case workspaceExecuting, workspaceDone:
		return s.viewBatchList(width, height)
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
	panelWidth := l.list.W
	innerWidth := max(panelWidth-2, 10) // minus left+right border chars

	var b strings.Builder

	// Filter bar (above panels).
	if s.filter.active {
		b.WriteString("  " + s.filter.input.View() + "\n")
	} else if s.filter.query != "" {
		b.WriteString("  " + helpStyle.Render("Filter: "+s.filter.query+"  (/ edit • esc clear)") + "\n")
	}

	nInstalled := len(items) - nUpgradeable

	// Calculate how much height each panel gets.
	availableH := l.list.H
	if s.filter.active || s.filter.query != "" {
		availableH--
	}

	// Determine which section has focus.
	cursorInUpdates := s.cursor < nUpgradeable

	// Allocate height: updates gets what it needs (capped), rest to installed.
	var updatesPanelH, installedPanelH int
	if nUpgradeable > 0 && nInstalled > 0 {
		maxUpdatesH := max(availableH*2/5, 5)
		updatesPanelH = min(nUpgradeable+2, maxUpdatesH) // +2 for top+bottom border
		installedPanelH = availableH - updatesPanelH - 1 // -1 for gap between panels
	} else if nUpgradeable > 0 {
		updatesPanelH = availableH
	} else {
		installedPanelH = availableH
	}

	// Render updates panel.
	if nUpgradeable > 0 {
		updateItems := items[:nUpgradeable]
		title := fmt.Sprintf("Updates Available (%d)", nUpgradeable)
		innerH := max(updatesPanelH-2, 1) // minus top+bottom border
		borderColor := dim
		if cursorInUpdates {
			borderColor = accent
		}
		content := s.renderPanelItems(updateItems, 0, innerH, innerWidth)
		b.WriteString(renderTitledPanel(title, content, panelWidth, innerH, borderColor))
		b.WriteString("\n")
	}

	// Render installed panel.
	if nInstalled > 0 {
		installedItems := items[nUpgradeable:]
		title := fmt.Sprintf("Installed (%d)", nInstalled)
		innerH := max(installedPanelH-2, 1) // minus top+bottom border
		borderColor := dim
		if !cursorInUpdates || nUpgradeable == 0 {
			borderColor = accent
		}
		content := s.renderPanelItems(installedItems, nUpgradeable, innerH, innerWidth)
		b.WriteString(renderTitledPanel(title, content, panelWidth, innerH, borderColor))
	}

	return b.String()
}

// renderTitledPanel renders a bordered panel with a title in the top border.
func renderTitledPanel(title, content string, width, innerH int, borderColor color.Color) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(borderColor)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	innerW := max(width-2, 1) // content width between left and right border chars

	// Top border: ╭─ Title ──────────────────╮
	titleRendered := titleStyle.Render(" " + title + " ")
	titleRuneLen := len([]rune(title)) + 2 // spaces around title
	dashesAfter := max(innerW-titleRuneLen-1, 0)
	topLine := borderStyle.Render("╭─") + titleRendered +
		borderStyle.Render(strings.Repeat("─", dashesAfter)) +
		borderStyle.Render("╮")

	// Content rows with side borders.
	contentLines := strings.Split(content, "\n")
	var body strings.Builder
	for i := range innerH {
		line := ""
		if i < len(contentLines) {
			line = contentLines[i]
		}
		paddedLine := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).Render(line)
		body.WriteString(borderStyle.Render("│") + paddedLine + borderStyle.Render("│") + "\n")
	}

	// Bottom border.
	bottomLine := borderStyle.Render("╰" + strings.Repeat("─", innerW) + "╯")

	return topLine + "\n" + body.String() + bottomLine
}

// renderPanelItems renders a scrollable slice of items for one section panel.
// globalOffset is the index offset for cursor calculation.
func (s workspaceScreen) renderPanelItems(items []workspaceItem, globalOffset, maxVisible, innerWidth int) string {
	// Find the cursor position relative to this section.
	localCursor := s.cursor - globalOffset
	if localCursor < 0 || localCursor >= len(items) {
		localCursor = -1 // cursor is in the other section
	}

	start, end := scrollWindow(max(localCursor, 0), len(items), maxVisible)
	visible := items[start:end]

	var lines []string
	for i, item := range visible {
		globalIdx := globalOffset + start + i
		cursor := cursorBlankStr
		if globalIdx == s.cursor {
			cursor = cursorStr
		}
		sel := checkbox(s.selected[item.key()])
		isCursor := globalIdx == s.cursor
		row := cursor + sel + " " + s.renderItemText(item, innerWidth, isCursor)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

func (s workspaceScreen) renderItemText(item workspaceItem, maxWidth int, isCursor bool) string {
	nameStyle := itemStyle // white
	if isCursor {
		nameStyle = itemActiveStyle // bold pink
	}

	if item.upgradeable {
		name := nameStyle.Render(item.pkg.Name)
		ver := helpStyle.Render(item.installed + " → " + item.available)
		return name + "  " + ver
	}
	name := nameStyle.Render(item.pkg.Name)
	ver := helpStyle.Render(item.installed)
	return name + "  " + ver
}

// viewBatchList renders the batch items with inline status icons.
// Used for both executing and done states.
func (s workspaceScreen) viewBatchList(width, height int) string {
	l := computeLayout(width, height, true)
	completed, failed, total := batchCounters(s.batchItems)
	action := strings.ToUpper(s.pendingAction[:1]) + s.pendingAction[1:]

	var b strings.Builder

	// Compact status bar: "Upgrading 1/2: ✓ Notepad++ · ⣯ Neovim · queued"
	var statusParts []string
	for _, bi := range s.batchItems {
		icon := bi.statusIcon(s.spinner)
		statusParts = append(statusParts, icon+" "+bi.item.pkg.Name)
	}

	if s.state == workspaceExecuting {
		verb := action
		switch action {
		case "Upgrade":
			verb = "Upgrading"
		case "Uninstall":
			verb = "Uninstalling"
		}
		header := fmt.Sprintf("%s %d/%d", verb, completed, total)
		b.WriteString("  " + sectionTitleStyle.Render(header) + "  " + strings.Join(statusParts, " · "))
	} else {
		if failed == 0 {
			b.WriteString("  " + successStyle.Render(fmt.Sprintf("%s complete: %d succeeded", action, completed)))
		} else {
			b.WriteString("  " + warnStyle.Render(fmt.Sprintf("%s finished: %d succeeded, %d failed", action, completed-failed, failed)))
		}
		b.WriteString("  " + strings.Join(statusParts, " · "))
	}
	b.WriteString("\n")

	// Log panel fills remaining space (header is just 1 line now).
	logReserve := 4 // status line + padding
	if s.state == workspaceDone {
		if logView := s.exec.doneView(width, l.list.H, logReserve); logView != "" {
			b.WriteString("\n" + logView + "\n")
		}
		b.WriteString("\n  " + helpStyle.Render("Press r to refresh or tab to switch screens") + "\n")
	} else {
		logH := max(l.list.H-logReserve, 5)
		s.exec.setSize(width, logH+12)
		b.WriteString("\n" + s.exec.view(width, logH) + "\n")
	}

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
