package main

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

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

	hiddenUpgrades int // upgrades hidden by ignore rules

	// Background refresh.
	refreshing    bool      // background refresh in flight
	cacheAge      time.Time // when displayed data was last cached (zero = fresh)
	refreshCtx    context.Context
	refreshCancel context.CancelFunc

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
	refreshCtx, refreshCancel := context.WithCancel(context.Background())
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
		layout:           computeLayout(80, 24),
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
		refreshCtx:       refreshCtx,
		refreshCancel:    refreshCancel,
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
	fromDisk    bool      // true if loaded from disk cache
	savedAt     time.Time // when the disk cache was written
}

// backgroundRefreshMsg carries fresh data from a background refresh.
type backgroundRefreshMsg struct {
	installed   []Package
	upgradeable []Package
	err         error
}

// incrementalUpdateMsg carries a per-package verification result after an action.
type incrementalUpdateMsg struct {
	action string    // "install", "upgrade", "uninstall"
	pkg    Package   // the targeted package
	result []Package // result of winget list --id --exact (0 or 1 entries)
	err    error
}

// startBackgroundRefreshMsg triggers a background refresh (used after post-action delay).
type startBackgroundRefreshMsg struct{}

type selfUpgradeScheduledMsg struct {
	err error
}

func (s workspaceScreen) init() tea.Cmd {
	// Phase A: try in-memory cache (tab switches within a session).
	inst, instOK := cache.getInstalled()
	upgr, upgrOK := cache.getUpgradeable()
	if instOK && upgrOK {
		return func() tea.Msg {
			return workspaceDataMsg{installed: inst, upgradeable: upgr}
		}
	}

	// Phase B: try disk cache (app restart within 24h).
	diskInst, diskUpgr, savedAt, diskOK := cache.loadFromDisk()
	if diskOK {
		return func() tea.Msg {
			return workspaceDataMsg{
				installed:   diskInst,
				upgradeable: diskUpgr,
				fromDisk:    true,
				savedAt:     savedAt,
			}
		}
	}

	// Phase C: cold start — show spinner, fetch from winget.
	return tea.Batch(s.spinner.Tick, s.fetchPackages())
}

// fetchPackages runs parallel winget list + upgrade with sequential fallback.
func (s workspaceScreen) fetchPackages() tea.Cmd {
	return func() tea.Msg {
		type result struct {
			pkgs []Package
			err  error
		}
		instCh := make(chan result, 1)
		upgrCh := make(chan result, 1)

		go func() {
			pkgs, err := getInstalledCtx(s.ctx)
			instCh <- result{pkgs, err}
		}()
		go func() {
			pkgs, err := getUpgradeableCtx(s.ctx)
			upgrCh <- result{pkgs, err}
		}()

		instResult := <-instCh
		upgrResult := <-upgrCh

		// If parallel calls failed, retry sequentially.
		if instResult.err != nil {
			pkgs, err := getInstalledCtx(s.ctx)
			if err != nil {
				return workspaceDataMsg{err: err}
			}
			instResult = result{pkgs, nil}
		}
		if upgrResult.err != nil {
			pkgs, err := getUpgradeableCtx(s.ctx)
			if err == nil {
				upgrResult = result{pkgs, nil}
			}
		}

		cache.setInstalled(instResult.pkgs)
		if upgrResult.err == nil {
			cache.setUpgradeable(upgrResult.pkgs)
		}
		return workspaceDataMsg{
			installed:   instResult.pkgs,
			upgradeable: upgrResult.pkgs,
			err:         upgrResult.err,
		}
	}
}

