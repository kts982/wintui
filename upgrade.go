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

type upgradeState int

const (
	upgradeLoading upgradeState = iota
	upgradeEmpty
	upgradeChooseAction
	upgradeSelecting
	upgradeConfirming
	upgradeExecuting
	upgradeDone
)

// singleUpgradeDoneMsg reports completion of a single package upgrade during a batch.
type singleUpgradeDoneMsg struct {
	index  int
	output string
	err    error
}

type upgradeScreen struct {
	state        upgradeState
	packages     []Package
	selected     map[int]bool
	cursor       int
	actionCursor int
	action       string // "all" or "selected"
	spinner      spinner.Model
	progress     progressBar
	output       string
	err          error
	detail       detailPanel
	filter       listFilter
	cancel       context.CancelFunc
	ctx          context.Context
	batchCurrent int
	batchTotal   int
	batchName    string
	batchIDs     []string
	batchSources []string
	batchOutputs []string
	batchErrs    []error
	batchErr     error
	launchRetry  *retryRequest
	retryAction  *retryRequest
}

var upgradeActions = []string{"Upgrade All", "Select Packages"}

func newUpgradeScreen() upgradeScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return upgradeScreen{
		state:    upgradeLoading,
		selected: make(map[int]bool),
		spinner:  sp,
		progress: newProgressBar(50),
		detail:   newDetailPanel(),
		filter:   newListFilter(),
	}
}

func newUpgradeScreenWithRetry(req retryRequest) upgradeScreen {
	s := newUpgradeScreen()
	s.state = upgradeExecuting
	s.batchIDs = []string{req.ID}
	s.batchSources = []string{req.Source}
	s.batchTotal = 1
	s.batchName = req.ID
	s.launchRetry = &req
	return s
}

func (s upgradeScreen) init() tea.Cmd {
	if s.launchRetry != nil {
		req := *s.launchRetry
		return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
			out, err := upgradePackageSourceCtx(context.Background(), req.ID, req.Source)
			return singleUpgradeDoneMsg{output: out, err: err, index: 0}
		})
	}
	// Check cache first
	if pkgs, ok := cache.getUpgradeable(); ok {
		return func() tea.Msg {
			return packagesLoadedMsg{packages: pkgs}
		}
	}
	return tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
		pkgs, err := getUpgradeable()
		if err == nil {
			cache.setUpgradeable(pkgs)
		}
		return packagesLoadedMsg{packages: pkgs, err: err}
	})
}

func (s upgradeScreen) update(msg tea.Msg) (screen, tea.Cmd) {
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
			s.state = upgradeEmpty
			s.err = fmt.Errorf("cancelled")
			s.progress = s.progress.stop()
			return s, nil
		}
		// Refresh with r
		if msg.String() == "r" && (s.state == upgradeChooseAction || s.state == upgradeEmpty || s.state == upgradeDone) {
			cache.invalidate()
			s.retryAction = nil
			s.state = upgradeLoading
			s.progress, _ = s.progress.start()
			return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
				pkgs, err := getUpgradeable()
				if err == nil {
					cache.setUpgradeable(pkgs)
				}
				return packagesLoadedMsg{packages: pkgs, err: err}
			})
		}
		// Show details
		if (msg.String() == "i" || msg.String() == "d") && (s.state == upgradeSelecting || s.state == upgradeChooseAction) {
			if len(s.packages) > 0 {
				idx := s.cursor
				if s.state == upgradeChooseAction {
					idx = 0
				}
				var cmd tea.Cmd
				pkg := s.packages[idx]
				s.detail, cmd = s.detail.show(pkg.ID, pkg.Source)
				return s, cmd
			}
		}
		switch s.state {
		case upgradeChooseAction:
			return s.updateAction(msg)
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
			case upgradeChooseAction:
				row := contentY - 1
				if row >= 0 && row < len(upgradeActions) {
					s.actionCursor = row
					return s.selectAction()
				}
			case upgradeSelecting:
				if contentY >= 1 && contentY-1 < len(s.packages) {
					s.cursor = contentY - 1
					s.selected[s.cursor] = !s.selected[s.cursor]
				}
			}
		}

	case packagesLoadedMsg:
		s.progress = s.progress.stop()
		s.retryAction = nil
		s.packages = msg.packages
		s.selected = make(map[int]bool)
		s.err = msg.err
		if msg.err != nil || len(msg.packages) == 0 {
			s.state = upgradeEmpty
		} else {
			s.state = upgradeChooseAction
		}
		return s, nil

	case singleUpgradeDoneMsg:
		s.batchOutputs = append(s.batchOutputs, msg.output)
		s.batchErrs = append(s.batchErrs, msg.err) // nil for success
		if msg.err != nil {
			s.batchErr = msg.err
		}
		s.batchCurrent = msg.index + 1
		if s.batchTotal > 0 {
			s.progress.percent = float64(s.batchCurrent) / float64(s.batchTotal)
		}

		if s.batchCurrent < s.batchTotal {
			s.batchName = s.batchIDs[s.batchCurrent]
			return s, upgradeSingleCmd(s.ctx, s.batchIDs[s.batchCurrent], s.batchSources[s.batchCurrent], s.batchCurrent)
		}

		s.progress = s.progress.stop()
		s.output = formatBatchResults(s.batchIDs, s.batchErrs, s.batchOutputs)
		s.err = s.batchErr
		s.state = upgradeDone
		s.retryAction = nil
		if s.batchTotal == 1 && s.batchErr != nil && requiresElevation(s.batchErr, s.output) && !isElevated() {
			source := ""
			if len(s.batchSources) > 0 {
				source = s.batchSources[0]
			}
			s.retryAction = &retryRequest{Op: retryOpUpgrade, ID: s.batchIDs[0], Source: source}
		}
		cache.invalidate() // packages changed
		return s, nil

	case commandDoneMsg:
		s.progress = s.progress.stop()
		s.output = msg.output
		s.err = msg.err
		s.state = upgradeDone
		cache.invalidate() // packages changed
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

