package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

type installState int

const (
	installInput installState = iota
	installSearching
	installResults
	installConfirm
	installExecuting
	installDone
)

type installScreen struct {
	state    installState
	input    textinput.Model
	spinner  spinner.Model
	progress progressBar
	packages []Package
	cursor   int
	output   string
	err      error
	detail   detailPanel
}

func newInstallScreen() installScreen {
	ti := textinput.New()
	ti.Placeholder = "Search for a package..."
	ti.PromptStyle = lipgloss.NewStyle().Foreground(accent)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(accent)
	ti.Focus()
	ti.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	return installScreen{
		state:    installInput,
		input:    ti,
		spinner:  sp,
		progress: newProgressBar(50),
		detail:   newDetailPanel(),
	}
}

func (s installScreen) init() tea.Cmd {
	return textinput.Blink
}

func (s installScreen) update(msg tea.Msg) (screen, tea.Cmd) {
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
		case installInput:
			switch msg.String() {
			case "enter":
				query := strings.TrimSpace(s.input.Value())
				if query == "" {
					return s, nil
				}
				s.state = installSearching
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

		case installResults:
			switch msg.String() {
			case "up", "k":
				if s.cursor > 0 {
					s.cursor--
				}
			case "down", "j":
				if s.cursor < len(s.packages)-1 {
					s.cursor++
				}
			case "i", "d":
				if len(s.packages) > 0 {
					var cmd tea.Cmd
					s.detail, cmd = s.detail.show(s.packages[s.cursor].ID)
					return s, cmd
				}
			case "enter":
				s.state = installConfirm
			case "esc":
				s.state = installInput
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}

		case installConfirm:
			switch msg.String() {
			case "y", "Y":
				pkg := s.packages[s.cursor]
				s.state = installExecuting
				s.progress, _ = s.progress.start()
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					out, err := installPackage(pkg.ID)
					return commandDoneMsg{output: out, err: err}
				})
			case "n", "N", "esc":
				s.state = installResults
			}

		case installDone:
			if msg.String() == "r" {
				cache.invalidate()
				s.state = installInput
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}
			if msg.String() == "enter" || msg.String() == "esc" {
				return s, goToMenu
			}
		}

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			contentY := msg.Y - 7
			if s.state == installResults && contentY >= 1 {
				row := contentY - 1
				if row >= 0 && row < len(s.packages) {
					s.cursor = row
				}
			}
		}

	case packagesLoadedMsg:
		s.progress = s.progress.stop()
		s.packages = msg.packages
		s.err = msg.err
		if msg.err != nil || len(msg.packages) == 0 {
			s.state = installDone
			if msg.err == nil {
				s.output = "No packages found matching your search."
			}
		} else {
			s.state = installResults
			s.cursor = 0
		}
		return s, nil

	case commandDoneMsg:
		s.progress = s.progress.stop()
		s.output = msg.output
		s.err = msg.err
		s.state = installDone
		cache.invalidate()
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
		if s.state == installInput {
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return s, cmd
		}
	}
	return s, nil
}

func (s installScreen) view(width, height int) string {
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Install Package") + "\n\n" +
			s.detail.view(width, height-2)
	}

	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Install Package") + "\n\n")

	switch s.state {
	case installInput:
		b.WriteString("  " + s.input.View() + "\n\n")

	case installSearching:
		b.WriteString(fmt.Sprintf("  %s Searching...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case installResults:
		b.WriteString(fmt.Sprintf("  %s\n\n",
			infoStyle.Render(fmt.Sprintf("%d result(s) found.", len(s.packages)))))
		maxVisible := height - 8
		if maxVisible < 5 {
			maxVisible = 5
		}
		start, end := scrollWindow(s.cursor, len(s.packages), maxVisible)
		for i := start; i < end; i++ {
			pkg := s.packages[i]
			cursor := cursorBlankStr
			style := itemStyle
			if i == s.cursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			label := fmt.Sprintf("%s  (%s)  %s", pkg.Name, pkg.ID, pkg.Version)
			b.WriteString(fmt.Sprintf("  %s%s\n", cursor, style.Render(label)))
		}

	case installConfirm:
		pkg := s.packages[s.cursor]
		b.WriteString(fmt.Sprintf("  Install %s (%s)?\n\n",
			itemActiveStyle.Render(pkg.Name), pkg.ID))
		b.WriteString("  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case installExecuting:
		b.WriteString(fmt.Sprintf("  %s Installing...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case installDone:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else if s.output != "" && len(s.packages) == 0 {
			b.WriteString("  " + warnStyle.Render(s.output) + "\n")
		} else {
			b.WriteString("  " + successStyle.Render("Installation complete!") + "\n")
		}
	}

	return b.String()
}

func (s installScreen) helpKeys() []key.Binding {
	switch s.state {
	case installInput:
		return []key.Binding{keySearch, keyEsc}
	case installSearching, installExecuting:
		return []key.Binding{keyEscCancel}
	case installResults:
		return []key.Binding{keyUp, keyDown, keyDetails, keyEnter, keyEsc}
	case installConfirm:
		return []key.Binding{keyConfirmY}
	case installDone:
		return []key.Binding{keyRefresh, keyTabs}
	}
	return []key.Binding{keyTabs}
}