// startBackgroundRefresh runs a full refresh in the background, returning backgroundRefreshMsg.
func (s workspaceScreen) startBackgroundRefresh() tea.Cmd {
	ctx := s.refreshCtx
	return func() tea.Msg {
		type result struct {
			pkgs []Package
			err  error
		}
		instCh := make(chan result, 1)
		upgrCh := make(chan result, 1)

		go func() {
			pkgs, err := getInstalledCtx(ctx)
			instCh <- result{pkgs, err}
		}()
		go func() {
			pkgs, err := getUpgradeableCtx(ctx)
			upgrCh <- result{pkgs, err}
		}()

		instResult := <-instCh
		upgrResult := <-upgrCh

		if instResult.err != nil {
			pkgs, err := getInstalledCtx(ctx)
			if err != nil {
				return backgroundRefreshMsg{err: err}
			}
			instResult = result{pkgs, nil}
		}
		if upgrResult.err != nil {
			pkgs, err := getUpgradeableCtx(ctx)
			if err == nil {
				upgrResult = result{pkgs, nil}
			}
		}

		cache.setInstalled(instResult.pkgs)
		if upgrResult.err == nil {
			cache.setUpgradeable(upgrResult.pkgs)
		}
		return backgroundRefreshMsg{
			installed:   instResult.pkgs,
			upgradeable: upgrResult.pkgs,
			err:         upgrResult.err,
		}
	}
}

// buildItems merges installed and upgradeable into a grouped list.
// Returns the items and the number of upgrades hidden by ignore rules.
func buildItems(installed, upgradeable []Package) ([]workspaceItem, int) {
	// Build upgrade lookup, filtering out ignored packages.
	hiddenCount := 0
	upgradeMap := make(map[string]Package, len(upgradeable))
	for _, pkg := range upgradeable {
		if appSettings.isIgnored(pkg.ID, pkg.Source, pkg.Available) {
			hiddenCount++
			continue
		}
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
		if !seen[k] && !appSettings.isIgnored(pkg.ID, pkg.Source, pkg.Available) {
			updates = append(updates, workspaceItem{
				pkg:         pkg,
				upgradeable: true,
				installed:   pkg.Version,
				available:   pkg.Available,
			})
		}
	}

	// Updates first, then installed.
	return append(updates, rest...), hiddenCount
}

func (s *workspaceScreen) rebuildItemsFromCache() {
	installed := cache.getInstalledRaw()
	upgradeable := cache.getUpgradeableRaw()
	if installed == nil && upgradeable == nil {
		return
	}
	var cursorKey string
	if s.cursor >= 0 && s.cursor < len(s.items) {
		cursorKey = s.items[s.cursor].key()
	}
	s.items, s.hiddenUpgrades = buildItems(installed, upgradeable)
	s.cursor = 0
	for i, item := range s.items {
		if item.key() == cursorKey {
			s.cursor = i
			break
		}
	}
}

// countUpgradeable returns how many items are upgradeable.

