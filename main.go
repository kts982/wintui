package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

// Set by GoReleaser via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	showVersion, retryReq, err := parseStartupArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if showVersion {
		fmt.Printf("wintui %s (%s) built %s\n", version, commit, date)
		return
	}

	// Load settings from config file
	appSettings = LoadSettings()

	p := tea.NewProgram(newApp(retryReq))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
