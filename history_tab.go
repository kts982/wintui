package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

// The History tab is a read-only view over history.json (issue #4 follow-on,
// v2.11.0). All write logic stays in history.go; this screen only loads and
// renders. Two modes, modeled on the cleanup list→drill pattern:
//
//   - historyModeBatches (Tier 1): newest-first batch list, selected batch
//     summarized in the detail pane.
//   - historyModeBatch (Tier 2): the selected batch's items, with the focused
//     item's full record plus its cross-batch timeline in the detail pane.
//
// Data is reloaded on tab focus (screenFocusMsg) and on 'r', so records
// written by a concurrent CLI run (scheduled `upgrade --auto`) appear without
// restarting the TUI.

type historyViewMode int

const (
	historyModeBatches historyViewMode = iota
	historyModeBatch
)

// historyLoadedMsg carries the (re)loaded envelope. err is the reader's
// corrupt/future-version error; missing-file is not an error (empty env).
type historyLoadedMsg struct {
	env historyEnvelope
	err error
}

type historyScreen struct {
	mode historyViewMode

	env     historyEnvelope
	loadErr error
	loaded  bool // first load landed; gates the "(loading…)" placeholder

	failedOnly bool

	// Tier 1: visible[i] indexes env.Records, newest-first, after filter.
	visible []int
	cursor  int // index into visible

	// Tier 2: the drilled batch (by index into env.Records; re-resolved by ID
	// after reloads) and its item rows after filter.
	batchIdx     int
	visibleItems []int // indexes batch.Items
	itemCursor   int   // index into visibleItems

	width  int
	height int
}

func newHistoryScreen() historyScreen {
	return historyScreen{width: 80, height: 24, batchIdx: -1}
}

func (s historyScreen) init() tea.Cmd { return loadHistoryTabCmd }

// loadHistoryTabCmd reads history.json off the update loop. Goes through the
// historyLoadFn seam so tests inject synthetic envelopes instead of %APPDATA%.
func loadHistoryTabCmd() tea.Msg {
	env, err := historyLoadFn()
	return historyLoadedMsg{env: env, err: err}
}

// ── Derived rows ──────────────────────────────────────────────────────

// computeVisible rebuilds the Tier-1 row set: newest-first indices into
// env.Records, honoring the failed-only filter. The cursor is clamped, not
// reset, so toggling the filter keeps the user near their place.
func (s *historyScreen) computeVisible() {
	s.visible = s.visible[:0]
	for i := len(s.env.Records) - 1; i >= 0; i-- {
		if s.failedOnly && s.env.Records[i].Summary.Failed == 0 {
			continue
		}
		s.visible = append(s.visible, i)
	}
	s.cursor = clampIndex(s.cursor, len(s.visible))
}

// computeVisibleItems rebuilds the Tier-2 row set for the drilled batch.
func (s *historyScreen) computeVisibleItems() {
	s.visibleItems = s.visibleItems[:0]
	if s.batchIdx < 0 || s.batchIdx >= len(s.env.Records) {
		return
	}
	for i, it := range s.env.Records[s.batchIdx].Items {
		if s.failedOnly && it.Status != historyStatusError {
			continue
		}
		s.visibleItems = append(s.visibleItems, i)
	}
	s.itemCursor = clampIndex(s.itemCursor, len(s.visibleItems))
}

func clampIndex(i, n int) int {
	if n == 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	if i < 0 {
		return 0
	}
	return i
}

func (s historyScreen) focusedRecord() (historyRecord, bool) {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return historyRecord{}, false
	}
	return s.env.Records[s.visible[s.cursor]], true
}

func (s historyScreen) drilledBatch() (historyRecord, bool) {
	if s.batchIdx < 0 || s.batchIdx >= len(s.env.Records) {
		return historyRecord{}, false
	}
	return s.env.Records[s.batchIdx], true
}

func (s historyScreen) focusedItem() (historyItem, bool) {
	batch, ok := s.drilledBatch()
	if !ok || s.itemCursor < 0 || s.itemCursor >= len(s.visibleItems) {
		return historyItem{}, false
	}
	return batch.Items[s.visibleItems[s.itemCursor]], true
}

// ── Update ────────────────────────────────────────────────────────────

