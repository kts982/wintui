package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Action history persists WinTUI-originated install/upgrade/uninstall operations
// to %APPDATA%\wintui\history.json so the CLI-first / scheduled-upgrade workflow
// leaves a trace (issue #4). This file is the storage layer only: the schema,
// the atomic reader/writer, and the size bound. Write-points (finishBatch, the
// CLI upgrade loops, the import paths) and the `wintui history` subcommand wire
// into recordHistory / loadHistory in later slices.
//
// Conventions mirror the rest of the tree verbatim so there is nothing novel for
// an AV heuristic to flag: the versioned-envelope int-version pattern from
// wintui_export.go and the temp-file + os.Rename atomic write from cache.go.

// historyEnvelopeVersion is the only version writers emit. The reader rejects a
// higher version with a clear error, and — unlike the regenerable cache — the
// writer refuses to overwrite a higher-version file (a downgrade must not
// destroy a newer binary's history).
const historyEnvelopeVersion = 1

// historyMaxRecords bounds the on-disk log. Records are appended newest-last, so
// the oldest are trimmed from the front. One record == one batch, so 1000 is
// generous (a year of daily upgrades is ~365 records).
const historyMaxRecords = 1000

// Trigger values: which surface originated the batch.
const (
	historyTriggerTUI       = "tui"        // workspace exec modal (manual, search-install, on-launch auto-update, Ctrl+E retry)
	historyTriggerTUIImport = "tui-import" // TUI import overlay install loop
	historyTriggerCLIAll    = "cli-all"    // wintui upgrade --all
	historyTriggerCLIAuto   = "cli-auto"   // wintui upgrade --auto
	historyTriggerCLIID     = "cli-id"     // wintui upgrade --id
	historyTriggerCLISelf   = "cli-self"   // wintui upgrade --self
	historyTriggerCLIImport = "cli-import" // wintui import
)

// Action values: the batch's dominant op (historyRecord.Action) and each item's
// op (historyItem.Action). Map directly from retryOp; a mixed batch uses "mixed".
const (
	historyActionUpgrade   = "upgrade"
	historyActionInstall   = "install"
	historyActionUninstall = "uninstall"
	historyActionMixed     = "mixed"
)

// Per-item status values. Frozen at schema v1 — these are wire values.
const (
	historyStatusOK      = "ok"
	historyStatusError   = "error"
	historyStatusPending = "pending" // self-upgrade deferred to restart
	historyStatusSkipped = "skipped" // held / not-found / not-attempted
)

// historyEnvelope is the on-disk JSON shape (mirrors exportEnvelope).
type historyEnvelope struct {
	Version   int             `json:"version"`
	Generator string          `json:"generator"`
	Created   time.Time       `json:"created"`
	Records   []historyRecord `json:"records"`
}

// historyRecord is one batch (one exec-modal run or one CLI invocation). Tier1
// of the design. The per-package timeline (Tier2) is a query over Records[].Items,
// not separate storage.
type historyRecord struct {
	ID        string         `json:"id"`        // time-sortable batch id, see newBatchID
	Timestamp time.Time      `json:"timestamp"` // UTC, batch completion
	Trigger   string         `json:"trigger"`   // one of the historyTrigger* values
	Action    string         `json:"action"`    // dominant op: upgrade|install|uninstall|mixed
	Items     []historyItem  `json:"items"`
	Summary   historySummary `json:"summary"`
}

// historyItem is one package within a batch.
type historyItem struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"` // source-qualified, avoids cross-source id collisions
	Action      string `json:"action"`
	FromVersion string `json:"from_version,omitempty"`
	ToVersion   string `json:"to_version,omitempty"` // intended target (Available) at action time
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	Notes       string `json:"notes,omitempty"` // e.g. the elevation-safety advisory
}

