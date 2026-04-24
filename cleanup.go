package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

type cleanupState int

const (
	cleanupLoading   cleanupState = iota
	cleanupReady                  // targets scanned, awaiting action
	cleanupConfirm                // confirm deletion
	cleanupExecuting              // deleting
	cleanupDone                   // results shown
	cleanupEmpty                  // nothing to clean
)

type cleanupScreen struct {
	state      cleanupState
	targets    []cleanupTarget
	selected   map[int]bool // selected targets for partial cleanup
	cursor     int
	spinner    spinner.Model
	progress   progressBar
	err        error
	deleted    int
	failed     int
	totalBytes int64
	freedBytes int64
	width      int
	height     int
	ctx        context.Context
	cancel     context.CancelFunc
	cancelled  bool
}

type cleanupTarget struct {
	path  string
	bytes int64
}

func newCleanupScreen() cleanupScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	return cleanupScreen{
		state:    cleanupLoading,
		selected: make(map[int]bool),
		spinner:  sp,
		progress: newProgressBar(50),
		ctx:      ctx,
		cancel:   cancel,
		width:    80,
		height:   24,
	}
}

func (s cleanupScreen) init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, tickProgress(), scanTempFilesCmd(s.ctx))
}

func scanTempFilesCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		return scanTempFiles(ctx)
	}
}

func scanTempFiles(ctx context.Context) tea.Msg {
	if ctx.Err() != nil {
		return filesScannedMsg{err: fmt.Errorf("cancelled")}
	}
	tmpDir := os.TempDir()
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	var old []cleanupTarget

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return filesScannedMsg{err: err}
	}

	for _, e := range entries {
		if ctx.Err() != nil {
			return filesScannedMsg{err: fmt.Errorf("cancelled")}
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(tmpDir, e.Name())
			old = append(old, cleanupTarget{path: path, bytes: cleanupTargetSize(path)})
		}
	}
	return filesScannedMsg{targets: old}
}

