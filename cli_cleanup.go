package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// `wintui cleanup scan` — READ-ONLY cleanup status from the CLI. It answers
// the biweekly "how much junk has piled up?" question without opening the
// TUI, and deliberately cannot delete anything: headless deletion is deferred
// until scan usage proves demand (roadmap v2.12.0, slice 3).
//
// Contract points from the design review:
//   - Every registered target appears in the bare listing, present or not,
//     so "missing" / "unresolved" reasons are reachable. --enabled and
//     --target narrow the list.
//   - --enabled means exactly one thing: the targets that start CHECKED in
//     the TUI (default-checked plus opted-in, i.e. Settings.cleanupTargetEnabled)
//     — the set a TUI deletion would act on. Not the auto-scan "safe" set.
//   - Admin-only targets are never routed through the elevated helper. In a
//     non-elevated process they short-circuit BEFORE walking (status
//     needs_admin, size null): the engine would otherwise return a partial,
//     wrong size on access-denied. An already-elevated invocation scans them.
//   - A size walk that skipped unreadable entries is reported as "partial"
//     with the size as a lower bound, never as a clean "0 B".
//   - JSON: string status, orthogonal admin state, explicit null (never
//     omission) for unmeasured size_bytes / items, zeros always present,
//     elevated at top level, arrays never null.

var (
	cleanupJSONFlag        bool
	cleanupScanEnabledFlag bool
	cleanupScanTargetFlags []string
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Inspect cleanup targets (read-only)",
	Long: `Inspect the disk-cleanup targets the TUI's Cleanup tab manages.

"cleanup scan" measures what each target would reclaim without deleting
anything. Deleting stays in the TUI (Cleanup tab), where every removal is
reviewed and confirmed.

Examples:
  wintui cleanup scan
  wintui cleanup scan --enabled
  wintui cleanup scan --target user_temp --target npm_cache
  wintui cleanup scan --json`,
}

var cleanupScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Measure what each cleanup target would reclaim, without deleting",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCleanupScan(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cleanupScanOptions{
			Enabled: cleanupScanEnabledFlag,
			Targets: cleanupScanTargetFlags,
		}, cleanupJSONFlag)
	},
}

func init() {
	cleanupCmd.PersistentFlags().BoolVar(&cleanupJSONFlag, "json", false, "Output JSON")
	cleanupScanCmd.Flags().BoolVar(&cleanupScanEnabledFlag, "enabled", false, "Only the targets that start checked in the TUI (default-checked plus opted-in)")
	cleanupScanCmd.Flags().StringArrayVar(&cleanupScanTargetFlags, "target", nil, "Scan only this target ID (repeatable)")
	_ = cleanupScanCmd.RegisterFlagCompletionFunc("target", completeCleanupTargetIDs)
	cleanupCmd.AddCommand(cleanupScanCmd)
}

// Seams for tests: the registry (temp-dir targets), the elevation probe, and
// the scan itself.
var (
	cleanupScanRegistryFn = cleanupTargetRegistry
	cleanupScanElevatedFn = func() bool { return isElevated() }
	cleanupScanFn         = cleanupScan
)

// cleanupScanConcurrency bounds parallel target walks. The TUI fires every
// scan at once; four keeps a bare all-target scan short without hammering
// the disk.
const cleanupScanConcurrency = 4

type cleanupScanOptions struct {
	Enabled bool
	Targets []string
}

// Status vocabulary (JSON string, table word).
const (
	cleanupStatusOK         = "ok"          // scanned, has reclaimable entries
	cleanupStatusEmpty      = "empty"       // scanned, nothing to reclaim
	cleanupStatusMissing    = "missing"     // resolved path does not exist
	cleanupStatusUnresolved = "unresolved"  // env var missing / no path
	cleanupStatusPartial    = "partial"     // scanned, but some entries were unreadable: size is a lower bound
	cleanupStatusNeedsAdmin = "needs_admin" // requires elevation; not scanned in this process
	cleanupStatusSkipped    = "skipped"     // engine guard (reparse point root, etc.)
	cleanupStatusError      = "error"       // could not scan
)

