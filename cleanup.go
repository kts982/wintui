package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

// cleanupState is the top-level UI state. Per-row scanning is independent of
// this enum and tracked through scanInflight + results maps; the user can
// freely cursor and toggle while scans land in the background.
type cleanupState int

const (
	cleanupReady          cleanupState = iota // primary state — list + (background) scans
	cleanupConfirming                         // confirm modal overlaid on the list
	cleanupExecuting                          // delete in progress; tabs blocked
	cleanupDone                               // all targets cleaned cleanly
	cleanupPartialFailure                     // at least one target reported failures
)

// cleanupGroupOrder is the user-visible order of group panels in the list.
// Drives both rendering and the cursor/visible[] traversal.
var cleanupGroupOrder = []cleanupGroup{
	cleanupGroupCoreTemp,
	cleanupGroupCaches,
	cleanupGroupDeveloper,
	cleanupGroupGPU,
	cleanupGroupWinTUI,
}

var cleanupGroupLabels = map[cleanupGroup]string{
	cleanupGroupCoreTemp:  "Core Temp",
	cleanupGroupCaches:    "Caches",
	cleanupGroupDeveloper: "Developer",
	cleanupGroupGPU:       "GPU",
	cleanupGroupWinTUI:    "WinTUI",
}

// ── Messages ──────────────────────────────────────────────────────────

type cleanupTargetScannedMsg struct {
	id     string
	gen    int // scan generation captured at startScan; mismatched gen = stale
	result cleanupTargetResult
}

type cleanupTargetDeletedMsg struct {
	id     string
	result cleanupTargetResult
}

// ── Screen ────────────────────────────────────────────────────────────

type cleanupScreen struct {
	state    cleanupState
	targets  []cleanupTargetDef
	visible  []int // indices into targets, after detect-if-present filter
	cursor   int   // index into visible
	checked  map[string]bool
	results  map[string]cleanupTargetResult
	inflight map[string]context.CancelFunc
	scanGen  int // bumped on cancel/rescan; stale messages are dropped

	execQueue   []string                       // ordered IDs being deleted
	execIdx     int                            // next index in execQueue to dispatch
	execResults map[string]cleanupTargetResult // populated as deletes return

	spinner spinner.Model
	width   int
	height  int
}

func newCleanupScreen() cleanupScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	registry := cleanupTargetRegistry()
	s := cleanupScreen{
		state:    cleanupReady,
		targets:  registry,
		checked:  make(map[string]bool),
		results:  make(map[string]cleanupTargetResult),
		inflight: make(map[string]context.CancelFunc),
		spinner:  sp,
		width:    80,
		height:   24,
	}
	s.computeVisible()
	for _, idx := range s.visible {
		def := registry[idx]
		if appSettings.cleanupTargetEnabled(def) {
			s.checked[def.id] = true
		}
	}
	return s
}

// applyTheme refreshes the cleanup screen's spinner style. All other
// styles in cleanup.go are inline lipgloss.NewStyle().Foreground(...)
// calls that re-read the global palette every frame, so no further
// work is needed here.
func (s cleanupScreen) applyTheme() screen {
	s.spinner.Style = lipgloss.NewStyle().Foreground(accent)
	return s
}

