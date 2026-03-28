package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

type packagesState int

const (
	packagesLoading packagesState = iota
	packagesReady
	packagesEmpty
	packagesConfirmUninstall
	packagesUninstalling
	packagesDone
)

var keyUninstall = key.NewBinding(
	key.WithKeys("u"),
	key.WithHelp("u", "uninstall selected"),
)

type packagesScreen struct {
	state         packagesState
	width         int
	height        int
	table         table.Model
	tableWidth    int
	spinner       spinner.Model
	progress      progressBar
	packages      []Package
	selected      map[string]bool // selected by package ID
	output        string
	err           error
	detail        detailPanel
	importFlow    importModel
	exportMsg     string
	statusMsg     string
	filter        listFilter
	ctx           context.Context
	cancel        context.CancelFunc
	launchRetry   *retryRequest
	retryAction   *retryRequest
	exec          executionLog
	uninstallOut  <-chan string
	uninstallErr  <-chan error
	batchPackages []Package
	batchOutputs  []string
	batchErrs     []error
	batchErr      error
	batchCurrent  int
	batchTotal    int
	batchName     string
	flashSeq      int
}

type packagesFlashClearMsg struct {
	seq int
}

type startPackagesUninstallMsg struct{}

func newPackagesScreen() packagesScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	return packagesScreen{
		state:      packagesLoading,
		width:      80,
		height:     24,
		selected:   make(map[string]bool),
		tableWidth: packagesTableWidth(80),
		spinner:    sp,
		progress:   newProgressBar(50),
		detail:     newDetailPanel(),
		importFlow: newImportModel(),
		filter:     newListFilter(),
		exec:       newExecutionLog(),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func newPackagesScreenWithRetry(req retryRequest) packagesScreen {
	s := newPackagesScreen()
	s.state = packagesUninstalling
	s.batchPackages = []Package{{Name: req.Name, ID: req.ID, Source: req.Source}}
	s.batchTotal = 1
	s.batchName = req.ID
	s.progress.active = false
	s.progress.percent = 0
	s.launchRetry = &req
	return s
}

func (s packagesScreen) init() tea.Cmd {
	if s.launchRetry != nil {
		return tea.Batch(s.spinner.Tick, func() tea.Msg {
			return startPackagesUninstallMsg{}
		})
	}
	if pkgs, ok := cache.getInstalled(); ok {
		return func() tea.Msg {
			return packagesLoadedMsg{packages: pkgs}
		}
	}
	return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
		pkgs, err := getInstalledCtx(s.ctx)
		if err == nil {
			cache.setInstalled(pkgs)
		}
		return packagesLoadedMsg{packages: pkgs, err: err}
	})
}

