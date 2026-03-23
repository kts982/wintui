package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
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
	state    cleanupState
	files    []string
	cursor   int
	spinner  spinner.Model
	progress progressBar
	err      error
	deleted  int
	failed   int
}

func newCleanupScreen() cleanupScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return cleanupScreen{
		state:    cleanupLoading,
		spinner:  sp,
		progress: newProgressBar(50),
	}
}

func (s cleanupScreen) init() tea.Cmd {
	s.progress, _ = s.progress.start()
	return tea.Batch(s.spinner.Tick, tickProgress(), scanTempFiles)
}

func scanTempFiles() tea.Msg {
	tmpDir := os.TempDir()
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	var old []string

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return filesScannedMsg{err: err}
	}

	for _, e := range entries {
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

func (s cleanupScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
			case "enter", "d":
				s.state = cleanupConfirm
			case "esc", "q":
				return s, goToMenu
			}

		case cleanupConfirm:
			switch msg.String() {
			case "y", "Y":
				s.state = cleanupExecuting
				s.progress, _ = s.progress.start()
				files := s.files
				return s, tea.Batch(s.spinner.Tick, tickProgress(), func() tea.Msg {
					deleted, failed := 0, 0
					for _, f := range files {
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
			if msg.String() == "enter" || msg.String() == "esc" {
				return s, goToMenu
			}
		}

	case filesScannedMsg:
		s.progress = s.progress.stop()
		s.files = msg.files
		s.err = msg.err
		if msg.err != nil || len(msg.files) == 0 {
			s.state = cleanupEmpty
		} else {
			s.state = cleanupReady
		}
		return s, nil

	case cleanupDoneMsg:
		s.progress = s.progress.stop()
		s.deleted = msg.deleted
		s.failed = msg.failed
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
		b.WriteString(fmt.Sprintf("  %s Scanning temp directory...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case cleanupEmpty:
		if s.err != nil {
			b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")
		} else {
			b.WriteString("  " + successStyle.Render("No old temp files found. All clean!") + "\n")
		}

	case cleanupReady:
		b.WriteString(fmt.Sprintf("  %s\n\n",
			warnStyle.Render(fmt.Sprintf("%d item(s) older than 7 days.", len(s.files)))))

		maxVisible := height - 8
		if maxVisible < 5 {
			maxVisible = 5
		}
		start, end := scrollWindow(s.cursor, len(s.files), maxVisible)
		for i := start; i < end; i++ {
			cursor := cursorBlankStr
			style := itemStyle
			if i == s.cursor {
				cursor = cursorStr
				style = itemActiveStyle
			}
			name := filepath.Base(s.files[i])
			b.WriteString(fmt.Sprintf("  %s%s\n", cursor, style.Render(name)))
		}

	case cleanupConfirm:
		b.WriteString(fmt.Sprintf("  Delete %d temp item(s)?\n\n",
			len(s.files)))
		b.WriteString("  " + warnStyle.Render("Press y to confirm, n to cancel"))

	case cleanupExecuting:
		b.WriteString(fmt.Sprintf("  %s Cleaning up...\n\n", s.spinner.View()))
		b.WriteString("  " + s.progress.view() + "\n")

	case cleanupDone:
		b.WriteString(fmt.Sprintf("  %s\n",
			successStyle.Render(fmt.Sprintf("Deleted %d item(s).", s.deleted))))
		if s.failed > 0 {
			b.WriteString(fmt.Sprintf("  %s\n",
				warnStyle.Render(fmt.Sprintf("%d item(s) could not be removed (in use or locked).", s.failed))))
		}
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
		return []key.Binding{keyUp, keyDown, keyEnter, keyEsc}
	case cleanupConfirm:
		return []key.Binding{keyConfirmY}
	}
	return []key.Binding{keyTabs}
}

// cleanupDoneMsg is sent when temp file deletion completes.
type cleanupDoneMsg struct {
	deleted int
	failed  int
}
