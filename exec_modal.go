package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// execModalPhase tracks the current phase of the execution modal.
type execModalPhase int

const (
	execPhaseReview   execModalPhase = iota // user reviews selected packages
	execPhaseRunning                        // batch is executing
	execPhaseComplete                       // all done, showing results
)

// execModal manages the multi-phase execution modal overlay.
// Review → Running → Complete, all within one modal.
type execModal struct {
	phase         execModalPhase
	action        retryOp // retryOpUpgrade, retryOpUninstall, etc.
	items         []batchItem
	itemMap       map[string]*batchItem
	idx           int // currently running item index
	spinner       spinner.Model
	forceElevated bool // true when retrying via Ctrl+E
	showCommands  bool // review phase: expand per-item winget command preview
	scroll        int  // body scroll offset when content exceeds modal height
}

func newExecModal(action retryOp, items []batchItem) execModal {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	m := execModal{
		phase:   execPhaseReview,
		action:  action,
		items:   items,
		itemMap: make(map[string]*batchItem, len(items)),
		spinner: sp,
	}
	for i := range items {
		m.itemMap[items[i].item.key()] = &m.items[i]
	}
	return m
}

func (m execModal) actionTitle() string {
	if m.action == retryOpApply {
		return "Apply"
	}
	s := string(m.action)
	return strings.ToUpper(s[:1]) + s[1:]
}

func (m execModal) actionVerb() string {
	switch m.action {
	case retryOpApply:
		return "Applying"
	case retryOpUpgrade:
		return "Upgrading"
	case retryOpUninstall:
		return "Uninstalling"
	default:
		return m.actionTitle() + "ing"
	}
}

func actionTitle(op retryOp) string {
	label := string(op)
	if label == "" {
		return "Apply"
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

func renderActionTag(op retryOp) string {
	return lipgloss.NewStyle().Foreground(dim).Render("[" + actionTitle(op) + "]")
}

func (m execModal) pendingRestartCount() int {
	n := 0
	for _, bi := range m.items {
		if bi.status == batchPendingRestart {
			n++
		}
	}
	return n
}

func (m execModal) hasPendingSelfUpgrade() bool {
	return m.pendingRestartCount() > 0
}

func (m execModal) pendingSelfUpgradeItem() (batchItem, bool) {
	for _, bi := range m.items {
		if bi.status == batchPendingRestart {
			return bi, true
		}
	}
	return batchItem{}, false
}

// view renders the modal centered in the content area (below chrome).
func (m execModal) view(width, height int) string {
	maxW := min(width-8, 80)
	innerW := max(maxW-6, 20) // border(2) + padding(4)
	// Height budget breakdown:
	//   height = visual margin(2) + box chrome(4) + content rows
	//   where box chrome = border(top+bottom=2) + padding(top+bottom=2)
	//   and the 2-row visual margin keeps lipgloss.Place from clipping the
	//   bottom border against whatever the parent draws below us.
	maxContent := max(height-6, 5) // content rows we can fit (header+body+footer)

	var title string
	var body []string
	var actions string

	switch m.phase {
	case execPhaseReview:
		title, body, actions = m.viewReview()
	case execPhaseRunning:
		title, body, actions = m.viewRunning()
	case execPhaseComplete:
		title, body, actions = m.viewComplete()
	}

	// Fixed header: title + separator.
	header := []string{
		sectionTitleStyle.Render(title),
		helpStyle.Render(strings.Repeat("─", innerW)),
	}

	// Fixed footer: blank line + actions (always visible).
	var footer []string
	if actions != "" {
		footer = []string{"", actions}
	}

	// Pre-wrap body lines to the inner width so our sliding window operates
	// on final rendered rows, not pre-wrap logical lines. Otherwise a single
	// wide command string can silently turn into 2–3 rows and push the
	// footer off-screen.
	wrappedBody := wrapModalLines(body, innerW)

	// Remaining space goes to the scrollable body.
	availableBody := maxContent - len(header) - len(footer)
	if availableBody < 1 {
		availableBody = 1
	}

	bodyWindow, scrollInfo := windowBody(wrappedBody, m.scroll, availableBody)
	if scrollInfo != "" && len(footer) >= 1 {
		// Append a compact scroll hint to the actions line so the user
		// knows there's more content and how to reach it.
		footer[len(footer)-1] = scrollInfo + "  •  " + footer[len(footer)-1]
	}

	lines := append([]string{}, header...)
	lines = append(lines, bodyWindow...)
	lines = append(lines, footer...)

	content := strings.Join(lines, "\n")

	// Render in a bordered box. We deliberately don't set MaxHeight —
	// lipgloss clips the bottom border when MaxHeight matches the natural
	// rendered size, so the budget above (availableBody + 2-row visual
	// margin) is what keeps the box within `height`.
	style := lipgloss.NewStyle().
		Width(maxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)

	rendered := style.Render(content)

	// Center in the content area.
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, rendered)
}

// wrapModalLines wraps each logical body line to width and flattens the
// result so the sliding window in view() operates on final visual rows.
// Blank entries stay blank (one row). Trailing/leading whitespace on
// already-short lines is preserved so indented command previews still
// look indented.
func wrapModalLines(lines []string, width int) []string {
	if width < 1 {
		width = 1
	}
	wrapper := lipgloss.NewStyle().Width(width).MaxWidth(width)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(wrapper.Render(line), "\n")...)
	}
	return out
}