func (s packagesScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		s.width = sizeMsg.Width
		s.height = sizeMsg.Height
	}

	// Detail panel gets priority
	if s.detail.visible() {
		var cmd tea.Cmd
		var handled bool
		s.detail, cmd, handled = s.detail.update(msg)
		if handled {
			return s, cmd
		}
	}

	// Import flow overlay
	if s.importFlow.active {
		var cmd tea.Cmd
		var handled bool
		s.importFlow, cmd, handled = s.importFlow.update(msg, s.packages)
		if handled {
			if !s.importFlow.active && s.importFlow.batchTotal > 0 {
				// Import completed with installations — reload
				next, reloadCmd := s.reload()
				return next, tea.Batch(
					reloadCmd,
					func() tea.Msg { return packageDataChangedMsg{origin: screenPackages} },
				)
			}
			return s, cmd
		}
	}

	if s.state == packagesUninstalling {
		if cmd, handled := s.exec.update(msg); handled {
			return s, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.detail = s.detail.withWindowSize(msg.Width, msg.Height)
		s.exec.setSize(msg.Width, contentAreaHeightForWindow(msg.Width, msg.Height, true))
		newWidth := packagesTableWidth(msg.Width)
		if newWidth != s.tableWidth {
			s.tableWidth = newWidth
		}
		if s.state == packagesReady {
			cursor := s.table.Cursor()
			s.rebuildTable()
			ensureTableCursorVisible(&s.table, clampTableCursor(cursor, len(s.table.Rows())))
		}
		return s, nil

	case tea.KeyPressMsg:
		// Filter input mode
		if s.filter.active {
			switch msg.String() {
			case "enter":
				s.filter = s.filter.apply()
				s.rebuildTable()
				return s, nil
			case "esc":
				s.filter = s.filter.deactivate()
				s.rebuildTable()
				return s, nil
			case "up", "down", "pgup", "pgdown":
				var cmd tea.Cmd
				s.table, cmd = s.table.Update(msg)
				return s, cmd
			default:
				var cmd tea.Cmd
				s.filter.input, cmd = s.filter.input.Update(msg)
				s.filter.query = s.filter.input.Value()
				s.rebuildTable()
				return s, cmd
			}
		}

		// Refresh
		if msg.String() == "r" && (s.state == packagesReady || s.state == packagesEmpty || s.state == packagesDone) {
			return s.reload()
		}

		// Cancel
		if msg.String() == "esc" && (s.state == packagesLoading || s.state == packagesUninstalling) {
			if s.cancel != nil {
				s.cancel()
			}
			if s.state == packagesLoading {
				s.progress = s.progress.stop()
				if len(s.packages) > 0 {
					s.state = packagesReady
					s.statusMsg = warnStyle.Render("Refresh cancelled")
					s.rebuildTable()
				} else {
					s.state = packagesEmpty
					s.err = fmt.Errorf("cancelled")
				}
			} else {
				s.progress = s.progress.stop()
				s.retryAction = nil
				s.err = fmt.Errorf("cancelled")
				s.output = formatUninstallResults(s.batchPackages[:len(s.batchErrs)], s.batchErrs, s.batchOutputs)
				s.selected = make(map[string]bool)
				s.state = packagesDone
			}
			return s, nil
		}

		switch s.state {
		case packagesReady:
			switch msg.String() {
			case "ctrl+e":
				if s.retryAction != nil && !isElevated() {
					if err := relaunchElevatedRetry(*s.retryAction); err != nil {
						s.statusMsg = errorStyle.Render("Failed to relaunch elevated: " + err.Error())
						return s, nil
					}
					return s, tea.Quit
				}
			case "/":
				s.filter = s.filter.activate()
				return s, textinput.Blink
			case "esc":
				if s.filter.query != "" {
					s.filter = s.filter.deactivate()
					s.rebuildTable()
					return s, nil
				}
				if s.selectedCount() > 0 {
					s.selected = make(map[string]bool)
					s.rebuildTable()
					return s, nil
				}
			case "i", "d":
				filtered := s.filteredPkgs()
				row := s.table.Cursor()
				if row >= 0 && row < len(filtered) {
					var cmd tea.Cmd
					pkg := filtered[row]
					s.detail = s.detail.withWindowSize(s.width, s.height)
					s.detail, cmd = s.detail.show(pkg.ID, pkg.Source)
					return s, cmd
				}
			case "e":
				selected := s.selectedPackages()
				if len(selected) > 0 {
					return s, exportPackages(selected)
				}
				return s, exportPackages(s.packages)
			case "m":
				var cmd tea.Cmd
				s.importFlow, cmd = s.importFlow.start(s.packages)
				return s, cmd
			case "space", "x":
				row := s.table.Cursor()
				filtered := s.filteredPkgs()
				if row >= 0 && row < len(filtered) {
					id := filtered[row].ID
					s.selected[id] = !s.selected[id]
					// Rebuild table to update checkbox display
					s.rebuildTable()
					s.table.SetCursor(row)
					// Move cursor down
					s.table, _ = s.table.Update(tea.KeyPressMsg{Code: tea.KeyDown})
					return s, nil
				}
			case "u":
				if s.selectedCount() > 0 {
					s.state = packagesConfirmUninstall
				}
			}
			var cmd tea.Cmd
			s.table, cmd = s.table.Update(msg)
			return s, cmd

		case packagesEmpty:
		case packagesDone:
			switch msg.String() {
			case "ctrl+e":
				if s.retryAction != nil && !isElevated() {
					if err := relaunchElevatedRetry(*s.retryAction); err != nil {
						s.err = fmt.Errorf("failed to relaunch elevated: %w", err)
						return s, nil
					}
					return s, tea.Quit
				}
			}
		case packagesConfirmUninstall:
			switch msg.String() {
			case "enter", "y", "Y":
				s.state = packagesUninstalling
				ctx, cancel := context.WithCancel(context.Background())
				s.ctx = ctx
				s.cancel = cancel
				s.retryAction = nil
				s.output = ""
				s.err = nil
				s.statusMsg = ""
				s.batchPackages = s.selectedPackages()
				s.batchTotal = len(s.batchPackages)
				s.batchCurrent = 0
				s.batchOutputs = nil
				s.batchErrs = nil
				s.batchErr = nil
				s.batchName = ""
				s.exec.reset()
				s.progress.active = false
				s.progress.percent = 0
				return s, tea.Batch(s.spinner.Tick, func() tea.Msg { return startPackagesUninstallMsg{} })
			case "n", "N", "esc":
				s.state = packagesReady
			}
		}

	case tea.MouseClickMsg:
		if s.state == packagesReady {
			if msg.Button == tea.MouseLeft {
				var cmd tea.Cmd
				s.table, cmd = s.table.Update(msg)
				return s, cmd
			}
		}

	case tea.MouseWheelMsg:
		if s.state == packagesReady {
			if msg.Button == tea.MouseWheelUp {
				s.table.MoveUp(3)
				return s, nil
			} else if msg.Button == tea.MouseWheelDown {
				s.table.MoveDown(3)
				return s, nil
			}
		}

	case packagesLoadedMsg:
		if s.state != packagesLoading {
			return s, nil
		}
		s.progress = s.progress.stop()
		if msg.err != nil || len(msg.packages) == 0 {
			s.err = msg.err
			s.state = packagesEmpty
			return s, nil
		}
		s.packages = deduplicatePackages(msg.packages)
		s.selected = make(map[string]bool)
		filtered := s.filter.filterPackages(s.packages)
		s.table = buildSelectableTable(filtered, s.selected, s.tableWidth, s.readyTableHeight())
		s.state = packagesReady
		return s, nil

	case startPackagesUninstallMsg:
		if s.state != packagesUninstalling {
			return s, nil
		}
		if s.batchTotal == 0 {
			return s, nil
		}
		return s, s.startUninstallStream(s.batchCurrent)

	case streamMsg:
		if s.state != packagesUninstalling {
			return s, nil
		}
		s.exec.appendLine(string(msg))
		return s, awaitStream(s.uninstallOut, s.uninstallErr)

	case streamDoneMsg:
		if s.state != packagesUninstalling {
			return s, nil
		}
		output := s.exec.currentOutput()
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
			return s, s.startUninstallStream(completed)
		}

		s.progress = s.progress.stop()
		s.output = formatUninstallResults(s.batchPackages, s.batchErrs, s.batchOutputs)
		s.err = s.batchErr
		s.state = packagesDone
		s.retryAction = nil
		if s.batchTotal == 1 && s.batchErr != nil && len(s.batchOutputs) == 1 &&
			requiresElevation(s.batchErr, s.batchOutputs[0]) && !isElevated() {
			pkg := s.batchPackages[0]
			s.retryAction = &retryRequest{Op: retryOpUninstall, ID: pkg.ID, Name: pkg.Name, Source: pkg.Source}
		}
		s.selected = make(map[string]bool)
		successCount, _ := batchResultCounts(s.batchErrs)
		if successCount > 0 {
			cache.invalidate()
			return s, func() tea.Msg { return packageDataChangedMsg{origin: screenPackages} }
		}
		return s, nil

	case exportDoneMsg:
		if msg.err != nil {
			s.exportMsg = "Export failed: " + msg.err.Error()
		} else {
			s.exportMsg = fmt.Sprintf("Exported %d package(s) to %s", msg.count, msg.path)
		}
		s.flashSeq++
		return s, clearPackagesFlashAfter(s.flashSeq, 8*time.Second)

	case packagesFlashClearMsg:
		if msg.seq == s.flashSeq {
			s.exportMsg = ""
			s.statusMsg = ""
		}
		return s, nil

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

// ── Helpers ────────────────────────────────────────────────────────

// packageSummary builds the info line: "219 installed (62 winget, 3 msstore, 5 system, 149 other)"
func packageSummary(pkgs []Package) string {
	total := len(pkgs)
	winget, msstore, system := 0, 0, 0
	for _, p := range pkgs {
		switch identityKind(p) {
		case "winget":
			winget++
		case "msstore":
			msstore++
		case "system":
			system++
		}
	}

	other := total - winget - msstore - system
	if winget == 0 && msstore == 0 && system == 0 {
		return fmt.Sprintf("%d package(s) installed.", total)
	}

	var parts []string
	if winget > 0 {
		parts = append(parts, fmt.Sprintf("%d winget", winget))
	}
	if msstore > 0 {
		parts = append(parts, fmt.Sprintf("%d msstore", msstore))
	}
	if system > 0 {
		parts = append(parts, fmt.Sprintf("%d system", system))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d other", other))
	}

	return fmt.Sprintf("%d installed (%s)", total, strings.Join(parts, ", "))
}

func (s packagesScreen) filteredPkgs() []Package {
	return s.filter.filterPackages(s.packages)
}

func (s *packagesScreen) rebuildTable() {
	filtered := s.filter.filterPackages(s.packages)
	s.table = buildSelectableTable(filtered, s.selected, s.tableWidth, s.readyTableHeight())
}

func (s packagesScreen) reload() (packagesScreen, tea.Cmd) {
	cache.invalidate()
	s.state = packagesLoading
	s.exportMsg = ""
	s.statusMsg = ""
	s.output = ""
	s.err = nil
	s.retryAction = nil
	s.batchPackages = nil
	s.batchOutputs = nil
	s.batchErrs = nil
	s.batchErr = nil
	s.batchCurrent = 0
	s.batchTotal = 0
	s.batchName = ""
	s.exec.reset()
	s.flashSeq++
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.progress, _ = s.progress.start()
	return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
		pkgs, err := getInstalledCtx(s.ctx)
		if err == nil {
			cache.setInstalled(pkgs)
		}
		return packagesLoadedMsg{packages: pkgs, err: err}
	})
}

