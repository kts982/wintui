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
	state          installState
	input          textinput.Model
	spinner        spinner.Model
	progress       progressBar
	packages       []Package
	cursor         int
	output         string
	err            error
	detail         detailPanel
	vp             viewport.Model
	outLines       []string
	installOutChan <-chan string
	installErrChan <-chan error
	ctx            context.Context
	cancel         context.CancelFunc
	launchRetry    *retryRequest
	retryAction    *retryRequest
}

func newInstallScreen() installScreen {
	ti := textinput.New()
	ti.Placeholder = "Search for a package..."
	styles := ti.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(accent)
	styles.Cursor.Color = accent
	ti.SetStyles(styles)
	ti.Focus()
	ti.SetWidth(40)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(10))
	vp.Style = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(accent)

	return installScreen{
		state:    installInput,
		input:    ti,
		spinner:  sp,
		progress: newProgressBar(50),
		detail:   newDetailPanel(),
		vp:       vp,
	}
}

func newInstallScreenWithRetry(req retryRequest) installScreen {
	s := newInstallScreen()
	s.state = installExecuting
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.launchRetry = &req
	return s
}

func awaitStream(outChan <-chan string, errChan <-chan error) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-outChan
		if !ok {
			return streamDoneMsg{err: <-errChan}
		}
		return streamMsg(line)
	}
}

