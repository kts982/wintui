package main

import (
	tea "charm.land/bubbletea/v2"
)

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

// listPageStep controls how many rows PgUp / PgDn jump in the package list.
const listPageStep = 10

// cursorSectionBounds returns the [start, end) index range of the displayed
// section currently containing the cursor. If the displayed list is empty,
// returns (0, 0).
func (s workspaceScreen) cursorSectionBounds() (start, end int) {
	q, sr, up, ins := s.displayItems()
	nQ, nS, nU, nI := len(q), len(sr), len(up), len(ins)
	switch {
	case s.cursor < nQ:
		return 0, nQ
	case s.cursor < nQ+nS:
		return nQ, nQ + nS
	case s.cursor < nQ+nS+nU:
		return nQ + nS, nQ + nS + nU
	case s.cursor < nQ+nS+nU+nI:
		return nQ + nS + nU, nQ + nS + nU + nI
	}
	return 0, 0
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

func batchModalAction(items []batchItem) retryOp {
	if len(items) == 0 {
		return ""
	}
	action := items[0].action
	for _, item := range items[1:] {
		if item.action != action {
			return retryOpApply
		}
	}
	return action
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

func (s workspaceScreen) openDetailToOverrides() (screen, tea.Cmd) {
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
	s.detail.editingOverrides = true
	s.detail.overrideCursor = 0
	s.detail.overrideEdit = appSettings.getOverride(item.pkg.ID, item.pkg.Source)
	s.detail.overrideSaved = false
	s.detail.overrideDeleted = false
	s.detail.overrideErrMsg = ""
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
		args = uninstallCommandArgs(bi.item.pkg, appSettings.PurgeOnUninstall, bi.allVersions)
	default:
		return ""
	}
	return formatWingetCommand(args)
}

func (s workspaceScreen) beginAction(action retryOp) (screen, tea.Cmd) {
	var items []batchItem

	if action == retryOpInstall {
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
			if action == retryOpUpgrade && !item.upgradeable {
				continue
			}
			items = append(items, newBatchItem(action, item))
		}
	}

	// If nothing is staged, fall back to the focused item when it matches.
	if len(items) == 0 {
		item, section, ok := s.focusedItemContext()
		if ok {
			switch action {
			case retryOpInstall:
				if section == "queue" || section == "search" {
					items = append(items, newBatchItem(retryOpInstall, item))
				}
			case retryOpUpgrade:
				if item.upgradeable {
					items = append(items, newBatchItem(retryOpUpgrade, item))
				}
			case retryOpUninstall:
				if section != "queue" && section != "search" {
					items = append(items, newBatchItem(retryOpUninstall, item))
				}
			}
		}
	}

	items = deduplicateUninstallItems(items)
	return s.openBatchModal(items)
}

// deduplicateUninstallItems collapses uninstall batch items that share the
// same package key into a single item with allVersions=true. This handles
// packages that have multiple versions installed side-by-side.
func deduplicateUninstallItems(items []batchItem) []batchItem {
	seen := make(map[string]int, len(items)) // key → index in result
	var result []batchItem
	for _, bi := range items {
		if bi.action != retryOpUninstall {
			result = append(result, bi)
			continue
		}
		k := bi.item.key()
		if idx, ok := seen[k]; ok {
			result[idx].allVersions = true
		} else {
			seen[k] = len(result)
			result = append(result, bi)
		}
	}
	return result
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

	items = deduplicateUninstallItems(items)
	return s.openBatchModal(items)
}
