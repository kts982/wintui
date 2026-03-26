package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
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
	table         table.Model
	spinner       spinner.Model
	progress      progressBar
	packages      []Package
	selected      map[int]bool // selected by filtered index
	count         int
	err           error
	detail        detailPanel
	importFlow    importModel
	exportMsg     string
	statusMsg     string
	filter        listFilter
	cancel        context.CancelFunc
	launchRetry   *retryRequest
	retryAction   *retryRequest
	attemptAction *retryRequest
	flashSeq      int
}

type packagesFlashClearMsg struct {
	seq int
}

func newPackagesScreen() packagesScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return packagesScreen{
		state:      packagesLoading,
		selected:   make(map[int]bool),
		spinner:    sp,
		progress:   newProgressBar(50),
		detail:     newDetailPanel(),
		importFlow: newImportModel(),
		filter:     newListFilter(),
	}
}

func newPackagesScreenWithRetry(req retryRequest) packagesScreen {
	s := newPackagesScreen()
	s.state = packagesUninstalling
	s.launchRetry = &req
	return s
}

func (s packagesScreen) init() tea.Cmd {
	if s.launchRetry != nil {
		req := *s.launchRetry
		return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
			out, err := uninstallPackageSourceCtx(context.Background(), req.ID, req.Source)
			return commandDoneMsg{output: out, err: err}
		})
	}
	if pkgs, ok := cache.getInstalled(); ok {
		return func() tea.Msg {
			return packagesLoadedMsg{packages: pkgs}
		}
	}
	return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
		pkgs, err := getInstalled()
		if err == nil {
			cache.setInstalled(pkgs)
		}
		return packagesLoadedMsg{packages: pkgs, err: err}
	})
}

func (s packagesScreen) update(msg tea.Msg) (screen, tea.Cmd) {
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
				return s.reload()
			}
			return s, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Filter input mode
		if s.filter.active {
			switch msg.String() {
			case "enter":
				s.filter = s.filter.apply()
				s.rebuildTable()
				s.selected = make(map[int]bool)
				return s, nil
			case "esc":
				s.filter = s.filter.deactivate()
				s.rebuildTable()
				s.selected = make(map[int]bool)
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
				s.selected = make(map[int]bool)
				return s, cmd
			}
		}

		// Refresh
		if msg.String() == "r" && (s.state == packagesReady || s.state == packagesEmpty || s.state == packagesDone) {
			return s.reload()
		}

		// Cancel
		if msg.String() == "esc" && s.state == packagesUninstalling {
			if s.cancel != nil {
				s.cancel()
			}
			s.state = packagesReady
			s.statusMsg = warnStyle.Render("Cancelled")
			s.progress = s.progress.stop()
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
					s.selected = make(map[int]bool)
					return s, nil
				}
			case "i", "d":
				filtered := s.filteredPkgs()
				row := s.table.Cursor()
				if row >= 0 && row < len(filtered) {
					var cmd tea.Cmd
					pkg := filtered[row]
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
			case " ", "x":
				row := s.table.Cursor()
				if row >= 0 {
					s.selected[row] = !s.selected[row]
					// Rebuild table to update checkbox display
					s.rebuildTable()
					s.table.SetCursor(row)
					// Move cursor down
					s.table, _ = s.table.Update(tea.KeyMsg{Type: tea.KeyDown})
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

		case packagesEmpty, packagesDone:
			if msg.String() == "enter" || msg.String() == "esc" {
				return s.reload()
			}

		case packagesConfirmUninstall:
			switch msg.String() {
			case "y", "Y":
				s.state = packagesUninstalling
				s.progress, _ = s.progress.start()
				ctx, cancel := context.WithCancel(context.Background())
				s.cancel = cancel
				selected := s.selectedPackages()
				s.attemptAction = nil
				if len(selected) == 1 {
					s.attemptAction = &retryRequest{Op: retryOpUninstall, ID: selected[0].ID, Source: selected[0].Source}
				}
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					var outputs []string
					var lastErr error
					for _, pkg := range selected {
						out, err := uninstallPackageSourceCtx(ctx, pkg.ID, pkg.Source)
						outputs = append(outputs, out)
						if err != nil {
							lastErr = err
						}
					}
					return commandDoneMsg{output: strings.Join(outputs, "\n"), err: lastErr}
				})
			case "n", "N", "esc":
				s.state = packagesReady
			}
		}

	case tea.MouseMsg:
		if s.state == packagesReady {
			switch {
			case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
				var cmd tea.Cmd
				s.table, cmd = s.table.Update(msg)
				return s, cmd
			case msg.Button == tea.MouseButtonWheelUp:
				s.table.MoveUp(3)
				return s, nil
			case msg.Button == tea.MouseButtonWheelDown:
				s.table.MoveDown(3)
				return s, nil
			}
		}

	case packagesLoadedMsg:
		s.progress = s.progress.stop()
		if msg.err != nil || len(msg.packages) == 0 {
			s.err = msg.err
			s.state = packagesEmpty
			return s, nil
		}
		s.packages = deduplicatePackages(msg.packages)
		s.count = len(s.packages)
		s.selected = make(map[int]bool)
		filtered := s.filter.filterPackages(s.packages)
		s.table = buildSelectableTable(filtered, s.selected, 120, 30)
		s.state = packagesReady
		return s, nil

	case commandDoneMsg:
		s.progress = s.progress.stop()
		cache.invalidate()
		count := s.selectedCount()
		s.retryAction = nil
		if msg.err != nil {
			if requiresElevation(msg.err, msg.output) {
				s.statusMsg = errorStyle.Render("Uninstall blocked: administrator privileges required. " + elevationRetryHint())
				if s.attemptAction != nil && !isElevated() {
					s.retryAction = s.attemptAction
				}
			} else {
				s.statusMsg = errorStyle.Render("Uninstall error: " + msg.err.Error())
			}
		} else {
			s.statusMsg = successStyle.Render(fmt.Sprintf("%d package(s) uninstalled!", count))
		}
		s.flashSeq++
		s.selected = make(map[int]bool)
		s.attemptAction = nil
		// Reload the list
		s.state = packagesLoading
		return s, tea.Batch(s.spinner.Tick, tickProgress(), clearPackagesFlashAfter(s.flashSeq, 8*time.Second), func() tea.Msg {
			pkgs, err := getInstalled()
			if err == nil {
				cache.setInstalled(pkgs)
			}
			return packagesLoadedMsg{packages: pkgs, err: err}
		})

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
	s.table = buildSelectableTable(filtered, s.selected, 120, 30)
}