func (s historyScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case screenFocusMsg:
		// Reload on every tab entry: a scheduled CLI run may have appended
		// records while another tab was active. State (mode, cursors, drilled
		// batch) is preserved and re-resolved when the load lands.
		return s, loadHistoryTabCmd

	case screenBlurMsg:
		return s, nil // nothing in flight to cancel; keep state

	case historyLoadedMsg:
		return s.applyLoad(msg)

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			s.moveCursor(-1)
		case tea.MouseWheelDown:
			s.moveCursor(1)
		}
		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

// applyLoad installs a fresh envelope, preserving the drill-down by batch ID
// (indices shift when records are appended or trimmed). A batch that vanished
// (trimmed by the record cap) drops the user back to the list.
func (s historyScreen) applyLoad(msg historyLoadedMsg) (screen, tea.Cmd) {
	s.loaded = true
	s.loadErr = msg.err
	if msg.err != nil {
		return s, nil // keep the previous env so 'r' after a fix can recover
	}

	drilledID := ""
	if s.mode == historyModeBatch {
		if batch, ok := s.drilledBatch(); ok {
			drilledID = batch.ID
		}
	}

	s.env = msg.env
	s.computeVisible()

	if s.mode == historyModeBatch {
		s.batchIdx = -1
		for i := range s.env.Records {
			if s.env.Records[i].ID == drilledID {
				s.batchIdx = i
				break
			}
		}
		if s.batchIdx < 0 {
			s.mode = historyModeBatches
		}
		s.computeVisibleItems()
	}
	return s, nil
}

func (s *historyScreen) moveCursor(delta int) {
	if s.mode == historyModeBatch {
		s.itemCursor = clampIndex(s.itemCursor+delta, len(s.visibleItems))
		return
	}
	s.cursor = clampIndex(s.cursor+delta, len(s.visible))
}

func (s historyScreen) handleKey(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		s.moveCursor(-1)
	case "down", "j":
		s.moveCursor(1)
	case "r":
		return s, loadHistoryTabCmd
	case "f":
		s.failedOnly = !s.failedOnly
		s.computeVisible()
		s.computeVisibleItems()
	case "enter":
		if s.mode == historyModeBatches {
			if _, ok := s.focusedRecord(); ok {
				s.mode = historyModeBatch
				s.batchIdx = s.visible[s.cursor]
				s.itemCursor = 0
				s.computeVisibleItems()
			}
		}
	case "esc", "left":
		if s.mode == historyModeBatch {
			s.mode = historyModeBatches
			s.batchIdx = -1
			return s, nil
		}
		if s.failedOnly {
			s.failedOnly = false
			s.computeVisible()
		}
	}
	return s, nil
}

// ── helpKeys ──────────────────────────────────────────────────────────

func (s historyScreen) helpKeys() []key.Binding {
	if s.loadErr != nil {
		return []key.Binding{keyRefresh}
	}
	if s.mode == historyModeBatch {
		return []key.Binding{keyUp, keyDown, keyEscOrLeft, keyFailedOnly, keyRefresh}
	}
	return []key.Binding{keyUp, keyDown, keyDetails, keyFailedOnly, keyRefresh}
}

// ── View ──────────────────────────────────────────────────────────────

func (s historyScreen) view(width, height int) string {
	if s.loadErr != nil {
		return s.viewMessage(width,
			errorStyle.Render("History unavailable")+"\n\n"+
				itemStyle.Render(wordWrap(s.loadErr.Error(), max(width-8, 20)))+"\n\n"+
				helpStyle.Render("Press r to retry."))
	}
	if !s.loaded {
		return s.viewMessage(width, helpStyle.Render("Loading history…"))
	}
	if len(s.env.Records) == 0 {
		return s.viewMessage(width,
			sectionTitleStyle.Render("No action history yet")+"\n"+
				itemStyle.Render(wordWrap(
					"WinTUI records the install/upgrade/uninstall operations it runs — "+
						"TUI batches and headless 'wintui upgrade' / 'wintui import' runs.",
					max(width-8, 20)))+"\n\n"+
				helpStyle.Render(wordWrap(
					"Actions run from plain winget or Store auto-updates are not captured.",
					max(width-8, 20))))
	}

	if s.mode == historyModeBatch {
		return s.viewBatch(width, height)
	}
	return s.viewBatches(width, height)
}

