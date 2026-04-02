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
	scroll  int // scroll offset for result view
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

func (m execModal) active() bool {
	return len(m.items) > 0
}

// view renders the modal content for the current phase.
func (m execModal) view(width, height int) string {
	maxW := min(width-8, 80)
	innerW := max(maxW-4, 20) // border + padding

	var title string
	var body []string
	var actions string

	switch m.phase {
	case execPhaseReview:
		title, body, actions = m.viewReview(innerW)
	case execPhaseRunning:
		title, body, actions = m.viewRunning(innerW)
	case execPhaseComplete:
		title, body, actions = m.viewComplete(innerW)
	}

	// Build the modal content.
	var content strings.Builder
	content.WriteString(sectionTitleStyle.Render(title) + "\n")
	content.WriteString(helpStyle.Render(strings.Repeat("─", innerW)) + "\n")

	for _, line := range body {
		content.WriteString(line + "\n")
	}

	if actions != "" {
		content.WriteString("\n")
		content.WriteString(actions)
	}

	// Render in a bordered box.
	style := lipgloss.NewStyle().
		Width(maxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)

	rendered := style.Render(content.String())

	// Center on screen.
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, rendered)
}

func (m execModal) viewReview(innerW int) (string, []string, string) {
	title := fmt.Sprintf("%s %d package(s)", m.actionTitle(), len(m.items))
	var body []string
	for _, bi := range m.items {
		body = append(body, "  "+checkbox(true)+" "+bi.item.pkg.Name+"  "+helpStyle.Render(bi.item.pkg.ID))
	}
	body = append(body, "")
	if m.action == "uninstall" {
		body = append(body, warnStyle.Render("This will remove the selected packages."))
	}
	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " " + m.action +
		"  •  " + lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel"
	return title, body, actions
}

func (m execModal) viewRunning(innerW int) (string, []string, string) {
	completed, _, total := batchCounters(m.items)
	title := fmt.Sprintf("%sing %d/%d", m.actionTitle(), completed, total)

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

func (m execModal) viewComplete(innerW int) (string, []string, string) {
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

	// Per-package results.
	for _, bi := range m.items {
		icon := bi.statusIcon(m.spinner)
		line := "  " + icon + " " + bi.item.pkg.Name
		if bi.err != nil {
			line += "  " + helpStyle.Render(bi.err.Error())
		}
		body = append(body, line)
	}

	// Execution log.
	if len(m.log) > 0 {
		body = append(body, "")
		body = append(body, helpStyle.Render("── Execution Log ──"))
		for _, line := range m.log {
			body = append(body, helpStyle.Render(line))
		}
	}

	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " close"
	return title, body, actions
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
