package main

import (
	"context"
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
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
	modal            *execModal
	streamOut        <-chan string
	streamErr        <-chan error
	err              error
	ctx              context.Context
	cancel           context.CancelFunc

	// Search/install integration.
	searchInput     textinput.Model
	searchActive    bool            // true when search input is focused
	searchQuery     string          // last executed search
	searchResults   []Package       // results from winget search
	searchLoading   bool            // search in progress
	installQueue    []workspaceItem // sticky queue of packages selected for install
	installQueueMap map[string]bool // quick lookup by packageSourceKey
}

func newWorkspaceScreen() workspaceScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	si := textinput.New()
	si.Placeholder = "search winget packages..."
	siStyles := si.Styles()
	siStyles.Focused.Prompt = lipgloss.NewStyle().Foreground(accent)
	siStyles.Cursor.Color = accent
	si.SetStyles(siStyles)
	si.Prompt = "🔍 "
	si.SetWidth(40)

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
		searchInput:      si,
		installQueueMap:  make(map[string]bool),
		ctx:              ctx,
		cancel:           cancel,
	}
}

// searchResultsMsg carries search results back from the async search.
type searchResultsMsg struct {
	query   string
	results []Package
	err     error
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

		// Modal states (confirm, executing, complete).
		if s.modal != nil {
			switch s.modal.phase {
			case execPhaseReview:
				switch msg.String() {
				case "enter":
					return s.startBatch()
				case "esc":
					s.modal = nil
					s.state = workspaceReady
					return s, nil
				}
				return s, nil
			case execPhaseRunning:
				if msg.String() == "esc" {
					if s.cancel != nil {
						s.cancel()
					}
					// Mark remaining queued items as skipped and finish.
					for i := range s.modal.items {
						if s.modal.items[i].status == batchQueued {
							s.modal.items[i].status = batchFailed
							s.modal.items[i].err = fmt.Errorf("cancelled")
						}
					}
					s.modal.phase = execPhaseComplete
					return s, nil
				}
				return s, nil
			case execPhaseComplete:
				switch msg.String() {
				case "enter":
					result, cmd := s.resetAndReload()
					return result, cmd
				case "ctrl+e":
					if s.modal.hasElevationCandidates() {
						retryItems := s.modal.elevationCandidateItems()
						m := newExecModal(s.modal.action, retryItems)
						m.phase = execPhaseRunning
						m.forceElevated = true
						s.modal = &m
						s.state = workspaceExecuting
						s.exec.reset()
						return s, tea.Batch(
							s.modal.spinner.Tick,
							func() tea.Msg { return startWorkspaceBatchMsg{} },
						)
					}
				}
				return s, nil
			}
		}

		// Search input.
		if s.searchActive {
			return s.updateSearch(msg)
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
			q, sr, up, ins := s.displayItems()
			totalItems := len(q) + len(sr) + len(up) + len(ins)
			if s.cursor < totalItems-1 {
				s.cursor++
				return s, s.focusSummary()
			}
		case "space":
			return s.toggleSelection()

		case "enter", "right", "l":
			return s.openDetail()
		case "/":
			s.filter = s.filter.activate()
			return s, s.filter.input.Focus()
		case "r":
			if s.state == workspaceReady || s.state == workspaceEmpty {
				result, cmd := s.resetAndReload()
				return result, cmd
			}
		case "a":
			s.selectAllUpgradeable()
			return s, nil
		case "s":
			s.searchActive = true
			s.searchInput.SetValue("")
			return s, s.searchInput.Focus()
		case "i":
			return s.beginAction("install")
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
		if s.state == workspaceExecuting && s.modal != nil {
			var cmd tea.Cmd
			s.modal.spinner, cmd = s.modal.spinner.Update(msg)
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
		if s.state == workspaceExecuting && s.modal != nil {
			s.exec.appendLine(string(msg))
			return s, awaitStream(nil, s.streamOut, s.streamErr)
		}

	case streamDoneMsg:
		if s.state == workspaceExecuting && s.modal != nil && s.modal.idx < len(s.modal.items) {
			s.modal.items[s.modal.idx].output = s.exec.currentOutput()
			if msg.err != nil {
				s.modal.items[s.modal.idx].status = batchFailed
				s.modal.items[s.modal.idx].err = msg.err
			} else {
				s.modal.items[s.modal.idx].status = batchDone
			}
			s.modal.idx++
			return s.processNextBatchItem()
		}
	case searchResultsMsg:
		s.searchLoading = false
		// Discard stale results from a previous search.
		if msg.query != s.searchQuery {
			return s, nil
		}
		if msg.err != nil {
			s.err = msg.err
			s.searchResults = nil
		} else {
			s.searchQuery = msg.query
			// Default empty source to "winget" for search results.
			for i := range msg.results {
				if msg.results[i].Source == "" {
					msg.results[i].Source = "winget"
				}
			}
			s.searchResults = msg.results
		}
		s.cursor = 0
		return s, s.focusSummary()

	case startRetryMsg:
		// Handle startup retry requests (e.g., from elevated relaunch).
		ritems := msg.req.items()
		var targets []workspaceItem
		for _, ri := range ritems {
			targets = append(targets, workspaceItem{
				pkg: Package{ID: ri.ID, Name: ri.Name, Source: ri.Source, Version: ri.Version},
			})
		}
		if len(targets) == 0 {
			return s, nil
		}
		bi := make([]batchItem, len(targets))
		for i, t := range targets {
			bi[i] = batchItem{item: t, status: batchQueued}
		}
		m := newExecModal(string(msg.req.Op), bi)
		m.phase = execPhaseRunning
		s.modal = &m
		s.state = workspaceExecuting
		s.exec.reset()
		return s, tea.Batch(
			s.modal.spinner.Tick,
			func() tea.Msg { return startWorkspaceBatchMsg{} },
		)
	}

	return s, nil
}

func (s workspaceScreen) updateSearch(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.searchActive = false
		s.searchInput.Blur()
		return s, nil
	case "enter":
		query := strings.TrimSpace(s.searchInput.Value())
		s.searchActive = false
		s.searchInput.Blur()
		if query == "" {
			// Empty search clears results.
			s.searchResults = nil
			s.searchQuery = ""
			return s, nil
		}
		s.searchLoading = true
		s.searchQuery = query
		ctx := s.ctx
		return s, func() tea.Msg {
			results, err := searchPackagesCtx(ctx, query)
			return searchResultsMsg{query: query, results: results, err: err}
		}
	default:
		var cmd tea.Cmd
		s.searchInput, cmd = s.searchInput.Update(msg)
		return s, cmd
	}
}

