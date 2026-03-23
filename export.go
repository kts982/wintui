package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type exportDoneMsg struct {
	path string
	err  error
}

func exportPackages(pkgs []Package) tea.Cmd {
	return func() tea.Msg {
		home, _ := os.UserHomeDir()
		ts := time.Now().Format("20060102_150405")
		path := filepath.Join(home, "Desktop", fmt.Sprintf("wintui_packages_%s.json", ts))

		type exportPkg struct {
			Name    string `json:"name"`
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		out := make([]exportPkg, len(pkgs))
		for i, p := range pkgs {
			out[i] = exportPkg{Name: p.Name, ID: p.ID, Version: p.Version}
		}

		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return exportDoneMsg{err: err}
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return exportDoneMsg{err: err}
		}
		return exportDoneMsg{path: path}
	}
}
