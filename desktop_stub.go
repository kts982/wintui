//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func desktopDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Desktop"), nil
}

func exportPath() (string, error) {
	dir, err := desktopDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prepare Desktop export folder: %w", err)
	}
	return filepath.Join(dir, exportFilename()), nil
}