func (s upgradeScreen) selectAction() (screen, tea.Cmd) {
	switch s.actionCursor {
	case 0: // Upgrade All
		s.action = "all"
		s.state = upgradeConfirming
	case 1: // Select Packages
		s.state = upgradeSelecting
		s.cursor = 0
	}
	return s, nil
}

func (s upgradeScreen) updateAction(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if s.actionCursor > 0 {
			s.actionCursor--
		}
	case "down", "j":
		if s.actionCursor < len(upgradeActions)-1 {
			s.actionCursor++
		}
	case "enter":
		return s.selectAction()
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
		s.selected[s.cursor] = !s.selected[s.cursor]
		if s.cursor < len(filtered)-1 {
			s.cursor++
		}
	case "a":
		allSelected := len(s.selected) == len(s.packages)
		s.selected = make(map[int]bool)
		if !allSelected {
			for i := range s.packages {
				s.selected[i] = true
			}
		}
	case "enter":
		count := 0
		for _, v := range s.selected {
			if v {
				count++
			}
		}
		if count > 0 {
			s.action = "selected"
			s.state = upgradeConfirming
		}
	case "esc":
		s.state = upgradeChooseAction
	}
	return s, nil
}

func (s upgradeScreen) updateConfirm(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		s.state = upgradeExecuting
		s.retryAction = nil
		s.progress, _ = s.progress.start()
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel

		// Build package list: all packages or selected ones
		var ids []string
		var sources []string
		if s.action == "all" {
			for _, p := range s.packages {
				ids = append(ids, p.ID)
				sources = append(sources, p.Source)
			}
		} else {
			for i, sel := range s.selected {
				if sel && i < len(s.packages) {
					ids = append(ids, s.packages[i].ID)
					sources = append(sources, s.packages[i].Source)
				}
			}
		}
		s.ctx = ctx
		s.batchIDs = ids
		s.batchSources = sources
		s.batchTotal = len(ids)
		s.batchCurrent = 0
		s.batchOutputs = nil
		s.batchErrs = nil
		s.batchErr = nil
		s.progress.active = false // don't use indeterminate animation for batch
		s.progress.percent = 0

		if s.batchTotal > 0 {
			s.batchName = s.batchIDs[0]
			return s, tea.Batch(s.spinner.Tick, upgradeSingleCmd(s.ctx, s.batchIDs[0], s.batchSources[0], 0))
		}
		return s, nil

	case "n", "N", "esc":
		if s.action == "all" {
			s.state = upgradeChooseAction
		} else {
			s.state = upgradeSelecting
		}
	}
	return s, nil
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

