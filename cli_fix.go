package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	fixPortableFlag bool
	fixDryRunFlag   bool
)

// Seams for testing — replaced so tests neither call winget, touch the real
// %LOCALAPPDATA%, nor write settings.json.
var (
	fixPortableInstalledFn   = func(ctx context.Context) ([]Package, error) { return getInstalledCtx(ctx) }
	fixPortablePackageDirsFn = wingetPortablePackageDirNames
	fixPortableApplyFn       = persistPackageOverride
)

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Apply targeted fixes for known winget footguns",
	Long: `Apply a targeted remediation.

--portable pins every portable (user-scope) winget package to user scope, so a
future upgrade can't drop it from your PATH. winget mis-scopes portable packages
on upgrade and strips their user PATH entry, so a CLI like 'claude' suddenly
"disappears" (winget-cli #4044/#5099). Pinning scope=user — plus elevate=false,
since portables never need admin — makes WinTUI always pass --scope user for
them. Idempotent; --dry-run previews without writing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !fixPortableFlag {
			return fmt.Errorf("specify a fix to apply, e.g. 'wintui fix --portable'")
		}
		return runFixPortable(fixPortableOptions{DryRun: fixDryRunFlag, JSON: jsonFlag}, currentSettings(), os.Stdout)
	},
}

type fixPortableOptions struct {
	DryRun bool
	JSON   bool
}

type fixPortableJSON struct {
	DryRun        bool     `json:"dry_run"`
	Fixed         []string `json:"fixed"`
	AlreadyPinned []string `json:"already_pinned"`
}

func runFixPortable(opts fixPortableOptions, settings Settings, out io.Writer) error {
	installed, err := fixPortableInstalledFn(context.Background())
	if err != nil {
		return err
	}
	dirNames := fixPortablePackageDirsFn()

	var fixed, already []Package
	for _, pkg := range installed {
		if !isPortableInstalled(pkg.ID, dirNames) {
			continue
		}
		_, existing, _ := settings.lookupOverride(pkg.ID, pkg.Source)
		if portableAlreadyPinned(existing) {
			already = append(already, pkg)
			continue
		}
		fixed = append(fixed, pkg)
		if !opts.DryRun {
			// Merge: keep any existing per-package rules (update policy, arch,
			// ignore), only set scope=user + elevate=false.
			f := false
			merged := existing
			merged.Scope = ScopeUser
			merged.Elevate = &f
			if err := fixPortableApplyFn(pkg.ID, pkg.Source, merged); err != nil {
				return fmt.Errorf("pinning %s: %w", pkg.ID, err)
			}
		}
	}

	if opts.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(fixPortableJSON{DryRun: opts.DryRun, Fixed: packageIDList(fixed), AlreadyPinned: packageIDList(already)})
	}
	renderFixPortable(opts.DryRun, fixed, already, out)
	return nil
}

func renderFixPortable(dryRun bool, fixed, already []Package, out io.Writer) {
	if len(fixed) == 0 && len(already) == 0 {
		fmt.Fprintln(out, "No portable winget packages found — nothing to pin.")
		return
	}
	if len(fixed) > 0 {
		verb := "Pinned"
		if dryRun {
			verb = "Would pin"
		}
		fmt.Fprintln(out, cliAccent(fmt.Sprintf("%s %d portable package(s) to user scope (scope=user, no elevation):", verb, len(fixed))))
		for _, p := range fixed {
			fmt.Fprintf(out, "  • %s (%s)\n", packageNameOrID(p), p.ID)
		}
	}
	if len(already) > 0 {
		fmt.Fprintf(out, "%d already pinned.\n", len(already))
	}
	switch {
	case dryRun && len(fixed) > 0:
		fmt.Fprintln(out, cliAccent("\nDry run — no changes written. Re-run without --dry-run to apply."))
	case len(fixed) > 0:
		fmt.Fprintln(out, cliAccent("\nFuture upgrades of these packages will pass --scope user and stay on your PATH."))
	}
}

// ── Portable detection (filesystem, no winget call) ────────────────

// winget extracts portable (and archive) packages to
// %LOCALAPPDATA%\Microsoft\WinGet\Packages\<PackageId>_<Source...>\ and adds
// them to the user PATH. Presence of such a dir is a free, reliable signal that
// a package is portable/user-scope — no `winget show` probe needed.

func wingetPortablePackagesDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if dir, err := os.UserCacheDir(); err == nil { // %LocalAppData% on Windows
			base = dir
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Microsoft", "WinGet", "Packages")
}

func wingetPortablePackageDirNames() []string {
	dir := wingetPortablePackagesDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// isPortableInstalled reports whether pkgID has a portable package directory.
// The trailing "_" before the source identifier prevents prefix collisions
// (e.g. "Foo.Bar" must not match "Foo.BarBaz_...").
func isPortableInstalled(pkgID string, dirNames []string) bool {
	if pkgID == "" {
		return false
	}
	prefix := pkgID + "_"
	for _, n := range dirNames {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

func portableAlreadyPinned(o PackageOverride) bool {
	return o.Scope == ScopeUser && o.Elevate != nil && !*o.Elevate
}

// ── Upgrade advisory ───────────────────────────────────────────────

// printPortableUpgradeAdvisory nudges the user toward `wintui fix --portable`
// when an upgrade set contains portable packages not yet pinned to user scope.
// winget can drop those from PATH on upgrade regardless of elevation, so this is
// not gated on isElevated().
func printPortableUpgradeAdvisory(out io.Writer, pkgs []Package, settings Settings) {
	risky := portableUnpinned(pkgs, settings)
	if len(risky) == 0 {
		return
	}
	fmt.Fprintln(out, cliAccent(fmt.Sprintf(
		"note: %d portable package(s) here aren't pinned to user scope; winget can drop them from PATH on upgrade. Run 'wintui fix --portable' to protect them.",
		len(risky))))
}

func portableUnpinned(pkgs []Package, settings Settings) []Package {
	dirNames := fixPortablePackageDirsFn()
	if len(dirNames) == 0 {
		return nil
	}
	var risky []Package
	for _, pkg := range pkgs {
		if !isPortableInstalled(pkg.ID, dirNames) {
			continue
		}
		if _, o, _ := settings.lookupOverride(pkg.ID, pkg.Source); portableAlreadyPinned(o) {
			continue // already pinned (scope=user, elevate=false)
		}
		risky = append(risky, pkg)
	}
	return risky
}

// ── small helpers ──────────────────────────────────────────────────

func packageIDList(pkgs []Package) []string {
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, p.ID)
	}
	return out
}

func packageNameOrID(p Package) string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	return p.ID
}
