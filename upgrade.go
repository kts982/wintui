package main

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

type upgradeState int

const (
	upgradeLoading upgradeState = iota
	upgradeEmpty
	upgradeSelecting
	upgradeConfirming
	upgradeExecuting
	upgradeDone
)

type startUpgradeBatchMsg struct{}

type upgradeScreen struct {
	state            upgradeState
	width            int
	height           int
	packages         []Package
	selected         map[int]bool
	selectedVersions map[string]string
	cursor           int
	action           string // "all" or "selected"
	spinner          spinner.Model
	progress         progressBar
	output           string
	err              error
	detail           detailPanel
	filter           listFilter
	cancel           context.CancelFunc
	ctx              context.Context
	batchCurrent     int
	batchTotal       int
	batchName        string
	batchIDs         []string
	batchSources     []string
	batchVersions    []string
	batchOutputs     []string
	batchErrs        []error
	batchErr         error
	launchRetry      *retryRequest
	retryAction      *retryRequest
	vp               viewport.Model
	outLines         []string
	currentLines     []string
	upgradeOut       <-chan string
	upgradeErr       <-chan error
}

func newUpgradeScreen() upgradeScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(10))
	vp.Style = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	return upgradeScreen{
		state:            upgradeLoading,
		width:            80,
		height:           24,
		selected:         make(map[int]bool),
		selectedVersions: make(map[string]string),
		spinner:          sp,
		progress:         newProgressBar(50),
		detail:           newDetailPanel(),
		filter:           newListFilter(),
		ctx:              ctx,
		cancel:           cancel,
		vp:               vp,
	}
}

func newUpgradeScreenWithRetry(req retryRequest) upgradeScreen {
	s := newUpgradeScreen()
	s.state = upgradeExecuting
	s.batchIDs = []string{req.ID}
	s.batchSources = []string{req.Source}
	s.batchVersions = []string{req.Version}
	s.batchTotal = 1
	s.batchName = req.ID
	s.launchRetry = &req
	if req.Version != "" {
		s.selectedVersions[packageSourceKey(req.ID, req.Source)] = req.Version
	}
	return s
}

func (s upgradeScreen) init() tea.Cmd {
	if s.launchRetry != nil {
		return tea.Batch(s.spinner.Tick, func() tea.Msg { return startUpgradeBatchMsg{} })
	}
	// Check cache first
	if pkgs, ok := cache.getUpgradeable(); ok {
		return func() tea.Msg {
			return packagesLoadedMsg{packages: pkgs}
		}
	}
	return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
		pkgs, err := getUpgradeableCtx(s.ctx)
		if err == nil {
			cache.setUpgradeable(pkgs)
		}
		return packagesLoadedMsg{packages: pkgs, err: err}
	})
}

