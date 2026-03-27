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
	cleanupLoading cleanupState = iota
	cleanupReady
	cleanupConfirm
	cleanupExecuting
	cleanupDone
	cleanupEmpty
)

type cleanupScreen struct {
	state     cleanupState
	files     []string
	cursor    int
	spinner   spinner.Model
	progress  progressBar
	err       error
	deleted   int
	failed    int
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled bool
}

func newCleanupScreen() cleanupScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	ctx, cancel := context.WithCancel(context.Background())
	return cleanupScreen{
		state:    cleanupLoading,
		spinner:  sp,
		progress: newProgressBar(50),
		ctx:      ctx,
		cancel:   cancel,
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
	var old []string

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
			old = append(old, filepath.Join(tmpDir, e.Name()))
		}
	}
	return filesScannedMsg{files: old}
}

func (s cleanupScreen) reload() (cleanupScreen, tea.Cmd) {
	s.state = cleanupLoading
	s.err = nil
	s.deleted = 0
	s.failed = 0
	s.cancelled = false
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.progress, _ = s.progress.start()
	return s, tea.Batch(s.spinner.Tick, tickProgress(), scanTempFilesCmd(s.ctx))
}

func (s cleanupScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
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
				if s.cursor < len(s.files)-1 {
					s.cursor++
				}
			case "enter":
				s.state = cleanupConfirm
			case "esc":
				if s.cursor > 0 {
					s.cursor = 0
				}
			}

		case cleanupConfirm:
			switch msg.String() {
			case "y", "Y":
				s.state = cleanupExecuting
				s.cancelled = false
				s.ctx, s.cancel = context.WithCancel(context.Background())
				s.progress, _ = s.progress.start()
				files := s.files
				ctx := s.ctx
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					deleted, failed := 0, 0
					for _, f := range files {
						if ctx.Err() != nil {
							return cleanupDoneMsg{deleted: deleted, failed: failed, cancelled: true}
						}
						if err := os.RemoveAll(f); err != nil {
							failed++
						} else {
							deleted++
						}
					}
					return cleanupDoneMsg{deleted: deleted, failed: failed}
				})
			case "n", "N", "esc":
				s.state = cleanupReady
			}

		case cleanupDone, cleanupEmpty:
		}

	case filesScannedMsg:
		if s.state != cleanupLoading {
			return s, nil
		}
		s.progress = s.progress.stop()
		s.files = msg.files
		s.err = msg.err
		s.cancelled = false
		if msg.err != nil || len(msg.files) == 0 {
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

func (s cleanupScreen) view(width, height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("Temp File Cleanup") + "\n\n")

	switch s.state {
	case cleanupLoading:
		fmt.Fprintf(&b, "  %s Scanning temp directory...\n\n", s.spinner.View())
		b.WriteString("  " + s.progress.view() + "\n")

	case cleanupEmpty:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else {
			b.WriteString("  " + successStyle.Render("No old temp files found. All clean!") + "\n")
		}
		b.WriteString("\n  " + helpStyle.Render("Press r to scan again or tab to switch screens") + "\n")

	case cleanupReady:
		b.WriteString(fmt.Sprintf("  %s\n",
			warnStyle.Render(fmt.Sprintf("%d item(s) older than 7 days will be removed.", len(s.files)))))
		b.WriteString("  " + helpStyle.Render("Preview of cleanup targets") + "\n\n")

		maxVisible := height - 12
		if maxVisible < 5 {
			maxVisible = 5
		}
		start := s.cursor
		maxStart := len(s.files) - maxVisible
		if maxStart < 0 {
			maxStart = 0
		}
		if start > maxStart {
			start = maxStart
		}
		end := start + maxVisible
		if end > len(s.files) {
			end = len(s.files)
		}
		for i := start; i < end; i++ {
			name := filepath.Base(s.files[i])
			fmt.Fprintf(&b, "  • %s\n", itemStyle.Render(name))
		}
		if len(s.files) > maxVisible {
			b.WriteString(fmt.Sprintf("\n  %s\n",
				helpStyle.Render(fmt.Sprintf("Showing %d-%d of %d (↑↓ to scroll)", start+1, end, len(s.files)))))
		}
		b.WriteString("\n  " + helpStyle.Render(fmt.Sprintf("Press enter to delete all %d item(s).", len(s.files))) + "\n")

	case cleanupConfirm:
		b.WriteString(fmt.Sprintf("  Delete all %d temp item(s)?\n\n",
			len(s.files)))
		b.WriteString("  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case cleanupExecuting:
		fmt.Fprintf(&b, "  %s Cleaning up...\n\n", s.spinner.View())
		b.WriteString("  " + s.progress.view() + "\n")

	case cleanupDone:
		if s.cancelled {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("Cleanup cancelled after deleting %d item(s).", s.deleted))))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n",
				successStyle.Render(fmt.Sprintf("Deleted %d item(s).", s.deleted))))
		}
		if s.failed > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d item(s) could not be removed (in use or locked).", s.failed))))
		}
		b.WriteString("\n  " + helpStyle.Render("Press r to scan again or tab to switch screens") + "\n")
	}

	return b.String()
}

func (s cleanupScreen) helpKeys() []key.Binding {
	switch s.state {
	case cleanupLoading, cleanupExecuting:
		return []key.Binding{keyEscCancel}
	case cleanupEmpty, cleanupDone:
		return []key.Binding{keyRefresh, keyTabs}
	case cleanupReady:
		bindings := []key.Binding{keyUp, keyDown, keyCleanAll, keyRefresh}
		if s.cursor > 0 {
			bindings = append(bindings, keyEscClear)
		}
		return bindings
	case cleanupConfirm:
		return []key.Binding{keyConfirmY}
	}
	return []key.Binding{keyTabs}
}

// cleanupDoneMsg is sent when temp file deletion completes.
type cleanupDoneMsg struct {
	deleted   int
	failed    int
	cancelled bool
}