func (s installScreen) init() tea.Cmd {
	if s.launchRetry != nil {
		req := *s.launchRetry
		return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
			out, err := installPackageSourceCtx(s.ctx, req.ID, req.Source)
			return commandDoneMsg{output: out, err: err}
		})
	}
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
	case tea.KeyPressMsg:
		if msg.String() == "esc" && s.state == installSearching {
			if s.cancel != nil {
				s.cancel()
			}
			s.progress = s.progress.stop()
			s.retryAction = nil
			s.err = nil
			s.output = ""
			s.state = installInput
			s.input.Focus()
			return s, textinput.Blink
		}
		if msg.String() == "esc" && s.state == installExecuting {
			if s.cancel != nil {
				s.cancel()
			}
			s.progress = s.progress.stop()
			s.retryAction = nil
			s.output = strings.Join(s.outLines, "\n")
			s.err = fmt.Errorf("cancelled")
			s.state = installDone
			return s, nil
		}

		switch s.state {
		case installInput:
			switch msg.String() {
			case "enter":
				query := strings.TrimSpace(s.input.Value())
				if query == "" {
					return s, nil
				}
				s.state = installSearching
				s.retryAction = nil
				s.ctx, s.cancel = context.WithCancel(context.Background())
				s.progress, _ = s.progress.start()
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					pkgs, err := searchPackagesCtx(s.ctx, query)
					return packagesLoadedMsg{packages: pkgs, err: err}
				})
			case "esc":
				if s.input.Value() != "" {
					s.input.SetValue("")
					s.input.Focus()
					return s, textinput.Blink
				}
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
					pkg := s.packages[s.cursor]
					s.detail, cmd = s.detail.show(pkg.ID, pkg.Source)
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
				s.retryAction = nil
				s.ctx, s.cancel = context.WithCancel(context.Background())
				s.progress, _ = s.progress.start()

				s.installOutChan, s.installErrChan = installPackageStreamCtx(s.ctx, pkg.ID, pkg.Source)
				s.outLines = nil
				s.vp.SetContent("")

				return s, tea.Batch(
					s.spinner.Tick,
					tickProgress(),
					awaitStream(s.installOutChan, s.installErrChan),
				)
			case "n", "N", "esc":
				s.state = installResults
			}

		case installDone:
			if msg.String() == "ctrl+e" && s.retryAction != nil && !isElevated() {
				if err := relaunchElevatedRetry(*s.retryAction); err != nil {
					s.err = fmt.Errorf("failed to relaunch elevated: %w", err)
					return s, nil
				}
				return s, tea.Quit
			}
			if msg.String() == "r" {
				cache.invalidate()
				s.state = installInput
				s.retryAction = nil
				s.err = nil
				s.output = ""
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}
			if msg.String() == "esc" {
				s.state = installInput
				s.retryAction = nil
				s.err = nil
				s.output = ""
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			contentY := msg.Y - 7
			if s.state == installResults && contentY >= 1 {
				row := contentY - 1
				if row >= 0 && row < len(s.packages) {
					s.cursor = row
				}
			}
		}

	case packagesLoadedMsg:
		if s.state != installSearching {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.retryAction = nil
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
		if s.state != installExecuting {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.output = msg.output
		s.err = msg.err
		s.state = installDone
		s.retryAction = nil
		if msg.err != nil && requiresElevation(msg.err, msg.output) && !isElevated() && s.launchRetry == nil {
			if len(s.packages) > 0 && s.cursor >= 0 && s.cursor < len(s.packages) {
				pkg := s.packages[s.cursor]
				s.retryAction = &retryRequest{Op: retryOpInstall, ID: pkg.ID, Source: pkg.Source}
			}
		}
		cache.invalidate()
		if msg.err == nil {
			return s, func() tea.Msg { return packageDataChangedMsg{origin: screenInstall} }
		}
		return s, nil

	case streamMsg:
		if s.state != installExecuting {
			return s, nil
		}
		s.outLines = append(s.outLines, string(msg))
		s.vp.SetContent(strings.Join(s.outLines, "\n"))
		s.vp.GotoBottom()
		return s, awaitStream(s.installOutChan, s.installErrChan)

	case streamDoneMsg:
		if s.state != installExecuting {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.err = msg.err
		s.output = strings.Join(s.outLines, "\n")
		s.state = installDone
		s.retryAction = nil
		if msg.err != nil && requiresElevation(msg.err, s.output) && !isElevated() && len(s.packages) > 0 && s.cursor >= 0 && s.cursor < len(s.packages) {
			pkg := s.packages[s.cursor]
			s.retryAction = &retryRequest{Op: retryOpInstall, ID: pkg.ID, Source: pkg.Source}
		}
		cache.invalidate()
		if msg.err == nil {
			return s, func() tea.Msg { return packageDataChangedMsg{origin: screenInstall} }
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

	default:
		if s.state == installInput {
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return s, cmd
		}
	}
	return s, nil
}

func (s installScreen) currentPackage() (Package, bool) {
	if s.cursor < 0 || s.cursor >= len(s.packages) {
		return Package{}, false
	}
	return s.packages[s.cursor], true
}

func (s installScreen) view(width, height int) string {
	if s.detail.visible() {
		return "  " + sectionTitleStyle.Render("Install Package") + "\n" +
			s.detail.view(width, height-2)
	}

	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Install Package") + "\n\n")

	switch s.state {
	case installInput:
		b.WriteString("  " + s.input.View() + "\n\n")

	case installSearching:
		fmt.Fprintf(&b, "  %s Searching...\n\n", s.spinner.View())
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
			if pkg.Source != "" {
				label += fmt.Sprintf("  [%s]", pkg.Source)
			}
			fmt.Fprintf(&b, "  %s%s\n", cursor, style.Render(label))
		}

	case installConfirm:
		pkg := s.packages[s.cursor]
		b.WriteString(fmt.Sprintf("  Install %s (%s)?\n\n",
			itemActiveStyle.Render(pkg.Name), pkg.ID))
		b.WriteString("  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case installExecuting:
		fmt.Fprintf(&b, "  %s Installing...\n\n", s.spinner.View())
		b.WriteString("  " + s.progress.view() + "\n")
		s.vp.SetWidth(width - 8)
		vpH := height - 12
		if vpH < 5 {
			vpH = 5
		}
		s.vp.SetHeight(vpH)
		b.WriteString("  " + s.vp.View() + "\n")

	case installDone:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
			if pkg, ok := s.currentPackage(); ok {
				b.WriteString("  " + helpStyle.Render(fmt.Sprintf("%s  (%s)", pkg.Name, pkg.ID)) + "\n")
			}
			if requiresElevation(s.err, s.output) {
				b.WriteString("  " + helpStyle.Render(elevationRetryHint()) + "\n")
			}
		} else if s.output != "" && len(s.packages) == 0 {
			b.WriteString("  " + warnStyle.Render(s.output) + "\n")
		} else {
			pkg, ok := s.currentPackage()
			if ok {
				b.WriteString("  " + successStyle.Render(pkg.Name+" installed successfully") + "\n")
				var meta []string
				meta = append(meta, pkg.ID)
				if pkg.Version != "" {
					meta = append(meta, pkg.Version)
				}
				if pkg.Source != "" {
					meta = append(meta, "["+pkg.Source+"]")
				}
				b.WriteString("  " + helpStyle.Render(strings.Join(meta, "  ")) + "\n")
			} else {
				b.WriteString("  " + successStyle.Render("Installation complete!") + "\n")
			}
		}
		b.WriteString("\n  " + helpStyle.Render("Press r to search again or esc to leave") + "\n")
	}

	return b.String()
}

func (s installScreen) helpKeys() []key.Binding {
	if s.detail.visible() {
		return s.detail.helpKeys()
	}
	switch s.state {
	case installInput:
		if s.input.Value() != "" {
			return []key.Binding{keySearch, keyEscClear}
		}
		return []key.Binding{keySearch}
	case installSearching, installExecuting:
		return []key.Binding{keyEscCancel}
	case installResults:
		return []key.Binding{keyUp, keyDown, keyDetails, keyEnter, keyEsc}
	case installConfirm:
		return []key.Binding{keyConfirmY}
	case installDone:
		if s.retryAction != nil && !isElevated() {
			return []key.Binding{keyRetryElevated, keySearchAgain, keyEsc, keyTabs}
		}
		return []key.Binding{keySearchAgain, keyEsc, keyTabs}
	}
	return []key.Binding{keyTabs}
}
