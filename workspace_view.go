package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"
)

// ── View ──────────────────────────────────────────────────────────

func (s workspaceScreen) view(width, height int) string {
	// Import overlay takes over the entire content area while active.
	if s.importer.active {
		return s.importer.view(width, height)
	}
	// Detail overlay takes over the full content area.
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Package Details") + "\n\n" +
			s.detail.view(width, height-2)
	}
	if s.modal != nil {
		return s.modal.view(width, height)
	}

	switch s.state {
	case workspaceLoading:
		return s.viewLoading()
	case workspaceEmpty:
		return "  " + helpStyle.Render("No packages found. Press s to search or r to refresh.") + "\n"
	case workspaceReady:
		return s.viewReady(width, height)
	case workspaceConfirm, workspaceExecuting:
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
	// Reserve a single line at the top for the transient export status when
	// present (cleared after a few seconds via clearExportMsg).
	statusLine := ""
	if s.exportingMsg != "" {
		statusLine = "  " + s.exportingMsg + "\n"
		height = max(height-1, 1)
	}

	l := computeLayout(width, height)

	// Render list panel using all sections.
	listView := s.renderSections(l)

	body := listView
	if l.hasDetail {
		sp := s.summary
		sp.setSize(l.detail.W, l.detail.H)
		body = lipgloss.JoinHorizontal(lipgloss.Top, listView, " ", sp.view())
	}
	return statusLine + body
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

// sectionSizing is the rendering-agnostic input to allocateSectionHeights.
// Callers build a slice of these when they want to share the same height
// budgeting without coupling allocation to a specific item type.
type sectionSizing struct {
	desiredH int // preferred panel height including borders
	minH     int // minimum usable panel height including borders
	exact    bool
}

func (s sectionDef) sizing() sectionSizing {
	return sectionSizing{desiredH: s.desiredH, minH: s.minH, exact: s.exact}
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
		b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
	} else if s.searchQuery != "" && len(s.searchResults) == 0 && len(s.installQueue) == 0 {
		b.WriteString("  " + helpStyle.Render(fmt.Sprintf("No results for %q. Press s to search again.", s.searchQuery)) + "\n")
	} else if s.filter.active {
		b.WriteString("  " + s.filter.input.View() + "\n")
	} else if s.filter.query != "" {
		b.WriteString("  " + helpStyle.Render("Filter: "+s.filter.query+"  (/ edit • esc clear)") + "\n")
	} else if s.selfAutoDeferred {
		notice := "WinTUI is marked Auto but self-upgrade can't run unattended — apply it from Updates (esc to dismiss)."
		b.WriteString("  " + lipgloss.NewStyle().Foreground(warning).Render(notice) + "\n")
	}

	availableH := l.list.H
	hasTopBar := s.searchActive || s.searchLoading || s.err != nil ||
		(s.searchQuery != "" && len(s.searchResults) == 0 && len(s.installQueue) == 0) ||
		s.filter.active || s.filter.query != "" ||
		s.selfAutoDeferred
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
			title += fmt.Sprintf(" · %d held", s.hiddenUpgrades)
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
			title:    fmt.Sprintf("Updates Available (0 · %d held)", s.hiddenUpgrades),
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

	sizings := make([]sectionSizing, len(sections))
	for i, sec := range sections {
		sizings[i] = sec.sizing()
	}
	sectionHeights := allocateSectionHeights(sizings, availableH)

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

func allocateSectionHeights(sections []sectionSizing, availableH int) []int {
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

	policyBadge := renderUpdatePolicyBadge(item.pkg.ID, item.pkg.Source)

	gear := ""
	if appSettings.hasOverride(item.pkg.ID, item.pkg.Source) {
		gear = " " + lipgloss.NewStyle().Foreground(override).Render("⚙")
	}

	// Check for a custom selected version.
	customVer := s.selectedVersions[item.key()]

	// Version columns use subtle, not dim: the version (or the upgrade
	// delta) is the value the user is here to scan, not chrome.
	if item.upgradeable {
		ver := item.installed + " → " + item.available
		if customVer != "" && customVer != item.available {
			ver = item.installed + " → " + customVer
		}
		return name + "  " + subtleStyle.Render(ver) + policyBadge + gear
	}

	// Install queue or search result: show version (custom or available).
	if item.installed == "" {
		ver := item.available
		if customVer != "" {
			ver = customVer
		}
		if ver != "" {
			return name + "  " + subtleStyle.Render(ver) + policyBadge + gear
		}
		return name + policyBadge + gear
	}

	// Regular installed package.
	return name + "  " + subtleStyle.Render(item.installed) + policyBadge + gear
}

func renderUpdatePolicyBadge(pkgID, source string) string {
	o := appSettings.getOverride(pkgID, source)
	switch o.displayedUpdatePolicy() {
	case PolicyAuto:
		// AUTO is intent the user has SET (vs. pass/fail or success/danger).
		// Painting it in the state-accent ties it visually to the staged
		// checkbox and upgrade-available chip in the detail panel.
		return " " + lipgloss.NewStyle().Bold(true).Foreground(state).Render("[AUTO]")
	case PolicyHold:
		return " " + lipgloss.NewStyle().Bold(true).Foreground(warning).Render("[HOLD]")
	default:
		return ""
	}
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

	focusedItem, _, focusedOK := s.focusedItemContext()
	canPolicy := focusedOK && canFetchDetails(focusedItem.pkg.Source)

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
		}
		if canPolicy {
			bindings = append(bindings, key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "policy")))
		}
		bindings = append(bindings, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall")))
	default: // installed
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "stage")),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "apply")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		}
		if canPolicy {
			bindings = append(bindings, key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "policy")))
		}
		bindings = append(bindings, key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "uninstall")))
	}

	bindings = append(bindings, key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")))
	return bindings
}

// fullHelpKeys returns grouped keybindings for the full help view.
func (s workspaceScreen) fullHelpKeys() [][]key.Binding {
	navigation := []key.Binding{
		key.NewBinding(key.WithKeys("↑/k"), key.WithHelp("↑/k", "move up")),
		key.NewBinding(key.WithKeys("↓/j"), key.WithHelp("↓/j", "move down")),
		key.NewBinding(key.WithKeys("pgup/pgdn"), key.WithHelp("pgup/pgdn", "page up/down")),
		key.NewBinding(key.WithKeys("home/end"), key.WithHelp("home/end", "top/bottom of section")),
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
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle update policy")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "export package list to Desktop")),
		key.NewBinding(key.WithKeys("I"), key.WithHelp("shift+i", "import packages from a file")),
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
		s.searchActive || s.importer.active ||
		s.state == workspaceExecuting || s.state == workspaceConfirm
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