func (s upgradeScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		s.width = sizeMsg.Width
		s.height = sizeMsg.Height
	}

	// Detail panel gets priority when visible
	if s.detail.visible() {
		var cmd tea.Cmd
		var handled bool
		s.detail, cmd, handled = s.detail.update(msg)
		if handled {
			return s, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.detail = s.detail.withWindowSize(msg.Width, msg.Height)
		return s, nil

	case detailVersionSelectedMsg:
		s.setSelectedVersion(msg.pkgID, msg.source, msg.version)
		if pkg, ok := s.packageByIdentity(msg.pkgID, msg.source); ok {
			var cmd tea.Cmd
			s.detail = s.detail.withWindowSize(s.width, s.height)
			s.detail, cmd = s.detail.showWithVersion(pkg, msg.version, true)
			return s, cmd
		}
		return s, nil

	case tea.KeyPressMsg:
		// Filter input mode — pass navigation keys through
		if s.filter.active {
			switch msg.String() {
			case "enter":
				s.filter = s.filter.apply()
				s.cursor = 0
				return s, nil
			case "esc":
				s.filter = s.filter.deactivate()
				s.cursor = 0
				return s, nil
			case "up", "k":
				if s.cursor > 0 {
					s.cursor--
				}
				return s, nil
			case "down", "j":
				filtered := s.filter.filterPackages(s.packages)
				if s.cursor < len(filtered)-1 {
					s.cursor++
				}
				return s, nil
			case "space", "x":
				// Toggle selection while filtering
				filtered := s.filter.filterPackages(s.packages)
				if s.cursor < len(filtered) {
					for j, p := range s.packages {
						if p.ID == filtered[s.cursor].ID {
							s.selected[j] = !s.selected[j]
							break
						}
					}
					if s.cursor < len(filtered)-1 {
						s.cursor++
					}
				}
				return s, nil
			default:
				var cmd tea.Cmd
				s.filter.input, cmd = s.filter.input.Update(msg)
				s.filter.query = s.filter.input.Value()
				s.cursor = 0
				return s, cmd
			}
		}

		// Cancel running operation with esc
		if msg.String() == "esc" && (s.state == upgradeLoading || s.state == upgradeExecuting) {
			if s.cancel != nil {
				s.cancel()
			}
			s.progress = s.progress.stop()
			s.retryAction = nil
			if s.state == upgradeLoading {
				s.err = fmt.Errorf("cancelled")
				if len(s.packages) > 0 {
					s.state = upgradeSelecting
				} else {
					s.state = upgradeEmpty
				}
			} else {
				s.state = upgradeDone
				s.err = fmt.Errorf("cancelled")
				s.output = formatBatchResults(
					s.batchIDs[:len(s.batchErrs)],
					s.batchErrs,
					s.batchOutputs,
				)
			}
			return s, nil
		}
		// Refresh with r
		if msg.String() == "r" && (s.state == upgradeSelecting || s.state == upgradeEmpty || s.state == upgradeDone) {
			cache.invalidate()
			s.retryAction = nil
			s.state = upgradeLoading
			s.ctx, s.cancel = context.WithCancel(context.Background())
			s.progress, _ = s.progress.start()
			return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
				pkgs, err := getUpgradeableCtx(s.ctx)
				if err == nil {
					cache.setUpgradeable(pkgs)
				}
				return packagesLoadedMsg{packages: pkgs, err: err}
			})
		}
		// Show details
		if (msg.String() == "i" || msg.String() == "d") && s.state == upgradeSelecting {
			filtered := s.filter.filterPackages(s.packages)
			if len(filtered) > 0 && s.cursor >= 0 && s.cursor < len(filtered) {
				var cmd tea.Cmd
				pkg := filtered[s.cursor]
				s.detail = s.detail.withWindowSize(s.width, s.height)
				s.detail, cmd = s.detail.showWithVersion(pkg, s.selectedVersionFor(pkg), true)
				return s, cmd
			}
		}
		switch s.state {
		case upgradeSelecting:
			return s.updateSelect(msg)
		case upgradeConfirming:
			return s.updateConfirm(msg)
		case upgradeDone, upgradeEmpty:
			if msg.String() == "ctrl+e" && s.retryAction != nil && !isElevated() {
				if err := relaunchElevatedRetry(*s.retryAction); err != nil {
					s.err = fmt.Errorf("failed to relaunch elevated: %w", err)
					return s, nil
				}
				return s, tea.Quit
			}
			if msg.String() == "enter" {
				return s, func() tea.Msg { return switchScreenMsg(screenUpgrade) }
			}
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			contentY := msg.Y - 9 // header(6 logo) + tabbar(2) + title(1)
			switch s.state {
			case upgradeSelecting:
				filtered := s.filter.filterPackages(s.packages)
				row := contentY - 5
				if row >= 0 && row < len(filtered) {
					s.cursor = row
					s.toggleFilteredSelection(filtered)
				}
			}
		}

	case packagesLoadedMsg:
		if s.state != upgradeLoading {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.retryAction = nil
		s.packages = msg.packages
		s.selected = make(map[int]bool)
		s.selectedVersions = make(map[string]string)
		s.err = msg.err
		if msg.err != nil || len(msg.packages) == 0 {
			s.state = upgradeEmpty
		} else {
			s.state = upgradeSelecting
		}
		return s, nil

	case startUpgradeBatchMsg:
		if s.state != upgradeExecuting || s.batchTotal == 0 {
			return s, nil
		}
		return s, s.startUpgradeStream(s.batchCurrent)

	case commandDoneMsg:
		if s.state != upgradeExecuting {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.output = msg.output
		s.err = msg.err
		s.state = upgradeDone
		cache.invalidate() // packages changed
		if msg.err == nil {
			return s, func() tea.Msg { return packageDataChangedMsg{origin: screenUpgrade} }
		}
		return s, nil

	case streamMsg:
		if s.state != upgradeExecuting {
			return s, nil
		}
		line := string(msg)
		s.currentLines = append(s.currentLines, line)
		s.outLines = append(s.outLines, line)
		s.vp.SetContent(strings.Join(s.outLines, "\n"))
		s.vp.GotoBottom()
		return s, awaitStream(s.upgradeOut, s.upgradeErr)

	case streamDoneMsg:
		if s.state != upgradeExecuting {
			return s, nil
		}
		output := strings.Join(s.currentLines, "\n")
		s.batchOutputs = append(s.batchOutputs, output)
		s.batchErrs = append(s.batchErrs, msg.err)
		if msg.err != nil {
			s.batchErr = msg.err
		}
		completed := s.batchCurrent + 1
		if s.batchTotal > 0 {
			s.progress.percent = float64(completed) / float64(s.batchTotal)
		}
		if completed < s.batchTotal {
			return s, s.startUpgradeStream(completed)
		}

		s.progress = s.progress.stop()
		s.output = formatBatchResults(s.batchIDs, s.batchErrs, s.batchOutputs)
		s.err = s.batchErr
		s.state = upgradeDone
		s.retryAction = nil
		if s.batchTotal == 1 && s.batchErr != nil && len(s.batchOutputs) == 1 &&
			requiresElevation(s.batchErr, s.batchOutputs[0]) && !isElevated() {
			source := ""
			if len(s.batchSources) > 0 {
				source = s.batchSources[0]
			}
			version := ""
			if len(s.batchVersions) > 0 {
				version = s.batchVersions[0]
			}
			s.retryAction = &retryRequest{
				Op:      retryOpUpgrade,
				ID:      s.batchIDs[0],
				Source:  source,
				Version: version,
			}
		}
		cache.invalidate()
		return s, func() tea.Msg { return packageDataChangedMsg{origin: screenUpgrade} }

	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd

	case progressTickMsg:
		var cmd tea.Cmd
		s.progress, cmd = s.progress.update(msg)
		return s, cmd

	case progress.FrameMsg:
		var cmd tea.Cmd
		s.progress, cmd = s.progress.update(msg)
		return s, cmd
	}
	return s, nil
}

