package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// This file pins the external binaries WinTUI spawns to absolute paths so we
// run exactly the binaries we intend (especially from the elevated helper)
// rather than whatever an earlier PATH entry resolves to. Note this is about
// pinning, not CWD: since Go 1.19 exec.Command already refuses CWD-relative
// resolution (ErrDot).

// powershellExePath returns the absolute path of Windows PowerShell 5.1,
// which ships with Windows at a fixed System32 location.
func powershellExePath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

var (
	wingetPathOnce sync.Once
	wingetPath     string
	wingetPathErr  error
)

// wingetExePath resolves winget via PATH once per process and validates that
// the result lives under a location Windows actually installs winget to.
func wingetExePath() (string, error) {
	wingetPathOnce.Do(func() {
		wingetPath, wingetPathErr = resolveWingetPath()
	})
	return wingetPath, wingetPathErr
}

func resolveWingetPath() (string, error) {
	path, err := exec.LookPath("winget")
	if err != nil {
		return "", fmt.Errorf("winget not found on PATH: %w", err)
	}
	if abs, absErr := filepath.Abs(path); absErr == nil {
		path = abs
	}
	if !wingetPathAllowed(path, os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles")) {
		return "", fmt.Errorf("refusing winget at %q: not under a known system app location", path)
	}
	return path, nil
}

// wingetPathAllowed reports whether path is a descendant of one of the two
// locations winget ships from: the per-user app execution aliases
// (%LOCALAPPDATA%\Microsoft\WindowsApps) or the packaged-app store
// (%ProgramFiles%\WindowsApps).
func wingetPathAllowed(path, localAppData, programFiles string) bool {
	var roots []string
	if localAppData != "" {
		roots = append(roots, filepath.Join(localAppData, "Microsoft", "WindowsApps"))
	}
	if programFiles != "" {
		roots = append(roots, filepath.Join(programFiles, "WindowsApps"))
	}
	for _, root := range roots {
		if pathIsDescendant(path, root) {
			return true
		}
	}
	return false
}

// pathIsDescendant reports whether path sits strictly below root. Comparison
// is case-insensitive (Windows filesystems are) and rejects the root itself,
// siblings that merely share a name prefix, and anything reached via "..".
func pathIsDescendant(path, root string) bool {
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(root)), strings.ToLower(filepath.Clean(path)))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