// resetAndReload creates a fresh context and reloads package data.
func (s *workspaceScreen) resetAndReload() (workspaceScreen, tea.Cmd) {
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	s.modal = nil
	s.state = workspaceLoading
	s.items = nil
	s.cursor = 0
	s.err = nil
	s.exec.reset()
	cache.invalidate()
	return *s, s.init()
}

func (s *workspaceScreen) focusSummary() tea.Cmd {
	queue, search, upgradeable, installed := s.displayItems()
	all := concat(queue, search, upgradeable, installed)
	if s.cursor >= 0 && s.cursor < len(all) {
		item := all[s.cursor]
		return s.summary.focus(&item.pkg, item.installed, item.available)
	}
	return s.summary.focus(nil, "", "")
}

func (s workspaceScreen) countSelected(items []workspaceItem) int {
	n := 0
	for _, item := range items {
		if s.selected[item.key()] {
			n++
		}
	}
	return n
}

func (s workspaceScreen) toggleSelection() (screen, tea.Cmd) {
	queue, search, upgradeable, installed := s.displayItems()
	allItems := concat(queue, search, upgradeable, installed)

	if s.cursor >= len(allItems) {
		return s, nil
	}
	item := allItems[s.cursor]
	k := item.key()

	// Is this a search result or install queue item?
	inSearchOrQueue := s.cursor < len(queue)+len(search)

	inSearch := s.cursor >= len(queue) && s.cursor < len(queue)+len(search)

	if inSearchOrQueue {
		// Toggle in/out of install queue.
		if s.installQueueMap[k] {
			delete(s.installQueueMap, k)
			newQueue := make([]workspaceItem, 0, len(s.installQueue))
			for _, qi := range s.installQueue {
				if qi.key() != k {
					newQueue = append(newQueue, qi)
				}
			}
			s.installQueue = newQueue
		} else {
			s.installQueueMap[k] = true
			s.installQueue = append(s.installQueue, item)
			// Auto-close search results after selecting from search.
			if inSearch {
				s.searchResults = nil
				s.searchQuery = ""
				s.cursor = 0
			}
		}
	} else {
		// Regular installed/upgradeable toggle.
		s.selected[k] = !s.selected[k]
		if !s.selected[k] {
			delete(s.selected, k)
		}
	}
	return s, nil
}