// historySummary is the per-batch roll-up, derived from Items.
type historySummary struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Failed  int `json:"failed"`
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`
}

// historyMu serializes the load-append-write of recordHistory. WinTUI's TUI and
// CLI never run in one process, so this covers rapid in-session writes (Ctrl+E
// retries, back-to-back batches). A concurrent *other* wintui process (e.g. a
// scheduled `upgrade --auto` while the TUI is open) can still last-writer-win and
// drop a record — accepted as a documented v2.10.0 limitation, same posture as
// the cache; a cross-process lock would add syscall surface we deliberately avoid.
var historyMu sync.Mutex

// historyRecordFn is the seam write-points call (and tests stub) so they never
// touch %APPDATA%.
var historyRecordFn = recordHistory

// historyFileName is the on-disk name, shared with the wintui_history cleanup
// target's glob so the two can't drift apart.
const historyFileName = "history.json"

// wintuiConfigDir returns %APPDATA%\wintui WITHOUT creating it — writers
// (historyPath, configPath) own directory creation; the cleanup scanner must
// not create state dirs as a side effect of scanning. Empty when the config
// dir can't be resolved (callers treat that as "skip").
func wintuiConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "wintui")
}

// historyPath returns %APPDATA%\wintui\history.json, mirroring diskCachePath so
// it shares the same state dir and the same MkdirAll-failure reporting.
func historyPath() string {
	dir := wintuiConfigDir()
	if dir == "" {
		dir = filepath.Join(".", "wintui")
	}
	recordStateDirResult("history dir", os.MkdirAll(dir, 0755))
	return filepath.Join(dir, historyFileName)
}

// newBatchID mints a time-sortable, collision-resistant batch id. Format:
// 20060102T150405Z-<4 hex>. math/rand (not crypto/rand) keeps the import surface
// lean; 16 bits of suffix is plenty to disambiguate same-second batches.
func newBatchID() string { return batchIDFor(time.Now()) }

func batchIDFor(t time.Time) string {
	return t.UTC().Format("20060102T150405") + "Z-" + fmt.Sprintf("%04x", rand.Intn(0x10000))
}

// summarize derives the per-batch roll-up from its items.
func summarize(items []historyItem) historySummary {
	s := historySummary{Total: len(items)}
	for _, it := range items {
		switch it.Status {
		case historyStatusOK:
			s.OK++
		case historyStatusError:
			s.Failed++
		case historyStatusPending:
			s.Pending++
		case historyStatusSkipped:
			s.Skipped++
		}
	}
	return s
}

// historyLoadKind classifies the on-disk file so the read and write paths can
// treat the same states differently (the reader surfaces corrupt/future as
// errors; the writer self-heals corrupt but refuses to clobber future).
type historyLoadKind int

const (
	historyOK historyLoadKind = iota
	historyMissing
	historyCorrupt
	historyFuture
)

func readHistoryRaw(path string) (historyEnvelope, historyLoadKind) {
	b, err := os.ReadFile(path)
	if err != nil {
		return historyEnvelope{}, historyMissing
	}
	// Probe the version first: a future schema may change a field's type and
	// fail to unmarshal into the v1 struct. Recognizing it as future here (not
	// "corrupt") preserves the no-clobber guarantee if a user downgrades WinTUI.
	var probe struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(b, &probe) == nil && probe.Version > historyEnvelopeVersion {
		return historyEnvelope{Version: probe.Version}, historyFuture
	}
	var env historyEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return historyEnvelope{}, historyCorrupt
	}
	if env.Version > historyEnvelopeVersion {
		return env, historyFuture
	}
	return env, historyOK
}

// loadHistory reads the real history file. Missing is not an error ("no history
// yet"); corrupt and future-version are.
func loadHistory() (historyEnvelope, error) { return loadHistoryFrom(historyPath()) }

func loadHistoryFrom(path string) (historyEnvelope, error) {
	env, kind := readHistoryRaw(path)
	switch kind {
	case historyMissing:
		return historyEnvelope{Version: historyEnvelopeVersion}, nil
	case historyCorrupt:
		return historyEnvelope{}, fmt.Errorf("history file is unreadable (corrupt JSON): %s", path)
	case historyFuture:
		return historyEnvelope{}, fmt.Errorf("unsupported history format v%d (this WinTUI reads up to v%d; upgrade WinTUI)", env.Version, historyEnvelopeVersion)
	default:
		return env, nil
	}
}

// recordHistory appends one batch record to the real history file.
func recordHistory(rec historyRecord) (string, error) { return recordHistoryTo(historyPath(), rec) }

func recordHistoryTo(path string, rec historyRecord) (string, error) {
	historyMu.Lock()
	defer historyMu.Unlock()

	env, kind := readHistoryRaw(path)
	if kind == historyFuture {
		// Never downgrade-and-destroy a newer binary's history.
		return "", fmt.Errorf("refusing to overwrite newer history format v%d", env.Version)
	}
	if kind != historyOK {
		// Missing or corrupt: start fresh (corrupt self-heals on next write).
		env = historyEnvelope{}
	}

	if rec.ID == "" {
		rec.ID = newBatchID()
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	rec.Summary = summarize(rec.Items) // invariant: summary always derives from items

	env.Records = append(env.Records, rec)
	boundHistoryRecords(&env)
	env.Version = historyEnvelopeVersion
	env.Generator = fmt.Sprintf("wintui %s", version)
	env.Created = time.Now().UTC()

	if err := writeHistoryEnvelope(path, env); err != nil {
		return "", err
	}
	return rec.ID, nil
}

// boundHistoryRecords trims the oldest records (front) once the cap is exceeded,
// copying into a fresh slice so the trimmed records can be garbage-collected.
func boundHistoryRecords(env *historyEnvelope) {
	if len(env.Records) <= historyMaxRecords {
		return
	}
	trimmed := make([]historyRecord, historyMaxRecords)
	copy(trimmed, env.Records[len(env.Records)-historyMaxRecords:])
	env.Records = trimmed
}

// writeHistoryEnvelope marshals and writes atomically (temp + rename), matching
// cache.go. Indented because history.json is an audit log users may inspect.
func writeHistoryEnvelope(path string, env historyEnvelope) error {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ── Record builders (write-point seam) ─────────────────────────────

// historyStatusFromBatch maps a finished batch item's status to a history
// status. batchQueued/batchRunning should never reach a completed batch; they
// default to "skipped" so an unexpected state is recorded honestly, not as "ok".
func historyStatusFromBatch(st batchItemStatus) string {
	switch st {
	case batchDone:
		return historyStatusOK
	case batchFailed:
		return historyStatusError
	case batchPendingRestart:
		return historyStatusPending
	default:
		return historyStatusSkipped
	}
}

// dominantAction returns a batch's overall action: the shared per-item action,
// or "mixed" when items disagree (e.g. an apply batch of install + uninstall).
func dominantAction(items []historyItem) string {
	if len(items) == 0 {
		return ""
	}
	first := items[0].Action
	for _, it := range items[1:] {
		if it.Action != first {
			return historyActionMixed
		}
	}
	return first
}

// newHistoryRecord assembles a record from already-built items, deriving the
// dominant action. ID / Timestamp / Summary are filled in by recordHistory.
func newHistoryRecord(trigger string, items []historyItem) historyRecord {
	return historyRecord{
		Trigger: trigger,
		Action:  dominantAction(items),
		Items:   items,
	}
}

// buildTUIHistoryRecord turns a finished workspace batch into a history record.
// Versions come straight from the batch item (no winget re-query): the installed
// version is the from-version. The target is the user's explicitly chosen
// version (selectedVersions, keyed by item.key()) when set — matching the actual
// winget command — otherwise the available (latest) version.
func buildTUIHistoryRecord(items []batchItem, trigger string, selectedVersions map[string]string) historyRecord {
	hi := make([]historyItem, 0, len(items))
	for _, bi := range items {
		// to_version is meaningful only for install/upgrade. An uninstall has
		// none, and must not inherit a stale per-package version pick (selections
		// persist globally) or the package's available update.
		to := ""
		if bi.action != retryOpUninstall {
			to = bi.item.available
			if v := selectedVersions[bi.item.key()]; v != "" {
				to = v
			}
		}
		it := historyItem{
			ID:          bi.item.pkg.ID,
			Name:        bi.item.pkg.Name,
			Source:      bi.item.pkg.Source,
			Action:      string(bi.action),
			FromVersion: bi.item.installed,
			ToVersion:   to,
			Status:      historyStatusFromBatch(bi.status),
		}
		if bi.status == batchFailed && bi.err != nil {
			it.Error = bi.err.Error()
		}
		hi = append(hi, it)
	}
	return newHistoryRecord(trigger, hi)
}

// cliUpgradeItem builds a per-package history item for the headless upgrade
// loops, from the data already on hand in the upgradeable list (no re-query).
func cliUpgradeItem(pkg Package, status string) historyItem {
	return historyItem{
		ID:          pkg.ID,
		Name:        pkg.Name,
		Source:      pkg.Source,
		Action:      historyActionUpgrade,
		FromVersion: pkg.Version,
		ToVersion:   pkg.Available,
		Status:      status,
	}
}

// recordImportHistory records an install batch from the parallel id/source/
// version/error slices the TUI import state machine carries. Installs have no
// from-version; the (optional) pinned version is the target. errs is indexed in
// step with ids; a nil entry (or a short slice) means success.
func recordImportHistory(trigger string, ids, sources, versions []string, errs []error) {
	if len(ids) == 0 {
		return
	}
	items := make([]historyItem, 0, len(ids))
	for i, id := range ids {
		it := historyItem{ID: id, Action: historyActionInstall, Status: historyStatusOK}
		if i < len(sources) {
			it.Source = sources[i]
		}
		if i < len(versions) {
			it.ToVersion = versions[i]
		}
		if i < len(errs) && errs[i] != nil {
			it.Status = historyStatusError
			it.Error = errs[i].Error()
		}
		items = append(items, it)
	}
	_, _ = historyRecordFn(newHistoryRecord(trigger, items))
}