// computeVisible filters detect-if-present targets whose resolved paths
// don't exist. Called at construction; not refreshed during a session
// (a freshly-installed Go toolchain mid-session won't appear until next launch).
func (s *cleanupScreen) computeVisible() {
	s.visible = s.visible[:0]
	for i, def := range s.targets {
		if def.detectIfPresent {
			path := def.pathFn()
			if path == "" {
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() {
				continue
			}
		}
		s.visible = append(s.visible, i)
	}
}

func (s cleanupScreen) init() tea.Cmd {
	cmds := []tea.Cmd{s.spinner.Tick}
	if cmd := s.beginAutoScan(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// beginAutoScan dispatches per-target scans according to the persisted mode.
// Mutates s.inflight directly because maps are reference values; the cmd
// closures only capture the per-target context.
func (s cleanupScreen) beginAutoScan() tea.Cmd {
	mode := normalizeCleanupAutoScan(appSettings.CleanupAutoScan)
	if mode == CleanupAutoScanOff {
		return nil
	}
	var cmds []tea.Cmd
	for _, idx := range s.visible {
		def := s.targets[idx]
		if mode == CleanupAutoScanSafe && def.group == cleanupGroupDeveloper {
			continue
		}
		cmds = append(cmds, s.startScan(def.id))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// startScan registers a cancel func in s.inflight and returns a cmd that
// runs cleanupScan and posts the result. Caller must NOT also write to
// s.inflight for the same id. The scan captures the current scanGen so
// the update handler can drop stale messages from goroutines that
// completed after a cancel/rescan.
func (s cleanupScreen) startScan(id string) tea.Cmd {
	if _, exists := s.inflight[id]; exists {
		return nil
	}
	def, ok := cleanupTargetByID(id)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel is retained in s.inflight and called by cancelAllScans or the scanned-msg handler
	s.inflight[id] = cancel
	gen := s.scanGen
	return func() tea.Msg {
		res := cleanupScan(ctx, def)
		return cleanupTargetScannedMsg{id: id, gen: gen, result: res}
	}
}

// cancelAllScans cancels every in-flight scan, clears the map, and bumps
// scanGen so any goroutines that finish after this point are seen as
// stale by the update handler. Pointer receiver is required: scanGen is a
// primitive int and must mutate the caller's screen, not a value copy.
// The bump matters even when inflight is already empty — a previous-cycle
// goroutine could have escaped its cancel check and still be on its way
// back.
func (s *cleanupScreen) cancelAllScans() {
	s.scanGen++
	for id, cancel := range s.inflight {
		cancel()
		delete(s.inflight, id)
	}
}

// ── Update ────────────────────────────────────────────────────────────

func (s cleanupScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case screenBlurMsg:
		// Locked behavior: cancel in-flight scans on tab away. Executing
		// state owns the foreground modal and is not interrupted (the tab
		// switch should be blocked at the app layer via blocksGlobalShortcuts).
		if s.state != cleanupExecuting {
			s.cancelAllScans()
		}
		return s, nil

	case cleanupTargetScannedMsg:
		// Drop stale results from goroutines that finished after a
		// cancel/rescan. The generation guard also defends against the
		// race where rescan re-registers the same id in inflight while
		// the previous cycle's goroutine is still in flight.
		if msg.gen != s.scanGen {
			return s, nil
		}
		if cancel, ok := s.inflight[msg.id]; ok {
			cancel()
		}
		delete(s.inflight, msg.id)
		s.results[msg.id] = msg.result
		return s, nil

	case cleanupTargetDeletedMsg:
		if s.state != cleanupExecuting {
			return s, nil
		}
		s.execResults[msg.id] = msg.result
		s.execIdx++
		if s.execIdx >= len(s.execQueue) {
			return s.finishExecute()
		}
		return s.dispatchNextDelete()

	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd

	case tea.MouseWheelMsg:
		// Move the target cursor one row per wheel tick, mirroring up/down.
		// The confirm/execute modals own the foreground, so leave them alone.
		if s.state == cleanupConfirming || s.state == cleanupExecuting {
			return s, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			if s.cursor > 0 {
				s.cursor--
			}
		case tea.MouseWheelDown:
			if s.cursor < len(s.visible)-1 {
				s.cursor++
			}
		}
		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s cleanupScreen) handleKey(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch s.state {
	case cleanupConfirming:
		switch msg.String() {
		case "enter":
			return s.startExecute()
		case "esc":
			s.state = cleanupReady
			return s, nil
		}
		return s, nil

	case cleanupExecuting:
		// Esc is best-effort cancel; current target finishes, no more dispatched.
		if msg.String() == "esc" {
			s.execIdx = len(s.execQueue) // skip remaining
		}
		return s, nil

	case cleanupDone, cleanupPartialFailure:
		if msg.String() == "r" {
			return s.rescanAll()
		}
		// Any other key returns to ready.
		s.state = cleanupReady
		return s, nil
	}

	// cleanupReady (default)
	switch msg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.visible)-1 {
			s.cursor++
		}
	case "space":
		return s.toggleFocused()
	case "a":
		return s.toggleFocusedGroup()
	case "s":
		if def, ok := s.focusedDef(); ok {
			cmd := s.startScan(def.id)
			return s, cmd
		}
	case "r":
		return s.rescanAll()
	case "enter":
		return s.tryEnterConfirm()
	case "esc":
		// Clear staged toggles on non-default-checked rows; default-checked
		// rows are sticky-on by design.
		for id := range s.checked {
			def, ok := cleanupTargetByID(id)
			if !ok || !def.defaultChecked {
				delete(s.checked, id)
			}
		}
	}
	return s, nil
}

func (s cleanupScreen) focusedDef() (cleanupTargetDef, bool) {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return cleanupTargetDef{}, false
	}
	return s.targets[s.visible[s.cursor]], true
}

// toggleFocused flips the focused row's checked state and persists the
// change (default-checked rows no-op the persistence layer per slice 3).
func (s cleanupScreen) toggleFocused() (screen, tea.Cmd) {
	def, ok := s.focusedDef()
	if !ok {
		return s, nil
	}
	now := !s.checked[def.id]
	if now {
		s.checked[def.id] = true
	} else {
		delete(s.checked, def.id)
	}
	next := appSettings.clone()
	next.setCleanupTargetEnabled(def, now)
	_ = persistSettings(next)
	return s, nil
}

// toggleFocusedGroup toggles every row in the focused row's group. If any
// row is currently unchecked, the group becomes fully-checked; otherwise
// fully-unchecked.
func (s cleanupScreen) toggleFocusedGroup() (screen, tea.Cmd) {
	def, ok := s.focusedDef()
	if !ok {
		return s, nil
	}
	group := def.group

	allChecked := true
	for _, idx := range s.visible {
		d := s.targets[idx]
		if d.group != group {
			continue
		}
		if !s.checked[d.id] {
			allChecked = false
			break
		}
	}
	target := !allChecked

	next := appSettings.clone()
	for _, idx := range s.visible {
		d := s.targets[idx]
		if d.group != group {
			continue
		}
		if target {
			s.checked[d.id] = true
		} else {
			delete(s.checked, d.id)
		}
		next.setCleanupTargetEnabled(d, target)
	}
	_ = persistSettings(next)
	return s, nil
}

// rescanAll cancels everything in flight and re-queues scans honoring the
// current auto-scan mode. Wired to "r" everywhere it's allowed.
func (s cleanupScreen) rescanAll() (screen, tea.Cmd) {
	s.cancelAllScans()
	s.results = make(map[string]cleanupTargetResult)
	s.state = cleanupReady
	cmd := s.beginAutoScan()
	return s, cmd
}

// tryEnterConfirm gates entry into the confirm modal: at least one checked
// target must have a non-zero size. Slow scans the user doesn't want must
// not block the action — only checked targets participate.
func (s cleanupScreen) tryEnterConfirm() (screen, tea.Cmd) {
	ids := s.checkedWithSizeIDs()
	if len(ids) == 0 {
		return s, nil
	}
	s.state = cleanupConfirming
	return s, nil
}

func (s cleanupScreen) checkedWithSizeIDs() []string {
	var ids []string
	for _, idx := range s.visible {
		def := s.targets[idx]
		if !s.checked[def.id] {
			continue
		}
		res, ok := s.results[def.id]
		if !ok || res.sizeBytes == 0 {
			continue
		}
		ids = append(ids, def.id)
	}
	return ids
}

// startExecute transitions from confirming to executing and dispatches the
// first delete. Subsequent dispatches are chained on cleanupTargetDeletedMsg.
func (s cleanupScreen) startExecute() (screen, tea.Cmd) {
	s.execQueue = s.checkedWithSizeIDs()
	s.execIdx = 0
	s.execResults = make(map[string]cleanupTargetResult)
	if len(s.execQueue) == 0 {
		s.state = cleanupReady
		return s, nil
	}
	s.state = cleanupExecuting
	return s.dispatchNextDelete()
}

func (s cleanupScreen) dispatchNextDelete() (screen, tea.Cmd) {
	if s.execIdx >= len(s.execQueue) {
		return s.finishExecute()
	}
	id := s.execQueue[s.execIdx]
	def, ok := cleanupTargetByID(id)
	if !ok {
		// Skip and proceed.
		s.execResults[id] = cleanupTargetResult{id: id}
		s.execIdx++
		return s.dispatchNextDelete()
	}
	if def.requiresAdmin && !isElevated() {
		return s, deleteElevatedCmd(id)
	}
	return s, deleteCmd(def)
}

func deleteCmd(def cleanupTargetDef) tea.Cmd {
	return func() tea.Msg {
		res := cleanupDelete(context.Background(), def)
		return cleanupTargetDeletedMsg{id: def.id, result: res}
	}
}

func deleteElevatedCmd(id string) tea.Cmd {
	return func() tea.Msg {
		res, err := globalElevator.cleanupTargetElevated(id)
		if err != nil {
			res = cleanupTargetResult{
				id:     id,
				failed: 1,
				errors: []error{err},
			}
		}
		return cleanupTargetDeletedMsg{id: id, result: res}
	}
}

func (s cleanupScreen) finishExecute() (screen, tea.Cmd) {
	anyFailed := false
	for id, r := range s.execResults {
		s.results[id] = r
		if r.failed > 0 {
			anyFailed = true
		}
	}
	if anyFailed {
		s.state = cleanupPartialFailure
	} else {
		s.state = cleanupDone
	}
	return s, sendCleanupToastCmd(s.execResults)
}

func sendCleanupToastCmd(results map[string]cleanupTargetResult) tea.Cmd {
	return func() tea.Msg {
		var freed int64
		var failed, success int
		for _, r := range results {
			freed += r.freedBytes
			failed += r.failed
			if r.failed == 0 && r.freedBytes > 0 {
				success++
			}
		}
		if failed > 0 {
			sendToast("WinTUI cleanup",
				fmt.Sprintf("Freed %s; %s had locked files.", formatBytes(freed), pluralize(failed, "target")))
		} else if success > 0 {
			sendToast("WinTUI cleanup",
				fmt.Sprintf("Freed %s across %s.", formatBytes(freed), pluralize(success, "target")))
		}
		return nil
	}
}

// blocksGlobalShortcuts prevents tab/q/etc. while a delete is in flight, so
// the user can't navigate away mid-modal. Scans + confirm are non-blocking.
func (s cleanupScreen) blocksGlobalShortcuts() bool {
	return s.state == cleanupExecuting
}

// ── helpKeys ──────────────────────────────────────────────────────────

func (s cleanupScreen) helpKeys() []key.Binding {
	switch s.state {
	case cleanupReady:
		return []key.Binding{
			keyUp, keyDown, keySelectRow, keySelectGroup,
			keyScanFocused, keyRefresh, keyEnter,
		}
	case cleanupConfirming:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "clean")),
			keyEscCancel,
		}
	case cleanupExecuting:
		return []key.Binding{keyEscCancel}
	case cleanupDone, cleanupPartialFailure:
		return []key.Binding{keyRefresh}
	}
	return nil
}

