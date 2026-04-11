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
func fetchDetail(id, source, version string) tea.Cmd {
	return func() tea.Msg {
		d, err := showPackage(id, source, version)
		return packageDetailMsg{
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
	spinner            spinner.Model
	scroll             int
	err                error
	pkgID              string
	source             string
	allowVersionSelect bool
	selectedVersion    string
	latestVersion      string
	installedVersion   string
	versions           []string
	versionCursor      int
	versionsLoading    bool
	selectingVersion   bool
	versionErr         string
	windowWidth        int
	windowHeight       int

	editingOverrides bool
	overrideCursor   int
	overrideEdit     PackageOverride
	overrideSaved    bool
	overrideDeleted  bool
	overrideErrMsg   string
}

func newDetailPanel() detailPanel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return detailPanel{state: detailHidden, spinner: sp, windowWidth: 80, windowHeight: 24}
}

func (p detailPanel) showWithVersion(pkg Package, selectedVersion string, allowVersionSelect bool) (detailPanel, tea.Cmd) {
	samePackage := p.pkgID == pkg.ID && p.source == pkg.Source

	p.state = detailLoading
	p.scroll = 0
	p.err = nil
	p.detail = PackageDetail{}
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
	p.selectingVersion = false
	p.versionErr = ""
	p.editingOverrides = false
	p.overrideSaved = false
	p.overrideDeleted = false

	cmds := []tea.Cmd{
		p.spinner.Tick,
		fetchDetail(p.pkgID, p.source, p.targetVersion()),
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

func (p detailPanel) withWindowSize(width, height int) detailPanel {
	if width > 0 {
		p.windowWidth = width
	}
	if height > 0 {
		p.windowHeight = height
	}
	return p.clampScroll()
}

func (p detailPanel) title() string {
	switch {
	case p.detail.Name != "":
		return p.detail.Name
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
	case tea.WindowSizeMsg:
		p = p.withWindowSize(msg.Width, msg.Height)
		return p, nil, true

	case packageDetailMsg:
		if msg.pkgID != p.pkgID || msg.source != p.source {
			return p, nil, false
		}
		if msg.version != p.targetVersion() {
			return p, nil, true
		}
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
			return p, nil, true
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
			case "pgup":
				p.versionCursor -= 8
				if p.versionCursor < 0 {
					p.versionCursor = 0
				}
				return p, nil, true
			case "pgdown":
				p.versionCursor += 8
				if p.versionCursor > len(p.versions)-1 {
					p.versionCursor = len(p.versions) - 1
				}
				if p.versionCursor < 0 {
					p.versionCursor = 0
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

		if p.editingOverrides {
			return p.updateOverrideEditor(msg)
		}

		switch msg.String() {
		case "esc", "left", "h":
			p.state = detailHidden
			return p, nil, true
		case "up", "k":
			if p.scroll > 0 {
				p.scroll--
			}
			return p, nil, true
		case "down", "j":
			if p.scroll < p.maxScroll() {
				p.scroll++
			}
			return p, nil, true
		case "pgup":
			p.scroll -= 8
			if p.scroll < 0 {
				p.scroll = 0
			}
			return p, nil, true
		case "pgdown":
			p.scroll += 8
			if p.scroll > p.maxScroll() {
				p.scroll = p.maxScroll()
			}
			return p, nil, true
		case "o":
			if p.state == detailReady && p.canOpenHomepage() {
				openExternalURL(p.detail.Homepage)
			}
			return p, nil, true
		case "n":
			if p.state == detailReady && p.canOpenReleaseNotes() {
				openExternalURL(p.detail.ReleaseNotesURL)
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
		case "p":
			if p.state == detailReady && p.pkgID != "" {
				p.editingOverrides = true
				p.overrideCursor = 0
				p.overrideEdit = appSettings.getOverride(p.pkgID, p.source)
				p.overrideSaved = false
				p.overrideDeleted = false
				p.overrideErrMsg = ""
				return p, nil, true
			}
		case "i":
			if p.state == detailReady && p.pkgID != "" {
				return p.toggleIgnore()
			}
		}
		return p, nil, true

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
		return []key.Binding{keyEscOrLeft}
	case detailReady:
		if p.versionsLoading {
			return []key.Binding{keyEscOrLeft}
		}
		if p.editingOverrides {
			return []key.Binding{keyCycle, keySaveOverrides, keyClearOverrides, keyEscCancel}
		}
		if p.selectingVersion {
			return []key.Binding{keyScroll, keyEnter, keyUseLatest, keyEscOrLeft}
		}
		if p.allowVersionSelect {
			bindings := []key.Binding{keyScroll, keyVersion}
			if p.selectedVersion != "" {
				bindings = append(bindings, keyUseLatest)
			}
			bindings = append(bindings, keyIgnore, keyOverrides)
			if p.canOpenHomepage() {
				bindings = append(bindings, keyOpen)
			}
			if p.canOpenReleaseNotes() {
				bindings = append(bindings, keyReleaseNotes)
			}
			bindings = append(bindings, keyEscOrLeft)
			return bindings
		}
		bindings := []key.Binding{keyScroll, keyIgnore, keyOverrides}
		if p.canOpenHomepage() {
			bindings = append(bindings, keyOpen)
		}
		if p.canOpenReleaseNotes() {
			bindings = append(bindings, keyReleaseNotes)
		}
		bindings = append(bindings, keyEscOrLeft)
		return bindings
	}
	return nil
}

func (p detailPanel) canOpenHomepage() bool {
	return strings.TrimSpace(p.detail.Homepage) != ""
}

func (p detailPanel) canOpenReleaseNotes() bool {
	return strings.TrimSpace(p.detail.ReleaseNotesURL) != ""
}

func appendDetailField(lines *[]string, label, value string) {
	if value == "" {
		return
	}
	*lines = append(*lines, fmt.Sprintf("  %s  %s",
		lipgloss.NewStyle().Bold(true).Width(18).Render(label),
		value))
}

func appendDetailSection(lines *[]string, heading, releaseNotesHeading string, d PackageDetail) {
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

	if d.ReleaseNotes != "" || d.ReleaseNotesURL != "" {
		*lines = append(*lines, "")
		heading := "Release Notes"
		if releaseNotesHeading != "" {
			heading = releaseNotesHeading
		}
		*lines = append(*lines, "  "+lipgloss.NewStyle().Bold(true).Render(heading))
		if d.ReleaseNotes != "" {
			for _, rl := range strings.Split(d.ReleaseNotes, "\n") {
				*lines = append(*lines, "  "+rl)
			}
		}
		if d.ReleaseNotesURL != "" {
			if d.ReleaseNotes != "" {
				*lines = append(*lines, "")
			}
			*lines = append(*lines, "  "+lipgloss.NewStyle().Foreground(secondary).Underline(true).Render(d.ReleaseNotesURL))
		}
	}
}

func detailContentWidth(totalWidth int) int {
	return max(20, totalWidth-8)
}

func wrapDetailLines(lines []string, width int) []string {
	wrapper := lipgloss.NewStyle().Width(width).MaxWidth(width)
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			wrapped = append(wrapped, "")
			continue
		}
		wrapped = append(wrapped, strings.Split(wrapper.Render(line), "\n")...)
	}
	return wrapped
}

func (p detailPanel) detailLines(totalWidth int) []string {
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

	if appSettings.hasOverride(p.pkgID, p.source) {
		o := appSettings.getOverride(p.pkgID, p.source)
		lines = append(lines, "")
		lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Package Rules"))
		if o.Ignore {
			appendDetailField(&lines, "Ignore", lipgloss.NewStyle().Foreground(warning).Render("all versions"))
		} else if o.IgnoreVersion != "" {
			appendDetailField(&lines, "Ignore", lipgloss.NewStyle().Foreground(warning).Render("v"+o.IgnoreVersion))
		}
		if o.Scope != "" {
			appendDetailField(&lines, "Scope", o.Scope)
		}
		if o.Architecture != "" {
			appendDetailField(&lines, "Architecture", o.Architecture)
		}
		if o.Elevate != nil {
			label := "never"
			if *o.Elevate {
				label = "always"
			}
			appendDetailField(&lines, "Elevate", label)
		}
	}

	if preview := p.commandPreview(); preview != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(accent).Render("Command Preview"))
		lines = append(lines, "  "+helpStyle.Render(preview))
	}

	sectionTitle := "Package Details"
	if p.allowVersionSelect {
		sectionTitle = "Target Details"
	}
	appendDetailSection(&lines, sectionTitle, p.releaseNotesHeading(), p.detail)

	panelWidth := max(24, totalWidth-4)
	return wrapDetailLines(lines, detailContentWidth(panelWidth))
}

// commandPreview returns a copy-pasteable shell command representing the
// action this detail context would run (upgrade if the package is installed,
// install otherwise). Returns "" when no action applies to this view.
func (p detailPanel) commandPreview() string {
	if p.pkgID == "" {
		return ""
	}
	if !p.allowVersionSelect && p.installedVersion == "" {
		return ""
	}
	version := strings.TrimSpace(p.selectedVersion)
	var args []string
	if p.installedVersion != "" {
		args = upgradeCommandArgs(p.pkgID, p.source, version)
	} else {
		args = installCommandArgs(p.pkgID, p.source, version)
	}
	return formatWingetCommand(args)
}

func (p detailPanel) releaseNotesHeading() string {
	if !p.allowVersionSelect {
		return "Release Notes"
	}
	version := strings.TrimSpace(p.detail.Version)
	if version == "" {
		version = strings.TrimSpace(p.targetVersion())
	}
	if version == "" || strings.EqualFold(version, "latest") {
		return "What's New"
	}
	return "What's New in " + version
}

func (p detailPanel) overlayHeight() int {
	contentHeight := contentAreaHeightForWindow(p.windowWidth, p.windowHeight, true)
	overlayHeight := contentHeight - 2
	if overlayHeight < 1 {
		return 1
	}
	return overlayHeight
}

func (p detailPanel) visibleDetailHeight(totalHeight int) int {
	innerHeight := totalHeight - 6
	if innerHeight < 5 {
		return 5
	}
	return innerHeight
}

func (p detailPanel) maxScroll() int {
	if p.state != detailReady {
		return 0
	}
	totalLines := len(p.detailLines(p.windowWidth))
	maxScroll := totalLines - p.visibleDetailHeight(p.overlayHeight())
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (p detailPanel) clampScroll() detailPanel {
	if p.scroll < 0 {
		p.scroll = 0
	}
	maxScroll := p.maxScroll()
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	return p
}

// view renders the detail panel.
func (p detailPanel) view(width, height int) string {
	if p.state == detailHidden {
		return ""
	}

	var b strings.Builder

	panelWidth := max(24, width-4)
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1).
		Width(panelWidth)

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
		if p.editingOverrides {
			return p.renderOverrideEditor(panelStyle)
		}
		if p.selectingVersion {
			return p.renderVersionPicker(panelStyle, height)
		}

		title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.title())
		if p.pkgID != "" && p.pkgID != p.title() {
			title += "  " + helpStyle.Render(p.pkgID)
		}
		b.WriteString("  " + title + "\n")

		visibleLines := p.detailLines(width)

		innerHeight := p.visibleDetailHeight(height)
		totalLines := len(visibleLines)
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
			b.WriteString(visibleLines[i] + "\n")
		}

		b.WriteString("\n")
		help := "  ↑↓/PgUp/PgDn scroll • i ignore • p overrides • esc close"
		if p.allowVersionSelect {
			help = "  ↑↓/PgUp/PgDn scroll • v choose version • i ignore • p overrides • esc close"
			if p.selectedVersion != "" {
				help = "  ↑↓/PgUp/PgDn scroll • v change version • c latest • i ignore • p overrides • esc close"
			}
			if p.canOpenHomepage() {
				help += " • o open homepage"
			}
			if p.canOpenReleaseNotes() {
				help += " • n release notes"
			}
		} else {
			if p.canOpenHomepage() {
				help = "  ↑↓/PgUp/PgDn scroll • o open homepage • i ignore • p overrides • esc close"
			}
			if p.canOpenReleaseNotes() {
				if p.canOpenHomepage() {
					help = "  ↑↓/PgUp/PgDn scroll • o open homepage • n release notes • i ignore • p overrides • esc close"
				} else {
					help = "  ↑↓/PgUp/PgDn scroll • n release notes • i ignore • p overrides • esc close"
				}
			}
		}
		b.WriteString(helpStyle.Render(help))
		if p.overrideSaved {
			b.WriteString("\n" + successStyle.Render("  Overrides saved."))
		} else if p.overrideDeleted {
			b.WriteString("\n" + successStyle.Render("  Overrides cleared."))
		}
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
	b.WriteString(helpStyle.Render("  ↑↓/PgUp/PgDn choose • enter select • c latest • esc back"))
	return indentBlock(panelStyle.Render(b.String()), 2)
}