func (s upgradeScreen) updateSelect(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	filtered := s.filter.filterPackages(s.packages)
	switch msg.String() {
	case "/":
		s.filter = s.filter.activate()
		return s, textinput.Blink
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(filtered)-1 {
			s.cursor++
		}
	case "space", "x":
		s.toggleFilteredSelection(filtered)
		if s.cursor < len(filtered)-1 {
			s.cursor++
		}
	case "a":
		s.toggleAllFiltered(filtered)
	case "u":
		if len(s.packages) > 0 {
			s.action = "all"
			s.state = upgradeConfirming
		}
	case "enter":
		if s.selectedCount() > 0 {
			s.action = "selected"
			s.state = upgradeConfirming
		}
	case "esc":
		switch {
		case s.filter.query != "":
			s.filter = s.filter.deactivate()
			s.cursor = 0
		case s.selectedCount() > 0:
			s.selected = make(map[int]bool)
		}
	}
	return s, nil
}

func (s upgradeScreen) updateConfirm(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", "Y":
		s.state = upgradeExecuting
		s.retryAction = nil
		s.progress, _ = s.progress.start()
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel

		var ids []string
		var sources []string
		var versions []string
		for _, pkg := range s.packagesForAction() {
			ids = append(ids, pkg.ID)
			sources = append(sources, pkg.Source)
			versions = append(versions, s.selectedVersionFor(pkg))
		}
		s.ctx = ctx
		s.batchIDs = ids
		s.batchSources = sources
		s.batchVersions = versions
		s.batchTotal = len(ids)
		s.batchCurrent = 0
		s.batchOutputs = nil
		s.batchErrs = nil
		s.batchErr = nil
		s.outLines = nil
		s.currentLines = nil
		s.vp.SetContent("")
		s.progress.active = false // don't use indeterminate animation for batch
		s.progress.percent = 0

		if s.batchTotal > 0 {
			return s, tea.Batch(s.spinner.Tick, s.startUpgradeStream(0))
		}
		return s, nil

	case "n", "N", "esc":
		s.state = upgradeSelecting
	}
	return s, nil
}