// ── Selection summary ────────────────────────────────────────────────

// selectedSummary returns how many rows the user has checked (regardless
// of scan state) and the total bytes those rows would free based on the
// scans that have come back so far. The count tracks the user's action;
// the byte total tracks the consequence — they're allowed to disagree
// (e.g. "5 selected, 0 B" when every checked row scanned empty, or
// "5 selected, 1.2 GB" while scans are still landing).
func (s cleanupScreen) selectedSummary() (count int, freed int64) {
	for _, idx := range s.visible {
		def := s.targets[idx]
		if !s.checked[def.id] {
			continue
		}
		count++
		if r, ok := s.results[def.id]; ok {
			freed += r.sizeBytes
		}
	}
	return count, freed
}

// ── View ──────────────────────────────────────────────────────────────

func (s cleanupScreen) view(width, height int) string {
	switch s.state {
	case cleanupConfirming:
		return s.viewConfirm(width, height)
	case cleanupExecuting:
		return s.viewExecuting(width, height)
	case cleanupDone, cleanupPartialFailure:
		return s.viewDone(width, height)
	default:
		return s.viewReady(width, height)
	}
}

func (s cleanupScreen) viewReady(width, height int) string {
	l := computeLayout(width, height-1) // -1 for the summary line
	summary := s.renderSummaryLine()

	left := s.renderGroupPanels(l.list.W, l.list.H)
	if !l.hasDetail {
		return summary + "\n" + left
	}
	right := s.renderDetailPane(l.detail.W, l.detail.H)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return summary + "\n" + body
}

