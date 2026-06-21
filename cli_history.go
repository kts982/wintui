package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	historyLimitFlag      int
	historySinceFlag      string
	historyFailedOnlyFlag bool
)

// historyLoadFn is the reader behind `wintui history`, a var so tests inject
// synthetic records instead of reading %APPDATA%.
var historyLoadFn = loadHistory

var historyCmd = &cobra.Command{
	Use:   "history [id]",
	Short: "Show WinTUI's record of install/upgrade/uninstall operations",
	Long: `List the operations WinTUI itself has run (Tier 1), newest first. Pass a
package id to see that package's timeline (Tier 2).

History records ONLY actions WinTUI runs -- TUI batches and the headless
'wintui upgrade' / 'wintui import' commands (so scheduled runs leave a trace).
It does NOT capture upgrades you run from plain winget, Microsoft Store
auto-updates, or other tools: winget exposes no event feed to observe those, so
history will not match 'winget list' deltas.

Exit codes: 0 normally; 1 when a selector/filter (an id, --since, or
--failed-only) matches nothing -- so 'wintui history <id>' works as a "did
WinTUI ever touch this?" predicate. An empty unfiltered history is not an error.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeHistoryIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		return runHistory(historyOptions{
			ID:         id,
			Limit:      historyLimitFlag,
			Since:      historySinceFlag,
			FailedOnly: historyFailedOnlyFlag,
			JSON:       jsonFlag,
		}, os.Stdout)
	},
}

type historyOptions struct {
	ID         string // positional package id => Tier2 timeline; empty => Tier1 batch list
	Limit      int    // 0 = no cap
	Since      string // Go duration (e.g. "168h"); empty = no time filter
	FailedOnly bool
	JSON       bool
}

func runHistory(opts historyOptions, out io.Writer) error {
	env, err := historyLoadFn()
	if err != nil {
		// Corrupt / unsupported-version: a real error (fang styles it, exit 1).
		return err
	}

	var since time.Time
	if s := strings.TrimSpace(opts.Since); s != "" {
		dur, perr := time.ParseDuration(s)
		if perr != nil {
			return fmt.Errorf("invalid --since %q: use a Go duration like 168h or 30m", opts.Since)
		}
		since = time.Now().Add(-dur)
	}

	// "filtered" decides the empty-result exit code: a selector/filter that
	// matches nothing exits 1 (predicate); an unfiltered empty history exits 0.
	filtered := opts.ID != "" || !since.IsZero() || opts.FailedOnly

	if opts.ID != "" {
		return historyTimeline(env, opts, since, out)
	}
	return historyBatches(env, opts, since, filtered, out)
}

// ── Tier 1: batch list ─────────────────────────────────────────────

func historyBatches(env historyEnvelope, opts historyOptions, since time.Time, filtered bool, out io.Writer) error {
	recs := make([]historyRecord, 0, len(env.Records))
	for i := len(env.Records) - 1; i >= 0; i-- { // newest-first
		r := env.Records[i]
		if !since.IsZero() && r.Timestamp.Before(since) {
			continue
		}
		if opts.FailedOnly && r.Summary.Failed == 0 {
			continue
		}
		recs = append(recs, r)
	}

	total := len(recs)
	if opts.Limit > 0 && len(recs) > opts.Limit {
		recs = recs[:opts.Limit]
	}

	if opts.JSON {
		payload := historyBatchesJSON{View: "batches", Count: len(recs), Batches: []historyBatchJSON{}}
		for _, r := range recs {
			payload.Batches = append(payload.Batches, batchToJSON(r))
		}
		return writeHistoryJSON(out, payload)
	}

	if total == 0 {
		if filtered {
			cliExitCode = 1
			fmt.Fprintln(out, "No matching history records.")
		} else {
			fmt.Fprintln(out, "No action history yet. WinTUI records the install/upgrade/uninstall operations it runs.")
		}
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tOP\tPACKAGES\tOK\tFAIL\tVIA")
	for _, r := range recs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n",
			r.Timestamp.Local().Format("2006-01-02 15:04"),
			r.Action,
			summarizeHistoryPackages(r.Items),
			r.Summary.OK, r.Summary.Failed, r.Trigger)
	}
	_ = tw.Flush()

	summary := fmt.Sprintf("%d batch(es)", total)
	if len(recs) < total {
		summary += fmt.Sprintf(" • showing most recent %d", len(recs))
	}
	summary += " • run 'wintui history <id>' for a package timeline"
	fmt.Fprintln(out, cliAccent("\n"+summary))
	return nil
}

// ── Tier 2: per-package timeline (a query over batch items) ────────

func historyTimeline(env historyEnvelope, opts historyOptions, since time.Time, out io.Writer) error {
	idLower := strings.ToLower(strings.TrimSpace(opts.ID))

	type entry struct {
		ts      time.Time
		batchID string
		item    historyItem
	}
	var entries []entry
	for i := len(env.Records) - 1; i >= 0; i-- { // newest-first
		r := env.Records[i]
		if !since.IsZero() && r.Timestamp.Before(since) {
			continue
		}
		for _, it := range r.Items {
			if strings.ToLower(it.ID) != idLower {
				continue
			}
			if opts.FailedOnly && it.Status != historyStatusError {
				continue
			}
			entries = append(entries, entry{ts: r.Timestamp, batchID: r.ID, item: it})
		}
	}

	total := len(entries)
	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[:opts.Limit]
	}

	if opts.JSON {
		payload := historyTimelineJSON{View: "timeline", PackageID: opts.ID, Count: len(entries), Records: []historyTimelineEntryJSON{}}
		for _, e := range entries {
			payload.Records = append(payload.Records, timelineToJSON(e.ts, e.batchID, e.item))
		}
		return writeHistoryJSON(out, payload)
	}

	if total == 0 {
		// A selected id with no records is a predicate miss.
		cliExitCode = 1
		fmt.Fprintf(out, "No history for %q.\n", opts.ID)
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WHEN\tOP\tFROM\tTO\tSTATUS\tNOTES")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			e.ts.Local().Format("2006-01-02 15:04"),
			e.item.Action,
			dashIfEmpty(e.item.FromVersion),
			dashIfEmpty(e.item.ToVersion),
			e.item.Status,
			timelineNote(e.item))
	}
	_ = tw.Flush()

	summary := fmt.Sprintf("%s • %d action(s) recorded", opts.ID, total)
	if len(entries) < total {
		summary += fmt.Sprintf(" • showing most recent %d", len(entries))
	}
	fmt.Fprintln(out, cliAccent("\n"+summary))
	return nil
}

// ── Completion ─────────────────────────────────────────────────────

// completeHistoryIDs completes `wintui history <TAB>` with package ids drawn
// from history.json (never winget; completion runs in a cold process). Only the
// first positional is completed.
func completeHistoryIDs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	env, err := historyLoadFn()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix := strings.ToLower(toComplete)
	seen := make(map[string]bool)
	var out []string
	for i := len(env.Records) - 1; i >= 0; i-- {
		for _, it := range env.Records[i].Items {
			if it.ID == "" || seen[it.ID] {
				continue
			}
			if prefix != "" && !strings.HasPrefix(strings.ToLower(it.ID), prefix) {
				continue
			}
			seen[it.ID] = true
			out = append(out, it.ID+"\t"+strings.TrimSpace(it.Name))
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// ── Rendering helpers ──────────────────────────────────────────────

func summarizeHistoryPackages(items []historyItem) string {
	const maxShown = 3
	names := make([]string, 0, len(items))
	for _, it := range items {
		n := it.Name
		if strings.TrimSpace(n) == "" {
			n = it.ID
		}
		names = append(names, n)
	}
	if len(names) <= maxShown {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:maxShown], ", ") + fmt.Sprintf(" (+%d)", len(names)-maxShown)
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func timelineNote(it historyItem) string {
	if it.Notes != "" {
		return it.Notes
	}
	if it.Status == historyStatusError && it.Error != "" {
		return truncate(it.Error, 50)
	}
	return ""
}

func writeHistoryJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ── JSON payloads ──────────────────────────────────────────────────

type historyBatchesJSON struct {
	View    string             `json:"view"`
	Count   int                `json:"count"`
	Batches []historyBatchJSON `json:"batches"`
}

type historyBatchJSON struct {
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Trigger    string    `json:"trigger"`
	Action     string    `json:"action"`
	OK         int       `json:"ok"`
	Failed     int       `json:"failed"`
	PackageIDs []string  `json:"package_ids"`
}

func batchToJSON(r historyRecord) historyBatchJSON {
	ids := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		ids = append(ids, it.ID)
	}
	return historyBatchJSON{
		ID:         r.ID,
		Timestamp:  r.Timestamp,
		Trigger:    r.Trigger,
		Action:     r.Action,
		OK:         r.Summary.OK,
		Failed:     r.Summary.Failed,
		PackageIDs: ids,
	}
}

type historyTimelineJSON struct {
	View      string                     `json:"view"`
	PackageID string                     `json:"package_id"`
	Count     int                        `json:"count"`
	Records   []historyTimelineEntryJSON `json:"records"`
}

type historyTimelineEntryJSON struct {
	Timestamp   time.Time `json:"timestamp"`
	BatchID     string    `json:"batch_id"`
	Action      string    `json:"action"`
	Source      string    `json:"source,omitempty"`
	FromVersion string    `json:"from_version,omitempty"`
	ToVersion   string    `json:"to_version,omitempty"`
	Status      string    `json:"status"`
	Error       string    `json:"error_msg,omitempty"`
	Notes       string    `json:"notes,omitempty"`
}

func timelineToJSON(ts time.Time, batchID string, it historyItem) historyTimelineEntryJSON {
	return historyTimelineEntryJSON{
		Timestamp:   ts,
		BatchID:     batchID,
		Action:      it.Action,
		Source:      it.Source,
		FromVersion: it.FromVersion,
		ToVersion:   it.ToVersion,
		Status:      it.Status,
		Error:       it.Error,
		Notes:       it.Notes,
	}
}
