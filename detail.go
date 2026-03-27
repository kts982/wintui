package main

import (
	"context"
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

type packageVersionsMsg struct {
	pkgID    string
	source   string
	versions []string
	err      error
}

type detailVersionSelectedMsg struct {
	pkgID   string
	source  string
	version string
}

// fetchDetail returns a Cmd that fetches package details async.
func fetchDetail(id, source string) tea.Cmd {
	return func() tea.Msg {
		d, err := showPackage(id, source)
		return packageDetailMsg{detail: d, err: err}
	}
}

func fetchVersions(id, source string) tea.Cmd {
	return func() tea.Msg {
		versions, err := showPackageVersionsCtx(context.Background(), id, source)
		return packageVersionsMsg{pkgID: id, source: source, versions: versions, err: err}
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
	state              detailState
	detail             PackageDetail
	spinner            spinner.Model
	scroll             int
	err                error
	pkgID              string
	source             string
	allowVersionSelect bool
	selectedVersion    string
	versions           []string
	versionCursor      int
	versionsLoading    bool
	selectingVersion   bool
	versionErr         string
}

func newDetailPanel() detailPanel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return detailPanel{state: detailHidden, spinner: sp}
}

// show starts loading details for a package.
func (p detailPanel) show(pkgID, source string) (detailPanel, tea.Cmd) {
	return p.showWithVersion(pkgID, source, "", false)
}

func (p detailPanel) showWithVersion(pkgID, source, selectedVersion string, allowVersionSelect bool) (detailPanel, tea.Cmd) {
	p.state = detailLoading
	p.scroll = 0
	p.err = nil
	p.pkgID = pkgID
	p.source = source
	p.allowVersionSelect = allowVersionSelect
	p.selectedVersion = selectedVersion
	p.versions = nil
	p.versionCursor = 0
	p.versionsLoading = false
	p.selectingVersion = false
	p.versionErr = ""
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

	case packageVersionsMsg:
		if msg.pkgID != p.pkgID || msg.source != p.source {
			return p, nil, false
		}
		if !p.versionsLoading {
			return p, nil, true
		}
		p.versionsLoading = false
		if msg.err != nil {
			p.versionErr = msg.err.Error()
			return p, nil, true
		}
		p.versions = msg.versions
		p.versionCursor = 0
		if p.selectedVersion != "" {
			for i, version := range p.versions {
				if version == p.selectedVersion {
					p.versionCursor = i
					break
				}
			}
		}
		p.selectingVersion = len(p.versions) > 0
		if len(p.versions) == 0 {
			p.versionErr = "No explicit versions reported for this package."
		} else {
			p.versionErr = ""
		}
		return p, nil, true

	case tea.KeyPressMsg:
		if p.versionsLoading {
			if msg.String() == "esc" {
				p.versionsLoading = false
				return p, nil, true
			}
		}

		if p.selectingVersion {
			switch msg.String() {
			case "up", "k":
				if p.versionCursor > 0 {
					p.versionCursor--
				}
				return p, nil, true
			case "down", "j":
				if p.versionCursor < len(p.versions)-1 {
					p.versionCursor++
				}
				return p, nil, true
			case "enter":
				if len(p.versions) == 0 {
					return p, nil, true
				}
				p.selectedVersion = p.versions[p.versionCursor]
				p.selectingVersion = false
				p.scroll = 0
				return p, func() tea.Msg {
					return detailVersionSelectedMsg{pkgID: p.pkgID, source: p.source, version: p.selectedVersion}
				}, true
			case "c":
				p.selectedVersion = ""
				p.selectingVersion = false
				p.scroll = 0
				p.versionErr = ""
				return p, func() tea.Msg {
					return detailVersionSelectedMsg{pkgID: p.pkgID, source: p.source, version: ""}
				}, true
			case "esc":
				p.selectingVersion = false
				return p, nil, true
			}
		}

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
		case "v":
			if p.state == detailReady && p.allowVersionSelect {
				p.versionErr = ""
				if len(p.versions) > 0 {
					p.selectingVersion = true
					return p, nil, true
				}
				p.versionsLoading = true
				return p, tea.Batch(p.spinner.Tick, fetchVersions(p.pkgID, p.source)), true
			}
		case "c":
			if p.state == detailReady && p.allowVersionSelect && p.selectedVersion != "" {
				p.selectedVersion = ""
				p.versionErr = ""
				return p, func() tea.Msg {
					return detailVersionSelectedMsg{pkgID: p.pkgID, source: p.source, version: ""}
				}, true
			}
		}

	case spinner.TickMsg:
		if p.state == detailLoading || p.versionsLoading {
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
		if p.versionsLoading {
			return []key.Binding{keyEsc}
		}
		if p.selectingVersion {
			return []key.Binding{keyUp, keyDown, keyEnter, keyUseLatest, keyEsc}
		}
		if p.allowVersionSelect {
			bindings := []key.Binding{keyUp, keyDown, keyVersion}
			if p.selectedVersion != "" {
				bindings = append(bindings, keyUseLatest)
			}
			bindings = append(bindings, keyOpen, keyEsc)
			return bindings
		}
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
		return indentBlock(panelStyle.Render(inner), 2)

	case detailError:
		inner := errorStyle.Render("  Error: "+p.err.Error()) + "\n\n" +
			helpStyle.Render("  esc close")
		return indentBlock(panelStyle.Render(inner), 2)

	case detailReady:
		if p.versionsLoading {
			title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.detail.Name)
			if p.pkgID != "" {
				title += "  " + helpStyle.Render(p.pkgID)
			}
			inner := "  " + title + "\n\n" +
				fmt.Sprintf("  %s Loading available versions...", p.spinner.View()) + "\n\n" +
				helpStyle.Render("  esc back")
			return indentBlock(panelStyle.Render(inner), 2)
		}
		if p.selectingVersion {
			return p.renderVersionPicker(panelStyle, height)
		}

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
		if p.allowVersionSelect {
			targetVersion := "latest"
			if p.selectedVersion != "" {
				targetVersion = p.selectedVersion
			}
			addField("Target Version", targetVersion)
		}
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
		if p.allowVersionSelect {
			help = "  ↑↓ scroll • v choose version • esc close"
			if p.selectedVersion != "" {
				help = "  ↑↓ scroll • v change version • c latest • esc close"
			}
			if d.Homepage != "" {
				help += " • o open homepage"
			}
		} else if d.Homepage != "" {
			help = "  ↑↓ scroll • o open homepage • esc close"
		}
		b.WriteString(helpStyle.Render(help))
		if p.versionErr != "" {
			b.WriteString("\n" + warnStyle.Render("  "+p.versionErr))
		}

		return indentBlock(panelStyle.Render(b.String()), 2)
	}

	return ""
}

