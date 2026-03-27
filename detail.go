package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

// ── Messages ───────────────────────────────────────────────────────

type packageDetailMsg struct {
	detail PackageDetail
	err    error
}

// fetchDetail returns a Cmd that fetches package details async.
func fetchDetail(id, source string) tea.Cmd {
	return func() tea.Msg {
		d, err := showPackage(id, source)
		return packageDetailMsg{detail: d, err: err}
	}
}

// ── Detail overlay ─────────────────────────────────────────────────
//
// This is a reusable sub-component that any screen can embed.
// It handles its own loading state, rendering, and key events.

type detailState int

const (
	detailHidden detailState = iota
	detailLoading
	detailReady
	detailError
)

type detailPanel struct {
	state   detailState
	detail  PackageDetail
	spinner spinner.Model
	scroll  int
	err     error
}

func newDetailPanel() detailPanel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return detailPanel{state: detailHidden, spinner: sp}
}

// show starts loading details for a package.
func (p detailPanel) show(pkgID, source string) (detailPanel, tea.Cmd) {
	p.state = detailLoading
	p.scroll = 0
	p.err = nil
	return p, tea.Batch(p.spinner.Tick, fetchDetail(pkgID, source))
}

// visible returns true if the panel is showing.
func (p detailPanel) visible() bool {
	return p.state != detailHidden
}

// update handles messages for the detail panel.
// Returns (panel, cmd, handled). If handled is true, the parent should not process the message further.
func (p detailPanel) update(msg tea.Msg) (detailPanel, tea.Cmd, bool) {
	if p.state == detailHidden {
		return p, nil, false
	}

	switch msg := msg.(type) {
	case packageDetailMsg:
		if msg.err != nil {
			p.err = msg.err
			p.state = detailError
		} else {
			p.detail = msg.detail
			p.state = detailReady
		}
		return p, nil, true

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			p.state = detailHidden
			return p, nil, true
		case "up", "k":
			if p.scroll > 0 {
				p.scroll--
			}
			return p, nil, true
		case "down", "j":
			p.scroll++
			return p, nil, true
		case "o":
			if p.state == detailReady && p.detail.Homepage != "" {
				openURL(p.detail.Homepage)
			}
			return p, nil, true
		}

	case spinner.TickMsg:
		if p.state == detailLoading {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			return p, cmd, true
		}
	}

	return p, nil, false
}

func (p detailPanel) helpKeys() []key.Binding {
	switch p.state {
	case detailLoading, detailError:
		return []key.Binding{keyEsc}
	case detailReady:
		return []key.Binding{keyUp, keyDown, keyOpen, keyEsc}
	}
	return nil
}

// view renders the detail panel.
func (p detailPanel) view(width, height int) string {
	if p.state == detailHidden {
		return ""
	}

	var b strings.Builder

	// Panel border
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1).
		Width(width - 4)

	switch p.state {
	case detailLoading:
		inner := fmt.Sprintf("  %s Loading package details...", p.spinner.View())
		return panelStyle.Render(inner)

	case detailError:
		inner := errorStyle.Render("  Error: "+p.err.Error()) + "\n\n" +
			helpStyle.Render("  esc close")
		return panelStyle.Render(inner)

	case detailReady:
		d := p.detail

		// Title
		title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(d.Name)
		if d.ID != "" {
			title += "  " + helpStyle.Render(d.ID)
		}
		b.WriteString("  " + title + "\n")

		// Build detail lines
		var lines []string
		addField := func(label, value string) {
			if value != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s",
					lipgloss.NewStyle().Bold(true).Width(16).Render(label),
					value))
			}
		}

		addField("Version", d.Version)
		addField("Source", d.Source)
		addField("Publisher", d.Publisher)
		addField("License", d.License)
		addField("Moniker", d.Moniker)
		if d.Homepage != "" {
			addField("Homepage", lipgloss.NewStyle().Foreground(secondary).Underline(true).Render(d.Homepage))
		}
		if d.ReleaseDate != "" {
			addField("Release Date", d.ReleaseDate)
		}
		addField("Installer", d.InstallerType)

		if d.Tags != "" {
			tags := strings.ReplaceAll(d.Tags, "\n", ", ")
			addField("Tags", tags)
		}

		if d.Copyright != "" {
			addField("Copyright", d.Copyright)
		}

		// Description (may be multi-line)
		if d.Description != "" {
			lines = append(lines, "")
			lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Render("Description"))
			for _, dl := range strings.Split(d.Description, "\n") {
				lines = append(lines, "  "+dl)
			}
		}

		// Release notes (may be long)
		if d.ReleaseNotes != "" {
			lines = append(lines, "")
			lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Render("Release Notes"))
			for _, rl := range strings.Split(d.ReleaseNotes, "\n") {
				lines = append(lines, "  "+rl)
			}
		}

		// Scroll
		innerHeight := height - 6
		if innerHeight < 5 {
			innerHeight = 5
		}
		totalLines := len(lines)
		start := p.scroll
		if totalLines > innerHeight && start > totalLines-innerHeight {
			start = totalLines - innerHeight
		}
		if start < 0 {
			start = 0
		}
		end := start + innerHeight
		if end > totalLines {
			end = totalLines
		}

		for i := start; i < end; i++ {
			b.WriteString(lines[i] + "\n")
		}

		// Help
		b.WriteString("\n")
		help := "  ↑↓ scroll • esc close"
		if d.Homepage != "" {
			help = "  ↑↓ scroll • o open homepage • esc close"
		}
		b.WriteString(helpStyle.Render(help))

		return panelStyle.Render(b.String())
	}

	return ""
}

// ── Open URL ───────────────────────────────────────────────────────

func openURL(url string) {
	if runtime.GOOS == "windows" {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	} else {
		exec.Command("xdg-open", url).Start()
	}
}
