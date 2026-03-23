package main

import (
	"context"
	"fmt"
	"strings"

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
	state      packagesState
	table      table.Model
	spinner    spinner.Model
	progress   progressBar
	packages   []Package
	selected   map[int]bool // selected by filtered index
	count      int
	err        error
	detail     detailPanel
	exportMsg  string
	statusMsg  string
	filter     listFilter
	cancel     context.CancelFunc
}

func newPackagesScreen() packagesScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return packagesScreen{
		state:    packagesLoading,
		selected: make(map[int]bool),
		spinner:  sp,
		progress: newProgressBar(50),
		detail:   newDetailPanel(),
		filter:   newListFilter(),
	}
}

func (s packagesScreen) init() tea.Cmd {
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
			return s, s.reload()
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
					s.detail, cmd = s.detail.show(filtered[row].ID)
					return s, cmd
				}
			case "e":
				return s, exportPackages(s.packages)
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
				return s, s.reload()
			}

		case packagesConfirmUninstall:
			switch msg.String() {
			case "y", "Y":
				s.state = packagesUninstalling
				s.progress, _ = s.progress.start()
				ctx, cancel := context.WithCancel(context.Background())
				s.cancel = cancel
				ids := s.selectedIDs()
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					var outputs []string
					var lastErr error
					for _, id := range ids {
						out, err := uninstallPackageCtx(ctx, id)
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
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && s.state == packagesReady {
			var cmd tea.Cmd
			s.table, cmd = s.table.Update(msg)
			return s, cmd
		}

	case packagesLoadedMsg:
		s.progress = s.progress.stop()
		if msg.err != nil || len(msg.packages) == 0 {
			s.err = msg.err
			s.state = packagesEmpty
			return s, nil
		}
		s.packages = msg.packages
		s.count = len(msg.packages)
		s.selected = make(map[int]bool)
		filtered := s.filter.filterPackages(s.packages)
		s.table = buildSelectableTable(filtered, s.selected, 120, 30)
		s.state = packagesReady
		return s, nil

	case commandDoneMsg:
		s.progress = s.progress.stop()
		cache.invalidate()
		count := s.selectedCount()
		if msg.err != nil {
			s.statusMsg = errorStyle.Render("Uninstall error: " + msg.err.Error())
		} else {
			s.statusMsg = successStyle.Render(fmt.Sprintf("%d package(s) uninstalled!", count))
		}
		s.selected = make(map[int]bool)
		// Reload the list
		s.state = packagesLoading
		return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
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
			s.exportMsg = "Exported to " + msg.path
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

func (s packagesScreen) filteredPkgs() []Package {
	return s.filter.filterPackages(s.packages)
}

func (s *packagesScreen) rebuildTable() {
	filtered := s.filter.filterPackages(s.packages)
	s.table = buildSelectableTable(filtered, s.selected, 120, 30)
}

func (s packagesScreen) reload() tea.Cmd {
	cache.invalidate()
	return func() tea.Msg { return switchScreenMsg(screenPackages) }
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

func (s packagesScreen) selectedIDs() []string {
	filtered := s.filteredPkgs()
	var ids []string
	for i, sel := range s.selected {
		if sel && i < len(filtered) {
			ids = append(ids, filtered[i].ID)
		}
	}
	return ids
}

func (s packagesScreen) selectedNames() []string {
	filtered := s.filteredPkgs()
	var names []string
	for i, sel := range s.selected {
		if sel && i < len(filtered) {
			names = append(names, filtered[i].Name)
		}
	}
	return names
}

// ── View ───────────────────────────────────────────────────────────

func (s packagesScreen) view(width, height int) string {
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Installed Packages") + "\n\n" +
			s.detail.view(width, height-2)
	}

	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Installed Packages") + "\n\n")

	switch s.state {
	case packagesLoading:
		b.WriteString(fmt.Sprintf("  %s Loading installed packages...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case packagesEmpty:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else {
			b.WriteString("  " + warnStyle.Render("No packages found.") + "\n")
		}

	case packagesReady:
		filtered := s.filteredPkgs()
		b.WriteString(fmt.Sprintf("  %s\n",
			infoStyle.Render(fmt.Sprintf("%d package(s) installed.", s.count))))

		filterView := s.filter.view()
		if filterView != "" {
			b.WriteString(filterView + fmt.Sprintf("  %s",
				helpStyle.Render(fmt.Sprintf("(%d shown)", len(filtered)))) + "\n")
		}

		// Selection count
		selCount := s.selectedCount()
		if selCount > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d selected for removal — press u to uninstall", selCount))))
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
		b.WriteString(fmt.Sprintf("  Uninstall %d package(s)?\n\n", len(names)))
		for _, name := range names {
			b.WriteString("  " + errorStyle.Render("  "+name) + "\n")
		}
		b.WriteString("\n  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case packagesUninstalling:
		b.WriteString(fmt.Sprintf("  %s Uninstalling...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case packagesDone:
		if s.statusMsg != "" {
			b.WriteString("  " + s.statusMsg + "\n")
		}
	}

	return b.String()
}

func (s packagesScreen) helpKeys() []key.Binding {
	switch s.state {
	case packagesLoading, packagesUninstalling:
		return []key.Binding{keyEscCancel}
	case packagesEmpty, packagesDone:
		return []key.Binding{keyRefresh, keyTabs}
	case packagesReady:
		if s.filter.active {
			return []key.Binding{keyUp, keyDown, keyEnter, keyEsc}
		}
		bindings := []key.Binding{keyUp, keyDown, keyFilter, keyToggle, keyDetails, keyExport, keyRefresh}
		if s.selectedCount() > 0 {
			bindings = append(bindings, keyUninstall)
		}
		return bindings
	case packagesConfirmUninstall:
		return []key.Binding{keyConfirmY}
	}
	return []key.Binding{keyTabs}
}
