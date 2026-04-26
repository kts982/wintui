package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
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

	listFlag  bool
	checkFlag bool
	jsonFlag  bool
)

var rootCmd = &cobra.Command{
	Use:   "wintui",
	Short: "WinTUI - A terminal UI for winget",
	Long:  `A modern, interactive terminal user interface for the Windows Package Manager (winget).`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		appSettings = LoadSettings()
		cleanupStaleSelfUpdateHelpers()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if listFlag {
			return runList()
		}
		if checkFlag {
			return runCheck()
		}

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

	rootCmd.Flags().BoolVar(&checkFlag, "check", false, "Check for available upgrades")
	rootCmd.Flags().BoolVar(&listFlag, "list", false, "List all installed packages")
	rootCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	rootCmd.MarkFlagsMutuallyExclusive("check", "list")
	// Deprecated in favor of subcommands; kept working for one minor release.
	_ = rootCmd.Flags().MarkDeprecated("check", "use 'wintui check' instead")
	_ = rootCmd.Flags().MarkDeprecated("list", "use 'wintui list' instead")

	// Compatibility with old -v flag
	rootCmd.Flags().BoolP("version", "v", false, "show version")

	checkCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	showCmd.Flags().BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	showCmd.Flags().StringVar(&showSource, "source", "", "Package source (winget|msstore); defaults to winget")
	upgradeCmd.Flags().BoolVar(&upgradeAllFlag, "all", false, "Upgrade all available (non-ignored) packages")
	rootCmd.AddCommand(checkCmd, listCmd, showCmd, upgradeCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if cliExitCode != 0 {
		os.Exit(cliExitCode)
	}
}
