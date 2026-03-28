package main

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	confirmModalStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accent).
				Background(lipgloss.Color("235")).
				Padding(1, 2)

	confirmDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
)

type confirmModal struct {
	title       string
	body        []string
	confirmVerb string
}

func renderConfirmModal(background string, width, height int, modal confirmModal) string {
	if modal.confirmVerb == "" {
		modal.confirmVerb = "confirm"
	}

	bg := lipgloss.Place(
		width,
		height,
		lipgloss.Left,
		lipgloss.Top,
		lipgloss.NewStyle().Faint(true).Render(strings.TrimRight(background, "\n")),
	)

	modalWidth := min(64, width-12)
	if modalWidth < 40 {
		modalWidth = max(32, width-6)
	}
	contentWidth := max(24, modalWidth-6)
	innerWidth := max(20, contentWidth)

	var body strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(modal.title)
	body.WriteString(title)
	body.WriteString("\n")
	dividerWidth := min(36, max(18, innerWidth-12))
	body.WriteString(lipgloss.PlaceHorizontal(
		innerWidth,
		lipgloss.Left,
		confirmDividerStyle.Render(strings.Repeat("─", dividerWidth)),
	))
	for _, line := range modal.body {
		body.WriteString("\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		body.WriteString(lipgloss.NewStyle().Width(innerWidth).Render(line))
	}
	body.WriteString("\n\n")
	body.WriteString(lipgloss.PlaceHorizontal(
		innerWidth,
		lipgloss.Center,
		renderModalHint(modal.confirmVerb),
	))

	panel := confirmModalStyle.Width(contentWidth).Render(body.String())
	panelWidth := lipgloss.Width(panel)
	panelHeight := lipgloss.Height(panel)

	x := max(0, (width-panelWidth)/2)
	y := max(2, (height-panelHeight)/3)

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(bg).Z(0),
		lipgloss.NewLayer(panel).
			X(x).
			Y(y).
			Z(1),
	)

	return compositor.Render()
}

func renderModalHint(confirmVerb string) string {
	enterKey := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("enter")
	escKey := lipgloss.NewStyle().Foreground(accent).Bold(true).Render("esc")
	return lipgloss.JoinHorizontal(
		lipgloss.Center,
		enterKey,
		helpStyle.Render(" "+confirmVerb+"  •  "),
		escKey,
		helpStyle.Render(" cancel"),
	)
}

func summarizeModalItems(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	summary := append([]string(nil), items[:limit]...)
	summary = append(summary, helpStyle.Render("… "+strconv.Itoa(len(items)-limit)+" more"))
	return summary
}