func cleanupTargetSize(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func (s cleanupScreen) reload() (cleanupScreen, tea.Cmd) {
	s.state = cleanupLoading
	s.err = nil
	s.deleted = 0
	s.failed = 0
	s.totalBytes = 0
	s.freedBytes = 0
	s.cancelled = false
	s.selected = make(map[int]bool)
	s.cursor = 0
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.progress, _ = s.progress.start()
	return s, tea.Batch(s.spinner.Tick, tickProgress(), scanTempFilesCmd(s.ctx))
}

func (s cleanupScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case tea.KeyPressMsg:
		if msg.String() == "r" && (s.state == cleanupReady || s.state == cleanupEmpty || s.state == cleanupDone) {
			return s.reload()
		}

		if msg.String() == "esc" && (s.state == cleanupLoading || s.state == cleanupExecuting) {
			if s.cancel != nil {
				s.cancel()
			}
			if s.state == cleanupLoading {
				s.progress = s.progress.stop()
			}
			return s, nil
		}

		switch s.state {
		case cleanupReady:
			switch msg.String() {
			case "up", "k":
				if s.cursor > 0 {
					s.cursor--
				}
			case "down", "j":
				if s.cursor < len(s.targets)-1 {
					s.cursor++
				}
			case "space":
				s.selected[s.cursor] = !s.selected[s.cursor]
				if !s.selected[s.cursor] {
					delete(s.selected, s.cursor)
				}
			case "a":
				if len(s.selected) == len(s.targets) {
					s.selected = make(map[int]bool)
				} else {
					for i := range s.targets {
						s.selected[i] = true
					}
				}
			case "enter":
				s.state = cleanupConfirm
			case "esc":
				if len(s.selected) > 0 {
					s.selected = make(map[int]bool)
				} else if s.cursor > 0 {
					s.cursor = 0
				}
			}

		case cleanupConfirm:
			switch msg.String() {
			case "enter":
				s.state = cleanupExecuting
				s.cancelled = false
				s.ctx, s.cancel = context.WithCancel(context.Background())
				s.progress, _ = s.progress.start()
				// Delete selected or all.
				targets := s.cleanupTargets()
				ctx := s.ctx
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					deleted, failed := 0, 0
					var freedBytes int64
					for _, target := range targets {
						if ctx.Err() != nil {
							return cleanupDoneMsg{deleted: deleted, failed: failed, freedBytes: freedBytes, cancelled: true}
						}
						if err := os.RemoveAll(target.path); err != nil {
							failed++
						} else {
							deleted++
							freedBytes += target.bytes
						}
					}
					return cleanupDoneMsg{deleted: deleted, failed: failed, freedBytes: freedBytes}
				})
			case "esc":
				s.state = cleanupReady
			}

		case cleanupDone, cleanupEmpty:
		}

	case filesScannedMsg:
		if s.state != cleanupLoading {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.targets = msg.targets
		s.err = msg.err
		s.totalBytes = 0
		for _, target := range msg.targets {
			s.totalBytes += target.bytes
		}
		s.cancelled = false
		if msg.err != nil || len(msg.targets) == 0 {
			s.state = cleanupEmpty
		} else {
			s.state = cleanupReady
		}
		return s, nil

	case cleanupDoneMsg:
		if s.state != cleanupExecuting {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.deleted = msg.deleted
		s.failed = msg.failed
		s.freedBytes = msg.freedBytes
		s.cancelled = msg.cancelled
		s.state = cleanupDone
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

// cleanupTargets returns selected targets, or all if none selected.
func (s cleanupScreen) cleanupTargets() []cleanupTarget {
	if len(s.selected) == 0 {
		return s.targets
	}
	var targets []cleanupTarget
	for i, t := range s.targets {
		if s.selected[i] {
			targets = append(targets, t)
		}
	}
	return targets
}

func (s cleanupScreen) selectedBytes() int64 {
	if len(s.selected) == 0 {
		return s.totalBytes
	}
	var total int64
	for i := range s.selected {
		if i < len(s.targets) {
			total += s.targets[i].bytes
		}
	}
	return total
}

// ── View ──────────────────────────────────────────────────────────

func (s cleanupScreen) view(width, height int) string {
	switch s.state {
	case cleanupLoading:
		return fmt.Sprintf("  %s Scanning temp directory...\n\n  %s\n", s.spinner.View(), s.progress.view())

	case cleanupEmpty:
		if s.err != nil {
			return "  " + errorStyle.Render("Error: "+s.err.Error()) + "\n\n  " +
				helpStyle.Render("Press r to scan again.") + "\n"
		}
		return "  " + successStyle.Render("No old temp files found. All clean!") + "\n\n  " +
			helpStyle.Render("Press r to scan again.") + "\n"

	case cleanupReady:
		return s.viewReady(width, height)

	case cleanupConfirm:
		targets := s.cleanupTargets()
		return s.viewConfirm(width, height, len(targets))

	case cleanupExecuting:
		return fmt.Sprintf("  %s Cleaning up...\n\n  %s\n", s.spinner.View(), s.progress.view())

	case cleanupDone:
		return s.viewDone()
	}
	return ""
}

func (s cleanupScreen) viewReady(width, height int) string {
	panelWidth := width - 4
	availH := max(height-4, 8)

	// Summary line.
	nSelected := len(s.selected)
	title := fmt.Sprintf("Cleanup Targets (%d items, %s)", len(s.targets), formatBytes(s.totalBytes))
	if nSelected > 0 {
		title = fmt.Sprintf("Cleanup Targets (%d items, %s / %d selected, %s)",
			len(s.targets), formatBytes(s.totalBytes), nSelected, formatBytes(s.selectedBytes()))
	}

	innerH := max(availH-2, 4) // minus border
	maxVisible := innerH

	start, end := scrollWindow(s.cursor, len(s.targets), maxVisible)
	visible := s.targets[start:end]

	var lines []string
	for i, target := range visible {
		globalIdx := start + i
		cursor := cursorBlankStr
		if globalIdx == s.cursor {
			cursor = cursorStr
		}
		sel := checkbox(s.selected[globalIdx])
		name := filepath.Base(target.path)
		size := helpStyle.Render(formatBytes(target.bytes))

		nameStyle := itemStyle
		if globalIdx == s.cursor {
			nameStyle = itemActiveStyle
		}
		lines = append(lines, cursor+sel+" "+nameStyle.Render(name)+"  "+size)
	}

	content := strings.Join(lines, "\n")
	panel := renderTitledPanel(title, content, panelWidth, innerH, accent)

	return panel + "\n"
}

func (s cleanupScreen) viewConfirm(width, height, count int) string {
	title := "Confirm Cleanup"
	body := fmt.Sprintf("Delete %d item(s)?\nEstimated space to free: %s", count, formatBytes(s.selectedBytes()))

	var content strings.Builder
	content.WriteString(sectionTitleStyle.Render(title) + "\n")
	content.WriteString(helpStyle.Render(strings.Repeat("─", 40)) + "\n")
	content.WriteString(warnStyle.Render(body) + "\n\n")
	content.WriteString(lipgloss.NewStyle().Bold(true).Foreground(accent).Render("enter") + " delete  •  " +
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render("esc") + " cancel")

	style := lipgloss.NewStyle().
		Width(min(width-8, 60)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)

	rendered := style.Render(content.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, rendered)
}

func (s cleanupScreen) viewDone() string {
	var b strings.Builder
	if s.cancelled {
		b.WriteString("  " + warnStyle.Render(fmt.Sprintf("Cleanup cancelled after deleting %d item(s).", s.deleted)) + "\n")
		b.WriteString("  " + helpStyle.Render("Freed "+formatBytes(s.freedBytes)+" before cancellation.") + "\n")
	} else {
		b.WriteString("  " + successStyle.Render(fmt.Sprintf("Deleted %d item(s).", s.deleted)) + "\n")
		b.WriteString("  " + helpStyle.Render("Freed "+formatBytes(s.freedBytes)+".") + "\n")
	}
	if s.failed > 0 {
		b.WriteString("  " + warnStyle.Render(fmt.Sprintf("%d item(s) could not be removed (in use or locked).", s.failed)) + "\n")
	}
	b.WriteString("\n  " + helpStyle.Render("Press r to scan again.") + "\n")
	return b.String()
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 || value >= 10 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func (s cleanupScreen) helpKeys() []key.Binding {
	switch s.state {
	case cleanupLoading, cleanupExecuting:
		return []key.Binding{keyEscCancel}
	case cleanupEmpty, cleanupDone:
		return []key.Binding{keyRefresh}
	case cleanupReady:
		bindings := []key.Binding{
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "select")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select all")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "clean")),
			keyRefresh,
		}
		return bindings
	case cleanupConfirm:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
			keyEscCancel,
		}
	}
	return nil
}

// cleanupDoneMsg is sent when temp file deletion completes.
type cleanupDoneMsg struct {
	deleted    int
	failed     int
	freedBytes int64
	cancelled  bool
}
