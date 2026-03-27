package main

import (
	"encoding/json"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

type exportDoneMsg struct {
	path  string
	count int
	err   error
}

func exportPackages(pkgs []Package) tea.Cmd {
	return func() tea.Msg {
		path, err := exportPath()
		if err != nil {
			return exportDoneMsg{err: err}
		}

		type exportPkg struct {
			Name    string `json:"name"`
			ID      string `json:"id"`
			Version string `json:"version"`
			Source  string `json:"source,omitempty"`
		}
		out := make([]exportPkg, len(pkgs))
		for i, p := range pkgs {
			out[i] = exportPkg{Name: p.Name, ID: p.ID, Version: p.Version, Source: p.Source}
		}

		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return exportDoneMsg{err: err}
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return exportDoneMsg{err: err}
		}
		return exportDoneMsg{path: path, count: len(pkgs)}
	}
}

func exportFilename() string {
	ts := time.Now().Format("20060102_150405")
	return "wintui_packages_" + ts + ".json"
}