func (s historyScreen) viewMessage(width int, body string) string {
	return lipgloss.NewStyle().Padding(1, 3).Width(max(width-4, 20)).Render(body)
}

// ── Tier 1: batch list ────────────────────────────────────────────────

func (s historyScreen) viewBatches(width, height int) string {
	l := computeLayout(width, height-1) // -1 for the summary line
	summary := s.renderSummaryLine()

	left := s.renderBatchList(l.list.W, l.list.H)
	if !l.hasDetail {
		return summary + "\n" + left
	}
	right := s.renderBatchDetail(l.detail.W, l.detail.H)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return summary + "\n" + body
}

func (s historyScreen) renderSummaryLine() string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(sectionTitleStyle.Render("Action History"))
	b.WriteString(helpStyle.Render(fmt.Sprintf("  %s", pluralize(len(s.visible), "batch", "batches"))))
	if s.failedOnly {
		b.WriteString(warnStyle.Render("  · failed only"))
	}
	b.WriteString(helpStyle.Render("  · newest first · WinTUI-run actions only"))
	return b.String()
}

func (s historyScreen) renderBatchList(width, height int) string {
	if height <= 0 {
		return ""
	}
	if len(s.visible) == 0 {
		msg := "  (no failed batches — f shows all)"
		if !s.failedOnly {
			msg = "  (no batches)"
		}
		return lipgloss.NewStyle().Height(height).Render(helpStyle.Render(msg))
	}
	panelWidth := max(width-4, 20)

	rows := make([]string, 0, len(s.visible))
	for vi, recIdx := range s.visible {
		rows = append(rows, s.renderBatchRow(s.env.Records[recIdx], vi == s.cursor, panelWidth-4))
	}

	// cursorLine is the cursor row's offset within the panel: top border + rows.
	panel := renderTitledPanel("Batches", strings.Join(rows, "\n"), panelWidth, len(rows), accent)
	lines := strings.Split(panel, "\n")
	cursorLine := s.cursor + 1

	start, end := scrollWindow(cursorLine, len(lines), height)
	visible := append([]string(nil), lines[start:end]...)
	blankLine := strings.Repeat(" ", panelWidth)
	for len(visible) < height {
		visible = append(visible, blankLine)
	}
	return strings.Join(visible, "\n")
}