func (s workspaceScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.layout = computeLayout(msg.Width, contentAreaHeightForWindow(msg.Width, msg.Height, true))
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
				case "?":
					s.modal.showCommands = !s.modal.showCommands
					s.modal.scroll = 0
					return s, nil
				case "up", "k":
					if s.modal.scroll > 0 {
						s.modal.scroll--
					}
					return s, nil
				case "down", "j":
					m := s.modal.maxScroll(s.width, contentAreaHeightForWindow(s.width, s.height, true))
					if s.modal.scroll < m {
						s.modal.scroll++
					}
					return s, nil
				case "pgup":
					s.modal.scroll -= 8
					if s.modal.scroll < 0 {
						s.modal.scroll = 0
					}
					return s, nil
				case "pgdown":
					m := s.modal.maxScroll(s.width, contentAreaHeightForWindow(s.width, s.height, true))
					s.modal.scroll += 8
					if s.modal.scroll > m {
						s.modal.scroll = m
					}
					return s, nil
				case "home":
					s.modal.scroll = 0
					return s, nil
				case "end":
					s.modal.scroll = s.modal.maxScroll(s.width, contentAreaHeightForWindow(s.width, s.height, true))
					return s, nil
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
					if s.modal.hasPendingSelfUpgrade() {
						return s.schedulePendingSelfUpgrade()
					}
					result, cmd := s.dismissModalAndRefresh()
					return result, cmd
				case "esc":
					if s.modal.hasPendingSelfUpgrade() {
						result, cmd := s.dismissModalAndRefresh()
						return result, cmd
					}
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
		case "g":
			return s.beginApplyAction()
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
		if msg.fromDisk {
			cache.prime(msg.installed, msg.upgradeable, msg.savedAt)
		}
		nextSettings := appSettings
		if nextSettings.expireVersionIgnores(msg.upgradeable) {
			_ = persistSettings(nextSettings)
		}
		s.items, s.hiddenUpgrades = buildItems(msg.installed, msg.upgradeable)
		if len(s.items) == 0 {
			s.state = workspaceEmpty
		} else {
			s.state = workspaceReady
			s.cursor = 0
		}
		if msg.fromDisk {
			// Show stale data immediately, start background refresh.
			s.cacheAge = msg.savedAt
			s.refreshing = true
			return s, tea.Batch(s.focusSummary(), s.startBackgroundRefresh())
		}
		return s, s.focusSummary()

	case backgroundRefreshMsg:
		s.refreshing = false
		if msg.err != nil && msg.installed == nil {
			// Background refresh failed; keep stale data.
			return s, nil
		}
		// Don't overwrite state if a modal/execution is active — just update the data.
		// The state transition will happen when the modal is dismissed.
		nextSettings := appSettings
		if nextSettings.expireVersionIgnores(msg.upgradeable) {
			_ = persistSettings(nextSettings)
		}
		if s.state == workspaceConfirm || s.state == workspaceExecuting {
			s.items, s.hiddenUpgrades = buildItems(msg.installed, msg.upgradeable)
			s.cacheAge = time.Time{}
			return s, nil
		}
		// Preserve cursor position by key.
		var cursorKey string
		if s.cursor >= 0 && s.cursor < len(s.items) {
			cursorKey = s.items[s.cursor].key()
		}
		s.items, s.hiddenUpgrades = buildItems(msg.installed, msg.upgradeable)
		s.cacheAge = time.Time{} // data is now fresh
		if len(s.items) == 0 {
			s.state = workspaceEmpty
		} else {
			s.state = workspaceReady
			// Restore cursor.
			s.cursor = 0
			for i, item := range s.items {
				if item.key() == cursorKey {
					s.cursor = i
					break
				}
			}
		}
		return s, s.focusSummary()

	case incrementalUpdateMsg:
		// Skip stale results from a cancelled batch (e.g. after r refresh).
		if msg.err == nil && s.state != workspaceLoading {
			s.applyIncrementalUpdate(msg)
			return s, s.focusSummary()
		}
		return s, nil

	case startBackgroundRefreshMsg:
		if !s.refreshing {
			s.refreshing = true
			return s, s.startBackgroundRefresh()
		}
		return s, nil

	case selfUpgradeScheduledMsg:
		if msg.err != nil {
			if s.modal != nil {
				for i := range s.modal.items {
					if s.modal.items[i].status == batchPendingRestart {
						s.modal.items[i].status = batchFailed
						s.modal.items[i].err = fmt.Errorf("self-upgrade handoff failed: %v", msg.err)
						break
					}
				}
			}
			return s, nil
		}
		return s, tea.Quit

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

	case overridesSavedMsg:
		s.rebuildItemsFromCache()
		return s, nil

	case startWorkspaceBatchMsg:
		if s.state == workspaceExecuting {
			return s.processNextBatchItem()
		}

	case streamMsg:
		// Ignore stream messages if batch was cancelled.
		if s.state == workspaceExecuting && s.modal != nil && s.modal.phase == execPhaseRunning {
			s.exec.appendLine(string(msg))
			return s, awaitStream(nil, s.streamOut, s.streamErr)
		}

	case streamDoneMsg:
		// Don't advance if batch was cancelled (phase already set to complete).
		if s.modal != nil && s.modal.phase == execPhaseRunning && s.modal.idx < len(s.modal.items) {
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
			bi[i] = batchItem{action: msg.req.Op, item: t, status: batchQueued}
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
	if s.refreshCancel != nil {
		s.refreshCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	refreshCtx, refreshCancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	s.refreshCtx = refreshCtx
	s.refreshCancel = refreshCancel
	s.modal = nil
	s.state = workspaceLoading
	s.items = nil
	s.cursor = 0
	s.err = nil
	s.refreshing = false
	s.cacheAge = time.Time{}
	s.exec.reset()
	cache.invalidate()
	cache.deleteDiskCache()
	return *s, s.init()
}

// applyIncrementalUpdate patches the item list in-place after a single package action.
func (s *workspaceScreen) applyIncrementalUpdate(msg incrementalUpdateMsg) {
	key := packageSourceKey(msg.pkg.ID, msg.pkg.Source)

	switch msg.action {
	case "install":
		if len(msg.result) > 0 {
			pkg := msg.result[0]
			newItem := workspaceItem{pkg: pkg, installed: pkg.Version}
			if pkg.Available != "" {
				newItem.upgradeable = true
				newItem.available = pkg.Available
			}
			s.items = append(s.items, newItem)
		}
	case "upgrade":
		for i, item := range s.items {
			if item.key() == key {
				if len(msg.result) > 0 {
					s.items[i].pkg.Version = msg.result[0].Version
					s.items[i].installed = msg.result[0].Version
				}
				s.items[i].upgradeable = false
				s.items[i].available = ""
				s.items[i].pkg.Available = ""
				break
			}
		}
	case "uninstall":
		if len(msg.result) == 0 {
			// Confirmed removed — filter out.
			newItems := make([]workspaceItem, 0, len(s.items))
			for _, item := range s.items {
				if item.key() != key {
					newItems = append(newItems, item)
				}
			}
			s.items = newItems
			if s.cursor >= len(s.items) {
				s.cursor = max(len(s.items)-1, 0)
			}
		}
	}

	// Sync in-memory + disk cache from current items.
	var installed, upgradeable []Package
	for _, item := range s.items {
		installed = append(installed, item.pkg)
		if item.upgradeable {
			upgradeable = append(upgradeable, item.pkg)
		}
	}
	cache.setInstalled(installed)
	cache.setUpgradeable(upgradeable)
}

// dismissModalAndRefresh closes the completion modal and starts a background refresh.
func (s *workspaceScreen) dismissModalAndRefresh() (workspaceScreen, tea.Cmd) {
	s.modal = nil
	s.state = workspaceReady
	if len(s.items) == 0 {
		s.state = workspaceEmpty
	}
	// Start a background refresh for eventual consistency.
	if s.refreshCancel != nil {
		s.refreshCancel()
	}
	refreshCtx, refreshCancel := context.WithCancel(context.Background())
	s.refreshCtx = refreshCtx
	s.refreshCancel = refreshCancel
	s.refreshing = true
	return *s, s.startBackgroundRefresh()
}

func (s workspaceScreen) schedulePendingSelfUpgrade() (screen, tea.Cmd) {
	if s.modal == nil {
		return s, nil
	}
	item, ok := s.modal.pendingSelfUpgradeItem()
	if !ok {
		return s, nil
	}

	version := s.selectedVersions[item.item.key()]
	source := item.item.pkg.Source
	return s, func() tea.Msg {
		return selfUpgradeScheduledMsg{
			err: startSelfUpgradeHandoff(source, version),
		}
	}
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

func (s workspaceScreen) focusedItemContext() (workspaceItem, string, bool) {
	queue, search, upgradeable, installed := s.displayItems()
	all := concat(queue, search, upgradeable, installed)
	if s.cursor < 0 || s.cursor >= len(all) {
		return workspaceItem{}, "", false
	}

	item := all[s.cursor]
	switch {
	case s.cursor < len(queue):
		return item, "queue", true
	case s.cursor < len(queue)+len(search):
		return item, "search", true
	case s.cursor < len(queue)+len(search)+len(upgradeable):
		return item, "upgrade", true
	default:
		return item, "installed", true
	}
}

func newBatchItem(action retryOp, item workspaceItem) batchItem {
	return batchItem{action: action, item: item, status: batchQueued}
}

func batchContainsAction(items []batchItem, action retryOp) bool {
	for _, item := range items {
		if item.action == action {
			return true
		}
	}
	return false
}

func batchModalAction(items []batchItem) string {
	if len(items) == 0 {
		return ""
	}
	action := items[0].action
	for _, item := range items[1:] {
		if item.action != action {
			return "apply"
		}
	}
	return string(action)
}

func sortBatchItems(items []batchItem) []batchItem {
	var regular []batchItem
	var self []batchItem
	for _, item := range items {
		if isSelfUpgradeBatchItem(item) {
			self = append(self, item)
			continue
		}
		regular = append(regular, item)
	}
	return append(regular, self...)
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

func (s workspaceScreen) openBatchModal(items []batchItem) (screen, tea.Cmd) {
	if len(items) == 0 {
		return s, nil
	}
	for i := range items {
		items[i].command = s.batchItemCommandPreview(items[i])
	}
	items = sortBatchItems(items)
	m := newExecModal(batchModalAction(items), items)
	s.modal = &m
	s.state = workspaceConfirm
	return s, nil
}

// batchItemCommandPreview renders the winget command that will run for a
// batch item, using the currently selected version (if any).
func (s workspaceScreen) batchItemCommandPreview(bi batchItem) string {
	version := s.selectedVersions[bi.item.key()]
	var args []string
	switch bi.action {
	case retryOpUpgrade:
		args = upgradeCommandArgs(bi.item.pkg.ID, bi.item.pkg.Source, version)
	case retryOpInstall:
		args = installCommandArgs(bi.item.pkg.ID, bi.item.pkg.Source, version)
	case retryOpUninstall:
		args = uninstallCommandArgs(bi.item.pkg, appSettings.PurgeOnUninstall)
	default:
		return ""
	}
	return formatWingetCommand(args)
}

func (s workspaceScreen) beginAction(action string) (screen, tea.Cmd) {
	var items []batchItem

	if action == "install" {
		// Install uses the install queue.
		for _, item := range s.installQueue {
			items = append(items, newBatchItem(retryOpInstall, item))
		}
	} else {
		// Collect selected items that match the action.
		for _, item := range s.items {
			if !s.selected[item.key()] {
				continue
			}
			if action == "upgrade" && !item.upgradeable {
				continue
			}
			items = append(items, newBatchItem(retryOp(action), item))
		}
	}

	// If nothing is staged, fall back to the focused item when it matches.
	if len(items) == 0 {
		item, section, ok := s.focusedItemContext()
		if ok {
			switch action {
			case "install":
				if section == "queue" || section == "search" {
					items = append(items, newBatchItem(retryOpInstall, item))
				}
			case "upgrade":
				if item.upgradeable {
					items = append(items, newBatchItem(retryOpUpgrade, item))
				}
			case "uninstall":
				if section != "queue" && section != "search" {
					items = append(items, newBatchItem(retryOpUninstall, item))
				}
			}
		}
	}

	return s.openBatchModal(items)
}

func (s workspaceScreen) beginApplyAction() (screen, tea.Cmd) {
	var items []batchItem

	for _, item := range s.installQueue {
		items = append(items, newBatchItem(retryOpInstall, item))
	}
	for _, item := range s.items {
		if !s.selected[item.key()] {
			continue
		}
		action := retryOpUninstall
		if item.upgradeable {
			action = retryOpUpgrade
		}
		items = append(items, newBatchItem(action, item))
	}

	if len(items) == 0 {
		item, section, ok := s.focusedItemContext()
		if !ok {
			return s, nil
		}
		switch section {
		case "queue", "search":
			items = append(items, newBatchItem(retryOpInstall, item))
		case "upgrade":
			items = append(items, newBatchItem(retryOpUpgrade, item))
		case "installed":
			items = append(items, newBatchItem(retryOpUninstall, item))
		}
	}

	return s.openBatchModal(items)
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

	current := &s.modal.items[s.modal.idx]
	item := current.item
	s.exec.appendSection(fmt.Sprintf("== %s %s (%s) ==", actionTitle(current.action), item.pkg.Name, item.pkg.ID))

	if isSelfUpgradeBatchItem(*current) {
		current.status = batchPendingRestart
		current.output = "WinTUI will close now. A temporary helper will finish the upgrade and reopen WinTUI in a new window."
		s.exec.appendLine("WinTUI will close now. A temporary helper will finish the upgrade and reopen WinTUI in a new window.")
		s.modal.idx++
		return s.processNextBatchItem()
	}

	current.status = batchRunning

	var outChan <-chan string
	var errChan <-chan error

	if s.modal.forceElevated {
		// Route through elevated helper for Ctrl+E retry.
		var initErr error
		switch current.action {
		case retryOpUpgrade:
			version := s.selectedVersions[item.key()]
			_, outChan, errChan, initErr = upgradePackageElevatedStreamCtx(item.pkg.ID, item.pkg.Source, version)
		case retryOpInstall:
			version := s.selectedVersions[item.key()]
			_, outChan, errChan, initErr = installPackageElevatedStreamCtx(item.pkg.ID, item.pkg.Source, version)
		default: // uninstall
			_, outChan, errChan, initErr = uninstallPackageElevatedStreamCtx(item.pkg)
		}
		if initErr != nil {
			current.status = batchFailed
			current.err = fmt.Errorf("elevation failed: %v", initErr)
			s.modal.idx++
			return s.processNextBatchItem()
		}
	} else {
		switch current.action {
		case retryOpUpgrade:
			version := s.selectedVersions[item.key()]
			_, outChan, errChan = upgradePackageStreamCtx(s.ctx, item.pkg.ID, item.pkg.Source, version)
		case retryOpInstall:
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
	if batchContainsAction(s.modal.items, retryOpInstall) {
		s.installQueue = nil
		s.installQueueMap = make(map[string]bool)
		s.searchResults = nil
		s.searchQuery = ""
	}

	// Emit incremental lookups for each successful item, using s.ctx so they
	// are cancelled if the user does a manual refresh (resetAndReload).
	ctx := s.ctx
	var cmds []tea.Cmd
	for _, bi := range s.modal.items {
		if bi.status == batchDone {
			pkg := bi.item.pkg
			act := string(bi.action)
			cmds = append(cmds, func() tea.Msg {
				result, err := lookupSinglePackageCtx(ctx, pkg.ID, pkg.Source)
				return incrementalUpdateMsg{action: act, pkg: pkg, result: result, err: err}
			})
		}
	}
	return s, tea.Batch(cmds...)
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
		return "  " + helpStyle.Render("No packages found. Press s to search or r to refresh.") + "\n"
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

// cacheStatusText returns a dim status string like "cached 2h ago · refreshing...".
func (s workspaceScreen) cacheStatusText() string {
	if s.cacheAge.IsZero() && !s.refreshing {
		return ""
	}
	var parts []string
	if !s.cacheAge.IsZero() {
		parts = append(parts, "cached "+humanDuration(time.Since(s.cacheAge))+" ago")
	}
	if s.refreshing {
		parts = append(parts, "refreshing...")
	}
	return strings.Join(parts, " · ")
}

// upgradeCount returns the number of upgradeable items in the current list.
func (s workspaceScreen) upgradeCount() int {
	n := 0
	for _, item := range s.items {
		if item.upgradeable {
			n++
		}
	}
	return n
}

func (s workspaceScreen) viewReady(width, height int) string {
	l := computeLayout(width, height)

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
	title    string
	items    []workspaceItem
	offset   int // global cursor offset for this section
	desiredH int // preferred panel height including borders
	minH     int // minimum usable panel height including borders
	exact    bool
}

const minScrollableSectionHeight = 6 // border(2) + at least 4 visible rows

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
	} else if s.err != nil {
		b.WriteString("  " + errorStyle.Render("Search error: "+s.err.Error()) + "  " + helpStyle.Render("(s to search again)") + "\n")
	} else if s.searchQuery != "" && len(s.searchResults) == 0 && len(s.installQueue) == 0 {
		b.WriteString("  " + helpStyle.Render(fmt.Sprintf("No results for %q. Press s to search again.", s.searchQuery)) + "\n")
	} else if s.filter.active {
		b.WriteString("  " + s.filter.input.View() + "\n")
	} else if s.filter.query != "" {
		b.WriteString("  " + helpStyle.Render("Filter: "+s.filter.query+"  (/ edit • esc clear)") + "\n")
	}

	availableH := l.list.H
	hasTopBar := s.searchActive || s.searchLoading || s.err != nil ||
		(s.searchQuery != "" && len(s.searchResults) == 0 && len(s.installQueue) == 0) ||
		s.filter.active || s.filter.query != ""
	if hasTopBar {
		availableH--
	}

	// Build section definitions with global offsets.
	var sections []sectionDef
	offset := 0

	if len(queue) > 0 {
		title := fmt.Sprintf("Install Queue (%d)", len(queue))
		sections = append(sections, sectionDef{
			title:    title,
			items:    queue,
			offset:   offset,
			desiredH: len(queue) + 2,
			minH:     3,
			exact:    true,
		})
		offset += len(queue)
	}
	if len(search) > 0 {
		title := fmt.Sprintf("Search Results (%d)", len(search))
		sections = append(sections, sectionDef{
			title:    title,
			items:    search,
			offset:   offset,
			desiredH: len(search) + 2,
			minH:     minScrollableSectionHeight,
		})
		offset += len(search)
	}
	if len(upgradeable) > 0 {
		selCount := s.countSelected(upgradeable)
		title := fmt.Sprintf("Updates Available (%d)", len(upgradeable))
		if selCount > 0 {
			title = fmt.Sprintf("Updates Available (%d / %d selected)", len(upgradeable), selCount)
		}
		if s.hiddenUpgrades > 0 {
			title += fmt.Sprintf(" · %d hidden", s.hiddenUpgrades)
		}
		sections = append(sections, sectionDef{
			title:    title,
			items:    upgradeable,
			offset:   offset,
			desiredH: len(upgradeable) + 2,
			minH:     minScrollableSectionHeight,
		})
		offset += len(upgradeable)
	}
	if len(upgradeable) == 0 && s.hiddenUpgrades > 0 {
		sections = append(sections, sectionDef{
			title:    fmt.Sprintf("Updates Available (0 · %d hidden)", s.hiddenUpgrades),
			desiredH: 2,
			minH:     2,
			offset:   offset,
		})
	}
	if len(installed) > 0 {
		selCount := s.countSelected(installed)
		title := fmt.Sprintf("Installed (%d)", len(installed))
		if selCount > 0 {
			title = fmt.Sprintf("Installed (%d / %d selected)", len(installed), selCount)
		}
		sections = append(sections, sectionDef{
			title:    title,
			items:    installed,
			offset:   offset,
			desiredH: len(installed) + 2,
			minH:     minScrollableSectionHeight,
		})
		offset += len(installed)
	}

	// Cache status indicator (rendered as a line above the sections).
	if status := s.cacheStatusText(); status != "" {
		b.WriteString("  " + helpStyle.Render(status) + "\n")
		availableH--
	}

	if len(sections) == 0 {
		msg := "No packages found."
		if s.filter.query != "" {
			msg = fmt.Sprintf("No matches for %q. Press esc to clear filter.", s.filter.query)
		}
		b.WriteString("  " + helpStyle.Render(msg) + "\n")
		return b.String()
	}

	sectionHeights := allocateSectionHeights(sections, availableH)

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
		panelH := sectionHeights[i]
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

func allocateSectionHeights(sections []sectionDef, availableH int) []int {
	if len(sections) == 0 {
		return nil
	}

	panelBudget := max(availableH, 0)
	if panelBudget == 0 {
		return make([]int, len(sections))
	}

	heights := make([]int, len(sections))
	used := 0
	for i, sec := range sections {
		h := max(sec.minH, 3)
		if sec.exact {
			h = max(min(sec.desiredH, panelBudget), 3)
		}
		heights[i] = h
		used += h
	}

	// If the preferred minimums do not fit, shrink from the bottom up while
	// keeping at least a 1-line interior (3 total rows with borders).
	for used > panelBudget {
		shrunk := false
		for i := len(heights) - 1; i >= 0 && used > panelBudget; i-- {
			if heights[i] <= 3 {
				continue
			}
			heights[i]--
			used--
			shrunk = true
		}
		if !shrunk {
			break
		}
	}

	remaining := panelBudget - used

	// First, grow sections toward their preferred content height.
	for remaining > 0 {
		progressed := false
		for i, sec := range sections {
			target := max(sec.desiredH, heights[i])
			if heights[i] >= target {
				continue
			}
			heights[i]++
			remaining--
			progressed = true
			if remaining == 0 {
				break
			}
		}
		if !progressed {
			break
		}
	}

	// Any leftover space should keep the last flexible section stretched to the
	// bottom so the left column uses all available height.
	if remaining > 0 {
		target := len(heights) - 1
		for i := len(sections) - 1; i >= 0; i-- {
			if !sections[i].exact {
				target = i
				break
			}
		}
		heights[target] += remaining
	}

	return heights
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

	// Determine which section the cursor is in.
	queue, search, upgradeable, _ := s.displayItems()
	nQueue := len(queue)
	nSearch := len(search)
	nUpgradeable := len(upgradeable)

	inQueue := s.cursor < nQueue
	inSearch := s.cursor >= nQueue && s.cursor < nQueue+nSearch
	inUpgrades := s.cursor >= nQueue+nSearch && s.cursor < nQueue+nSearch+nUpgradeable

	// Context-sensitive compact help.
	var bindings []key.Binding

	switch {
	case inQueue:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "remove")),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "apply")),
			key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install")),
		}
	case inSearch:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "queue")),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "apply")),
			key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install")),
		}
	case inUpgrades:
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "stage")),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "apply")),
			key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade")),
			key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall")),
		}
	default: // installed
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "stage")),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "apply")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
			key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall")),
		}
	}

	bindings = append(bindings, key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")))
	return bindings
}

// fullHelpKeys returns grouped keybindings for the full help view.
func (s workspaceScreen) fullHelpKeys() [][]key.Binding {
	navigation := []key.Binding{
		key.NewBinding(key.WithKeys("↑/k"), key.WithHelp("↑/k", "move up")),
		key.NewBinding(key.WithKeys("↓/j"), key.WithHelp("↓/j", "move down")),
		key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "stage focused")),
		key.NewBinding(key.WithKeys("enter/→"), key.WithHelp("enter/→", "open details")),
		key.NewBinding(key.WithKeys("←/esc"), key.WithHelp("←/esc", "close details")),
	}
	actions := []key.Binding{
		key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "apply staged changes")),
		key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "install queued/focused")),
		key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "upgrade selected/focused")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall selected/focused")),
		key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "stage all updates")),
	}
	search := []key.Binding{
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "search for apps")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter current list")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
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