func (s *packagesScreen) startUninstallStream(index int) tea.Cmd {
	if index < 0 || index >= len(s.batchPackages) {
		return nil
	}
	s.batchCurrent = index
	pkg := s.batchPackages[index]
	label := uninstallPackageLabel(pkg)
	s.batchName = label
	header := "== " + label
	if pkg.ID != "" && pkg.ID != label {
		header += " (" + pkg.ID + ")"
	}
	header += " =="
	s.exec.appendSection(header)
	s.uninstallOut, s.uninstallErr = uninstallPackageStreamCtx(s.ctx, pkg)
	return awaitStream(s.uninstallOut, s.uninstallErr)
}

func uninstallPackageLabel(pkg Package) string {
	if pkg.Name != "" {
		return pkg.Name
	}
	if pkg.ID != "" {
		return pkg.ID
	}
	return "Package"
}

func formatUninstallResults(pkgs []Package, errs []error, outputs []string) string {
	var b strings.Builder
	for i, pkg := range pkgs {
		if i >= len(errs) {
			break
		}
		label := uninstallPackageLabel(pkg)
		if errs[i] == nil {
			b.WriteString(successStyle.Render("  ✓ ") + label + "\n")
			continue
		}
		reason := errs[i].Error()
		if requiresElevation(errs[i], outputs[i]) {
			reason = "requires administrator privileges; retry from an elevated terminal"
		} else if detail := extractFailReason(outputs[i]); detail != "" {
			reason = detail
		}
		b.WriteString(errorStyle.Render("  ✗ ") + label + "\n")
		b.WriteString("    " + helpStyle.Render(reason) + "\n")
	}
	return b.String()
}

