package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// cliExitCode is the exit code main returns after rootCmd.Execute() succeeds.
// CLI subcommands (e.g. runCheck) set it instead of calling os.Exit directly so
// that cobra hooks, deferred work, and tests can run on the normal return path.
var cliExitCode int

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for available upgrades",
	Long: `Print the list of upgradeable packages, honoring per-package ignore rules.
Exits 1 if any updates are available, 0 otherwise.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheck()
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed packages",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList()
	},
}

func runList() error {
	pkgs, err := getInstalledCtx(context.Background())
	if err != nil {
		return err
	}

	if jsonFlag {
		return printJSON(pkgs)
	}

	printPackageTable(
		[]string{"Name", "ID", "Version", "Source"},
		func(p Package) []string { return []string{p.Name, p.ID, p.Version, p.Source} },
		pkgs,
	)
	fmt.Printf("\n%d package(s) installed.\n", len(pkgs))
	return nil
}

func runCheck() error {
	raw, err := getUpgradeableCtx(context.Background())
	if err != nil {
		return err
	}

	// Route through the shared planner so --check honors the same ignore
	// rules the TUI does. Hidden packages must not flip the exit code.
	pkgs, _ := selectUpgrades(raw, appSettings)

	if jsonFlag {
		if err := printJSON(pkgs); err != nil {
			return err
		}
	} else {
		if len(pkgs) == 0 {
			fmt.Println("All packages are up to date.")
		} else {
			printPackageTable(
				[]string{"Name", "ID", "Version", "Available"},
				func(p Package) []string { return []string{p.Name, p.ID, p.Version, p.Available} },
				pkgs,
			)
			fmt.Printf("\n%d package(s) have updates available.\n", len(pkgs))
		}
	}

	// Exit code 1 if updates exist, 0 if up to date.
	if len(pkgs) > 0 {
		cliExitCode = 1
	}
	return nil
}

func printJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func printPackageTable(headers []string, rowFn func(Package) []string, pkgs []Package) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, pkg := range pkgs {
		fmt.Fprintln(tw, strings.Join(rowFn(pkg), "\t"))
	}
	_ = tw.Flush()
}
