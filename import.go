package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── Import flow state machine ──────────────────────────────────────

type importState int

const (
	importScanning   importState = iota // scanning Desktop for export files
	importFileSelect                    // multiple files found, pick one
	importReview                        // review packages with selection
	importConfirm                       // confirm before installing
	importInstalling                    // batch installing
	importDone                          // results
)

// importPkg holds a package from the export JSON annotated with
// install-readiness metadata.
type importPkg struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Source  string `json:"source"`

	Installed    bool `json:"-"`
	NonCanonical bool `json:"-"`
}

// ── Messages ───────────────────────────────────────────────────────

type importFilesMsg struct {
	files []string
	err   error
}

type importLoadedMsg struct {
	packages []importPkg
	err      error
}

type singleImportInstallDoneMsg struct {
	index  int
	output string
	err    error
}

// ── Import model ───────────────────────────────────────────────────

type importModel struct {
	active     bool
	state      importState
	files      []string
	fileCursor int
	packages   []importPkg
	selected   map[int]bool
	cursor     int
	showAll    bool
	spinner    spinner.Model
	progress   progressBar
	err        error

	// Batch install
	ctx          context.Context
	cancel       context.CancelFunc
	batchCurrent int
	batchTotal   int
	batchName    string
	batchIDs     []string
	batchSources []string
	batchOutputs []string
	batchErrs    []error
	batchErr     error

	statusMsg string
}

func newImportModel() importModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return importModel{
		selected: make(map[int]bool),
		spinner:  sp,
		progress: newProgressBar(50),
	}
}

// ── File scanning & loading ────────────────────────────────────────

func loadImportFile(path string, installed []Package) ([]importPkg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkgs []importPkg
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	for i := range pkgs {
		pkgs[i].NonCanonical = isNonCanonical(pkgs[i].ID)
		pkgs[i].Installed = importPackageInstalled(pkgs[i], installed)
	}
	return pkgs, nil
}

func importPackageInstalled(pkg importPkg, installed []Package) bool {
	canonicalPkg := Package{
		Name:    pkg.Name,
		ID:      pkg.ID,
		Version: pkg.Version,
		Source:  resolveImportSource(pkg),
	}
	for _, existing := range installed {
		if strings.EqualFold(existing.ID, pkg.ID) {
			return true
		}
		if !strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(pkg.Name)) {
			continue
		}
		if isNonCanonical(existing.ID) && shouldHideNonCanonicalDuplicate(existing, canonicalPkg) {
			return true
		}
	}
	return false
}

func resolveImportSource(pkg importPkg) string {
	if pkg.Source == "winget" || pkg.Source == "msstore" {
		return pkg.Source
	}
	if isNonCanonical(pkg.ID) {
		return ""
	}
	if looksLikeStoreProductID(pkg.ID) {
		return "msstore"
	}
	if strings.Contains(pkg.ID, ".") {
		return "winget"
	}
	return ""
}

func importSourceLabel(pkg importPkg) string {
	if pkg.NonCanonical {
		return ""
	}
	switch resolveImportSource(pkg) {
	case "winget":
		return "winget"
	case "msstore":
		return "msstore"
	default:
		return "default"
	}
}