func (s cleanupScreen) renderSummaryLine() string {
	count, freed := s.selectedSummary()
	scanning := len(s.inflight)

	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(sectionTitleStyle.Render("Cleanup Targets"))
	b.WriteString(helpStyle.Render(fmt.Sprintf("  %d shown", len(s.visible))))
	if count > 0 {
		b.WriteString(stateStyle.Render(fmt.Sprintf("  · %d selected, %s", count, formatBytes(freed))))
	}
	if scanning > 0 {
		b.WriteString(helpStyle.Render(fmt.Sprintf("  · scanning %d…", scanning)))
	}
	// Locked plan: header chip appears only when elevated — the "you got
	// the perk" signal. Non-elevated users instead see [suggests admin]
	// chips on the relevant rows.
	if isElevated() {
		b.WriteString(stateStyle.Render("  [admin]"))
	}
	return b.String()
}

func (s cleanupScreen) renderGroupPanels(width, height int) string {
	if height <= 0 {
		return ""
	}
	if len(s.visible) == 0 {
		return lipgloss.NewStyle().
			Height(height).
			Render(helpStyle.Render("  (no cleanup targets present on this machine)"))
	}
	panelWidth := max(width-4, 20)

	// Bucket visible[] indices by group, preserving registry order, and
	// record each group's cursor offset into s.visible so we can map the
	// global cursor to a row within one specific group.
	type groupView struct {
		group  cleanupGroup
		rows   []int // indices into s.targets
		offset int   // cursor offset into s.visible
	}
	byGroup := map[cleanupGroup][]int{}
	for _, idx := range s.visible {
		g := s.targets[idx].group
		byGroup[g] = append(byGroup[g], idx)
	}
	var groups []groupView
	viOffset := 0
	for _, group := range cleanupGroupOrder {
		rows := byGroup[group]
		if len(rows) == 0 {
			continue
		}
		groups = append(groups, groupView{group: group, rows: rows, offset: viOffset})
		viOffset += len(rows)
	}

	// Find which group contains the cursor so we can border it with accent
	// and later keep that row inside the single left-column viewport.
	cursorGroup := -1
	for i, gv := range groups {
		if s.cursor >= gv.offset && s.cursor < gv.offset+len(gv.rows) {
			cursorGroup = i
			break
		}
	}

	var lines []string
	cursorLine := 0
	for i, gv := range groups {
		groupLineOffset := len(lines)
		rowLines := make([]string, 0, len(gv.rows))
		for j, regIdx := range gv.rows {
			def := s.targets[regIdx]
			isFocused := (gv.offset + j) == s.cursor
			if isFocused {
				cursorLine = groupLineOffset + 1 + j // top border + row offset
			}
			rowLines = append(rowLines, s.renderRow(def, isFocused, panelWidth-4))
		}

		borderColor := dim
		if i == cursorGroup {
			borderColor = accent
		}
		title := cleanupGroupLabels[gv.group]
		panel := renderTitledPanel(title, strings.Join(rowLines, "\n"), panelWidth, len(rowLines), borderColor)
		lines = append(lines, strings.Split(panel, "\n")...)
	}

	if len(lines) == 0 {
		return ""
	}
	if cursorLine < 0 {
		cursorLine = 0
	}
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}

	start, end := scrollWindow(cursorLine, len(lines), height)
	visible := append([]string(nil), lines[start:end]...)
	blankLine := strings.Repeat(" ", panelWidth)
	for len(visible) < height {
		visible = append(visible, blankLine)
	}
	return strings.Join(visible, "\n")
}

