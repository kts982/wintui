package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// cliExitCode is the exit code main returns after rootCmd.Execute() succeeds.
// CLI subcommands (e.g. runCheck) set it instead of calling os.Exit directly so
// that cobra hooks, deferred work, and tests can run on the normal return path.
var cliExitCode int

var checkNotesFlag bool

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for available upgrades",
	Long: `Print the list of upgradeable packages, honoring per-package update policy.
Exits 1 if any updates are available, 0 otherwise.

--notes renders each pending update's target-version release notes inline, so
you can review what you're about to install before upgrading.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheck()
	},
}

var listCmd = &cobra.Command{
	Use:   "list [query]",
	Short: "List installed packages, optionally filtered by name or id",
	Long: `List installed packages. With a query argument, only packages whose name or
id contains the query (case-insensitive) are shown — handy for "is X installed?"
checks, like winget list. Exits 1 when a query is given and nothing matches.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeInstalledIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		return runList(query)
	},
}

// showSource is the --source flag for `wintui show <id>`. Defaults to winget.
var showSource string

var (
	upgradeAllFlag  bool
	upgradeAutoFlag bool
	upgradeSelfFlag bool
	upgradeIDsFlag  []string
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade packages without launching the TUI",
	Long: `Upgrade packages headlessly.

--all upgrades every non-held package. --auto upgrades only packages whose
per-package update policy is Auto. --id upgrades one or more named packages
(repeatable). Held packages are skipped by --all/--auto; naming a held
package via --id is an error.

--self upgrades WinTUI itself via the same PowerShell handoff the TUI uses at
startup. The other modes deliberately skip the running WinTUI binary, so a
CLI-only user needs --self to keep WinTUI current.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		modes := 0
		if upgradeAllFlag {
			modes++
		}
		if upgradeAutoFlag {
			modes++
		}
		if upgradeSelfFlag {
			modes++
		}
		if len(upgradeIDsFlag) > 0 {
			modes++
		}
		switch {
		case modes > 1:
			return fmt.Errorf("specify only one of --all, --auto, --self, or --id")
		case upgradeAllFlag:
			return runUpgradeAll()
		case upgradeAutoFlag:
			return runUpgradeAuto()
		case upgradeSelfFlag:
			return runUpgradeSelf()
		case len(upgradeIDsFlag) > 0:
			return runUpgradeIDs(upgradeIDsFlag)
		default:
			return fmt.Errorf("specify --all, --auto, --self, or --id <package>")
		}
	},
}

var (
	doctorFullFlag     bool
	doctorDevToolsFlag bool
	doctorVerboseFlag  bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run a verdict-first WinTUI / winget readiness check",
	Long: `Print a one-line verdict (OK / WARN: N issues / FAIL: N issues) and exit
0 / 1 / 2 respectively. Shares the check engine with the Health tab.

--verbose adds the per-row table beneath the verdict.
--full re-adds the verbose system-diagnostics rows (RAM, Defender, ping,
extra drives, OS/uptime, PATH, Windows PowerShell) that were trimmed from
the slim TUI default.
--dev-tools appends a developer-tools detection group.
--json emits the full report as structured JSON.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor(doctorOptions{
			Full:     doctorFullFlag,
			DevTools: doctorDevToolsFlag,
			Verbose:  doctorVerboseFlag,
			JSON:     jsonFlag,
		}, os.Stdout)
	},
}

type doctorOptions struct {
	Full     bool
	DevTools bool
	Verbose  bool
	JSON     bool
}

type doctorCounts struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Info int `json:"info"`
}

type doctorOutput struct {
	Verdict  string        `json:"verdict"` // OK | WARN | FAIL
	Summary  string        `json:"summary"` // "OK" | "WARN: 2 issues" | "FAIL: 1 issue"
	ExitCode int           `json:"exit_code"`
	Counts   doctorCounts  `json:"counts"`
	Checks   []healthCheck `json:"checks"`
}