type cleanupScanTargetJSON struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Group          string   `json:"group"`
	GroupLabel     string   `json:"group_label"`
	Path           string   `json:"path"`
	Mode           string   `json:"mode"`
	Globs          []string `json:"globs"`
	MinAgeSeconds  int64    `json:"min_age_seconds"`
	RequiresAdmin  bool     `json:"requires_admin"`
	DefaultChecked bool     `json:"default_checked"`
	Enabled        bool     `json:"enabled"`
	Present        bool     `json:"present"`
	Scanned        bool     `json:"scanned"`
	Status         string   `json:"status"`
	SizeBytes      *int64   `json:"size_bytes"`
	Items          *int     `json:"items"`
	Unreadable     int      `json:"unreadable"`
	Errors         []string `json:"errors"`
}

type cleanupScanJSON struct {
	Elevated       bool                    `json:"elevated"`
	Selection      string                  `json:"selection"` // all | enabled | targets
	Count          int                     `json:"count"`
	Scanned        int                     `json:"scanned"`
	NeedsAdmin     int                     `json:"needs_admin"`
	TotalSizeBytes int64                   `json:"total_size_bytes"`
	Partial        bool                    `json:"partial"` // a scanned target was only partly readable: the total is a lower bound
	Targets        []cleanupScanTargetJSON `json:"targets"`
}

func cleanupModeName(m cleanupMode) string {
	if m == cleanupModeGlob {
		return "glob"
	}
	return "purge_contents"
}

// cleanupScanStatus derives the public status from a finished engine result.
func cleanupScanStatus(r cleanupTargetResult) string {
	switch r.skipped {
	case cleanupSkipUnresolved:
		return cleanupStatusUnresolved
	case cleanupSkipMissing:
		return cleanupStatusMissing
	case cleanupSkipGuarded:
		return cleanupStatusSkipped
	case cleanupSkipNotElevated:
		return cleanupStatusNeedsAdmin
	}
	switch {
	case len(r.errors) > 0 && r.files == 0:
		return cleanupStatusError
	case r.unreadable > 0 || r.failed > 0 || len(r.errors) > 0:
		return cleanupStatusPartial
	case r.files == 0:
		return cleanupStatusEmpty
	}
	return cleanupStatusOK
}

// runCleanupScan is the runner behind `wintui cleanup scan`.
func runCleanupScan(ctx context.Context, out, errOut io.Writer, opts cleanupScanOptions, asJSON bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Enabled && len(opts.Targets) > 0 {
		return fmt.Errorf("--enabled and --target are mutually exclusive")
	}
	registry := cleanupScanRegistryFn()
	settings := currentSettings()
	elevated := cleanupScanElevatedFn()

	// Selection.
	selection := "all"
	wanted := map[string]bool{}
	if len(opts.Targets) > 0 {
		selection = "targets"
		known := make([]string, 0, len(registry))
		for _, def := range registry {
			known = append(known, def.id)
		}
		for _, t := range opts.Targets {
			id := strings.ToLower(strings.TrimSpace(t))
			if !slices.Contains(known, id) {
				return fmt.Errorf("unknown cleanup target %q: expected one of %s", t, strings.Join(known, ", "))
			}
			wanted[id] = true
		}
	} else if opts.Enabled {
		selection = "enabled"
	}

	var defs []cleanupTargetDef
	for _, def := range registry {
		switch selection {
		case "targets":
			if !wanted[def.id] {
				continue
			}
		case "enabled":
			if !settings.cleanupTargetEnabled(def) {
				continue
			}
		}
		defs = append(defs, def)
	}

	// Pre-scan classification (cheap: resolve + stat), then bounded-parallel
	// walks for the scannable ones, results kept in registry order.
	rows := make([]cleanupScanTargetJSON, len(defs))
	type job struct {
		idx int
		def cleanupTargetDef
	}
	var jobs []job
	for i, def := range defs {
		path, exists := cleanupResolveTarget(def)
		row := cleanupScanTargetJSON{
			ID:             def.id,
			Label:          def.label,
			Group:          string(def.group),
			GroupLabel:     cleanupGroupLabels[def.group],
			Path:           path,
			Mode:           cleanupModeName(def.mode),
			Globs:          append([]string{}, def.globs...),
			MinAgeSeconds:  int64(def.minAge.Seconds()),
			RequiresAdmin:  def.requiresAdmin,
			DefaultChecked: def.defaultChecked,
			Enabled:        settings.cleanupTargetEnabled(def),
			Present:        path != "" && exists,
			Errors:         []string{},
		}
		switch {
		case path == "":
			row.Status = cleanupStatusUnresolved
		case !exists:
			row.Status = cleanupStatusMissing
		case def.requiresAdmin && !elevated:
			row.Status = cleanupStatusNeedsAdmin
		default:
			jobs = append(jobs, job{i, def})
		}
		rows[i] = row
	}

	if !asJSON && len(jobs) > 0 {
		fmt.Fprintf(errOut, "Scanning %s…\n", pluralize(len(jobs), "target"))
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, cleanupScanConcurrency)
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := cleanupScanFn(ctx, j.def)
			row := &rows[j.idx]
			row.Scanned = res.skipped == cleanupSkipNone
			row.Status = cleanupScanStatus(res)
			if res.resolvedPath != "" {
				row.Path = res.resolvedPath
			}
			if row.Scanned && row.Status != cleanupStatusError {
				size, items := res.sizeBytes, res.files
				row.SizeBytes = &size
				row.Items = &items
			}
			row.Unreadable = res.unreadable
			for _, e := range res.errors {
				if e != nil {
					row.Errors = append(row.Errors, e.Error())
				}
			}
		}(j)
	}
	wg.Wait()

	report := cleanupScanJSON{Elevated: elevated, Selection: selection, Count: len(rows), Targets: rows}
	for _, r := range rows {
		if r.Scanned {
			report.Scanned++
		}
		if r.Status == cleanupStatusNeedsAdmin {
			report.NeedsAdmin++
		}
		if r.SizeBytes != nil {
			report.TotalSizeBytes += *r.SizeBytes
		}
		if r.Status == cleanupStatusPartial {
			report.Partial = true
		}
	}
	if asJSON {
		return writeJSON(out, report)
	}
	printCleanupScanTable(out, report)
	return nil
}

