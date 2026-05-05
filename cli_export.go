package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	exportOutputFlag       string
	exportWithVersionsFlag bool
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export installed packages as a portable JSON file",
	Long: `Write the installed package list to a JSON file you can take to another
machine and feed to ` + "`wintui import`" + `.

Versions are excluded by default — restoring exact versions on a fresh
machine is a footgun (the registered version may have aged out of winget).
Pass --with-versions if you genuinely need the snapshot pinned.

WinTUI itself is included in the export; on import it will be marked
already-installed (you must have WinTUI to run import in the first place).

By default the JSON is written to stdout, suitable for piping. Pass
--output to write to a file instead.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExport(exportOptions{
			Output:       exportOutputFlag,
			WithVersions: exportWithVersionsFlag,
		}, os.Stdout)
	},
}

var (
	importDryRunFlag bool
	importAllFlag    bool
)

var importCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Install packages from a wintui export file",
	Long: `Read a wintui export file and install its packages headlessly.

Default behavior installs the safe subset: packages that are not already
installed, are not raw/non-canonical identifiers, and don't share a name
with a different installed package.

--dry-run prints the install plan (will-install / already-installed /
review-needed / non-restorable) without touching anything.

--all also installs entries flagged as possible name matches with packages
already on this machine (e.g. exporting Git.Git when Microsoft.Git is
installed locally). Combine with --dry-run to preview.

The TUI flow (` + "`wintui` Packages → import" + `) gives row-level toggling for
fine-grained selection.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runImport(args[0], importOptions{
			DryRun: importDryRunFlag,
			All:    importAllFlag,
		}, os.Stdout)
	},
}

type exportOptions struct {
	Output       string
	WithVersions bool
}

type importOptions struct {
	DryRun bool
	All    bool
}

func runExport(opts exportOptions, stdout io.Writer) error {
	ctx := context.Background()
	pkgs, err := getInstalledCtx(ctx)
	if err != nil {
		return err
	}
	pkgs, dropped := resolveTruncatedForExport(ctx, pkgs)
	if len(dropped) > 0 {
		// Always to stderr: when --output is unset, stdout is the JSON pipe
		// and a human warning prefix would invalidate it. Routing to stderr
		// makes `wintui export | jq` work and matches CLI convention
		// (structured output on stdout, diagnostics on stderr).
		fmt.Fprintf(os.Stderr, "Skipped %s with truncated IDs that couldn't be recovered (winget couldn't resolve the full ID): %s\n",
			pluralize(len(dropped), "entry", "entries"),
			strings.Join(dropped, ", "))
	}
	return writeExport(pkgs, opts, stdout)
}

// resolveTruncatedForExport walks the installed list and recovers any
// canonical package whose ID came back from `winget list` ending in
// U+2026 (the table-truncation marker). Truncated canonical IDs would
// round-trip through the export and fail at import time with
// `winget --id ... --exact` returning 0x8a150014.
//
// Non-canonical IDs (MSIX hashes, ARP\Machine\... entries, GUIDs)
// frequently report idTruncated=true on narrow pipe widths, but they
// can't be resolved against the winget catalog by design — the import
// side filters them as "non-canonical (can't restore)" regardless. Keep
// them verbatim so the export captures the full inventory; the user
// sees them surface in import dry-run rather than silently vanishing.
//
// Entries that genuinely can't be recovered (canonical IDs whose full
// form winget no longer reports) are dropped and named in the returned
// slice so the caller can warn.
func resolveTruncatedForExport(ctx context.Context, pkgs []Package) (kept []Package, dropped []string) {
	kept = make([]Package, 0, len(pkgs))
	for _, p := range pkgs {
		if !p.idTruncated || isNonCanonical(p.ID) {
			kept = append(kept, p)
			continue
		}
		resolved, err := resolveTruncatedPackage(ctx, p)
		if err != nil || resolved.idTruncated {
			label := p.Name
			if label == "" {
				label = p.ID
			}
			dropped = append(dropped, label)
			continue
		}
		kept = append(kept, resolved)
	}
	return kept, dropped
}