func buildDoctorOutput(report healthReport) doctorOutput {
	var counts doctorCounts
	for _, c := range report.Checks {
		switch strings.ToUpper(c.Status) {
		case "PASS":
			counts.Pass++
		case "WARN":
			counts.Warn++
		case "FAIL":
			counts.Fail++
		case "INFO":
			counts.Info++
		}
	}

	verdict := "OK"
	exit := 0
	summary := "OK"
	switch {
	case counts.Fail > 0:
		verdict = "FAIL"
		exit = 2
		summary = fmt.Sprintf("FAIL: %s", pluralIssues(counts.Fail))
	case counts.Warn > 0:
		verdict = "WARN"
		exit = 1
		summary = fmt.Sprintf("WARN: %s", pluralIssues(counts.Warn))
	}

	return doctorOutput{
		Verdict:  verdict,
		Summary:  summary,
		ExitCode: exit,
		Counts:   counts,
		Checks:   report.Checks,
	}
}

func pluralIssues(n int) string {
	if n == 1 {
		return "1 issue"
	}
	return fmt.Sprintf("%d issues", n)
}

func runDoctor(opts doctorOptions, out io.Writer) error {
	report := runDoctorReport(opts.Full, opts.DevTools)
	result := buildDoctorOutput(report)
	cliExitCode = result.ExitCode

	if opts.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintln(out, result.Summary)
	if opts.Verbose {
		fmt.Fprintln(out)
		printDoctorTable(out, report.Checks)
	}
	return nil
}

func printDoctorTable(out io.Writer, checks []healthCheck) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tCHECK\tDETAILS")
	for _, c := range checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Status, c.Check, c.Details)
	}
	_ = tw.Flush()

	var recs []string
	for _, c := range checks {
		if c.Status != "PASS" && c.Status != "INFO" && c.Recommendation != "" {
			recs = appendUnique(recs, c.Recommendation)
		}
	}
	if len(recs) > 0 {
		fmt.Fprintln(out, "\nRecommendations:")
		for _, r := range recs {
			fmt.Fprintf(out, "  • %s\n", r)
		}
	}
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