func (s upgradeScreen) selectedCount() int {
	count := 0
	for _, v := range s.selected {
		if v {
			count++
		}
	}
	return count
}

func (s upgradeScreen) selectedVersionFor(pkg Package) string {
	return s.selectedVersions[packageSourceKey(pkg.ID, pkg.Source)]
}

func (s *upgradeScreen) setSelectedVersion(id, source, version string) {
	key := packageSourceKey(id, source)
	if strings.TrimSpace(version) == "" {
		delete(s.selectedVersions, key)
		return
	}
	s.selectedVersions[key] = version
}

func (s upgradeScreen) packagesForAction() []Package {
	if s.action == "all" {
		return append([]Package(nil), s.packages...)
	}
	pkgs := make([]Package, 0, s.selectedCount())
	for i, pkg := range s.packages {
		if s.selected[i] {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

func (s upgradeScreen) customVersionCount(pkgs []Package) int {
	count := 0
	for _, pkg := range pkgs {
		if s.selectedVersionFor(pkg) != "" {
			count++
		}
	}
	return count
}

func (s upgradeScreen) renderSelectingBody(height int) string {
	var b strings.Builder
	filtered := s.filter.filterPackages(s.packages)
	selected := s.selectedCount()
	b.WriteString(fmt.Sprintf("  %s\n",
		infoStyle.Render(fmt.Sprintf("%d package(s) with updates available.", len(s.packages)))))
	filterView := s.filter.view()
	if filterView != "" {
		if s.filter.active {
			b.WriteString(filterView + "\n")
		} else {
			b.WriteString(filterView + fmt.Sprintf("  %s",
				helpStyle.Render(fmt.Sprintf("(%d shown)", len(filtered)))) + "\n")
		}
	}
	if selected > 0 {
		b.WriteString(fmt.Sprintf("  %s\n",
			warnStyle.Render(fmt.Sprintf("%d selected — enter to upgrade selected or u to upgrade all", selected))))
	} else {
		b.WriteString("  " + helpStyle.Render("space select • a select visible • u upgrade all") + "\n")
	}
	b.WriteString("\n")
	if len(filtered) == 0 {
		b.WriteString("  " + warnStyle.Render("No packages match the current filter.") + "\n")
		return b.String()
	}
	maxVisible := height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}
	start, end := scrollWindow(s.cursor, len(filtered), maxVisible)
	for i := start; i < end; i++ {
		pkg := filtered[i]
		cursor := cursorBlankStr
		style := itemStyle
		if i == s.cursor {
			cursor = cursorStr
			style = itemActiveStyle
		}
		origIdx := -1
		for j, p := range s.packages {
			if p.ID == pkg.ID {
				origIdx = j
				break
			}
		}
		check := checkbox(origIdx >= 0 && s.selected[origIdx])
		label := fmt.Sprintf("%s  (%s)  %s → %s", pkg.Name, pkg.ID, pkg.Version, pkg.Available)
		if version := s.selectedVersionFor(pkg); version != "" {
			label += fmt.Sprintf("  [target %s]", version)
		}
		fmt.Fprintf(&b, "  %s%s %s\n", cursor, check, style.Render(label))
	}
	return b.String()
}

func (s upgradeScreen) confirmBackgroundView(height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Upgrade Packages") + "\n\n")
	b.WriteString(s.renderSelectingBody(height))
	return b.String()
}

func (s upgradeScreen) upgradeConfirmModal() confirmModal {
	targets := s.packagesForAction()
	customVersions := s.customVersionCount(targets)
	body := make([]string, 0, 8)
	if len(targets) == 1 {
		pkg := targets[0]
		body = append(body, infoStyle.Render(pkg.Name))
		body = append(body, helpStyle.Render(pkg.ID))
		if version := s.selectedVersionFor(pkg); version != "" {
			body = append(body, "Target version: "+itemActiveStyle.Render(version))
		} else {
			body = append(body, "Latest available: "+itemActiveStyle.Render(pkg.Available))
		}
		if pkg.Source != "" {
			body = append(body, "Source: "+pkg.Source)
		}
	} else {
		body = append(body, infoStyle.Render(fmt.Sprintf("%d package(s) will be upgraded.", len(targets))))
		for _, item := range summarizeModalItems(packageNames(targets), 3) {
			body = append(body, "• "+item)
		}
		if customVersions > 0 {
			body = append(body, "", helpStyle.Render(fmt.Sprintf("%d package(s) will use an explicit target version.", customVersions)))
		}
	}

	title := "Upgrade Package?"
	if len(targets) != 1 {
		title = "Upgrade Packages?"
	}
	return confirmModal{
		title:       title,
		body:        body,
		confirmVerb: "upgrade",
	}
}

func packageNames(pkgs []Package) []string {
	names := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.Name != "" {
			names = append(names, pkg.Name)
			continue
		}
		names = append(names, pkg.ID)
	}
	return names
}

func (s upgradeScreen) packageByIdentity(id, source string) (Package, bool) {
	for _, pkg := range s.packages {
		if pkg.ID == id && pkg.Source == source {
			return pkg, true
		}
	}
	return Package{}, false
}

func (s upgradeScreen) filteredSelectionIndex(filtered []Package) int {
	if s.cursor < 0 || s.cursor >= len(filtered) {
		return -1
	}
	target := filtered[s.cursor].ID
	for i, pkg := range s.packages {
		if pkg.ID == target {
			return i
		}
	}
	return -1
}

func (s *upgradeScreen) toggleFilteredSelection(filtered []Package) {
	idx := s.filteredSelectionIndex(filtered)
	if idx < 0 {
		return
	}
	s.selected[idx] = !s.selected[idx]
}

func (s *upgradeScreen) toggleAllFiltered(filtered []Package) {
	if len(filtered) == 0 {
		return
	}
	indices := make([]int, 0, len(filtered))
	allSelected := true
	for _, pkg := range filtered {
		for i, base := range s.packages {
			if base.ID == pkg.ID {
				indices = append(indices, i)
				if !s.selected[i] {
					allSelected = false
				}
				break
			}
		}
	}
	for _, idx := range indices {
		if allSelected {
			delete(s.selected, idx)
		} else {
			s.selected[idx] = true
		}
	}
}

// formatBatchResults builds a per-package summary from batch upgrade results.
func formatBatchResults(ids []string, errs []error, outputs []string) string {
	var b strings.Builder
	for i, id := range ids {
		if i >= len(errs) {
			break
		}
		if errs[i] == nil {
			b.WriteString(successStyle.Render("  ✓ ") + id + "\n")
		} else {
			// Extract a short reason from the raw output
			reason := errs[i].Error()
			if requiresElevation(errs[i], outputs[i]) {
				reason = "requires administrator privileges; retry from an elevated terminal"
			}
			detail := extractFailReason(outputs[i])
			if detail != "" && !requiresElevation(errs[i], outputs[i]) {
				reason = detail
			}
			b.WriteString(errorStyle.Render("  ✗ ") + id + "\n")
			b.WriteString("    " + helpStyle.Render(reason) + "\n")
		}
	}
	return b.String()
}

func batchResultCounts(errs []error) (successCount, failCount int) {
	for _, err := range errs {
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}
	return successCount, failCount
}

func batchRequiresElevation(errs []error, outputs []string) bool {
	for i, err := range errs {
		output := ""
		if i < len(outputs) {
			output = outputs[i]
		}
		if requiresElevation(err, output) {
			return true
		}
	}
	return false
}

func (s upgradeScreen) packageLabel(id string) string {
	for _, pkg := range s.packages {
		if pkg.ID == id {
			if pkg.Name != "" {
				return pkg.Name
			}
			break
		}
	}
	if id != "" {
		return id
	}
	if s.batchName != "" {
		return s.batchName
	}
	return "Package"
}

// extractFailReason pulls the most relevant failure line from winget output.
func extractFailReason(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "failed with exit code") ||
			strings.Contains(lower, "installer failed") {
			return trimmed
		}
	}
	return ""
}