// windowBody returns a slice of body lines starting at scroll, fitting within
// window rows, plus a short scroll hint ("↑↓ N/total" style) when the body
// overflows. scroll is clamped to valid range. Returns ("", "") when the body
// fits entirely.
func windowBody(body []string, scroll, window int) ([]string, string) {
	total := len(body)
	if total <= window {
		return body, ""
	}
	maxScroll := total - window
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + window
	hint := helpStyle.Render(fmt.Sprintf("↑↓ scroll %d-%d/%d", scroll+1, end, total))
	return body[scroll:end], hint
}

// maxScroll returns the largest valid scroll offset for the current body.
// Used by the key handler to clamp ↓/PgDn movements. Must mirror the
// layout math in view() so scroll keys stop where the last line lands.
func (m execModal) maxScroll(width, height int) int {
	maxW := min(width-8, 80)
	innerW := max(maxW-6, 20)
	maxContent := max(height-6, 5)

	var body []string
	var actions string
	switch m.phase {
	case execPhaseReview:
		_, body, actions = m.viewReview()
	case execPhaseRunning:
		_, body, actions = m.viewRunning()
	case execPhaseComplete:
		_, body, actions = m.viewComplete()
	}

	headerLines := 2
	footerLines := 0
	if actions != "" {
		footerLines = 2
	}
	availableBody := maxContent - headerLines - footerLines
	if availableBody < 1 {
		availableBody = 1
	}
	wrapped := wrapModalLines(body, innerW)
	if len(wrapped) <= availableBody {
		return 0
	}
	return len(wrapped) - availableBody
}

func (m execModal) viewReview() (string, []string, string) {
	title := fmt.Sprintf("%s %d package(s)", m.actionTitle(), len(m.items))
	if m.action == retryOpApply {
		title = fmt.Sprintf("Apply %d change(s)", len(m.items))
	}
	var body []string
	for _, bi := range m.items {
		line := "  " + checkbox(true) + " " + bi.item.pkg.Name + "  " + helpStyle.Render(bi.item.pkg.ID)
		if m.action == retryOpApply {
			line += "  " + renderActionTag(bi.action)
		}
		body = append(body, line)
		if m.showCommands && bi.command != "" {
			body = append(body, "      "+helpStyle.Render(bi.command))
		}
	}
	if m.action == retryOpUninstall {
		body = append(body, "")
		body = append(body, warnStyle.Render("This will remove the selected packages."))
	} else if m.action == retryOpApply {
		for _, bi := range m.items {
			if bi.action == retryOpUninstall {
				body = append(body, "")
				body = append(body, warnStyle.Render("Uninstall actions are included in this batch."))
				break
			}
		}
	}
	verb := string(m.action)
	if m.action == retryOpApply {
		verb = "apply"
	}
	toggleLabel := "show commands"
	if m.showCommands {
		toggleLabel = "hide commands"
	}
	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " " + verb +
		"  •  " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("?") + " " + toggleLabel +
		"  •  " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel"
	return title, body, actions
}

// renderInlineProgress returns a compact "[████░░░░░░] 42%" style bar for
// use alongside a batch item during execution.
func renderInlineProgress(percent int) string {
	const barWidth = 20
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := percent * barWidth / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return lipgloss.NewStyle().Foreground(accent).Render(bar) +
		" " + helpStyle.Render(fmt.Sprintf("%d%%", percent))
}

func (m execModal) viewRunning() (string, []string, string) {
	completed, _, total := batchCounters(m.items)
	title := fmt.Sprintf("%s %d/%d", m.actionVerb(), completed, total)

	var body []string
	for _, bi := range m.items {
		icon := bi.statusIcon(m.spinner)
		line := "  " + icon + " " + bi.item.pkg.Name
		if m.action == retryOpApply {
			line += "  " + renderActionTag(bi.action)
		}
		if bi.status == batchRunning {
			line += "  " + bi.liveStatus()
		} else if text := bi.statusText(); text != "" {
			line += "  " + text
		}
		body = append(body, line)
	}

	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel"
	return title, body, actions
}

