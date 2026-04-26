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

// showSource is the --source flag for `wintui show <id>`. Defaults to winget.
var showSource string

// upgradeAllFlag is set by the `wintui upgrade --all` flag. When more action
// modes (--auto, --id) land, swap this single bool for a richer set of mutually
// exclusive flags via cobra.MarkFlagsOneRequired.
var upgradeAllFlag bool

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade packages without launching the TUI",
	Long: `Upgrade packages headlessly. Currently only --all is supported; per-package
'auto' policy will land alongside the Auto/Ask/Hold work.

Honors the same per-package ignore rules the TUI uses, so a hidden package
will not be upgraded by --all.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if upgradeAllFlag {
			return runUpgradeAll()
		}
		return fmt.Errorf("specify --all (use 'wintui upgrade --all')")
	},
}

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show effective install/upgrade command and overrides for a package",
	Long: `Print the install and upgrade arguments WinTUI would pass to winget for the
given package, along with any per-package overrides currently in settings.

This is read-only and does not query winget; it reflects the local config only.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShow(args[0], showSource)
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

// showOutput is the structured payload for `wintui show <id> --json`.
type showOutput struct {
	ID          string           `json:"id"`
	Source      string           `json:"source"`
	InstallArgs []string         `json:"install_args"`
	UpgradeArgs []string         `json:"upgrade_args"`
	Override    *PackageOverride `json:"override,omitempty"`
}

func runShow(id, source string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("package id is required")
	}
	if source == "" {
		source = "winget"
	}

	out := showOutput{
		ID:          id,
		Source:      source,
		InstallArgs: installCommandArgs(id, source, ""),
		UpgradeArgs: upgradeCommandArgs(id, source, ""),
	}
	if appSettings.hasOverride(id, source) {
		o := appSettings.getOverride(id, source)
		out.Override = &o
	}

	if jsonFlag {
		return printJSON(out)
	}

	fmt.Printf("ID:     %s\n", out.ID)
	fmt.Printf("Source: %s\n", out.Source)
	fmt.Println()
	fmt.Println("Effective install command:")
	fmt.Printf("  winget %s\n", strings.Join(out.InstallArgs, " "))
	fmt.Println()
	fmt.Println("Effective upgrade command:")
	fmt.Printf("  winget %s\n", strings.Join(out.UpgradeArgs, " "))
	if out.Override != nil {
		fmt.Println()
		fmt.Println("Per-package overrides:")
		if out.Override.Scope != "" {
			fmt.Printf("  scope:          %s\n", out.Override.Scope)
		}
		if out.Override.Architecture != "" {
			fmt.Printf("  architecture:   %s\n", out.Override.Architecture)
		}
		if out.Override.Elevate != nil {
			fmt.Printf("  elevate:        %t\n", *out.Override.Elevate)
		}
		if out.Override.Ignore {
			fmt.Println("  ignore:         true")
		}
		if out.Override.IgnoreVersion != "" {
			fmt.Printf("  ignore_version: %s\n", out.Override.IgnoreVersion)
		}
	}
	return nil
}

// runUpgradeAll runs `winget upgrade` for every visible upgradeable package
// (i.e. those not hidden by ignore rules), streaming output to stdout and
// reporting per-package success/failure. Sets cliExitCode = 1 if any failed.
func runUpgradeAll() error {
	ctx := context.Background()
	raw, err := getUpgradeableCtx(ctx)
	if err != nil {
		return err
	}
	visible, hidden := selectUpgrades(raw, appSettings)

	if len(visible) == 0 {
		if hidden > 0 {
			fmt.Printf("All non-hidden packages are up to date (%d hidden by ignore rules).\n", hidden)
		} else {
			fmt.Println("All packages are up to date.")
		}
		return nil
	}

	fmt.Printf("Upgrading %d package(s)", len(visible))
	if hidden > 0 {
		fmt.Printf(" (%d hidden by ignore rules)", hidden)
	}
	fmt.Println(":")

	var failures []string
	for _, pkg := range visible {
		fmt.Printf("\n→ %s (%s) %s → %s\n", pkg.Name, pkg.ID, pkg.Version, pkg.Available)
		if err := streamUpgradeToStdout(ctx, pkg); err != nil {
			fmt.Printf("  ✗ failed: %v\n", err)
			failures = append(failures, pkg.ID)
		} else {
			fmt.Printf("  ✓ upgraded\n")
		}
	}

	fmt.Printf("\n%d/%d succeeded.", len(visible)-len(failures), len(visible))
	if len(failures) > 0 {
		fmt.Printf(" Failed: %s", strings.Join(failures, ", "))
		cliExitCode = 1
	}
	fmt.Println()
	return nil
}

// streamUpgradeToStdout drives a single package upgrade through the same
// streaming pipeline the TUI uses, indenting each output line under stdout.
// Returns the final winget error (or nil on success).
func streamUpgradeToStdout(ctx context.Context, pkg Package) error {
	_, outChan, errChan := upgradePackageStreamCtx(ctx, pkg.ID, pkg.Source, "")
	for line := range outChan {
		// Skip the TUI's progress sentinels; they are not human-readable.
		if _, isProgress := parseProgressSentinel(line); isProgress {
			continue
		}
		if line != "" {
			fmt.Println("  " + line)
		}
	}
	return <-errChan
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