// upgradeSingleCmd upgrades a single package and sends a completion message.
func upgradeSingleCmd(ctx context.Context, id, source string, index int) tea.Cmd {
	return func() tea.Msg {
		out, err := upgradePackageSourceCtx(ctx, id, source)
		return singleUpgradeDoneMsg{output: out, err: err, index: index}
	}
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

	case upgradeChooseAction:
		b.WriteString(fmt.Sprintf("  %s\n\n",
			infoStyle.Render(fmt.Sprintf("%d package(s) with updates available.", len(s.packages)))))
		for i, action := range upgradeActions {
			cursor := cursorBlankStr
			style := itemStyle
			if i == s.actionCursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			fmt.Fprintf(&b, "  %s%s\n", cursor, style.Render(action))
		}

	case upgradeSelecting:
		b.WriteString("  Select packages to upgrade:\n")
		filterView := s.filter.view()
		if filterView != "" {
			b.WriteString(filterView + "\n")
		}
		b.WriteString("\n")
		filtered := s.filter.filterPackages(s.packages)
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
			// Find original index for selection state
			origIdx := -1
			for j, p := range s.packages {
				if p.ID == pkg.ID {
					origIdx = j
					break
				}
			}
			check := checkbox(origIdx >= 0 && s.selected[origIdx])
			label := fmt.Sprintf("%s  (%s)  %s → %s", pkg.Name, pkg.ID, pkg.Version, pkg.Available)
			fmt.Fprintf(&b, "  %s%s %s\n", cursor, check, style.Render(label))
		}
		selected := 0
		for _, v := range s.selected {
			if v {
				selected++
			}
		}
		fmt.Fprintf(&b, "\n  %s\n", infoStyle.Render(fmt.Sprintf("%d selected", selected)))

	case upgradeConfirming:
		if s.action == "all" {
			fmt.Fprintf(&b, "  Upgrade all %d package(s)?\n\n", len(s.packages))
		} else {
			count := 0
			for _, v := range s.selected {
				if v {
					count++
				}
			}
			fmt.Fprintf(&b, "  Upgrade %d selected package(s)?\n\n", count)
		}
		b.WriteString("  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case upgradeExecuting:
		if s.batchTotal > 0 {
			b.WriteString(fmt.Sprintf("  %s Upgrading %d of %d: %s\n\n",
				s.spinner.View(), s.batchCurrent+1, s.batchTotal, s.batchName))
		} else {
			fmt.Fprintf(&b, "  %s Upgrading packages...\n\n", s.spinner.View())
		}
		b.WriteString("  " + s.progress.view() + "\n")

	case upgradeDone:
		if s.err != nil {
			if requiresElevation(s.err, s.output) || batchRequiresElevation(s.batchErrs, s.batchOutputs) {
				b.WriteString("  " + warnStyle.Render("Completed with errors") + "\n")
				b.WriteString("  " + helpStyle.Render("Some packages require administrator privileges.") + "\n")
				b.WriteString("  " + helpStyle.Render(elevationRetryHint()) + "\n\n")
			} else {
				b.WriteString("  " + warnStyle.Render("Completed with errors") + "\n\n")
			}
		} else {
			b.WriteString("  " + successStyle.Render("Upgrade complete!") + "\n\n")
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
	}

	return b.String()
}

func (s upgradeScreen) helpKeys() []key.Binding {
	switch s.state {
	case upgradeLoading, upgradeExecuting:
		return []key.Binding{keyEscCancel}
	case upgradeEmpty, upgradeDone:
		if s.retryAction != nil && !isElevated() {
			return []key.Binding{keyRetryElevated, keyRefresh, keyTabs}
		}
		return []key.Binding{keyRefresh, keyTabs}
	case upgradeChooseAction:
		return []key.Binding{keyUp, keyDown, keyEnter, keyRefresh}
	case upgradeSelecting:
		if s.filter.active {
			return []key.Binding{keyUp, keyDown, keyToggle, keyEnter, keyEsc}
		}
		return []key.Binding{keyUp, keyDown, keyFilter, keyToggle, keyToggleAll, keyDetails, keyEnter, keyEsc}
	case upgradeConfirming:
		return []key.Binding{keyConfirmY}
	}
	return []key.Binding{keyTabs}
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
