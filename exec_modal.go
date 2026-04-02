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
	phase   execModalPhase
	action  string // "upgrade" or "uninstall"
	items   []batchItem
	itemMap map[string]*batchItem
	idx     int // currently running item index
	log     []string
	spinner spinner.Model
}

func newExecModal(action string, items []batchItem) execModal {
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
	return strings.ToUpper(m.action[:1]) + m.action[1:]
}

func (m execModal) actionVerb() string {
	switch m.action {
	case "upgrade":
		return "Upgrading"
	case "uninstall":
		return "Uninstalling"
	default:
		return m.actionTitle() + "ing"
	}
}

func (m execModal) active() bool {
	return len(m.items) > 0
}

// view renders the modal centered in the content area (below chrome).
func (m execModal) view(width, height int) string {
	maxW := min(width-8, 80)
	innerW := max(maxW-6, 20) // border(2) + padding(4)
	maxH := height - 4        // leave room for chrome above and help below

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

	// Build the modal content lines.
	var lines []string
	lines = append(lines, sectionTitleStyle.Render(title))
	lines = append(lines, helpStyle.Render(strings.Repeat("─", innerW)))

	lines = append(lines, body...)

	if actions != "" {
		lines = append(lines, "")
		lines = append(lines, actions)
	}

	// Cap content height — ensure action button always visible.
	// Reserve 3 lines: blank + actions + bottom padding.
	maxBodyLines := maxH - 3
	if len(lines) > maxBodyLines {
		lines = lines[:maxBodyLines]
	}

	content := strings.Join(lines, "\n")

	// Render in a bordered box.
	style := lipgloss.NewStyle().
		Width(maxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)

	rendered := style.Render(content)

	// Center in the content area.
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, rendered)
}

func (m execModal) viewReview() (string, []string, string) {
	title := fmt.Sprintf("%s %d package(s)", m.actionTitle(), len(m.items))
	var body []string
	for _, bi := range m.items {
		body = append(body, "  "+checkbox(true)+" "+bi.item.pkg.Name+"  "+helpStyle.Render(bi.item.pkg.ID))
	}
	if m.action == "uninstall" {
		body = append(body, "")
		body = append(body, warnStyle.Render("This will remove the selected packages."))
	}
	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " " + m.action +
		"  •  " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel"
	return title, body, actions
}

func (m execModal) viewRunning() (string, []string, string) {
	completed, _, total := batchCounters(m.items)
	title := fmt.Sprintf("%s %d/%d", m.actionVerb(), completed, total)

	var body []string
	for _, bi := range m.items {
		icon := bi.statusIcon(m.spinner)
		line := "  " + icon + " " + bi.item.pkg.Name
		if text := bi.statusText(); text != "" {
			line += "  " + text
		}
		body = append(body, line)
	}

	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel"
	return title, body, actions
}

func (m execModal) viewComplete() (string, []string, string) {
	completed, failed, _ := batchCounters(m.items)
	succeeded := completed - failed

	var title string
	if failed == 0 {
		title = fmt.Sprintf("%s Complete", m.actionTitle())
	} else {
		title = fmt.Sprintf("%s Results", m.actionTitle())
	}

	var body []string

	// Summary line.
	if failed == 0 {
		body = append(body, successStyle.Render(fmt.Sprintf("All %d packages succeeded.", succeeded)))
	} else {
		body = append(body, warnStyle.Render(fmt.Sprintf("%d succeeded, %d failed", succeeded, failed)))
	}
	body = append(body, "")

	// Per-package results with compact log summary.
	for _, bi := range m.items {
		icon := bi.statusIcon(m.spinner)
		line := "  " + icon + " " + lipgloss.NewStyle().Bold(true).Render(bi.item.pkg.Name)
		if bi.err != nil {
			line += "  " + errorStyle.Render(bi.err.Error())
		} else {
			line += "  " + successStyle.Render("done")
		}
		body = append(body, line)

		// Show key log lines for this package (compact).
		for _, logLine := range extractKeyLogLines(bi.output) {
			body = append(body, "    "+helpStyle.Render(logLine))
		}
	}

	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " close"
	return title, body, actions
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
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", m.action)),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	case execPhaseRunning:
		return []key.Binding{
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	case execPhaseComplete:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "close")),
		}
	}
	return nil
}
