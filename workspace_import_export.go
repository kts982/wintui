package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
)

// installedPackages flattens the workspace's items list to a plain []Package
// for callers that don't care about upgradeable/staged metadata (the import
// reviewer is the main consumer — it just needs the by-ID + by-name lookup).
func (s workspaceScreen) installedPackages() []Package {
	pkgs := make([]Package, 0, len(s.items))
	for _, it := range s.items {
		pkgs = append(pkgs, it.pkg)
	}
	return pkgs
}

// beginImport activates the import overlay and queues the file scan. The
// overlay drives the rest of the lifecycle through importModel.update; the
// workspace loop just routes messages until !s.importer.active.
func (s workspaceScreen) beginImport() (screen, tea.Cmd) {
	var cmd tea.Cmd
	s.importer, cmd = s.importer.activate()
	return s, cmd
}

// exportDoneMsg is delivered when the background export goroutine finishes.
// On success, path is the file we wrote to, count is how many packages
// landed in it, and dropped reports entries skipped because their IDs
// were truncated and couldn't be recovered. On failure, err is non-nil.
type exportDoneMsg struct {
	path    string
	count   int
	dropped int
	err     error
}

// beginExport spawns a one-shot goroutine that materializes the current
// installed list to a wintui-prefixed JSON file on the user's Desktop.
// The file path stamps both date and time so back-to-back presses of `e`
// produce distinct files. Successful runs surface a status line + a
// Windows toast.
func (s workspaceScreen) beginExport() (screen, tea.Cmd) {
	if s.exportingActive {
		return s, nil
	}
	s.exportingActive = true
	s.exportingMsg = ""
	pkgs := s.installedPackages()
	return s, exportToDesktopCmd(pkgs)
}

// exportDestinationPath returns the file we'd write to right now. The
// filename includes both date and time so two presses of `e` within the
// same day produce distinct files (the previous YYYY-MM-DD-only naming
// silently overwrote earlier exports). Public for testability — pure
// given the home directory.
func exportDestinationPath(now time.Time) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("could not resolve home directory: %w", err)
	}
	desktop := filepath.Join(home, "Desktop")
	if _, err := os.Stat(desktop); err != nil {
		// Fall back to the home directory itself if Desktop doesn't exist
		// (some users redirect it to OneDrive, others run in stripped envs).
		desktop = home
	}
	name := fmt.Sprintf("wintui-packages-%s.json", now.Format("2006-01-02_150405"))
	return filepath.Join(desktop, name), nil
}

func exportToDesktopCmd(pkgs []Package) tea.Cmd {
	return func() tea.Msg {
		// Resolve any truncated IDs before writing — same contract as the
		// CLI export. A truncated ID would round-trip into the file and
		// fail at import time under `winget install --id ... --exact`.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resolved, dropped := resolveTruncatedForExport(ctx, pkgs)

		path, err := exportDestinationPath(time.Now())
		if err != nil {
			return exportDoneMsg{err: err}
		}
		env := buildExportEnvelope(resolved, false, time.Now())
		data, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return exportDoneMsg{err: err}
		}
		data = append(data, '\n')
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return exportDoneMsg{err: err}
		}
		// Fire a toast in the same goroutine — sendToast itself is
		// fire-and-forget and respects the user's notification setting.
		body := fmt.Sprintf("Wrote %s to Desktop.", pluralize(len(env.Packages), "package"))
		if len(dropped) > 0 {
			body += fmt.Sprintf(" Skipped %s with unrecoverable truncated IDs.",
				pluralize(len(dropped), "entry", "entries"))
		}
		sendToast("WinTUI export", body)
		return exportDoneMsg{path: path, count: len(env.Packages), dropped: len(dropped)}
	}
}

// handleExportDone updates workspace state with the result of an export run.
// Called from the workspace update switch when an exportDoneMsg arrives.
func (s workspaceScreen) handleExportDone(msg exportDoneMsg) (screen, tea.Cmd) {
	s.exportingActive = false
	if msg.err != nil {
		s.exportingMsg = errorStyle.Render("Export failed: " + msg.err.Error())
		return s, nil
	}
	body := fmt.Sprintf("Exported %s to %s",
		pluralize(msg.count, "package"), msg.path)
	if msg.dropped > 0 {
		body += fmt.Sprintf(" (skipped %s with truncated IDs)",
			pluralize(msg.dropped, "entry", "entries"))
	}
	s.exportingMsg = successStyle.Render(body)
	// Status line is transient — clear it after a few seconds so it doesn't
	// linger across unrelated user actions.
	return s, tea.Tick(8*time.Second, func(time.Time) tea.Msg {
		return clearExportMsg{}
	})
}

type clearExportMsg struct{}