func looksLikeStoreProductID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) < 12 || len(id) > 16 {
		return false
	}
	for _, r := range id {
		if !unicode.IsDigit(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// ── Update ─────────────────────────────────────────────────────────

func (m importModel) update(msg tea.Msg, installed []Package) (importModel, tea.Cmd, bool) {
	if !m.active {
		return m, nil, false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch m.state {
		case importScanning:
			if msg.String() == "esc" {
				if m.cancel != nil {
					m.cancel()
				}
				m.active = false
				return m, nil, true
			}
			return m, nil, true

		case importFileSelect:
			switch msg.String() {
			case "up", "k":
				if m.fileCursor > 0 {
					m.fileCursor--
				}
			case "down", "j":
				if m.fileCursor < len(m.files)-1 {
					m.fileCursor++
				}
			case "enter":
				m.state = importScanning
				m.ctx, m.cancel = context.WithCancel(context.Background())
				m.progress, _ = m.progress.start()
				path := m.files[m.fileCursor]
				return m, tea.Batch(m.spinner.Tick, tickProgress(), func() tea.Msg {
					if m.ctx.Err() != nil {
						return importLoadedMsg{err: fmt.Errorf("cancelled")}
					}
					pkgs, err := loadImportFile(path, installed)
					if err != nil {
						return importLoadedMsg{err: err}
					}
					if m.ctx.Err() != nil {
						return importLoadedMsg{err: fmt.Errorf("cancelled")}
					}
					return importLoadedMsg{packages: pkgs}
				}), true
			case "esc":
				m.active = false
				return m, nil, true
			}
			return m, nil, true

		case importReview:
			switch msg.String() {
			case "up", "k":
				m.moveCursor(-1)
			case "down", "j":
				m.moveCursor(1)
			case "space", "x":
				m.toggleCurrentSelection()
			case "a":
				m.toggleAllSelectable()
			case "v":
				if m.skippedCount() > 0 {
					m.showAll = !m.showAll
					m.clampCursor()
				}
			case "enter":
				if m.selectedCount() > 0 {
					m.state = importConfirm
				}
			case "esc":
				m.active = false
				return m, nil, true
			}
			return m, nil, true

		case importConfirm:
			switch msg.String() {
			case "y", "Y":
				m.state = importInstalling
				m.progress, _ = m.progress.start()
				ctx, cancel := context.WithCancel(context.Background())
				m.cancel = cancel
				m.ctx = ctx

				var ids, sources []string
				for i, sel := range m.selected {
					if sel && i < len(m.packages) {
						ids = append(ids, m.packages[i].ID)
						sources = append(sources, resolveImportSource(m.packages[i]))
					}
				}
				m.batchIDs = ids
				m.batchSources = sources
				m.batchTotal = len(ids)
				m.batchCurrent = 0
				m.batchOutputs = nil
				m.batchErrs = nil
				m.batchErr = nil
				m.progress.active = false
				m.progress.percent = 0

				if m.batchTotal > 0 {
					m.batchName = m.batchIDs[0]
					return m, tea.Batch(m.spinner.Tick,
						importInstallSingleCmd(ctx, ids[0], sources[0], 0)), true
				}
			case "n", "N", "esc":
				m.state = importReview
			}
			return m, nil, true

		case importInstalling:
			if msg.String() == "esc" {
				if m.cancel != nil {
					m.cancel()
				}
				m.state = importDone
				m.statusMsg = warnStyle.Render("Cancelled")
				m.progress = m.progress.stop()
			}
			return m, nil, true

		case importDone:
			if msg.String() == "enter" || msg.String() == "esc" {
				m.active = false
				return m, nil, true
			}
			return m, nil, true
		}
		// Absorb all key events while import is active
		return m, nil, true

	case importFilesMsg:
		if m.state != importScanning {
			return m, nil, true
		}
		m.progress = m.progress.stop()
		if msg.err != nil {
			m.err = msg.err
			m.state = importDone
			return m, nil, true
		}
		m.files = msg.files
		m.fileCursor = 0
		m.state = importFileSelect
		return m, nil, true

	case importLoadedMsg:
		if m.state != importScanning {
			return m, nil, true
		}
		m.progress = m.progress.stop()
		if msg.err != nil {
			m.err = msg.err
			m.state = importDone
			return m, nil, true
		}
		m.packages = msg.packages
		m.selected = make(map[int]bool)
		for i, pkg := range m.packages {
			if !pkg.Installed && !pkg.NonCanonical {
				m.selected[i] = true
			}
		}
		m.cursor = 0
		m.showAll = false
		m.clampCursor()
		m.state = importReview
		return m, nil, true

	case singleImportInstallDoneMsg:
		if m.state != importInstalling {
			return m, nil, true
		}
		m.batchOutputs = append(m.batchOutputs, msg.output)
		m.batchErrs = append(m.batchErrs, msg.err)
		if msg.err != nil {
			m.batchErr = msg.err
		}
		m.batchCurrent = msg.index + 1
		if m.batchTotal > 0 {
			m.progress.percent = float64(m.batchCurrent) / float64(m.batchTotal)
		}
		if m.batchCurrent < m.batchTotal {
			m.batchName = m.batchIDs[m.batchCurrent]
			return m, importInstallSingleCmd(m.ctx,
				m.batchIDs[m.batchCurrent], m.batchSources[m.batchCurrent], m.batchCurrent), true
		}
		m.progress = m.progress.stop()
		m.state = importDone
		cache.invalidate()
		return m, nil, true

	case spinner.TickMsg:
		if m.state == importScanning || m.state == importInstalling {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd, true
		}

	case progressTickMsg:
		if m.state == importScanning || m.state == importInstalling {
			var cmd tea.Cmd
			m.progress, cmd = m.progress.update(msg)
			return m, cmd, true
		}

	case progress.FrameMsg:
		if m.state == importScanning || m.state == importInstalling {
			var cmd tea.Cmd
			m.progress, cmd = m.progress.update(msg)
			return m, cmd, true
		}
	}

	return m, nil, false
}

func importInstallSingleCmd(ctx context.Context, id, source string, index int) tea.Cmd {
	return func() tea.Msg {
		out, err := installPackageSourceCtx(ctx, id, source, "")
		return singleImportInstallDoneMsg{output: out, err: err, index: index}
	}
}

func (m importModel) selectedCount() int {
	count := 0
	for _, v := range m.selected {
		if v {
			count++
		}
	}
	return count
}

func (m importModel) reviewCounts() (installable, installed, nonCanonical int) {
	for _, pkg := range m.packages {
		switch {
		case pkg.Installed:
			installed++
		case pkg.NonCanonical:
			nonCanonical++
		default:
			installable++
		}
	}
	return installable, installed, nonCanonical
}

func (m importModel) skippedCount() int {
	_, installed, nonCanonical := m.reviewCounts()
	return installed + nonCanonical
}

func (m importModel) visiblePackageIndices() []int {
	indices := make([]int, 0, len(m.packages))
	for i, pkg := range m.packages {
		if !m.showAll && (pkg.Installed || pkg.NonCanonical) {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func (m *importModel) clampCursor() {
	visible := m.visiblePackageIndices()
	if len(visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
}

func (m *importModel) moveCursor(delta int) {
	if len(m.visiblePackageIndices()) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	m.clampCursor()
}

func (m importModel) currentVisiblePackageIndex() (int, bool) {
	visible := m.visiblePackageIndices()
	if len(visible) == 0 || m.cursor < 0 || m.cursor >= len(visible) {
		return 0, false
	}
	return visible[m.cursor], true
}

func (m *importModel) toggleCurrentSelection() bool {
	index, ok := m.currentVisiblePackageIndex()
	if !ok {
		return false
	}
	pkg := m.packages[index]
	if pkg.Installed || pkg.NonCanonical {
		return false
	}
	m.selected[index] = !m.selected[index]
	return true
}

func (m *importModel) toggleAllSelectable() {
	installable, _, _ := m.reviewCounts()
	if installable == 0 {
		return
	}

	allSelected := true
	for i, pkg := range m.packages {
		if pkg.Installed || pkg.NonCanonical {
			continue
		}
		if !m.selected[i] {
			allSelected = false
			break
		}
	}

	if allSelected {
		m.selected = make(map[int]bool)
		return
	}

	selected := make(map[int]bool, installable)
	for i, pkg := range m.packages {
		if !pkg.Installed && !pkg.NonCanonical {
			selected[i] = true
		}
	}
	m.selected = selected
}

// ── View ───────────────────────────────────────────────────────────

func (m importModel) view(width, height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Import Packages") + "\n\n")

	switch m.state {
	case importScanning:
		fmt.Fprintf(&b, "  %s Scanning for export files...\n\n", m.spinner.View())
		b.WriteString("  " + m.progress.view() + "\n")

	case importFileSelect:
		b.WriteString("  " + infoStyle.Render("Select an export file:") + "\n\n")
		for i, f := range m.files {
			cursor := cursorBlankStr
			style := itemStyle
			if i == m.fileCursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			name := filepath.Base(f)
			fmt.Fprintf(&b, "  %s%s\n", cursor, style.Render(name))
		}

	case importReview:
		installable, installed, nonCanonical := m.reviewCounts()
		skipped := installed + nonCanonical
		visible := m.visiblePackageIndices()

		b.WriteString(fmt.Sprintf("  %s\n",
			infoStyle.Render(fmt.Sprintf("%d package(s) in file", len(m.packages)))))
		if installable > 0 && !m.showAll {
			b.WriteString(fmt.Sprintf("  %s\n",
				helpStyle.Render(fmt.Sprintf("Showing %d actionable package(s)", installable))))
		}
		if m.showAll && len(m.packages) > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				helpStyle.Render(fmt.Sprintf("Showing all %d package(s)", len(m.packages)))))
		}
		if installed > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				helpStyle.Render(fmt.Sprintf("%d already installed (skipped)", installed))))
		}
		if nonCanonical > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d non-restorable raw identity (flagged)", nonCanonical))))
		}
		selCount := m.selectedCount()
		if selCount > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				successStyle.Render(fmt.Sprintf("%d selected for install — press enter to proceed", selCount))))
		}
		if skipped > 0 {
			hint := "Press v to show skipped entries"
			if m.showAll {
				hint = "Press v to focus installable packages"
			}
			b.WriteString(fmt.Sprintf("  %s\n", helpStyle.Render(hint)))
		}
		b.WriteString("\n")

		if len(visible) == 0 {
			switch {
			case len(m.packages) == 0:
				b.WriteString("  " + warnStyle.Render("No packages found in this export.") + "\n")
			case !m.showAll && skipped > 0:
				b.WriteString("  " + warnStyle.Render("Nothing to install from this file.") + "\n")
				b.WriteString("  " + helpStyle.Render("All entries are already installed or non-restorable.") + "\n")
			default:
				b.WriteString("  " + warnStyle.Render("No packages to show.") + "\n")
			}
			break
		}

		maxVisible := height - 12
		if maxVisible < 5 {
			maxVisible = 5
		}
		start, end := scrollWindow(m.cursor, len(visible), maxVisible)
		for i := start; i < end; i++ {
			index := visible[i]
			pkg := m.packages[index]
			cursor := cursorBlankStr
			style := itemStyle
			if i == m.cursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			var status string
			switch {
			case pkg.Installed:
				status = helpStyle.Render("[installed]")
			case pkg.NonCanonical:
				status = warnStyle.Render("[raw]      ")
			default:
				status = checkbox(m.selected[index])
			}
			label := fmt.Sprintf("%s  (%s)  %s", pkg.Name, pkg.ID, pkg.Version)
			if source := importSourceLabel(pkg); source != "" {
				label += fmt.Sprintf("  [%s]", source)
			}
			fmt.Fprintf(&b, "  %s%s %s\n", cursor, status, style.Render(label))
		}

	case importConfirm:
		count := m.selectedCount()
		fmt.Fprintf(&b, "  Install %d package(s)?\n\n", count)
		b.WriteString("  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case importInstalling:
		if m.batchTotal > 0 {
			b.WriteString(fmt.Sprintf("  %s Installing %d of %d: %s\n\n",
				m.spinner.View(), m.batchCurrent+1, m.batchTotal, m.batchName))
		} else {
			fmt.Fprintf(&b, "  %s Installing...\n\n", m.spinner.View())
		}
		b.WriteString("  " + m.progress.view() + "\n")

	case importDone:
		if m.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+m.err.Error()) + "\n")
		} else if m.statusMsg != "" {
			b.WriteString("  " + m.statusMsg + "\n")
		} else if m.batchTotal > 0 {
			successCount, failCount := batchResultCounts(m.batchErrs)
			if failCount == 0 {
				b.WriteString(fmt.Sprintf("  %s\n\n",
					successStyle.Render(fmt.Sprintf("%d package(s) installed from this export.", successCount))))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n\n",
					warnStyle.Render(fmt.Sprintf("Import finished: %d succeeded, %d failed", successCount, failCount))))
			}
			output := formatBatchResults(m.batchIDs, m.batchErrs, m.batchOutputs)
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
		} else {
			b.WriteString("  " + successStyle.Render("Import complete!") + "\n")
		}
		b.WriteString("\n  " + helpStyle.Render("Press enter or esc to return to Installed packages") + "\n")
	}

	return b.String()
}