func (s packagesScreen) selectedCount() int {
	count := 0
	for _, v := range s.selected {
		if v {
			count++
		}
	}
	return count
}

func (s packagesScreen) selectedPackages() []Package {
	var pkgs []Package
	for _, pkg := range s.packages {
		if s.selected[pkg.ID] {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

func (s packagesScreen) readyTableHeight() int {
	contentHeight := contentAreaHeightForWindow(s.width, s.height, true)
	tableH := contentHeight - 8
	if s.filter.query != "" || s.filter.active {
		tableH -= 2
	}
	if s.selectedCount() > 0 {
		tableH -= 1
	}
	if tableH < 5 {
		return 5
	}
	return tableH
}

func (s packagesScreen) renderReadyBody(height int) string {
	var b strings.Builder
	filtered := s.filteredPkgs()
	b.WriteString("  " + infoStyle.Render(packageSummary(s.packages)) + "\n")

	filterView := s.filter.view()
	if filterView != "" {
		b.WriteString(filterView + fmt.Sprintf("  %s",
			helpStyle.Render(fmt.Sprintf("(%d shown)", len(filtered)))) + "\n")
	}

	selCount := s.selectedCount()
	if selCount > 0 {
		b.WriteString(fmt.Sprintf("  %s\n",
			warnStyle.Render(fmt.Sprintf("%d selected — press u to uninstall or e to export", selCount))))
	}

	tableH := s.readyTableHeight()
	s.table.SetHeight(tableH)
	b.WriteString("\n  " + s.table.View() + "\n")

	if s.exportMsg != "" {
		b.WriteString("  " + successStyle.Render(s.exportMsg) + "\n")
	}
	if s.statusMsg != "" {
		b.WriteString("  " + s.statusMsg + "\n")
	}

	return b.String()
}

func (s packagesScreen) confirmBackgroundView(height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Installed Packages") + "\n\n")
	b.WriteString(s.renderReadyBody(height))
	return b.String()
}

func (s packagesScreen) uninstallConfirmModal() confirmModal {
	selected := s.selectedPackages()
	body := []string{
		infoStyle.Render(fmt.Sprintf("%d package(s) will be removed.", len(selected))),
	}
	for _, item := range summarizeModalItems(packageNames(selected), 3) {
		body = append(body, "• "+item)
	}
	return confirmModal{
		title:       "Uninstall Packages?",
		body:        body,
		confirmVerb: "uninstall",
	}
}

func clearPackagesFlashAfter(seq int, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg {
		return packagesFlashClearMsg{seq: seq}
	})
}

// ── View ───────────────────────────────────────────────────────────

func (s packagesScreen) view(width, height int) string {
	if s.importFlow.active {
		return s.importFlow.view(width, height)
	}
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Installed Packages") + "\n\n" +
			s.detail.view(width, height-2)
	}

	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Installed Packages") + "\n\n")

	switch s.state {
	case packagesLoading:
		fmt.Fprintf(&b, "  %s Loading installed packages...\n\n", s.spinner.View())
		b.WriteString("  " + s.progress.view() + "\n")

	case packagesEmpty:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else {
			b.WriteString("  " + warnStyle.Render("No packages found.") + "\n")
		}
		b.WriteString("\n  " + helpStyle.Render("Press r to reload or tab to switch screens") + "\n")

	case packagesReady:
		b.WriteString(s.renderReadyBody(height))

	case packagesConfirmUninstall:
		return renderConfirmModal(
			s.confirmBackgroundView(height),
			width,
			height,
			s.uninstallConfirmModal(),
		)

	case packagesUninstalling:
		if s.batchTotal > 0 {
			fmt.Fprintf(&b, "  %s Uninstalling %d of %d: %s\n\n",
				s.spinner.View(), s.batchCurrent+1, s.batchTotal, s.batchName)
		} else {
			fmt.Fprintf(&b, "  %s Uninstalling...\n\n", s.spinner.View())
		}
		b.WriteString("  " + s.progress.view() + "\n")
		b.WriteString(s.exec.view(width, height) + "\n")

	case packagesDone:
		successCount, failCount := batchResultCounts(s.batchErrs)
		if s.err != nil && successCount == 0 && failCount == 0 {
			if strings.Contains(strings.ToLower(s.err.Error()), "cancelled") {
				b.WriteString("  " + warnStyle.Render("Uninstall cancelled before any packages completed") + "\n\n")
			} else {
				b.WriteString("  " + warnStyle.Render("Uninstall failed before any packages completed") + "\n\n")
			}
		} else if s.batchTotal == 1 && failCount == 0 && len(s.batchPackages) == 1 {
			pkg := s.batchPackages[0]
			b.WriteString("  " + successStyle.Render(uninstallPackageLabel(pkg)+" uninstalled successfully") + "\n")
			if pkg.ID != "" {
				b.WriteString("  " + helpStyle.Render(pkg.ID) + "\n\n")
			} else {
				b.WriteString("\n")
			}
		} else if failCount > 0 {
			b.WriteString("  " + warnStyle.Render(
				fmt.Sprintf("Uninstall finished: %d succeeded, %d failed", successCount, failCount),
			) + "\n")
			if successCount > 0 {
				b.WriteString("  " + helpStyle.Render(
					fmt.Sprintf("%d package(s) were removed before the run completed.", successCount),
				) + "\n")
			}
			if batchRequiresElevation(s.batchErrs, s.batchOutputs) {
				b.WriteString("  " + helpStyle.Render("Some packages require administrator privileges.") + "\n")
				b.WriteString("  " + helpStyle.Render(elevationRetryHint()) + "\n\n")
			} else {
				b.WriteString("\n")
			}
		} else {
			b.WriteString("  " + successStyle.Render(
				fmt.Sprintf("%d package(s) uninstalled successfully", successCount),
			) + "\n\n")
		}

		if s.output != "" {
			lines := strings.Split(s.output, "\n")
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
			b.WriteString("\n  " + helpStyle.Render("Press ctrl+e to retry elevated, r to reload, or tab to switch screens") + "\n")
		} else {
			b.WriteString("\n  " + helpStyle.Render("Press r to reload or tab to switch screens") + "\n")
		}
	}

	return b.String()
}

// ── Table builders ─────────────────────────────────────────────────

// upgradeableIDs returns a set of package IDs that have updates available.
func upgradeableIDs() map[string]bool {
	pkgs, ok := cache.getUpgradeable()
	if !ok {
		return nil
	}
	ids := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		ids[p.ID] = true
	}
	return ids
}