func concat(slices ...[]workspaceItem) []workspaceItem {
	var total int
	for _, s := range slices {
		total += len(s)
	}
	result := make([]workspaceItem, 0, total)
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
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

// displayItems returns the full ordered list for rendering and navigation:
// install queue → search results → upgradeable → installed.
func (s workspaceScreen) displayItems() (queue, search, upgradeable, installed []workspaceItem) {
	queue = s.installQueue

	// Search results: exclude items already in install queue or installed.
	installedKeys := make(map[string]bool, len(s.items))
	for _, item := range s.items {
		installedKeys[item.key()] = true
	}
	queueKeys := make(map[string]bool, len(s.installQueue))
	for _, item := range s.installQueue {
		queueKeys[item.key()] = true
	}
	for _, pkg := range s.searchResults {
		k := packageSourceKey(pkg.ID, pkg.Source)
		if !installedKeys[k] && !queueKeys[k] {
			search = append(search, workspaceItem{
				pkg:       pkg,
				installed: "",
				available: pkg.Version,
			})
		}
	}

	// Split existing items into upgradeable and installed.
	base := s.filteredItems()
	for _, item := range base {
		if item.upgradeable {
			upgradeable = append(upgradeable, item)
		} else {
			installed = append(installed, item)
		}
	}
	return
}

func (s workspaceScreen) openDetail() (screen, tea.Cmd) {
	queue, search, upgradeable, installed := s.displayItems()
	all := concat(queue, search, upgradeable, installed)
	if s.cursor >= len(all) {
		return s, nil
	}
	item := all[s.cursor]
	if !canFetchDetails(item.pkg.Source) {
		return s, nil
	}
	var cmd tea.Cmd
	allowVersions := item.upgradeable || canFetchDetails(item.pkg.Source)
	s.detail, cmd = s.detail.showWithVersion(item.pkg, "", allowVersions)
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
	var targets []workspaceItem

	if action == "install" {
		// Install uses the install queue.
		targets = append(targets, s.installQueue...)
	} else {
		// Collect selected items that match the action.
		for _, item := range s.items {
			if !s.selected[item.key()] {
				continue
			}
			if action == "upgrade" && !item.upgradeable {
				continue
			}
			targets = append(targets, item)
		}
	}

	// If nothing selected, act on the focused item.
	if len(targets) == 0 && action != "install" {
		queue, search, upgradeable, installed := s.displayItems()
		all := concat(queue, search, upgradeable, installed)
		if s.cursor < len(all) {
			item := all[s.cursor]
			if action == "upgrade" && !item.upgradeable {
				return s, nil
			}
			targets = []workspaceItem{item}
		}
	}

	if len(targets) == 0 {
		return s, nil
	}

	items := make([]batchItem, len(targets))
	for i, t := range targets {
		items[i] = batchItem{item: t, status: batchQueued}
	}
	m := newExecModal(action, items)
	s.modal = &m
	s.state = workspaceConfirm
	return s, nil
}

type startWorkspaceBatchMsg struct{}

func (s workspaceScreen) startBatch() (screen, tea.Cmd) {
	// Fresh context for each batch so a previous Esc cancel doesn't poison it.
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel

	s.state = workspaceExecuting
	s.modal.phase = execPhaseRunning
	s.modal.idx = 0
	s.exec.reset()
	return s, tea.Batch(
		s.modal.spinner.Tick,
		func() tea.Msg { return startWorkspaceBatchMsg{} },
	)
}

func (s workspaceScreen) processNextBatchItem() (screen, tea.Cmd) {
	if s.modal.idx >= len(s.modal.items) {
		return s.finishBatch()
	}

	s.modal.items[s.modal.idx].status = batchRunning
	item := s.modal.items[s.modal.idx].item
	s.exec.appendSection(fmt.Sprintf("== %s (%s) ==", item.pkg.Name, item.pkg.ID))

	var outChan <-chan string
	var errChan <-chan error

	if s.modal.forceElevated {
		// Route through elevated helper for Ctrl+E retry.
		var initErr error
		switch s.modal.action {
		case "upgrade":
			version := s.selectedVersions[item.key()]
			_, outChan, errChan, initErr = upgradePackageElevatedStreamCtx(item.pkg.ID, item.pkg.Source, version)
		case "install":
			version := s.selectedVersions[item.key()]
			_, outChan, errChan, initErr = installPackageElevatedStreamCtx(item.pkg.ID, item.pkg.Source, version)
		default: // uninstall
			_, outChan, errChan, initErr = uninstallPackageElevatedStreamCtx(item.pkg)
		}
		if initErr != nil {
			s.modal.items[s.modal.idx].status = batchFailed
			s.modal.items[s.modal.idx].err = fmt.Errorf("elevation failed: %v", initErr)
			s.modal.idx++
			return s.processNextBatchItem()
		}
	} else {
		switch s.modal.action {
		case "upgrade":
			version := s.selectedVersions[item.key()]
			_, outChan, errChan = upgradePackageStreamCtx(s.ctx, item.pkg.ID, item.pkg.Source, version)
		case "install":
			version := s.selectedVersions[item.key()]
			_, outChan, errChan = installPackageStreamCtx(s.ctx, item.pkg.ID, item.pkg.Source, version)
		default: // uninstall
			_, outChan, errChan = uninstallPackageStreamCtx(s.ctx, item.pkg)
		}
	}
	s.streamOut = outChan
	s.streamErr = errChan

	return s, tea.Batch(s.modal.spinner.Tick, awaitStream(nil, s.streamOut, s.streamErr))
}

func (s workspaceScreen) finishBatch() (screen, tea.Cmd) {
	s.modal.phase = execPhaseComplete
	s.selected = make(map[string]bool)
	if s.modal.action == "install" {
		s.installQueue = nil
		s.installQueueMap = make(map[string]bool)
		s.searchResults = nil
		s.searchQuery = ""
	}
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
	case workspaceConfirm, workspaceExecuting:
		if s.modal != nil {
			return s.modal.view(width, height)
		}
		return s.viewReady(width, height)
	}
	return ""
}