func (s *upgradeScreen) startUpgradeStream(index int) tea.Cmd {
	if index < 0 || index >= len(s.batchIDs) {
		return nil
	}
	s.batchCurrent = index
	s.currentLines = nil
	if len(s.outLines) > 0 {
		s.outLines = append(s.outLines, "")
	}
	label := s.packageLabel(s.batchIDs[index])
	targetVersion := ""
	if index < len(s.batchVersions) {
		targetVersion = s.batchVersions[index]
	}
	s.batchName = label
	if targetVersion != "" {
		s.batchName += " -> " + targetVersion
	}
	header := "== " + label
	if label != s.batchIDs[index] {
		header += " (" + s.batchIDs[index] + ")"
	}
	if targetVersion != "" {
		header += " -> " + targetVersion
	}
	header += " =="
	s.outLines = append(s.outLines, header)
	s.vp.SetContent(strings.Join(s.outLines, "\n"))
	s.vp.GotoBottom()
	s.upgradeOut, s.upgradeErr = upgradePackageStreamCtx(
		s.ctx,
		s.batchIDs[index],
		s.batchSources[index],
		targetVersion,
	)
	return awaitStream(s.upgradeOut, s.upgradeErr)
}

func (s upgradeScreen) view(width, height int) string {
	// Detail panel overlay
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Upgrade Packages") + "\n\n" +
			s.detail.view(width, height-2)
	}

	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Upgrade Packages") + "\n\n")

	switch s.state {
	case upgradeLoading:
		fmt.Fprintf(&b, "  %s Scanning for updates...\n\n", s.spinner.View())
		b.WriteString("  " + s.progress.view() + "\n")

	case upgradeEmpty:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else {
			b.WriteString("  " + successStyle.Render("All packages are up to date!") + "\n")
		}
		b.WriteString("\n  " + helpStyle.Render("Press r to scan again or tab to switch screens") + "\n")

	case upgradeSelecting:
		b.WriteString(s.renderSelectingBody(height))

	case upgradeConfirming:
		return renderConfirmModal(
			s.confirmBackgroundView(height),
			width,
			height,
			s.upgradeConfirmModal(),
		)

	case upgradeExecuting:
		if s.batchTotal > 0 {
			b.WriteString(fmt.Sprintf("  %s Upgrading %d of %d: %s\n\n",
				s.spinner.View(), s.batchCurrent+1, s.batchTotal, s.batchName))
		} else {
			fmt.Fprintf(&b, "  %s Upgrading packages...\n\n", s.spinner.View())
		}
		b.WriteString("  " + s.progress.view() + "\n")
		s.vp.SetWidth(width - 8)
		vpH := height - 12
		if vpH < 5 {
			vpH = 5
		}
		s.vp.SetHeight(vpH)
		b.WriteString(indentBlock(s.vp.View(), 2) + "\n")

	case upgradeDone:
		successCount, failCount := batchResultCounts(s.batchErrs)
		if s.err != nil && successCount == 0 && failCount == 0 {
			if strings.Contains(strings.ToLower(s.err.Error()), "cancelled") {
				b.WriteString("  " + warnStyle.Render("Upgrade cancelled before any packages completed") + "\n\n")
			} else {
				b.WriteString("  " + warnStyle.Render("Upgrade failed before any packages completed") + "\n\n")
			}
		} else if s.batchTotal == 1 && failCount == 0 && len(s.batchIDs) == 1 {
			b.WriteString("  " + successStyle.Render(s.packageLabel(s.batchIDs[0])+" upgraded successfully") + "\n")
			b.WriteString("  " + helpStyle.Render(s.batchIDs[0]) + "\n\n")
		} else if failCount > 0 {
			b.WriteString("  " + warnStyle.Render(
				fmt.Sprintf("Upgrade finished: %d succeeded, %d failed", successCount, failCount),
			) + "\n")
			if successCount > 0 {
				b.WriteString("  " + helpStyle.Render(
					fmt.Sprintf("%d package(s) were upgraded before the run completed.", successCount),
				) + "\n")
			}
			if requiresElevation(s.err, s.output) || batchRequiresElevation(s.batchErrs, s.batchOutputs) {
				b.WriteString("  " + helpStyle.Render("Some packages require administrator privileges.") + "\n")
				b.WriteString("  " + helpStyle.Render(elevationRetryHint()) + "\n\n")
			} else {
				b.WriteString("\n")
			}
		} else {
			b.WriteString("  " + successStyle.Render(
				fmt.Sprintf("%d package(s) upgraded successfully", successCount),
			) + "\n\n")
		}

		if s.err != nil {
			if s.output == "" && (s.batchTotal == 0 || (successCount == 0 && failCount == 0)) {
				if requiresElevation(s.err, s.output) || batchRequiresElevation(s.batchErrs, s.batchOutputs) {
					b.WriteString("  " + warnStyle.Render("Completed with errors") + "\n")
					b.WriteString("  " + helpStyle.Render("Some packages require administrator privileges.") + "\n")
					b.WriteString("  " + helpStyle.Render(elevationRetryHint()) + "\n\n")
				} else if !strings.Contains(strings.ToLower(s.err.Error()), "cancelled") {
					b.WriteString("  " + warnStyle.Render("Completed with errors") + "\n\n")
				}
			}
		}
		if s.output != "" {
			// Batch results are already formatted; non-batch needs cleaning
			output := s.output
			if s.batchTotal == 0 {
				output = cleanWingetOutput(output)
			}
			lines := strings.Split(output, "\n")
			maxLines := height - 8
			if maxLines < 5 {
				maxLines = 5
			}
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				lines = append(lines, helpStyle.Render("  ... (output truncated)"))
			}
			for _, line := range lines {
				b.WriteString(line + "\n")
			}
		}
		if s.retryAction != nil && !isElevated() {
			b.WriteString("\n  " + helpStyle.Render("Press ctrl+e to retry elevated, r to rescan, or tab to switch screens") + "\n")
		} else {
			b.WriteString("\n  " + helpStyle.Render("Press r to rescan or tab to switch screens") + "\n")
		}
	}

	return b.String()
}