func (p detailPanel) renderVersionPicker(panelStyle lipgloss.Style, height int) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.detail.Name)
	if p.pkgID != "" {
		title += "  " + helpStyle.Render(p.pkgID)
	}
	b.WriteString("  " + title + "\n\n")

	targetVersion := "latest"
	if p.selectedVersion != "" {
		targetVersion = p.selectedVersion
	}
	b.WriteString("  " + infoStyle.Render("Choose Target Version") + "\n")
	b.WriteString("  " + helpStyle.Render("Current: "+targetVersion) + "\n\n")

	if len(p.versions) == 0 {
		b.WriteString("  " + warnStyle.Render("No versions available for selection.") + "\n\n")
		b.WriteString(helpStyle.Render("  esc back"))
		return indentBlock(panelStyle.Render(b.String()), 2)
	}

	maxVisible := height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}
	start, end := scrollWindow(p.versionCursor, len(p.versions), maxVisible)
	for i := start; i < end; i++ {
		cursor := cursorBlankStr
		style := itemStyle
		if i == p.versionCursor {
			cursor = cursorStr
			style = itemActiveStyle
		}
		version := p.versions[i]
		suffix := ""
		if version == p.selectedVersion {
			suffix = helpStyle.Render("  [selected]")
		}
		fmt.Fprintf(&b, "  %s%s%s\n", cursor, style.Render(version), suffix)
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  ↑↓ choose • enter select • c latest • esc back"))
	return indentBlock(panelStyle.Render(b.String()), 2)
}

// ── Open URL ───────────────────────────────────────────────────────

func openURL(url string) {
	if runtime.GOOS == "windows" {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	} else {
		exec.Command("xdg-open", url).Start()
	}
}