func (s packagesScreen) reload() (packagesScreen, tea.Cmd) {
	cache.invalidate()
	s.state = packagesLoading
	s.exportMsg = ""
	s.statusMsg = ""
	s.retryAction = nil
	s.flashSeq++
	s.progress, _ = s.progress.start()
	return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
		pkgs, err := getInstalled()
		if err == nil {
			cache.setInstalled(pkgs)
		}
		return packagesLoadedMsg{packages: pkgs, err: err}
	})
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

func (s packagesScreen) selectedNames() []string {
	selected := s.selectedPackages()
	var names []string
	for _, pkg := range selected {
		names = append(names, pkg.Name)
	}
	return names
}

func (s packagesScreen) selectedPackages() []Package {
	filtered := s.filteredPkgs()
	var pkgs []Package
	for i, sel := range s.selected {
		if sel && i < len(filtered) {
			pkgs = append(pkgs, filtered[i])
		}
	}
	return pkgs
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

	case packagesReady:
		filtered := s.filteredPkgs()
		b.WriteString("  " + infoStyle.Render(packageSummary(s.packages)) + "\n")

		filterView := s.filter.view()
		if filterView != "" {
			b.WriteString(filterView + fmt.Sprintf("  %s",
				helpStyle.Render(fmt.Sprintf("(%d shown)", len(filtered)))) + "\n")
		}

		// Selection count
		selCount := s.selectedCount()
		if selCount > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d selected — press u to uninstall or e to export", selCount))))
		}

		// Dynamically set table height to fill available space
		tableH := height - 8
		if s.filter.query != "" || s.filter.active {
			tableH -= 2
		}
		if selCount > 0 {
			tableH -= 1
		}
		if tableH < 5 {
			tableH = 5
		}
		s.table.SetHeight(tableH)
		b.WriteString("\n  " + s.table.View() + "\n")

		if s.exportMsg != "" {
			b.WriteString("  " + successStyle.Render(s.exportMsg) + "\n")
		}
		if s.statusMsg != "" {
			b.WriteString("  " + s.statusMsg + "\n")
		}

	case packagesConfirmUninstall:
		names := s.selectedNames()
		fmt.Fprintf(&b, "  Uninstall %d package(s)?\n\n", len(names))
		for _, name := range names {
			b.WriteString("  " + errorStyle.Render("  "+name) + "\n")
		}
		b.WriteString("\n  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case packagesUninstalling:
		fmt.Fprintf(&b, "  %s Uninstalling...\n\n", s.spinner.View())
		b.WriteString("  " + s.progress.view() + "\n")

	case packagesDone:
		if s.statusMsg != "" {
			b.WriteString("  " + s.statusMsg + "\n")
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
func buildSelectableTable(pkgs []Package, selected map[int]bool, width, height int) table.Model {
	usable := width - 8
	if usable < 40 {
		usable = 40
	}

	upgrades := upgradeableIDs()
	checkW := 5
	updW := 3 // "↑" indicator column

	// Determine which optional columns fit
	showSource := usable >= 90
	showUpdate := upgrades != nil && usable >= 60

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
	nameW := remaining * 30 / 100
	idW := remaining * 40 / 100
	verW := remaining - nameW - idW

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
		if selected[i] {
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

func (s packagesScreen) helpKeys() []key.Binding {
	if s.importFlow.active {
		return s.importFlow.helpKeys()
	}
	switch s.state {
	case packagesLoading, packagesUninstalling:
		return []key.Binding{keyEscCancel}
	case packagesEmpty, packagesDone:
		return []key.Binding{keyRefresh, keyTabs}
	case packagesReady:
		if s.filter.active {
			return []key.Binding{keyUp, keyDown, keyEnter, keyEsc}
		}
		bindings := []key.Binding{keyUp, keyDown, keyFilter, keyToggle, keyDetails, keyExport, keyImport, keyRefresh}
		if s.retryAction != nil && !isElevated() {
			bindings = append(bindings, keyRetryElevated)
		}
		if s.selectedCount() > 0 {
			bindings = append(bindings, keyUninstall)
		}
		return bindings
	case packagesConfirmUninstall:
		return []key.Binding{keyConfirmY}
	}
	return []key.Binding{keyTabs}
}