// buildSelectableTable creates a table with a checkbox column.
// selected maps row index → bool.
// Responsively adds Source and update indicator columns when width allows.
func buildSelectableTable(pkgs []Package, selected map[string]bool, width, height int) table.Model {
	usable := width - 8
	if usable < 40 {
		usable = 40
	}

	upgrades := upgradeableIDs()
	checkW := 5
	updW := 3 // "↑" indicator column

	// Determine which optional columns fit
	showSource := usable >= 88
	showUpdate := upgrades != nil && usable >= 58

	// Reserve space for fixed columns
	reserved := checkW
	if showUpdate {
		reserved += updW
	}
	srcW := 0
	if showSource {
		srcW = 9
		reserved += srcW
	}

	remaining := usable - reserved
	nameW, idW, verW := responsiveInstalledColumnWidths(remaining)

	var columns []table.Column
	columns = append(columns, table.Column{Title: "     ", Width: checkW})
	if showUpdate {
		columns = append(columns, table.Column{Title: " ↑", Width: updW})
	}
	columns = append(columns,
		table.Column{Title: "Name", Width: nameW},
		table.Column{Title: "ID", Width: idW},
		table.Column{Title: "Version", Width: verW},
	)
	if showSource {
		columns = append(columns, table.Column{Title: "Source", Width: srcW})
	}

	rows := make([]table.Row, len(pkgs))
	for i, p := range pkgs {
		check := " [ ] "
		if selected[p.ID] {
			check = " [X] "
		}
		var row table.Row
		row = append(row, check)
		if showUpdate {
			if upgrades[p.ID] {
				row = append(row, " ↑")
			} else {
				row = append(row, "  ")
			}
		}
		row = append(row, p.Name, p.ID, p.Version)
		if showSource {
			row = append(row, identityKind(p))
		}
		rows[i] = row
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithWidth(width),
		table.WithHeight(height),
	)
	st := table.DefaultStyles()
	st.Header = st.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(accent).
		BorderBottom(true).
		Bold(true).
		Foreground(secondary)
	st.Selected = st.Selected.
		Foreground(lipgloss.Color("229")).
		Background(accent).
		Bold(false)
	t.SetStyles(st)
	return t
}