func (s workspaceScreen) viewLoading() string {
	return "  " + s.spinner.View() + " Loading packages...\n"
}

func (s workspaceScreen) viewReady(width, height int) string {
	l := computeLayout(width, height, true)

	// Render list panel using all sections.
	listView := s.renderSections(l)

	if !l.hasDetail {
		return listView
	}

	// Render detail panel.
	sp := s.summary
	sp.setSize(l.detail.W, l.detail.H)
	detailView := sp.view()

	return lipgloss.JoinHorizontal(lipgloss.Top, listView, " ", detailView)
}

// sectionDef describes one panel section for rendering.
type sectionDef struct {
	title  string
	items  []workspaceItem
	offset int // global cursor offset for this section
	fixedH int // 0 = flexible, >0 = fixed height (for small sections)
}

func (s workspaceScreen) renderSections(l layout) string {
	panelWidth := l.list.W
	innerWidth := max(panelWidth-2, 10)

	queue, search, upgradeable, installed := s.displayItems()

	var b strings.Builder

	// Search input bar or filter bar.
	if s.searchActive {
		b.WriteString("  " + s.searchInput.View() + "\n")
	} else if s.searchLoading {
		b.WriteString("  " + s.spinner.View() + " Searching...\n")
	} else if s.filter.active {
		b.WriteString("  " + s.filter.input.View() + "\n")
	} else if s.filter.query != "" {
		b.WriteString("  " + helpStyle.Render("Filter: "+s.filter.query+"  (/ edit • esc clear)") + "\n")
	}

	availableH := l.list.H
	if s.searchActive || s.searchLoading || s.filter.active || s.filter.query != "" {
		availableH--
	}

	// Build section definitions with global offsets.
	var sections []sectionDef
	offset := 0

	if len(queue) > 0 {
		title := fmt.Sprintf("Selected for Install (%d)", len(queue))
		sections = append(sections, sectionDef{title: title, items: queue, offset: offset, fixedH: len(queue) + 2})
		offset += len(queue)
	}
	if len(search) > 0 {
		title := fmt.Sprintf("Search Results (%d)", len(search))
		sections = append(sections, sectionDef{title: title, items: search, offset: offset})
		offset += len(search)
	}
	if len(upgradeable) > 0 {
		selCount := s.countSelected(upgradeable)
		title := fmt.Sprintf("Updates Available (%d)", len(upgradeable))
		if selCount > 0 {
			title = fmt.Sprintf("Updates Available (%d / %d selected)", len(upgradeable), selCount)
		}
		sections = append(sections, sectionDef{title: title, items: upgradeable, offset: offset, fixedH: len(upgradeable) + 2})
		offset += len(upgradeable)
	}
	if len(installed) > 0 {
		selCount := s.countSelected(installed)
		title := fmt.Sprintf("Installed (%d)", len(installed))
		if selCount > 0 {
			title = fmt.Sprintf("Installed (%d / %d selected)", len(installed), selCount)
		}
		sections = append(sections, sectionDef{title: title, items: installed, offset: offset})
		offset += len(installed)
	}

	if len(sections) == 0 {
		return "  " + helpStyle.Render("No packages found.") + "\n"
	}

	// Allocate heights: fixed sections get their requested height,
	// flexible sections split the remainder.
	fixedTotal := 0
	flexCount := 0
	for _, sec := range sections {
		if sec.fixedH > 0 {
			fixedTotal += sec.fixedH + 1 // +1 for gap
		} else {
			flexCount++
		}
	}
	flexH := 0
	if flexCount > 0 {
		flexH = max((availableH-fixedTotal)/flexCount, 4)
	}

	// Determine which section has the cursor.
	cursorSection := -1
	for i, sec := range sections {
		if s.cursor >= sec.offset && s.cursor < sec.offset+len(sec.items) {
			cursorSection = i
			break
		}
	}

	// Render each section.
	for i, sec := range sections {
		panelH := sec.fixedH
		if panelH == 0 {
			panelH = flexH
		}
		innerH := max(panelH-2, 1) // minus top+bottom border

		borderColor := dim
		if i == cursorSection {
			borderColor = accent
		}

		content := s.renderPanelItems(sec.items, sec.offset, innerH, innerWidth)
		b.WriteString(renderTitledPanel(sec.title, content, panelWidth, innerH, borderColor))
		if i < len(sections)-1 {
			b.WriteString("\n")
		}
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
		isCursor := globalIdx == s.cursor
		// Show batch status icon if this package is in a batch,
		// or install queue selection, or regular selection.
		var sel string
		if s.modal != nil && s.modal.itemMap != nil {
			if bi, ok := s.modal.itemMap[item.key()]; ok {
				sel = " " + bi.statusIcon(s.modal.spinner) + " "
			} else {
				sel = checkbox(s.selected[item.key()] || s.installQueueMap[item.key()])
			}
		} else {
			sel = checkbox(s.selected[item.key()] || s.installQueueMap[item.key()])
		}
		row := cursor + sel + " " + s.renderItemText(item, innerWidth, isCursor)
		lines = append(lines, row)
	}

	return strings.Join(lines, "\n")
}