func runList(query string) error {
	pkgs, err := getInstalledCtx(context.Background())
	if err != nil {
		return err
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q != "" {
		pkgs = filterPackagesByQuery(pkgs, q)
		if len(pkgs) == 0 {
			// A query with no matches is a "not installed" answer — exit 1 so
			// `wintui list firefox` works as a predicate (mirrors winget list).
			cliExitCode = 1
		}
	}

	if jsonFlag {
		return printJSON(pkgs)
	}

	if len(pkgs) == 0 {
		if q != "" {
			fmt.Printf("No installed package matching %q.\n", query)
		} else {
			fmt.Println("No packages installed.")
		}
		return nil
	}

	printPackageTable(
		[]string{"Name", "ID", "Version", "Source"},
		func(p Package) []string { return []string{p.Name, p.ID, p.Version, p.Source} },
		pkgs,
	)
	if q != "" {
		fmt.Printf("\n%s\n", cliAccent(fmt.Sprintf("%d package(s) matching %q.", len(pkgs), query)))
	} else {
		fmt.Printf("\n%s\n", cliAccent(fmt.Sprintf("%d package(s) installed.", len(pkgs))))
	}
	return nil
}

// filterPackagesByQuery returns packages whose name or id contains qLower
// (already lowercased), matching winget list's case-insensitive substring style.
func filterPackagesByQuery(pkgs []Package, qLower string) []Package {
	out := make([]Package, 0, len(pkgs))
	for _, p := range pkgs {
		if strings.Contains(strings.ToLower(p.Name), qLower) ||
			strings.Contains(strings.ToLower(p.ID), qLower) {
			out = append(out, p)
		}
	}
	return out
}

func runCheck() error {
	raw, err := getUpgradeableCtx(context.Background())
	if err != nil {
		return err
	}

	// Route through the shared planner so check honors the same package
	// policy the TUI does. Held packages must not flip the exit code.
	pkgs, _ := selectUpgrades(raw, appSettings)

	switch {
	case jsonFlag:
		if err := printJSON(pkgs); err != nil {
			return err
		}
	case checkNotesFlag:
		printCheckWithNotes(context.Background(), pkgs, os.Stdout)
	case len(pkgs) == 0:
		fmt.Println(cliSuccess("All packages are up to date."))
	default:
		printPackageTable(
			[]string{"Name", "ID", "Version", "Available"},
			func(p Package) []string { return []string{p.Name, p.ID, p.Version, p.Available} },
			pkgs,
		)
		fmt.Printf("\n%s\n", cliAccent(fmt.Sprintf("%d package(s) have updates available.", len(pkgs))))
	}

	// Exit code 1 if updates exist, 0 if up to date.
	if len(pkgs) > 0 {
		cliExitCode = 1
	}
	notifyCheckFoundUpdates(len(pkgs))
	return nil
}

// printCheckWithNotes renders each pending update's target-version release notes
// inline — the built-in form of `wintui check --json | … | wintui notes`. Notes
// are fetched one package at a time (each is a winget call), so this is heavier
// than a plain check; it's meant for the "review before upgrading" flow.
func printCheckWithNotes(ctx context.Context, pkgs []Package, out io.Writer) {
	if len(pkgs) == 0 {
		fmt.Fprintln(out, cliSuccess("All packages are up to date."))
		return
	}
	fmt.Fprintf(out, "%s\n", cliAccent(fmt.Sprintf("%d package(s) have updates available. Fetching release notes…", len(pkgs))))
	for _, p := range pkgs {
		name := p.Name
		if name == "" {
			name = p.ID
		}
		header := fmt.Sprintf("══ %s (%s)  %s → %s ══", name, p.ID, p.Version, p.Available)
		fmt.Fprintf(out, "\n%s\n", styleNotesHeader(header))
		detail, err := notesFetchFn(ctx, p.ID, p.Source)
		if err != nil {
			fmt.Fprintf(out, "  %s\n", cliDanger(fmt.Sprintf("(couldn't load notes: %v)", err)))
			continue
		}
		renderNotesBody(detail, out)
	}
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
	switch source {
	case "":
		source = "winget"
	case "winget", "msstore":
		// OK; the underlying winget arg builders only honor these two today.
	default:
		return fmt.Errorf("invalid --source %q: must be 'winget' or 'msstore'", source)
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
		if normalizeUpdatePolicy(out.Override.UpdatePolicy) != PolicyAsk {
			fmt.Printf("  update_policy:  %s\n", out.Override.UpdatePolicy)
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

// streamUpgradeFn is the per-package upgrade dispatcher used by upgradeAll.
// Tests replace this with a stub so the self-upgrade skip path can be
// exercised without invoking winget.
var streamUpgradeFn = streamUpgradeToStdout

// runUpgradeAll is the cobra entry point: loads the upgradeable list from
// winget and dispatches to upgradeAll for the actual loop. The loop is
// extracted so it can be unit-tested without a winget call.
func runUpgradeAll() error {
	ctx := context.Background()
	raw, err := getUpgradeableCtx(ctx)
	if err != nil {
		return err
	}
	return upgradeAll(ctx, raw, appSettings, os.Stdout)
}

func runUpgradeAuto() error {
	ctx := context.Background()
	raw, err := getUpgradeableCtx(ctx)
	if err != nil {
		return err
	}
	return upgradeAuto(ctx, raw, appSettings, os.Stdout)
}

func runUpgradeIDs(ids []string) error {
	ctx := context.Background()
	raw, err := getUpgradeableCtx(ctx)
	if err != nil {
		return err
	}
	return upgradeIDs(ctx, ids, raw, appSettings, os.Stdout)
}

// upgradeIDs upgrades each requested ID using the cached upgradeable list.
// IDs not present in the list are reported as "no update available" (exit 0).
// Held packages produce an error (exit 1) — naming a held package via --id is
// treated as a sign the user forgot the policy. Self-package is skipped with
// the same hint as --all/--auto and does not flip the exit code.
func upgradeIDs(ctx context.Context, ids []string, raw []Package, settings Settings, out io.Writer) error {
	upgradeable := make(map[string]Package, len(raw))
	for _, pkg := range raw {
		upgradeable[strings.ToLower(strings.TrimSpace(pkg.ID))] = pkg
	}

	requested := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		requested = append(requested, id)
	}
	if len(requested) == 0 {
		return fmt.Errorf("specify --id <package>")
	}

	fmt.Fprintf(out, "Upgrading %d package(s) by ID:\n", len(requested))

	var (
		failures    []string
		held        []string
		notFound    []string
		skippedSelf bool
		upgraded    int
	)

	for _, id := range requested {
		key := strings.ToLower(id)
		pkg, ok := upgradeable[key]
		if !ok {
			fmt.Fprintf(out, "\n%s\n  • no update available\n", cliAccent("→ "+id))
			notFound = append(notFound, id)
			continue
		}

		fmt.Fprintf(out, "\n%s\n", cliAccent(fmt.Sprintf("→ %s (%s) %s → %s", pkg.Name, pkg.ID, pkg.Version, pkg.Available)))

		if settings.updatePolicy(pkg.ID, pkg.Source, pkg.Available) == PolicyHold {
			fmt.Fprintln(out, "  "+cliDanger("✗ held by policy.")+" Remove the hold from settings or use the TUI.")
			held = append(held, pkg.ID)
			continue
		}
		if isSelfPackageID(pkg.ID) && isRunningInstalledWinTUI() {
			fmt.Fprintln(out, "  • skipped: WinTUI can't upgrade itself in a batch. Run 'wintui upgrade --self' (or launch the TUI) to update WinTUI.")
			skippedSelf = true
			continue
		}
		if err := streamUpgradeFn(ctx, pkg, out); err != nil {
			fmt.Fprintf(out, "  %s\n", cliDanger(fmt.Sprintf("✗ failed: %v", err)))
			failures = append(failures, pkg.ID)
		} else {
			fmt.Fprintln(out, "  "+cliSuccess("✓ upgraded"))
			upgraded++
		}
	}

	fmt.Fprintf(out, "\n%d/%d succeeded.", upgraded, len(requested))
	if len(failures) > 0 {
		fmt.Fprintf(out, " %s", cliDanger("Failed: "+strings.Join(failures, ", ")))
		cliExitCode = 1
	}
	if len(held) > 0 {
		fmt.Fprintf(out, " %s", cliDanger("Held: "+strings.Join(held, ", ")))
		cliExitCode = 1
	}
	if len(notFound) > 0 {
		fmt.Fprintf(out, " No update: %s", strings.Join(notFound, ", "))
	}
	if skippedSelf {
		fmt.Fprint(out, " (WinTUI self-upgrade skipped)")
	}
	fmt.Fprintln(out)
	notifyCLIUpgradeFinish(upgraded, len(failures))
	return nil
}

// upgradeAll runs `winget upgrade` for every visible upgradeable package
// (i.e. those not held by policy), streaming output to out and
// reporting per-package success/failure. Sets cliExitCode = 1 if any failed.
//
// The running WinTUI binary is skipped: the TUI hands self-upgrades off to
// a PowerShell script that waits for wintui.exe to exit, then runs winget.
// Replicating that dance from a one-shot CLI is fragile, so we point the
// user at the TUI's verified path instead.
func upgradeAll(ctx context.Context, raw []Package, settings Settings, out io.Writer) error {
	plan := planUpgrades(raw, settings)
	return upgradePlanned(ctx, plan.Visible, plan.HiddenCount(), "visible", out)
}

func upgradeAuto(ctx context.Context, raw []Package, settings Settings, out io.Writer) error {
	plan := planUpgrades(raw, settings)
	return upgradePlanned(ctx, plan.Auto, plan.HiddenCount(), "auto", out)
}

func upgradePlanned(ctx context.Context, pkgs []Package, held int, mode string, out io.Writer) error {
	if len(pkgs) == 0 {
		switch {
		case mode == "auto" && held > 0:
			fmt.Fprintf(out, "No auto-update packages have updates available (%d held by policy).\n", held)
		case mode == "auto":
			fmt.Fprintln(out, "No auto-update packages have updates available.")
		case held > 0:
			fmt.Fprintf(out, "All non-held packages are up to date (%d held by policy).\n", held)
		default:
			fmt.Fprintln(out, "All packages are up to date.")
		}
		return nil
	}

	if mode == "auto" {
		fmt.Fprintf(out, "Auto-upgrading %d package(s)", len(pkgs))
	} else {
		fmt.Fprintf(out, "Upgrading %d package(s)", len(pkgs))
	}
	if held > 0 {
		fmt.Fprintf(out, " (%d held by policy)", held)
	}
	fmt.Fprintln(out, ":")

	var failures []string
	var skippedSelf bool
	for _, pkg := range pkgs {
		fmt.Fprintf(out, "\n%s\n", cliAccent(fmt.Sprintf("→ %s (%s) %s → %s", pkg.Name, pkg.ID, pkg.Version, pkg.Available)))
		if isSelfPackageID(pkg.ID) && isRunningInstalledWinTUI() {
			fmt.Fprintln(out, "  • skipped: WinTUI can't upgrade itself in a batch. Run 'wintui upgrade --self' (or launch the TUI) to update WinTUI.")
			skippedSelf = true
			continue
		}
		if err := streamUpgradeFn(ctx, pkg, out); err != nil {
			fmt.Fprintf(out, "  %s\n", cliDanger(fmt.Sprintf("✗ failed: %v", err)))
			failures = append(failures, pkg.ID)
		} else {
			fmt.Fprintln(out, "  "+cliSuccess("✓ upgraded"))
		}
	}

	// The skipped self-package is excluded from both sides of the summary:
	// counting it in the denominator made a clean run read "0/1 succeeded"
	// when WinTUI was the only pending update.
	attempted := len(pkgs)
	if skippedSelf {
		attempted--
	}
	upgraded := attempted - len(failures)
	if attempted == 0 && skippedSelf {
		fmt.Fprintln(out, "\nNothing upgraded: only WinTUI itself had an update. Run 'wintui upgrade --self'.")
		return nil
	}
	fmt.Fprintf(out, "\n%d/%d succeeded.", upgraded, attempted)
	if len(failures) > 0 {
		fmt.Fprintf(out, " %s", cliDanger("Failed: "+strings.Join(failures, ", ")))
		cliExitCode = 1
	}
	if skippedSelf {
		fmt.Fprint(out, " (WinTUI self-upgrade skipped)")
	}
	fmt.Fprintln(out)
	notifyCLIUpgradeFinish(upgraded, len(failures))
	return nil
}

// streamUpgradeToStdout drives a single package upgrade through the same
// streaming pipeline the TUI uses, indenting each output line under out.
// Returns the final winget error (or nil on success).
func streamUpgradeToStdout(ctx context.Context, pkg Package, out io.Writer) error {
	_, outChan, errChan := upgradePackageStreamCtx(ctx, pkg, "")
	for line := range outChan {
		// Skip the TUI's progress sentinels; they are not human-readable.
		if _, isProgress := parseProgressSentinel(line); isProgress {
			continue
		}
		if line != "" {
			fmt.Fprintln(out, "  "+line)
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
