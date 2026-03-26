package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func desktopDir() (string, error) {
	if path, err := readDesktopPath(`Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`); err == nil {
		return path, nil
	}
	if path, err := readDesktopPath(`Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders`); err == nil {
		return path, nil
	}

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

func readDesktopPath(keyPath string) (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()

	value, _, err := key.GetStringValue("Desktop")
	if err != nil {
		return "", err
	}
	value = os.ExpandEnv(value)
	if value == "" {
		return "", fmt.Errorf("desktop path is empty")
	}
	return value, nil
}
