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

type detailFetchRole string

const (
	detailFetchTarget    detailFetchRole = "target"
	detailFetchInstalled detailFetchRole = "installed"
)

type packageDetailMsg struct {
	role    detailFetchRole
	pkgID   string
	source  string
	version string
	detail  PackageDetail
	err     error
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
func fetchDetail(role detailFetchRole, id, source, version string) tea.Cmd {
	return func() tea.Msg {
		d, err := showPackage(id, source, version)
		return packageDetailMsg{
			role:    role,
			pkgID:   id,
			source:  source,
			version: version,
			detail:  d,
			err:     err,
		}
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
	compareDetail      PackageDetail
	spinner            spinner.Model
	scroll             int
	err                error
	compareErr         error
	pkgID              string
	source             string
	allowVersionSelect bool
	selectedVersion    string
	latestVersion      string
	installedVersion   string
	versions           []string
	versionCursor      int
	versionsLoading    bool
	compareLoading     bool
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
	return p.showWithVersion(Package{ID: pkgID, Source: source}, "", false)
}

func (p detailPanel) showWithVersion(pkg Package, selectedVersion string, allowVersionSelect bool) (detailPanel, tea.Cmd) {
	samePackage := p.pkgID == pkg.ID && p.source == pkg.Source

	p.state = detailLoading
	p.scroll = 0
	p.err = nil
	p.compareErr = nil
	p.detail = PackageDetail{}
	p.compareDetail = PackageDetail{}
	p.pkgID = pkg.ID
	p.source = pkg.Source
	p.allowVersionSelect = allowVersionSelect
	p.selectedVersion = selectedVersion
	p.latestVersion = ""
	p.installedVersion = ""
	if allowVersionSelect {
		if pkg.Available != "" {
			p.installedVersion = pkg.Version
			p.latestVersion = pkg.Available
		} else if pkg.Version != "" {
			p.latestVersion = pkg.Version
		}
	}
	if !samePackage {
		p.versions = nil
	}
	p.versionCursor = 0
	if p.selectedVersion != "" {
		for i, version := range p.versions {
			if version == p.selectedVersion {
				p.versionCursor = i
				break
			}
		}
	}
	p.versionsLoading = false
	p.compareLoading = p.installedVersion != ""
	p.selectingVersion = false
	p.versionErr = ""

	cmds := []tea.Cmd{
		p.spinner.Tick,
		fetchDetail(detailFetchTarget, p.pkgID, p.source, p.targetVersion()),
	}
	return p, tea.Batch(cmds...)
}

func (p detailPanel) targetVersion() string {
	if strings.TrimSpace(p.selectedVersion) != "" {
		return p.selectedVersion
	}
	return p.latestVersion
}

func (p detailPanel) targetVersionLabel() string {
	if strings.TrimSpace(p.selectedVersion) != "" {
		return p.selectedVersion
	}
	if strings.TrimSpace(p.latestVersion) != "" {
		return "latest"
	}
	return ""
}

func (p detailPanel) title() string {
	switch {
	case p.detail.Name != "":
		return p.detail.Name
	case p.compareDetail.Name != "":
		return p.compareDetail.Name
	case p.pkgID != "":
		return p.pkgID
	default:
		return "Package Details"
	}
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
		if msg.pkgID != p.pkgID || msg.source != p.source {
			return p, nil, false
		}
		switch msg.role {
		case detailFetchTarget:
			if msg.version != p.targetVersion() {
				return p, nil, true
			}
			if msg.err != nil {
				p.err = msg.err
				p.state = detailError
			} else {
				p.detail = msg.detail
				p.state = detailReady
				if p.compareLoading {
					if p.installedVersion == msg.version {
						p.compareDetail = msg.detail
						p.compareErr = nil
						p.compareLoading = false
					} else {
						return p, fetchDetail(detailFetchInstalled, p.pkgID, p.source, p.installedVersion), true
					}
				}
			}
		case detailFetchInstalled:
			if msg.version != p.installedVersion {
				return p, nil, true
			}
			p.compareLoading = false
			if msg.err != nil {
				p.compareErr = msg.err
			} else {
				p.compareDetail = msg.detail
				p.compareErr = nil
			}
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
		if p.state == detailLoading || p.versionsLoading || p.compareLoading {
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

func appendDetailField(lines *[]string, label, value string) {
	if value == "" {
		return
	}
	*lines = append(*lines, fmt.Sprintf("  %s  %s",
		lipgloss.NewStyle().Bold(true).Width(18).Render(label),
		value))
}

func appendDetailSection(lines *[]string, heading string, d PackageDetail, includeReleaseNotes bool) {
	*lines = append(*lines, "")
	*lines = append(*lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(accent).Render(heading))
	appendDetailField(lines, "Version", d.Version)
	appendDetailField(lines, "Publisher", d.Publisher)
	appendDetailField(lines, "License", d.License)
	appendDetailField(lines, "Moniker", d.Moniker)
	if d.Homepage != "" {
		appendDetailField(lines, "Homepage", lipgloss.NewStyle().Foreground(secondary).Underline(true).Render(d.Homepage))
	}
	appendDetailField(lines, "Release Date", d.ReleaseDate)
	appendDetailField(lines, "Installer", d.InstallerType)
	if d.Tags != "" {
		appendDetailField(lines, "Tags", strings.ReplaceAll(d.Tags, "\n", ", "))
	}
	appendDetailField(lines, "Copyright", d.Copyright)

	if d.Description != "" {
		*lines = append(*lines, "")
		*lines = append(*lines, "  "+lipgloss.NewStyle().Bold(true).Render("Description"))
		for _, dl := range strings.Split(d.Description, "\n") {
			*lines = append(*lines, "  "+dl)
		}
	}

	if includeReleaseNotes && d.ReleaseNotes != "" {
		*lines = append(*lines, "")
		*lines = append(*lines, "  "+lipgloss.NewStyle().Bold(true).Render("Release Notes"))
		for _, rl := range strings.Split(d.ReleaseNotes, "\n") {
			*lines = append(*lines, "  "+rl)
		}
	}
}

// view renders the detail panel.
func (p detailPanel) view(width, height int) string {
	if p.state == detailHidden {
		return ""
	}

	var b strings.Builder

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
			title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.title())
			if p.pkgID != "" && p.pkgID != p.title() {
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

		title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.title())
		if p.pkgID != "" && p.pkgID != p.title() {
			title += "  " + helpStyle.Render(p.pkgID)
		}
		b.WriteString("  " + title + "\n")

		var lines []string
		if p.installedVersion != "" {
			appendDetailField(&lines, "Installed Version", p.installedVersion)
		}
		if p.latestVersion != "" {
			appendDetailField(&lines, "Latest Available", p.latestVersion)
		}
		if p.allowVersionSelect {
			appendDetailField(&lines, "Target Version", p.targetVersionLabel())
		}
		source := p.detail.Source
		if source == "" {
			source = p.source
		}
		appendDetailField(&lines, "Source", source)

		sectionTitle := "Package Details"
		if p.allowVersionSelect {
			sectionTitle = "Target Details"
		}
		appendDetailSection(&lines, sectionTitle, p.detail, true)
		if p.installedVersion != "" {
			switch {
			case p.compareLoading:
				lines = append(lines, "")
				lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Installed Details"))
				lines = append(lines, fmt.Sprintf("  %s Loading installed version details...", p.spinner.View()))
			case p.compareErr != nil:
				lines = append(lines, "")
				lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Installed Details"))
				lines = append(lines, "  "+warnStyle.Render("Unable to load installed version details: "+p.compareErr.Error()))
			default:
				appendDetailSection(&lines, "Installed Details", p.compareDetail, false)
			}
		}

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

		b.WriteString("\n")
		help := "  ↑↓ scroll • esc close"
		if p.allowVersionSelect {
			help = "  ↑↓ scroll • v choose version • esc close"
			if p.selectedVersion != "" {
				help = "  ↑↓ scroll • v change version • c latest • esc close"
			}
			if p.detail.Homepage != "" {
				help += " • o open homepage"
			}
		} else if p.detail.Homepage != "" {
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
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.title())
	if p.pkgID != "" && p.pkgID != p.title() {
		title += "  " + helpStyle.Render(p.pkgID)
	}
	b.WriteString("  " + title + "\n\n")

	targetVersion := p.targetVersionLabel()
	if targetVersion == "" {
		targetVersion = "latest"
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
