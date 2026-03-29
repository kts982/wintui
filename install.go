package main

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
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
	state            installState
	width            int
	height           int
	input            textinput.Model
	spinner          spinner.Model
	progress         progressBar
	packages         []Package
	selectedVersions map[string]string
	cursor           int
	output           string
	err              error
	detail           detailPanel
	exec             executionLog
	installOutChan   <-chan string
	installErrChan   <-chan error
	retryArgs        []string
	ctx              context.Context
	cancel           context.CancelFunc
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

	return installScreen{
		state:            installInput,
		width:            80,
		height:           24,
		input:            ti,
		spinner:          sp,
		progress:         newProgressBar(50),
		detail:           newDetailPanel(),
		exec:             newExecutionLog(),
		selectedVersions: make(map[string]string),
	}
}

func (s installScreen) init() tea.Cmd {
	return textinput.Blink
}

func (s installScreen) update(msg tea.Msg) (screen, tea.Cmd) {
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

	if s.state == installExecuting {
		if cmd, handled := s.exec.update(msg); handled {
			return s, cmd
		}
	}
	if s.state == installDone {
		if cmd, handled := s.exec.doneUpdate(msg); handled {
			return s, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s.detail = s.detail.withWindowSize(msg.Width, msg.Height)
		s.exec.setSize(msg.Width, contentAreaHeightForWindow(msg.Width, msg.Height, true))
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
		if msg.String() == "esc" && s.state == installSearching {
			if s.cancel != nil {
				s.cancel()
			}
			s.progress = s.progress.stop()
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
			s.output = s.exec.fullOutput()
			s.err = fmt.Errorf("cancelled")
			s.state = installDone
			s.exec.setDoneExpanded(true)
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
					s.detail = s.detail.withWindowSize(s.width, s.height)
					s.detail, cmd = s.detail.showWithVersion(pkg, s.selectedVersionFor(pkg), true)
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
			case "enter", "y", "Y":
				pkg := s.packages[s.cursor]
				version := s.selectedVersionFor(pkg)
				s.state = installExecuting
				s.ctx, s.cancel = context.WithCancel(context.Background())
				s.progress, _ = s.progress.start()

				s.retryArgs, s.installOutChan, s.installErrChan = installPackageStreamCtx(s.ctx, pkg.ID, pkg.Source, version)
				s.exec.reset()

				return s, tea.Batch(
					s.spinner.Tick,
					tickProgress(),
					awaitStream(s.retryArgs, s.installOutChan, s.installErrChan),
				)
			case "n", "N", "esc":
				s.state = installResults
			}

		case installDone:
			if msg.String() == "ctrl+e" {
				if s.err != nil && requiresElevation(s.err, s.output) && len(s.retryArgs) > 0 {
					s.state = installExecuting
					s.progress, _ = s.progress.start()
					s.exec.reset()
					s.exec.appendSection("== Retrying Elevated ==")
					out, err := globalElevator.runCommandElevated(s.retryArgs...)
					s.installOutChan = out
					s.installErrChan = err
					return s, tea.Batch(
						s.spinner.Tick,
						tickProgress(),
						awaitStream(s.retryArgs, s.installOutChan, s.installErrChan),
					)
				}
			}
			if msg.String() == "r" {
				cache.invalidate()
				s.state = installInput
				s.err = nil
				s.output = ""
				s.input.SetValue("")
				s.input.Focus()
				return s, textinput.Blink
			}
			if msg.String() == "esc" {
				s.state = installInput
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
		s.packages = msg.packages
		s.selectedVersions = make(map[string]string)
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
		cache.invalidate()
		if msg.err == nil {
			return s, func() tea.Msg { return packageDataChangedMsg{origin: screenInstall} }
		}
		return s, nil

	case streamMsg:
		if s.state != installExecuting {
			return s, nil
		}
		s.exec.appendLine(normalizeActionStreamLine(retryOpInstall, string(msg)))
		return s, awaitStream(s.retryArgs, s.installOutChan, s.installErrChan)

	case streamDoneMsg:
		if s.state != installExecuting {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.err = msg.err
		s.retryArgs = msg.retryArgs
		s.output = s.exec.fullOutput()
		s.state = installDone
		s.exec.setDoneExpanded(msg.err != nil)
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

func (s installScreen) packageByIdentity(id, source string) (Package, bool) {
	for _, pkg := range s.packages {
		if pkg.ID == id && pkg.Source == source {
			return pkg, true
		}
	}
	return Package{}, false
}

func (s installScreen) selectedVersionFor(pkg Package) string {
	return s.selectedVersions[packageSourceKey(pkg.ID, pkg.Source)]
}

func (s *installScreen) setSelectedVersion(id, source, version string) {
	key := packageSourceKey(id, source)
	if strings.TrimSpace(version) == "" {
		delete(s.selectedVersions, key)
		return
	}
	s.selectedVersions[key] = version
}

func (s installScreen) renderResultsBody(height int) string {
	var b strings.Builder
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
		if version := s.selectedVersionFor(pkg); version != "" {
			label += fmt.Sprintf("  → %s", version)
		}
		fmt.Fprintf(&b, "  %s%s\n", cursor, style.Render(label))
	}
	return b.String()
}

func (s installScreen) confirmBackgroundView(height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Install Package") + "\n\n")
	b.WriteString(s.renderResultsBody(height))
	return b.String()
}

func (s installScreen) installConfirmModal() confirmModal {
	pkg := s.packages[s.cursor]
	body := []string{
		infoStyle.Render(pkg.Name),
		helpStyle.Render(pkg.ID),
	}
	if version := s.selectedVersionFor(pkg); version != "" {
		body = append(body, "Target version: "+itemActiveStyle.Render(version))
	} else if pkg.Version != "" {
		body = append(body, "Latest available: "+itemActiveStyle.Render(pkg.Version))
	}
	if pkg.Source != "" {
		body = append(body, "Source: "+pkg.Source)
	}
	return confirmModal{
		title:       "Install Package?",
		body:        body,
		confirmVerb: "install",
	}
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
		b.WriteString(s.renderResultsBody(height))

	case installConfirm:
		return renderConfirmModal(
			s.confirmBackgroundView(height),
			width,
			height,
			s.installConfirmModal(),
		)

	case installExecuting:
		if pkg, ok := s.currentPackage(); ok {
			if version := s.selectedVersionFor(pkg); version != "" {
				fmt.Fprintf(&b, "  %s Installing %s (%s) -> %s...\n\n",
					s.spinner.View(), pkg.Name, pkg.ID, version)
			} else {
				fmt.Fprintf(&b, "  %s Installing %s (%s)...\n\n",
					s.spinner.View(), pkg.Name, pkg.ID)
			}
		} else {
			fmt.Fprintf(&b, "  %s Installing...\n\n", s.spinner.View())
		}
		b.WriteString("  " + s.progress.view() + "\n")
		b.WriteString(s.exec.view(width, height) + "\n")

	case installDone:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
			if requiresElevation(s.err, s.output) && !appSettings.AutoElevate {
				b.WriteString("  " + warnStyle.Render("This action requires administrator privileges.") + "\n")
			}
			if pkg, ok := s.currentPackage(); ok {
				meta := []string{fmt.Sprintf("%s  (%s)", pkg.Name, pkg.ID)}
				if version := s.selectedVersionFor(pkg); version != "" {
					meta = append(meta, "target "+version)
				}
				b.WriteString("  " + helpStyle.Render(strings.Join(meta, "  ")) + "\n")
			}
		} else if s.output != "" && len(s.packages) == 0 {
			b.WriteString("  " + warnStyle.Render(s.output) + "\n")
		} else {
			pkg, ok := s.currentPackage()
			if ok {
				b.WriteString("  " + successStyle.Render(pkg.Name+" installed successfully") + "\n")
				var meta []string
				meta = append(meta, pkg.ID)
				if version := s.selectedVersionFor(pkg); version != "" {
					meta = append(meta, "target "+version)
				} else if pkg.Version != "" {
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
		if logView := s.exec.doneView(width, height, 14); logView != "" {
			b.WriteString("\n" + logView + "\n")
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
	case installSearching:
		return []key.Binding{keyEscCancel}
	case installExecuting:
		return s.exec.helpKeys()
	case installResults:
		return []key.Binding{keyScroll, keyDetails, keyEnter, keyEsc}
	case installConfirm:
		return []key.Binding{keyConfirm, keyCancel}
	case installDone:
		bindings := append([]key.Binding(nil), s.exec.doneHelpKeys()...)
		if s.err != nil && requiresElevation(s.err, s.output) && !appSettings.AutoElevate {
			bindings = append(bindings, keyRetryElevated)
		}
		bindings = append(bindings, keySearchAgain, keyEsc, keyTabs)
		return bindings
	}
	return []key.Binding{keyTabs}
}

func (s installScreen) blocksGlobalShortcuts() bool {
	return s.state == installConfirm || s.detail.visible()
}