func (s cleanupScreen) renderRow(def cleanupTargetDef, focused bool, innerW int) string {
	cursor := cursorBlankStr
	if focused {
		cursor = cursorStr
	}
	box := checkbox(s.checked[def.id])

	nameStyle := itemStyle
	if focused {
		nameStyle = itemActiveStyle
	}
	name := nameStyle.Render(def.label)

	var rightChip string
	switch {
	case s.inflight[def.id] != nil:
		rightChip = helpStyle.Render(s.spinner.View() + " scanning")
	default:
		if r, ok := s.results[def.id]; ok {
			switch r.skipped {
			case cleanupSkipMissing:
				rightChip = helpStyle.Render("(empty)")
			case cleanupSkipNotElevated:
				rightChip = warnStyle.Render("needs admin")
			case cleanupSkipGuarded:
				rightChip = errorStyle.Render("guarded")
			default:
				if r.sizeBytes > 0 {
					rightChip = stateStyle.Render(formatBytes(r.sizeBytes))
				} else {
					rightChip = helpStyle.Render("0 B")
				}
			}
		} else {
			rightChip = helpStyle.Render("[scan]")
		}
	}

	var adminChip string
	if def.requiresAdmin {
		adminChip = " " + chipStyle.Render("[suggests admin]")
	}

	left := cursor + box + " " + name + adminChip
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(rightChip)
	gap := max(innerW-leftWidth-rightWidth, 1)
	return left + strings.Repeat(" ", gap) + rightChip
}