func (s historyScreen) renderBatchRow(r historyRecord, focused bool, innerW int) string {
	cursor := cursorBlankStr
	if focused {
		cursor = cursorStr
	}

	whenStyle := itemStyle
	if focused {
		whenStyle = itemActiveStyle
	}
	when := whenStyle.Render(r.Timestamp.Local().Format("2006-01-02 15:04"))
	op := stateStyle.Render(fmt.Sprintf("%-9s", r.Action))

	left := cursor + when + "  " + op + " " +
		itemStyle.Render(summarizeHistoryPackages(r.Items))

	right := s.renderCountsChip(r.Summary) + " " + chipStyle.Render("["+r.Trigger+"]")

	rightWidth := lipgloss.Width(right)
	gap := innerW - lipgloss.Width(left) - rightWidth
	if gap < 1 {
		// Right chips take priority (status at a glance); shrink the package
		// summary until the row fits.
		prefix := cursor + when + "  " + op + " "
		avail := max(innerW-rightWidth-lipgloss.Width(prefix)-1, 4)
		left = prefix + itemStyle.Render(truncate(summarizeHistoryPackages(r.Items), avail))
		gap = max(innerW-lipgloss.Width(left)-rightWidth, 1)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (s historyScreen) renderCountsChip(sum historySummary) string {
	parts := []string{successStyle.Render(fmt.Sprintf("%d✓", sum.OK))}
	if sum.Failed > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d✗", sum.Failed)))
	}
	if sum.Pending > 0 {
		parts = append(parts, warnStyle.Render(fmt.Sprintf("%d…", sum.Pending)))
	}
	if sum.Skipped > 0 {
		parts = append(parts, helpStyle.Render(fmt.Sprintf("%d·", sum.Skipped)))
	}
	return strings.Join(parts, " ")
}

func (s historyScreen) renderBatchDetail(width, height int) string {
	r, ok := s.focusedRecord()
	if !ok {
		return helpStyle.Render("(no batch focused)")
	}
	innerW := max(width-2, 20)

	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Batch details") + "\n")
	b.WriteString(chipStyle.Render("["+r.Action+"]") + " " + chipStyle.Render("[via "+r.Trigger+"]") + "\n")
	b.WriteString(helpStyle.Render(r.Timestamp.Local().Format("2006-01-02 15:04")) + "\n")
	b.WriteString(helpStyle.Render(r.ID) + "\n\n")

	sum := r.Summary
	b.WriteString(stateStyle.Render(pluralize(sum.Total, "package")) + "  " + s.renderCountsChip(sum) + "\n\n")

	// Item lines, capped so the pane never overflows: the ~11 lines of header,
	// summary, and footer around this block are already spent.
	maxItems := max(height-11, 3)
	for i, it := range r.Items {
		if i >= maxItems {
			b.WriteString(helpStyle.Render(fmt.Sprintf("  (+%d more — enter for details)", len(r.Items)-i)) + "\n")
			break
		}
		b.WriteString(s.renderItemLine(it, innerW) + "\n")
	}

	b.WriteString("\n" + helpStyle.Render("enter drills into this batch"))
	return b.String()
}

// renderItemLine is the compact one-line item form used in the detail pane.
func (s historyScreen) renderItemLine(it historyItem, innerW int) string {
	name := it.Name
	if strings.TrimSpace(name) == "" {
		name = it.ID
	}
	line := "  " + historyStatusIcon(it.Status) + " " + itemStyle.Render(truncate(name, max(innerW-16, 8)))
	if v := historyVersionSpan(it); v != "" {
		line += helpStyle.Render("  " + v)
	}
	return line
}

func historyStatusIcon(status string) string {
	switch status {
	case historyStatusOK:
		return successStyle.Render("✓")
	case historyStatusError:
		return errorStyle.Render("✗")
	case historyStatusPending:
		return warnStyle.Render("…")
	default:
		return helpStyle.Render("·")
	}
}

// historyVersionSpan renders "from → to" with whichever sides exist.
func historyVersionSpan(it historyItem) string {
	from := strings.TrimSpace(it.FromVersion)
	to := strings.TrimSpace(it.ToVersion)
	switch {
	case from != "" && to != "":
		return from + " → " + to
	case to != "":
		return "→ " + to
	case from != "":
		return from
	}
	return ""
}

// ── Tier 2: batch drill-down ──────────────────────────────────────────

func (s historyScreen) viewBatch(width, height int) string {
	batch, ok := s.drilledBatch()
	if !ok {
		return s.viewMessage(width, helpStyle.Render("(batch no longer in history)"))
	}

	l := computeLayout(width, height-1)

	var head strings.Builder
	head.WriteString("  ")
	head.WriteString(sectionTitleStyle.Render("Batch " + batch.Timestamp.Local().Format("2006-01-02 15:04")))
	head.WriteString(stateStyle.Render("  " + batch.Action))
	head.WriteString(chipStyle.Render("  [via " + batch.Trigger + "]"))
	if s.failedOnly {
		head.WriteString(warnStyle.Render("  · failed only"))
	}
	head.WriteString(helpStyle.Render("  · esc back"))

	left := s.renderItemList(batch, l.list.W, l.list.H)
	if !l.hasDetail {
		return head.String() + "\n" + left
	}
	right := s.renderItemDetail(l.detail.W, l.detail.H)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return head.String() + "\n" + body
}

func (s historyScreen) renderItemList(batch historyRecord, width, height int) string {
	if height <= 0 {
		return ""
	}
	if len(s.visibleItems) == 0 {
		msg := "  (no failed items in this batch — f shows all)"
		if !s.failedOnly {
			msg = "  (empty batch)"
		}
		return lipgloss.NewStyle().Height(height).Render(helpStyle.Render(msg))
	}
	panelWidth := max(width-4, 20)

	rows := make([]string, 0, len(s.visibleItems))
	for vi, itemIdx := range s.visibleItems {
		rows = append(rows, s.renderItemRow(batch.Items[itemIdx], vi == s.itemCursor, panelWidth-4))
	}

	panel := renderTitledPanel(pluralize(len(s.visibleItems), "Package"), strings.Join(rows, "\n"), panelWidth, len(rows), accent)
	lines := strings.Split(panel, "\n")
	cursorLine := s.itemCursor + 1

	start, end := scrollWindow(cursorLine, len(lines), height)
	visible := append([]string(nil), lines[start:end]...)
	blankLine := strings.Repeat(" ", panelWidth)
	for len(visible) < height {
		visible = append(visible, blankLine)
	}
	return strings.Join(visible, "\n")
}

func (s historyScreen) renderItemRow(it historyItem, focused bool, innerW int) string {
	cursor := cursorBlankStr
	if focused {
		cursor = cursorStr
	}
	name := it.Name
	if strings.TrimSpace(name) == "" {
		name = it.ID
	}
	nameStyle := itemStyle
	if focused {
		nameStyle = itemActiveStyle
	}

	right := helpStyle.Render(historyVersionSpan(it))
	left := cursor + historyStatusIcon(it.Status) + " " + nameStyle.Render(truncate(name, max(innerW-lipgloss.Width(right)-8, 8)))

	gap := max(innerW-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (s historyScreen) renderItemDetail(width, height int) string {
	it, ok := s.focusedItem()
	if !ok {
		return helpStyle.Render("(no package focused)")
	}
	innerW := max(width-2, 20)

	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render(truncate(it.ID, innerW)) + "\n")
	if strings.TrimSpace(it.Name) != "" && it.Name != it.ID {
		b.WriteString(itemStyle.Render(truncate(it.Name, innerW)) + "\n")
	}
	chips := []string{chipStyle.Render("[" + it.Action + "]")}
	if it.Source != "" {
		chips = append(chips, chipStyle.Render("["+it.Source+"]"))
	}
	b.WriteString(strings.Join(chips, " ") + "\n\n")

	if v := historyVersionSpan(it); v != "" {
		b.WriteString(stateStyle.Render(v) + "\n")
	}
	b.WriteString(historyStatusIcon(it.Status) + " " + itemStyle.Render(it.Status) + "\n")

	if it.Error != "" {
		b.WriteString("\n" + errorStyle.Render("Error") + "\n")
		b.WriteString(helpStyle.Render(wordWrap(it.Error, innerW)) + "\n")
	}
	if it.Notes != "" {
		b.WriteString("\n" + warnStyle.Render(wordWrap(it.Notes, innerW)) + "\n")
	}

	// Cross-batch timeline for this package — the Tier-2 answer to "when did
	// WinTUI last touch this?". Mirrors `wintui history <id>` (newest-first,
	// case-insensitive ID match).
	b.WriteString("\n" + sectionTitleStyle.Render("Timeline") + "\n")
	usedLines := strings.Count(b.String(), "\n")
	maxEntries := max(height-usedLines-1, 3)
	entries := s.packageTimeline(it.ID)
	for i, e := range entries {
		if i >= maxEntries {
			b.WriteString(helpStyle.Render(fmt.Sprintf("  (+%d earlier)", len(entries)-i)) + "\n")
			break
		}
		b.WriteString("  " + historyStatusIcon(e.item.Status) + " " +
			helpStyle.Render(e.when.Local().Format("2006-01-02")) + " " +
			stateStyle.Render(e.item.Action))
		if v := historyVersionSpan(e.item); v != "" {
			b.WriteString(helpStyle.Render("  " + truncate(v, max(innerW-20, 6))))
		}
		b.WriteString("\n")
	}
	return b.String()
}

type historyTimelineEntry struct {
	when  time.Time
	item  historyItem
	batch string
}

// packageTimeline collects every recorded action for a package ID across all
// batches, newest-first.
func (s historyScreen) packageTimeline(id string) []historyTimelineEntry {
	idLower := strings.ToLower(strings.TrimSpace(id))
	var out []historyTimelineEntry
	for i := len(s.env.Records) - 1; i >= 0; i-- {
		r := s.env.Records[i]
		for _, it := range r.Items {
			if strings.ToLower(it.ID) != idLower {
				continue
			}
			out = append(out, historyTimelineEntry{when: r.Timestamp, item: it, batch: r.ID})
		}
	}
	return out
}