func (s workspaceScreen) renderItemText(item workspaceItem, _ int, isCursor bool) string {
	nameStyle := itemStyle // white
	if isCursor {
		nameStyle = itemActiveStyle // bold pink
	}
	name := nameStyle.Render(item.pkg.Name)

	// Check for a custom selected version.
	customVer := s.selectedVersions[item.key()]

	if item.upgradeable {
		ver := item.installed + " → " + item.available
		if customVer != "" && customVer != item.available {
			ver = item.installed + " → " + customVer
		}
		return name + "  " + helpStyle.Render(ver)
	}

	// Install queue or search result: show version (custom or available).
	if item.installed == "" {
		ver := item.available
		if customVer != "" {
			ver = customVer
		}
		if ver != "" {
			return name + "  " + helpStyle.Render(ver)
		}
		return name
	}

	// Regular installed package.
	return name + "  " + helpStyle.Render(item.installed)
}

// viewBatchList renders the batch items with inline status icons.
// Used for both executing and done states.
// renderResultModal renders a scrollable modal with batch execution results.

// ── Help keys ─────────────────────────────────────────────────────

func (s workspaceScreen) helpKeys() []key.Binding {
	if s.detail.visible() {
		return s.detail.helpKeys()
	}
	if s.modal != nil {
		return s.modal.helpKeys()
	}
	if s.state != workspaceReady {
		return nil
	}
	// Compact help — most important keys + ? for full help.
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "select")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "search remote")),
		key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall")),
	}
	if len(s.installQueue) > 0 {
		bindings = append(bindings, key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install queued")))
	}
	bindings = append(bindings, key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")))
	return bindings
}

// fullHelpKeys returns grouped keybindings for the full help view.
func (s workspaceScreen) fullHelpKeys() [][]key.Binding {
	navigation := []key.Binding{
		key.NewBinding(key.WithKeys("↑/k"), key.WithHelp("↑/k", "move up")),
		key.NewBinding(key.WithKeys("↓/j"), key.WithHelp("↓/j", "move down")),
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "select package")),
		key.NewBinding(key.WithKeys("enter/→"), key.WithHelp("enter/→", "open details")),
		key.NewBinding(key.WithKeys("←/esc"), key.WithHelp("←/esc", "close details")),
	}
	actions := []key.Binding{
		key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade selected")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall selected")),
		key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install queued")),
		key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all updates")),
	}
	search := []key.Binding{
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "search remote packages")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter current list")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh packages")),
	}
	general := []key.Binding{
		key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "pick version (in details)")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open homepage (in details)")),
		key.NewBinding(key.WithKeys("1-4"), key.WithHelp("1-4", "switch tabs")),
		key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
	return [][]key.Binding{navigation, actions, search, general}
}

func (s workspaceScreen) blocksGlobalShortcuts() bool {
	return s.modal != nil || s.detail.visible() || s.filter.active ||
		s.searchActive || s.state == workspaceExecuting || s.state == workspaceConfirm
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
