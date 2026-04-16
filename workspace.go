package main

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

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
	action retryOp   // retryOpInstall, retryOpUpgrade, retryOpUninstall
	pkg    Package   // the targeted package
	result []Package // result of winget list --id --exact (0 or 1 entries)
	err    error
}

// startBackgroundRefreshMsg triggers a background refresh (used after post-action delay).
type startBackgroundRefreshMsg struct{}

type selfUpgradeScheduledMsg struct {
	err error
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
		case "esc":
			if s.searchResults != nil || s.searchQuery != "" {
				s.searchResults = nil
				s.searchQuery = ""
				s.cursor = 0
				return s, s.focusSummary()
			}
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
			return s.beginAction(retryOpInstall)
		case "g":
			return s.beginApplyAction()
		case "u":
			return s.beginAction(retryOpUpgrade)
		case "x", "delete":
			return s.beginAction(retryOpUninstall)
		case "p":
			return s.openDetailToOverrides()
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

	case streamProgressMsg:
		if s.state == workspaceExecuting && s.modal != nil && s.modal.phase == execPhaseRunning && s.modal.idx < len(s.modal.items) {
			s.modal.items[s.modal.idx].progress = int(msg)
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
			s.err = nil // clear any prior search error
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
		m := newExecModal(msg.req.Op, bi)
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
