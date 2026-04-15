package main

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

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
	case retryOpInstall:
		if len(msg.result) > 0 {
			pkg := msg.result[0]
			newItem := workspaceItem{pkg: pkg, installed: pkg.Version}
			if pkg.Available != "" {
				newItem.upgradeable = true
				newItem.available = pkg.Available
			}
			s.items = append(s.items, newItem)
		}
	case retryOpUpgrade:
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
	case retryOpUninstall:
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
			act := bi.action
			cmds = append(cmds, func() tea.Msg {
				result, err := lookupSinglePackageCtx(ctx, pkg.ID, pkg.Source)
				return incrementalUpdateMsg{action: act, pkg: pkg, result: result, err: err}
			})
		}
	}
	return s, tea.Batch(cmds...)
}