func packagesTableWidth(screenWidth int) int {
	width := screenWidth - 4
	if width < 60 {
		return 60
	}
	return width
}

func responsiveInstalledColumnWidths(remaining int) (nameW, idW, verW int) {
	const (
		minName = 12
		minID   = 18
		minVer  = 10
	)

	base := minName + minID + minVer
	if remaining <= base {
		return minName, minID, minVer
	}

	extra := remaining - base
	nameW = minName + extra*35/100
	idW = minID + extra*45/100
	verW = remaining - nameW - idW
	if verW < minVer {
		deficit := minVer - verW
		steal := deficit / 2
		if idW-minID > steal {
			idW -= steal
		}
		if nameW-minName > deficit-steal {
			nameW -= deficit - steal
		}
		verW = remaining - nameW - idW
		if verW < minVer {
			verW = minVer
		}
	}
	return nameW, idW, verW
}

func clampTableCursor(cursor, rowCount int) int {
	if rowCount <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= rowCount {
		return rowCount - 1
	}
	return cursor
}

func ensureTableCursorVisible(t *table.Model, cursor int) {
	t.SetCursor(cursor)
	t.MoveDown(0)
}

func (s packagesScreen) helpKeys() []key.Binding {
	if s.importFlow.active {
		return s.importFlow.helpKeys()
	}
	if s.detail.visible() {
		return s.detail.helpKeys()
	}
	switch s.state {
	case packagesLoading, packagesUninstalling:
		if s.state == packagesUninstalling {
			return s.exec.helpKeys()
		}
		return []key.Binding{keyEscCancel}
	case packagesEmpty:
		return []key.Binding{keyRefresh, keyTabs}
	case packagesDone:
		if s.retryAction != nil && !isElevated() {
			return []key.Binding{keyRetryElevated, keyRefresh, keyTabs}
		}
		return []key.Binding{keyRefresh, keyTabs}
	case packagesReady:
		if s.filter.active {
			filtered := s.filteredPkgs()
			bindings := []key.Binding{keyScroll}
			if len(filtered) > 0 {
				bindings = append(bindings, keyToggle)
			}
			return append(bindings, keyApply, keyEsc)
		}
		filtered := s.filteredPkgs()
		selected := s.selectedCount()
		bindings := []key.Binding{keyScroll}
		if s.filter.query != "" {
			bindings = append(bindings, keyFilterEdit)
		} else {
			bindings = append(bindings, keyFilter)
		}
		if len(filtered) > 0 {
			bindings = append(bindings, keyToggle, keyDetails)
		}
		if selected > 0 {
			bindings = append(bindings, keyUninstall)
		}
		if !useCompactHelp(s.width) {
			bindings = append(bindings, keyExport, keyImport)
		}
		if !useCompactHelp(s.width) || (s.filter.query == "" && selected == 0) {
			bindings = append(bindings, keyRefresh)
		}
		if s.filter.query != "" || selected > 0 {
			bindings = append(bindings, keyEscClear)
		}
		if s.retryAction != nil && !isElevated() {
			bindings = append(bindings, keyRetryElevated)
		}
		return bindings
	case packagesConfirmUninstall:
		return []key.Binding{keyConfirm, keyCancel}
	}
	return []key.Binding{keyTabs}
}

func (s packagesScreen) blocksGlobalShortcuts() bool {
	return s.state == packagesConfirmUninstall
}
