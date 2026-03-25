package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// start begins the import flow by scanning for export files.
func (m importModel) start(installed []Package) (importModel, tea.Cmd) {
	m.active = true
	m.state = importScanning
	m.err = nil
	m.statusMsg = ""
	m.packages = nil
	m.files = nil
	m.selected = make(map[int]bool)
	m.batchTotal = 0
	m.progress, _ = m.progress.start()
	return m, tea.Batch(m.spinner.Tick, tickProgress(), scanExportFiles(installed))
}

// ── File scanning & loading ────────────────────────────────────────

func scanExportFiles(installed []Package) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return importFilesMsg{err: err}
		}
		pattern := filepath.Join(home, "Desktop", "wintui_packages_*.json")
		files, err := filepath.Glob(pattern)
		if err != nil {
			return importFilesMsg{err: err}
		}
		// Sort by modification time, newest first
		sort.Slice(files, func(i, j int) bool {
			fi, _ := os.Stat(files[i])
			fj, _ := os.Stat(files[j])
			if fi == nil || fj == nil {
				return files[i] > files[j]
			}
			return fi.ModTime().After(fj.ModTime())
		})
		if len(files) == 0 {
			return importFilesMsg{err: fmt.Errorf("no wintui_packages_*.json found on Desktop — export first with e")}
		}
		// Single file: auto-load it
		if len(files) == 1 {
			pkgs, loadErr := loadImportFile(files[0], installed)
			if loadErr != nil {
				return importLoadedMsg{err: loadErr}
			}
			return importLoadedMsg{packages: pkgs}
		}
		return importFilesMsg{files: files}
	}
}

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
	case tea.KeyMsg:
		switch m.state {
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
				m.progress, _ = m.progress.start()
				path := m.files[m.fileCursor]
				return m, tea.Batch(m.spinner.Tick, tickProgress(), func() tea.Msg {
					pkgs, err := loadImportFile(path, installed)
					if err != nil {
						return importLoadedMsg{err: err}
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
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.packages)-1 {
					m.cursor++
				}
			case " ", "x":
				if m.cursor < len(m.packages) {
					pkg := m.packages[m.cursor]
					if !pkg.Installed && !pkg.NonCanonical {
						m.selected[m.cursor] = !m.selected[m.cursor]
					}
				}
				if m.cursor < len(m.packages)-1 {
					m.cursor++
				}
			case "a":
				allSelected := true
				for i, pkg := range m.packages {
					if !pkg.Installed && !pkg.NonCanonical && !m.selected[i] {
						allSelected = false
						break
					}
				}
				m.selected = make(map[int]bool)
				if !allSelected {
					for i, pkg := range m.packages {
						if !pkg.Installed && !pkg.NonCanonical {
							m.selected[i] = true
						}
					}
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
		m.state = importReview
		return m, nil, true

	case singleImportInstallDoneMsg:
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
		out, err := installPackageSourceCtx(ctx, id, source)
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
		installable, installed, nonCanonical := 0, 0, 0
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

		b.WriteString(fmt.Sprintf("  %s\n",
			infoStyle.Render(fmt.Sprintf("%d package(s) in file", len(m.packages)))))
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
		b.WriteString("\n")

		maxVisible := height - 12
		if maxVisible < 5 {
			maxVisible = 5
		}
		start, end := scrollWindow(m.cursor, len(m.packages), maxVisible)
		for i := start; i < end; i++ {
			pkg := m.packages[i]
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
				status = checkbox(m.selected[i])
			}
			label := fmt.Sprintf("%s  (%s)  %s", pkg.Name, pkg.ID, pkg.Version)
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
			successCount, failCount := 0, 0
			for _, err := range m.batchErrs {
				if err == nil {
					successCount++
				} else {
					failCount++
				}
			}
			if failCount == 0 {
				b.WriteString(fmt.Sprintf("  %s\n\n",
					successStyle.Render(fmt.Sprintf("All %d package(s) installed successfully!", successCount))))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n\n",
					warnStyle.Render(fmt.Sprintf("Completed: %d succeeded, %d failed", successCount, failCount))))
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
		b.WriteString("\n  " + helpStyle.Render("Press enter or esc to return") + "\n")
	}

	return b.String()
}

func (m importModel) helpKeys() []key.Binding {
	switch m.state {
	case importScanning:
		return []key.Binding{keyEscCancel}
	case importFileSelect:
		return []key.Binding{keyUp, keyDown, keyEnter, keyEsc}
	case importReview:
		return []key.Binding{keyUp, keyDown, keyToggle, keyToggleAll, keyEnter, keyEsc}
	case importConfirm:
		return []key.Binding{keyConfirmY}
	case importInstalling:
		return []key.Binding{keyEscCancel}
	case importDone:
		return []key.Binding{keyEnter, keyEsc}
	}
	return []key.Binding{keyEsc}
}