// ── Ignore toggle ─────────────────────────────────────────────────

func (p detailPanel) toggleIgnore() (detailPanel, tea.Cmd, bool) {
	o := appSettings.getOverride(p.pkgID, p.source)
	switch {
	case o.Ignore:
		o.Ignore = false
		o.IgnoreVersion = ""
	case o.IgnoreVersion != "":
		o.IgnoreVersion = ""
	case p.installedVersion != "" && p.latestVersion != "":
		o.IgnoreVersion = p.latestVersion
	default:
		o.Ignore = true
	}
	if err := persistPackageOverride(p.pkgID, p.source, o); err != nil {
		p.overrideErrMsg = "Save failed: " + err.Error()
		return p, nil, true
	}
	p.overrideSaved = true
	p.overrideDeleted = false
	p.overrideErrMsg = ""
	return p, func() tea.Msg { return overridesSavedMsg{pkgID: p.pkgID} }, true
}

// ── Override editor ───────────────────────────────────────────────

type overridesSavedMsg struct {
	pkgID string
}

func (p detailPanel) updateOverrideEditor(msg tea.KeyPressMsg) (detailPanel, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if p.overrideCursor > 0 {
			p.overrideCursor--
		}
	case "down", "j":
		if p.overrideCursor < len(overrideDefs)-1 {
			p.overrideCursor++
		}
	case "enter", "right", "l", "space":
		p.cycleOverrideForward()
	case "left", "h":
		p.cycleOverrideBackward()
	case "s":
		if err := persistPackageOverride(p.pkgID, p.source, p.overrideEdit); err != nil {
			p.overrideErrMsg = "Save failed: " + err.Error()
			return p, nil, true
		}
		p.overrideSaved = true
		p.overrideDeleted = false
		p.overrideErrMsg = ""
		p.editingOverrides = false
		return p, func() tea.Msg { return overridesSavedMsg{pkgID: p.pkgID} }, true
	case "d":
		p.overrideEdit = PackageOverride{}
		if err := persistPackageOverride(p.pkgID, p.source, p.overrideEdit); err != nil {
			p.overrideErrMsg = "Save failed: " + err.Error()
			return p, nil, true
		}
		p.overrideDeleted = true
		p.overrideSaved = false
		p.overrideErrMsg = ""
		p.editingOverrides = false
		return p, func() tea.Msg { return overridesSavedMsg{pkgID: p.pkgID} }, true
	case "esc":
		p.editingOverrides = false
	}
	return p, nil, true
}

