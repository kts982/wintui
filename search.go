package main

import (
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

type searchState int

const (
	searchInput searchState = iota
	searchSearching
	searchResults
	searchEmpty
)

type searchScreen struct {
	state    searchState
	input    textinput.Model
	spinner  spinner.Model
	progress progressBar
	table    table.Model
	packages []Package
	err      error
	detail   detailPanel
}

func newSearchScreen() searchScreen {
	ti := textinput.New()
	ti.Placeholder = "Search for a package..."
	ti.PromptStyle = lipgloss.NewStyle().Foreground(accent)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(accent)
	ti.Focus()
	ti.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	return searchScreen{
		state:    searchInput,
		input:    ti,
		spinner:  sp,
		progress: newProgressBar(50),
		detail:   newDetailPanel(),
	}
}

func (s searchScreen) init() tea.Cmd {
	return textinput.Blink
}

func (s searchScreen) update(msg tea.Msg) (screen, tea.Cmd) {
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
		switch s.state {
		case searchInput:
			switch msg.String() {
			case "enter":
				query := strings.TrimSpace(s.input.Value())
				if query == "" {
					return s, nil
				}
				s.state = searchSearching
				s.progress, _ = s.progress.start()
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					pkgs, err := searchPackages(query)
					return packagesLoadedMsg{packages: pkgs, err: err}
				})
			case "esc":
				return s, goToMenu
			}
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return s, cmd

		case searchResults:
			switch msg.String() {
			case "i", "d":
				// Get selected row index from table cursor
				row := s.table.Cursor()
				if row >= 0 && row < len(s.packages) {
					var cmd tea.Cmd
					s.detail, cmd = s.detail.show(s.packages[row].ID)
					return s, cmd
				}
			case "esc", "q":
				s.state = searchInput
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}
			var cmd tea.Cmd
			s.table, cmd = s.table.Update(msg)
			return s, cmd

		case searchEmpty:
			if msg.String() == "enter" || msg.String() == "esc" {
				s.state = searchInput
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}
		}

	case packagesLoadedMsg:
		s.progress = s.progress.stop()
		if msg.err != nil {
			s.err = msg.err
			s.state = searchEmpty
			return s, nil
		}
		if len(msg.packages) == 0 {
			s.state = searchEmpty
			return s, nil
		}
		s.packages = msg.packages
		s.table = buildPackageTable(msg.packages, 120, 15)
		s.state = searchResults
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

	default:
		if s.state == searchInput {
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return s, cmd
		}
	}
	return s, nil
}

func (s searchScreen) view(width, height int) string {
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Search Packages") + "\n\n" +
			s.detail.view(width, height-2)
	}

	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Search Packages") + "\n\n")

	switch s.state {
	case searchInput:
		b.WriteString("  " + s.input.View() + "\n\n")

	case searchSearching:
		b.WriteString(fmt.Sprintf("  %s Searching...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case searchResults:
		b.WriteString("  " + s.table.View() + "\n\n")

	case searchEmpty:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else {
			b.WriteString("  " + warnStyle.Render("No results found.") + "\n")
		}
	}

	return b.String()
}

func (s searchScreen) helpKeys() []key.Binding {
	switch s.state {
	case searchInput:
		return []key.Binding{keySearch, keyEsc}
	case searchSearching:
		return []key.Binding{keyEscCancel}
	case searchResults:
		return []key.Binding{keyUp, keyDown, keyDetails, keyEsc}
	case searchEmpty:
		return []key.Binding{keyEnter, keyEsc}
	}
	return []key.Binding{keyTabs}
}

// buildPackageTable creates a responsive bubbles/table from a package slice.
// If showAvailable is true, an "Available" column is shown (for upgrade lists).
func buildPackageTable(pkgs []Package, width, height int) table.Model {
	return buildPackageTableOpts(pkgs, width, height, false)
}

func buildUpgradeTable(pkgs []Package, width, height int) table.Model {
	return buildPackageTableOpts(pkgs, width, height, true)
}

// buildSelectableTable creates a table with a checkbox column.
// selected maps row index → bool.
func buildSelectableTable(pkgs []Package, selected map[int]bool, width, height int) table.Model {
	usable := width - 8
	if usable < 40 {
		usable = 40
	}
	checkW := 5
	nameW := (usable - checkW) * 35 / 100
	idW := (usable - checkW) * 40 / 100
	verW := usable - checkW - nameW - idW

	columns := []table.Column{
		{Title: "     ", Width: checkW},
		{Title: "Name", Width: nameW},
		{Title: "ID", Width: idW},
		{Title: "Version", Width: verW},
	}
	rows := make([]table.Row, len(pkgs))
	for i, p := range pkgs {
		check := " [ ] "
		if selected[i] {
			check = " [X] "
		}
		rows[i] = table.Row{check, p.Name, p.ID, p.Version}
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

func buildPackageTableOpts(pkgs []Package, width, height int, showAvailable bool) table.Model {
	usable := width - 8
	if usable < 40 {
		usable = 40
	}

	var columns []table.Column
	var rows []table.Row

	if showAvailable {
		nameW := usable * 30 / 100
		idW := usable * 30 / 100
		verW := usable * 18 / 100
		availW := usable - nameW - idW - verW
		columns = []table.Column{
			{Title: "Name", Width: nameW},
			{Title: "ID", Width: idW},
			{Title: "Version", Width: verW},
			{Title: "Available", Width: availW},
		}
		rows = make([]table.Row, len(pkgs))
		for i, p := range pkgs {
			rows[i] = table.Row{p.Name, p.ID, p.Version, p.Available}
		}
	} else {
		nameW := usable * 35 / 100
		idW := usable * 40 / 100
		verW := usable - nameW - idW
		columns = []table.Column{
			{Title: "Name", Width: nameW},
			{Title: "ID", Width: idW},
			{Title: "Version", Width: verW},
		}
		rows = make([]table.Row, len(pkgs))
		for i, p := range pkgs {
			rows[i] = table.Row{p.Name, p.ID, p.Version}
		}
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