func (s upgradeScreen) helpKeys() []key.Binding {
	if s.detail.visible() {
		return s.detail.helpKeys()
	}
	switch s.state {
	case upgradeLoading, upgradeExecuting:
		return []key.Binding{keyEscCancel}
	case upgradeEmpty, upgradeDone:
		if s.retryAction != nil && !isElevated() {
			return []key.Binding{keyRetryElevated, keyRefresh, keyTabs}
		}
		return []key.Binding{keyRefresh, keyTabs}
	case upgradeSelecting:
		if s.filter.active {
			filtered := s.filter.filterPackages(s.packages)
			bindings := []key.Binding{keyUp, keyDown}
			if len(filtered) > 0 {
				bindings = append(bindings, keyToggle)
			}
			bindings = append(bindings, keyApply, keyEsc)
			return bindings
		}
		filtered := s.filter.filterPackages(s.packages)
		selected := s.selectedCount()
		bindings := []key.Binding{keyUp, keyDown}
		if s.filter.query != "" {
			bindings = append(bindings, keyFilterEdit)
		} else {
			bindings = append(bindings, keyFilter)
		}
		if len(filtered) > 0 {
			bindings = append(bindings, keyToggleAll)
			bindings = append(bindings, keyToggle, keyDetails)
		}
		if len(s.packages) > 0 {
			bindings = append(bindings, keyUpgradeAll)
		}
		if selected > 0 {
			bindings = append(bindings, keyUpgradeSelected)
		}
		bindings = append(bindings, keyRefresh)
		if s.filter.query != "" || selected > 0 {
			bindings = append(bindings, keyEscClear)
		}
		return bindings
	case upgradeConfirming:
		return []key.Binding{keyConfirm, keyCancel}
	}
	return []key.Binding{keyTabs}
}

func (s upgradeScreen) blocksGlobalShortcuts() bool {
	return s.state == upgradeConfirming
}

// scrollWindow calculates visible range for long lists.
func scrollWindow(cursor, total, maxVisible int) (start, end int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}