// writeExport renders + emits the envelope. Split from runExport so tests
// can drive it with a synthetic package list without touching winget.
func writeExport(pkgs []Package, opts exportOptions, stdout io.Writer) error {
	env := buildExportEnvelope(pkgs, opts.WithVersions, time.Now())

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if opts.Output == "" {
		_, err := stdout.Write(data)
		return err
	}
	if err := os.WriteFile(opts.Output, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Exported %d package(s) to %s.\n", len(env.Packages), opts.Output)
	return nil
}

func runImport(path string, opts importOptions, stdout io.Writer) error {
	installed, err := getInstalledCtx(context.Background())
	if err != nil {
		return err
	}
	pkgs, err := loadImportFile(path, installed)
	if err != nil {
		return err
	}

	plan := planImport(pkgs, opts.All)

	// --json is a structured-preflight reader; pairing it with a real
	// install would silently drop the install output, so treat --json as
	// implying --dry-run.
	if jsonFlag {
		return printJSON(plan)
	}

	printImportPlan(stdout, plan, opts)

	if opts.DryRun {
		return nil
	}

	if len(plan.WillInstall) == 0 {
		fmt.Fprintln(stdout, "Nothing to install.")
		return nil
	}

	return executeImport(stdout, plan.WillInstall)
}

// importPlan partitions an import file's contents into actionable buckets.
// The CLI dry-run, the JSON output, and (eventually) the TUI review pane
// all consume the same plan so terminology stays consistent.
type importPlan struct {
	WillInstall      []importPkg `json:"will_install"`
	AlreadyInstalled []importPkg `json:"already_installed"`
	ReviewNeeded     []importPkg `json:"review_needed,omitempty"`
	NonCanonical     []importPkg `json:"non_canonical,omitempty"`
}

// planImport categorizes every entry in the file. With all=true, collision
// rows move from ReviewNeeded into WillInstall; the user has explicitly
// opted in to potential duplicates.
func planImport(pkgs []importPkg, all bool) importPlan {
	var plan importPlan
	for _, pkg := range pkgs {
		switch {
		case pkg.Installed:
			plan.AlreadyInstalled = append(plan.AlreadyInstalled, pkg)
		case pkg.NonCanonical:
			plan.NonCanonical = append(plan.NonCanonical, pkg)
		case len(pkg.Collisions) > 0:
			if all {
				plan.WillInstall = append(plan.WillInstall, pkg)
			} else {
				plan.ReviewNeeded = append(plan.ReviewNeeded, pkg)
			}
		default:
			plan.WillInstall = append(plan.WillInstall, pkg)
		}
	}
	return plan
}

func printImportPlan(out io.Writer, plan importPlan, opts importOptions) {
	header := "Plan"
	if opts.DryRun {
		header = "Plan (dry-run, no changes will be made)"
	}
	fmt.Fprintf(out, "%s\n%s\n\n", header, strings.Repeat("─", len(header)))

	if len(plan.WillInstall) > 0 {
		fmt.Fprintf(out, "Will install (%d):\n", len(plan.WillInstall))
		printImportRows(out, plan.WillInstall)
		fmt.Fprintln(out)
	}
	if len(plan.AlreadyInstalled) > 0 {
		fmt.Fprintf(out, "Already installed (%d, skipped):\n", len(plan.AlreadyInstalled))
		printImportRows(out, plan.AlreadyInstalled)
		fmt.Fprintln(out)
	}
	if len(plan.ReviewNeeded) > 0 {
		fmt.Fprintf(out, "Review needed — name match with existing package (%d):\n", len(plan.ReviewNeeded))
		for _, pkg := range plan.ReviewNeeded {
			fmt.Fprintf(out, "  · %s  (%s)\n", pkg.Name, pkg.ID)
			fmt.Fprintf(out, "    ↳ already installed locally as %s\n", strings.Join(pkg.Collisions, ", "))
			fmt.Fprintln(out, "    ↳ tip: pick one — either skip this entry or remove the existing install first")
		}
		fmt.Fprintln(out, "  Re-run with --all to install these anyway.")
		fmt.Fprintln(out)
	}
	if len(plan.NonCanonical) > 0 {
		fmt.Fprintf(out, "Non-canonical (%d, can't restore):\n", len(plan.NonCanonical))
		for _, pkg := range plan.NonCanonical {
			fmt.Fprintf(out, "  · %s  (%s)\n", pkg.Name, pkg.ID)
		}
		fmt.Fprintln(out)
	}
}

func printImportRows(out io.Writer, pkgs []importPkg) {
	// Sort for stable output (test-friendly + nicer to scan visually).
	sorted := make([]importPkg, len(pkgs))
	copy(sorted, pkgs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	for _, pkg := range sorted {
		extras := ""
		if pkg.Version != "" {
			extras = " " + pkg.Version
		}
		if src := importSourceLabel(pkg); src != "" {
			extras += "  [" + src + "]"
		}
		fmt.Fprintf(out, "  · %s%s\n", pkg.ID, extras)
	}
}

func executeImport(out io.Writer, pkgs []importPkg) error {
	fmt.Fprintf(out, "Installing %d package(s)...\n", len(pkgs))

	ctx := context.Background()
	var failures int
	for i, pkg := range pkgs {
		fmt.Fprintf(out, "[%d/%d] %s\n", i+1, len(pkgs), pkg.ID)
		source := resolveImportSource(pkg)
		// Honor any pinned version from the export. Empty Version means
		// "install latest" (the default --with-versions=false case);
		// non-empty pins to that exact version, surfacing winget's own
		// "version not found" error if it has aged out of the catalog.
		_, err := installPackageSourceCtx(ctx, Package{ID: pkg.ID, Source: source}, pkg.Version)
		if err != nil {
			failures++
			fmt.Fprintf(out, "      ✗ %v\n", err)
		}
	}

	if failures > 0 {
		fmt.Fprintf(out, "\nDone — %d succeeded, %d failed.\n", len(pkgs)-failures, failures)
		cliExitCode = 1
		return nil
	}
	fmt.Fprintf(out, "\nDone — %d package(s) installed.\n", len(pkgs))
	return nil
}