func (m execModal) viewComplete() (string, []string, string) {
	completed, failed, _ := batchCounters(m.items)
	pending := m.pendingRestartCount()
	succeeded := completed - failed

	var title string
	if failed == 0 {
		title = fmt.Sprintf("%s Complete", m.actionTitle())
	} else {
		title = fmt.Sprintf("%s Results", m.actionTitle())
	}

	var body []string

	// Summary line.
	if pending > 0 && failed == 0 {
		body = append(body, warnStyle.Render(fmt.Sprintf("%d completed, %d pending restart", succeeded-pending, pending)))
	} else if pending > 0 {
		body = append(body, warnStyle.Render(fmt.Sprintf("%d completed, %d failed, %d pending restart", succeeded-pending, failed, pending)))
	} else if failed == 0 {
		body = append(body, successStyle.Render(fmt.Sprintf("All %d packages succeeded.", succeeded)))
	} else {
		body = append(body, warnStyle.Render(fmt.Sprintf("%d succeeded, %d failed", succeeded, failed)))
	}
	body = append(body, "")

	// Per-package results with compact log summary.
	for _, bi := range m.items {
		icon := bi.statusIcon(m.spinner)
		line := "  " + icon + " " + lipgloss.NewStyle().Bold(true).Render(bi.item.pkg.Name)
		if m.action == retryOpApply {
			line += "  " + renderActionTag(bi.action)
		}
		if bi.status == batchPendingRestart {
			line += "  " + warnStyle.Render("restart WinTUI to finish upgrade")
		} else if bi.err != nil {
			line += "  " + errorStyle.Render(bi.err.Error())
		} else {
			line += "  " + successStyle.Render("done")
		}
		body = append(body, line)

		if bi.status == batchFailed && bi.blockedByProc {
			body = append(body, "    "+warnStyle.Render("Close the running application and press ctrl+e to retry."))
		}

		// Show key log lines for this package (compact).
		for _, logLine := range extractKeyLogLines(bi.output) {
			body = append(body, "    "+helpStyle.Render(logLine))
		}
	}

	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " close"
	if pending > 0 {
		actions = lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " restart & finish  •  " +
			lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " close"
	}

	// Offer retry via ctrl+e when any failed items could benefit — either
	// elevation candidates or items blocked by a running process (user closes
	// the app manually then retries).
	offerRetry := false
	label := "retry elevated"
	if !appSettings.AutoElevate && !isElevated() && m.hasElevationCandidates() {
		offerRetry = true
	}
	if m.hasBlockedByProcess() {
		offerRetry = true
		if !m.hasElevationCandidates() {
			label = "retry"
		}
	}
	if offerRetry {
		actions = lipgloss.NewStyle().Bold(true).Foreground(accent).Render("ctrl+e") + " " + label + "  •  " + actions
	}

	return title, body, actions
}

// hasElevationCandidates returns true if any failed items could benefit from elevation.
func (m execModal) hasElevationCandidates() bool {
	for _, bi := range m.items {
		if bi.status != batchFailed || bi.err == nil {
			continue
		}
		if likelyBenefitsFromElevation(bi.err, bi.output) {
			return true
		}
	}
	return false
}

// hasBlockedByProcess returns true if any failed items were blocked because a
// related process is running.
func (m execModal) hasBlockedByProcess() bool {
	for _, bi := range m.items {
		if bi.status == batchFailed && bi.blockedByProc {
			return true
		}
	}
	return false
}

// elevationCandidateItems returns the failed items eligible for retry —
// either elevation candidates or those blocked by a running process.
func (m execModal) elevationCandidateItems() []batchItem {
	var items []batchItem
	for _, bi := range m.items {
		if bi.status != batchFailed || bi.err == nil {
			continue
		}
		if likelyBenefitsFromElevation(bi.err, bi.output) || bi.blockedByProc {
			items = append(items, batchItem{
				action:      bi.action,
				item:        bi.item,
				status:      batchQueued,
				allVersions: bi.allVersions,
			})
		}
	}
	return items
}

// extractKeyLogLines picks the most informative lines from a package's output.
func extractKeyLogLines(output string) []string {
	if output == "" {
		return nil
	}
	var result []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip section headers.
		if strings.HasPrefix(trimmed, "==") {
			continue
		}
		// Keep informative lines.
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "successfully") ||
			strings.Contains(lower, "failed") ||
			strings.Contains(lower, "error") ||
			strings.Contains(lower, "no installed package") ||
			strings.Contains(lower, "requires") {
			result = append(result, trimmed)
		}
	}
	return result
}

func (m execModal) helpKeys() []key.Binding {
	switch m.phase {
	case execPhaseReview:
		toggleHelp := "show commands"
		if m.showCommands {
			toggleHelp = "hide commands"
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", string(m.action))),
			key.NewBinding(key.WithKeys("?"), key.WithHelp("?", toggleHelp)),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	case execPhaseRunning:
		return []key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	case execPhaseComplete:
		bindings := []key.Binding{}
		offerRetry := false
		label := "retry elevated"
		if !appSettings.AutoElevate && !isElevated() && m.hasElevationCandidates() {
			offerRetry = true
		}
		if m.hasBlockedByProcess() {
			offerRetry = true
			if !m.hasElevationCandidates() {
				label = "retry"
			}
		}
		if offerRetry {
			bindings = append(bindings, key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", label)))
		}
		enterDesc := "close"
		if m.hasPendingSelfUpgrade() {
			enterDesc = "restart & finish"
			bindings = append(bindings, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")))
		}
		bindings = append(bindings, key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", enterDesc)))
		return bindings
	}
	return nil
}