func (s cleanupScreen) renderDetailPane(width, _ int) string {
	def, ok := s.focusedDef()
	if !ok {
		return helpStyle.Render("(no target focused)")
	}

	innerW := max(width-2, 20)
	var b strings.Builder

	b.WriteString(sectionTitleStyle.Render(def.label) + "\n")

	chips := []string{chipStyle.Render("[" + cleanupGroupLabels[def.group] + "]")}
	if def.requiresAdmin {
		chips = append(chips, chipStyle.Render("[suggests admin]"))
	}
	if def.defaultChecked {
		chips = append(chips, chipStyle.Render("[default on]"))
	} else {
		chips = append(chips, chipStyle.Render("[opt-in]"))
	}
	b.WriteString(strings.Join(chips, " ") + "\n\n")

	path := def.pathFn()
	if path == "" {
		b.WriteString(helpStyle.Render("Path: (env not set)") + "\n")
	} else {
		b.WriteString(helpStyle.Render("Resolved: ") + "\n")
		b.WriteString(urlStyle.Render(wordWrap(path, innerW)) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(itemStyle.Render(wordWrap(def.description, innerW)) + "\n\n")

	if _, scanning := s.inflight[def.id]; scanning {
		b.WriteString(helpStyle.Render(s.spinner.View()+" Scanning…") + "\n")
	} else if r, ok := s.results[def.id]; ok {
		b.WriteString(s.renderResultBlock(r, innerW))
	} else {
		b.WriteString(helpStyle.Render("Press s to scan this target.") + "\n")
	}

	if def.requiresAdmin && !isElevated() {
		b.WriteString("\n" + warnStyle.Render(wordWrap(
			"Windows restricts access to this folder. WinTUI suggests admin to clean here without errors.",
			innerW,
		)))
	}

	if def.warning != "" {
		b.WriteString("\n\n" + warnStyle.Render("⚠ "+wordWrap(def.warning, innerW)))
	}

	return b.String()
}

func (s cleanupScreen) renderResultBlock(r cleanupTargetResult, innerW int) string {
	switch r.skipped {
	case cleanupSkipMissing:
		return helpStyle.Render("(empty / not present)")
	case cleanupSkipNotElevated:
		return warnStyle.Render(wordWrap(
			"Windows wouldn't let WinTUI clean here. Ctrl+E asks for admin and retries.",
			innerW,
		))
	case cleanupSkipGuarded:
		return errorStyle.Render("Path is guarded against bulk delete.")
	case cleanupSkipUnresolved:
		return helpStyle.Render("(target environment not configured)")
	}

	var b strings.Builder
	b.WriteString(stateStyle.Render(formatBytes(r.sizeBytes)))
	b.WriteString(helpStyle.Render(fmt.Sprintf("  in %d items", r.files)))
	if r.freedBytes > 0 {
		b.WriteString("\n" + successStyle.Render("Freed "+formatBytes(r.freedBytes)))
	}
	if r.failed > 0 {
		b.WriteString("\n" + warnStyle.Render(fmt.Sprintf("%d item(s) couldn't be removed.", r.failed)))
	}
	return b.String()
}

func (s cleanupScreen) viewConfirm(width, height int) string {
	ids := s.execQueueIDsForConfirm()
	var freed int64
	var adminCount int
	var lines []string
	for _, id := range ids {
		def, ok := cleanupTargetByID(id)
		if !ok {
			continue
		}
		res := s.results[id]
		freed += res.sizeBytes
		if def.requiresAdmin {
			adminCount++
		}
		lines = append(lines, fmt.Sprintf("  • %s  %s",
			def.label,
			helpStyle.Render(formatBytes(res.sizeBytes)),
		))
	}

	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Confirm Cleanup") + "\n")
	b.WriteString(helpStyle.Render(strings.Repeat("─", 40)) + "\n")
	fmt.Fprintf(&b, "Delete contents of %s — about %s\n\n",
		pluralize(len(ids), "target"), formatBytes(freed))
	b.WriteString(strings.Join(lines, "\n") + "\n")

	if adminCount > 0 && !isElevated() {
		verb := "needs"
		if adminCount > 1 {
			verb = "need"
		}
		b.WriteString("\n" + warnStyle.Render(wordWrap(
			fmt.Sprintf("%s %s admin. UAC will prompt once for the elevated helper.",
				pluralize(adminCount, "target"), verb), 56)) + "\n")
		b.WriteString(helpStyle.Render(wordWrap(
			"If your antivirus flags this, that's a known false positive on bulk file delete — WinTUI only touches the listed paths.",
			56)) + "\n")
	}

	b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " clean  ·  " +
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel")

	style := lipgloss.NewStyle().
		Width(min(width-8, 60)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, style.Render(b.String()))
}

// execQueueIDsForConfirm returns the list of IDs that would be cleaned now,
// computed identically to checkedWithSizeIDs but available before the
// transition (the confirm view runs before startExecute).
func (s cleanupScreen) execQueueIDsForConfirm() []string {
	if s.state == cleanupConfirming {
		return s.checkedWithSizeIDs()
	}
	return s.execQueue
}

func (s cleanupScreen) viewExecuting(width, height int) string {
	current := ""
	if s.execIdx < len(s.execQueue) {
		if def, ok := cleanupTargetByID(s.execQueue[s.execIdx]); ok {
			current = def.label
		}
	}
	body := fmt.Sprintf("%s Cleaning (%d of %d)", s.spinner.View(), s.execIdx+1, len(s.execQueue))
	if current != "" {
		body += "\n" + helpStyle.Render("  "+current)
	}
	style := lipgloss.NewStyle().
		Width(min(width-8, 60)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, style.Render(body))
}

func (s cleanupScreen) viewDone(width, height int) string {
	var freed int64
	var failed int
	var ids []string
	for id := range s.execResults {
		ids = append(ids, id)
	}
	// Sort by freed bytes descending so the most-impactful target appears
	// first; ID is the tie-breaker for deterministic ordering when sizes
	// match (e.g. zero-freed entries).
	sort.Slice(ids, func(i, j int) bool {
		fi := s.execResults[ids[i]].freedBytes
		fj := s.execResults[ids[j]].freedBytes
		if fi != fj {
			return fi > fj
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		r := s.execResults[id]
		freed += r.freedBytes
		failed += r.failed
	}

	var b strings.Builder
	if s.state == cleanupPartialFailure {
		b.WriteString(warnStyle.Render(fmt.Sprintf("Cleanup finished with %s.", pluralize(failed, "failure"))) + "\n")
	} else {
		b.WriteString(successStyle.Render("Cleanup complete.") + "\n")
	}
	b.WriteString(helpStyle.Render(fmt.Sprintf("Freed %s across %s.", formatBytes(freed), pluralize(len(ids), "target"))) + "\n\n")

	for _, id := range ids {
		r := s.execResults[id]
		def, _ := cleanupTargetByID(id)
		switch {
		case r.failed > 0:
			b.WriteString(errorStyle.Render("  ✗ ") + def.label + "\n")
			if r.skipped == cleanupSkipNotElevated {
				b.WriteString("    " + helpStyle.Render(
					"Windows wouldn't let WinTUI clean here. Ctrl+E asks for admin and retries.") + "\n")
			} else if len(r.errors) > 0 {
				b.WriteString("    " + helpStyle.Render(r.errors[0].Error()) + "\n")
			}
		case r.freedBytes > 0:
			b.WriteString(successStyle.Render("  ✓ ") + def.label +
				helpStyle.Render(fmt.Sprintf("  freed %s", formatBytes(r.freedBytes))) + "\n")
		default:
			b.WriteString(helpStyle.Render("  · ") + def.label + helpStyle.Render("  (nothing to free)") + "\n")
		}
	}

	b.WriteString("\n" + helpStyle.Render("Press r to scan again, any other key to return."))

	style := lipgloss.NewStyle().Padding(1, 2).Width(width - 4).MaxHeight(height)
	return style.Render(b.String())
}
