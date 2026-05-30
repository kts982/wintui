package main

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/fang/v2"
	"github.com/spf13/cobra"
)

// Set by GoReleaser via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var (
	retryOpVal   string
	retryID      string
	retryName    string
	retrySource  string
	retryVersion string
	retryBatch   string

	jsonFlag bool
)

var rootCmd = &cobra.Command{
	Use:   "wintui",
	Short: "WinTUI - A terminal UI for winget",
	Long:  `A modern, interactive terminal user interface for the Windows Package Manager (winget).`,
	Example: `# Launch the interactive TUI
wintui

# List available upgrades (exit 1 if any) or installed packages
wintui check
wintui list

# Upgrade packages without the TUI
wintui upgrade --all
wintui upgrade --auto
wintui upgrade --id Mozilla.Firefox

# Inspect a package's effective install/upgrade args
wintui show Mozilla.Firefox

# One-line readiness verdict (exit 0/1/2)
wintui doctor --verbose

# Switch the color theme from the CLI
wintui theme --list
wintui theme nord

# Machine-readable output (--json works on check, list, show, doctor)
wintui check --json`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		appSettings = LoadSettings()
		// Apply the saved theme before any TUI/CLI rendering starts.
		// Default assumes a dark terminal — app.Init follows up with
		// tea.RequestBackgroundColor and flips to the light variant
		// (when defined) once the terminal replies.
		setActiveTheme(normalizeTheme(appSettings.Theme), true)
		cleanupStaleSelfUpdateHelpers()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var req *retryRequest
		if retryOpVal != "" {
			req = &retryRequest{Op: retryOp(retryOpVal)}
			if retryBatch != "" {
				items, err := decodeRetryItems(retryBatch)
				if err != nil {
					return fmt.Errorf("invalid retry batch: %w", err)
				}
				req.Items = items
			} else {
				req.ID = retryID
				req.Name = retryName
				req.Source = retrySource
				req.Version = retryVersion
			}
			if !req.valid() {
				return fmt.Errorf("invalid retry request")
			}
		}

		if req == nil {
			if appSettings.AutoSelfUpdate && isRunningInstalledWinTUI() {
				fmt.Fprintln(os.Stdout, "Checking for WinTUI updates…")
			}
			scheduled, err := maybeStartStartupSelfUpdate()
			if scheduled {
				fmt.Fprintf(os.Stdout, "WinTUI update available. Closing now so winget can upgrade %s.\nStart wintui again after the upgrade completes.\nLog: %s\n", selfPackageID, selfUpdateLogPath())
				return nil
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "WinTUI auto-update handoff failed: %v\n", err)
			}
		}

		p := tea.NewProgram(newApp(req))
		_, err := p.Run()
		globalElevator.shutdown()
		return err
	},
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (%s) built %s", version, commit, date)
	rootCmd.Flags().StringVar(&retryOpVal, "retry-op", "", "Operation to retry")
	rootCmd.Flags().StringVar(&retryID, "id", "", "Package ID to retry")
	rootCmd.Flags().StringVar(&retryName, "name", "", "Package name to retry")
	rootCmd.Flags().StringVar(&retrySource, "source", "", "Package source to retry")
	rootCmd.Flags().StringVar(&retryVersion, "package-version", "", "Package version to retry")
	rootCmd.Flags().StringVar(&retryBatch, "retry-batch", "", "Base64 encoded batch retry data")
	// Internal retry-handoff plumbing for the elevated-helper round-trip; not
	// for direct user use. Hide them from --help so the v2.4.0 subcommand
	// surface is the discoverable shape.
	for _, name := range []string{"retry-op", "id", "name", "source", "package-version", "retry-batch"} {
		_ = rootCmd.Flags().MarkHidden(name)
	}

	// Compatibility with old -v flag
	rootCmd.Flags().BoolP("version", "v", false, "show version")

	checkCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	showCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	showCmd.Flags().StringVar(&showSource, "source", "", "Package source (winget|msstore); defaults to winget")
	upgradeCmd.Flags().BoolVar(&upgradeAllFlag, "all", false, "Upgrade all available non-held packages")
	upgradeCmd.Flags().BoolVar(&upgradeAutoFlag, "auto", false, "Upgrade packages marked Auto")
	upgradeCmd.Flags().StringArrayVar(&upgradeIDsFlag, "id", nil, "Upgrade a specific package by ID (repeatable)")
	doctorCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	doctorCmd.Flags().BoolVar(&doctorVerboseFlag, "verbose", false, "Show per-row check table beneath the verdict")
	doctorCmd.Flags().BoolVar(&doctorFullFlag, "full", false, "Re-add the trimmed system-diagnostics rows (RAM, Defender, ping, etc.)")
	doctorCmd.Flags().BoolVar(&doctorDevToolsFlag, "dev-tools", false, "Append a developer-tools detection group")
	exportCmd.Flags().StringVar(&exportOutputFlag, "output", "", "Write to PATH instead of stdout")
	exportCmd.Flags().BoolVar(&exportWithVersionsFlag, "with-versions", false, "Include exact installed versions in the export")
	importCmd.Flags().BoolVar(&importDryRunFlag, "dry-run", false, "Print the plan without installing anything")
	importCmd.Flags().BoolVar(&importAllFlag, "all", false, "Also install packages flagged as possible name matches")
	importCmd.Flags().BoolVar(&jsonFlag, "json", false, "Print the plan as JSON (implies --dry-run)")
	themeCmd.Flags().BoolVar(&themeListFlag, "list", false, "List all available themes")

	// Dynamic completion of package IDs from the on-disk cache (no winget call).
	showCmd.ValidArgsFunction = completeInstalledIDs
	_ = upgradeCmd.RegisterFlagCompletionFunc("id", completeUpgradeableIDs)

	rootCmd.AddCommand(checkCmd, listCmd, showCmd, upgradeCmd, doctorCmd, exportCmd, importCmd, themeCmd)
}

func main() {
	// fang wraps cobra with styled help / usage / errors / version, themed from
	// the active palette (fangOptions). It silences cobra's own error print and
	// renders a styled one itself, so we don't print the error again here. We do
	// NOT pass WithNotifySignal: fang then installs no signal handler, leaving
	// the bubbletea TUI in full control of Ctrl+C. The cliExitCode path is
	// preserved exactly — subcommands set it and we exit on it after a clean run.
	if err := fang.Execute(context.Background(), rootCmd, fangOptions()...); err != nil {
		os.Exit(1)
	}
	if cliExitCode != 0 {
		os.Exit(cliExitCode)
	}
}