func (p *detailPanel) cycleOverrideForward() {
	def := overrideDefs[p.overrideCursor]
	cur := p.overrideEdit.getValue(def.key)
	idx := 0
	for i, c := range def.choices {
		if c == cur {
			idx = i
			break
		}
	}
	idx = (idx + 1) % len(def.choices)
	p.overrideEdit.setValue(def.key, def.choices[idx])
}

func (p *detailPanel) cycleOverrideBackward() {
	def := overrideDefs[p.overrideCursor]
	cur := p.overrideEdit.getValue(def.key)
	idx := 0
	for i, c := range def.choices {
		if c == cur {
			idx = i
			break
		}
	}
	idx--
	if idx < 0 {
		idx = len(def.choices) - 1
	}
	p.overrideEdit.setValue(def.key, def.choices[idx])
}

func (p detailPanel) renderOverrideEditor(panelStyle lipgloss.Style) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.title())
	if p.pkgID != "" && p.pkgID != p.title() {
		title += "  " + helpStyle.Render(p.pkgID)
	}
	b.WriteString("  " + title + "\n\n")
	b.WriteString("  " + infoStyle.Render("Package Rules") + "\n")
	b.WriteString("  " + helpStyle.Render("Set per-package ignore rules and option overrides.") + "\n\n")

	for i, def := range overrideDefs {
		cursor := cursorBlankStr
		labelStyle := itemStyle
		if i == p.overrideCursor {
			cursor = cursorStr
			labelStyle = itemActiveStyle
		}
		val := p.overrideEdit.getValue(def.key)
		valDisplay := renderSettingValue(def, val)
		label := labelStyle.Render(fmt.Sprintf("%-16s", def.label))
		b.WriteString(fmt.Sprintf("  %s%s %s\n", cursor, label, valDisplay))
	}

	// Show hint for focused override.
	def := overrideDefs[p.overrideCursor]
	val := p.overrideEdit.getValue(def.key)
	if hint := def.currentHint(val); hint != "" {
		b.WriteString("\n  " + helpStyle.Render(hint))
	}

	if p.overrideErrMsg != "" {
		b.WriteString("\n" + errorStyle.Render("  "+p.overrideErrMsg))
	}

	b.WriteString("\n\n")
	actions := lipgloss.NewStyle().Bold(true).Foreground(accent).Render("s") + " save  •  " +
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render("d") + " clear all  •  " +
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render("←→") + " cycle  •  " +
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel"
	b.WriteString("  " + helpStyle.Render(actions))

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

var openExternalURL = openURL