func printCleanupScanTable(out io.Writer, report cleanupScanJSON) {
	if len(report.Targets) == 0 {
		fmt.Fprintln(out, "No cleanup targets selected.")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tGROUP\tENABLED\tSIZE\tITEMS\tADMIN\tSTATUS")
	for _, r := range report.Targets {
		enabled := "no"
		if r.Enabled {
			enabled = "yes"
		}
		size, items := "-", "-"
		if r.SizeBytes != nil {
			size = formatBytes(*r.SizeBytes)
			if r.Status == cleanupStatusPartial {
				size = "≥ " + size
			}
		}
		if r.Items != nil {
			items = fmt.Sprintf("%d", *r.Items)
		}
		admin := "-"
		switch {
		case r.RequiresAdmin && report.Elevated:
			admin = "ok"
		case r.RequiresAdmin:
			admin = "needs admin"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.GroupLabel, enabled, size, items, admin, r.Status)
	}
	_ = tw.Flush()

	total := formatBytes(report.TotalSizeBytes)
	if report.Partial {
		total = "≥ " + total + " (some targets were only partly readable)"
	}
	summary := fmt.Sprintf("%s scanned, %s reclaimable", pluralize(report.Scanned, "target"), total)
	if report.NeedsAdmin > 0 {
		summary += fmt.Sprintf(" · %d need admin (run elevated to measure)", report.NeedsAdmin)
	}
	if !report.Elevated {
		summary += " · not elevated"
	}
	fmt.Fprintf(out, "\n%s\n", cliAccent(summary))
	for _, r := range report.Targets {
		if len(r.Errors) == 0 {
			continue
		}
		shown := r.Errors
		if len(shown) > 2 {
			shown = shown[:2]
		}
		for _, e := range shown {
			fmt.Fprintf(out, "  %s: %s\n", r.ID, e)
		}
		if len(r.Errors) > 2 {
			fmt.Fprintf(out, "  %s: … and %d more\n", r.ID, len(r.Errors)-2)
		}
	}
	fmt.Fprintln(out, "Deletion stays in the TUI (Cleanup tab), where every removal is reviewed and confirmed.")
}

func completeCleanupTargetIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var out []string
	for _, def := range cleanupScanRegistryFn() {
		if strings.HasPrefix(def.id, strings.ToLower(toComplete)) {
			out = append(out, def.id+"\t"+def.label)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
