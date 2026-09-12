package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for the fixes applied after the v2.12.0 branch review.

// TestTUIDeltaWritesKeepConcurrentCLIChanges: the two TUI write points that
// used to flush the whole (stale) snapshot — the Cleanup-tab toggle and the
// version-hold expiry on refresh — must no longer revert a key another
// process changed after the TUI started.
func TestTUIDeltaWritesKeepConcurrentCLIChanges(t *testing.T) {
	setupConfigTest(t)

	// The TUI loaded at startup and holds a version hold for Git.Git.
	start := currentSettings().clone()
	start.setOverride("Git.Git", "winget", PackageOverride{IgnoreVersion: "1.0"})
	if err := SaveSettings(start); err != nil {
		t.Fatal(err)
	}
	setAppSettings(start)

	// Meanwhile a CLI command in another process changed install_mode.
	other := start.clone()
	other.InstallMode = ModeSilent
	if err := SaveSettings(other); err != nil {
		t.Fatal(err)
	}

	// TUI: tick a cleanup target.
	def := cleanupTargetDef{id: "npm_cache", group: cleanupGroupDeveloper}
	if err := persistCleanupTargetEnabled(def, true); err != nil {
		t.Fatal(err)
	}
	disk, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if disk.InstallMode != ModeSilent {
		t.Errorf("cleanup toggle reverted the CLI change: install_mode=%q", disk.InstallMode)
	}
	if !disk.cleanupTargetEnabled(def) || !currentSettings().cleanupTargetEnabled(def) {
		t.Error("cleanup toggle not persisted to disk and memory")
	}
	if currentSettings().InstallMode != ModeDefault {
		t.Error("memory must keep its own view; only the delta is applied there")
	}

	// TUI: a refresh shows Git.Git moved past the held version.
	expireVersionIgnoresPersist([]Package{{ID: "Git.Git", Source: "winget", Available: "2.0"}})
	disk, err = loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if disk.InstallMode != ModeSilent {
		t.Errorf("version-hold expiry reverted the CLI change: install_mode=%q", disk.InstallMode)
	}
	if _, o, ok := disk.lookupOverride("Git.Git", "winget"); ok && o.IgnoreVersion != "" {
		t.Errorf("expired version hold still on disk: %+v", o)
	}
	if _, o, ok := currentSettings().lookupOverride("Git.Git", "winget"); ok && o.IgnoreVersion != "" {
		t.Errorf("expired version hold still in memory: %+v", o)
	}

	// Nothing to expire: no write at all (mtime unchanged).
	before, _ := os.Stat(configPath())
	time.Sleep(20 * time.Millisecond)
	expireVersionIgnoresPersist([]Package{{ID: "Other.Pkg", Source: "winget", Available: "9"}})
	after, _ := os.Stat(configPath())
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("expireVersionIgnoresPersist wrote although nothing expired")
	}
}

// TestPermanentFSErrorsFailFast guards the retry budget: a permanent failure
// must not stall the caller (the TUI Update goroutine) for long. The sleep is
// stubbed and the schedule asserted directly — a wall-clock ceiling was flaky
// on CI runners, where scan-on-write in the temp dir alone can exceed it.
func TestPermanentFSErrorsFailFast(t *testing.T) {
	var slept []time.Duration
	fsSleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { fsSleep = time.Sleep })

	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked.json")
	if err := os.Mkdir(blocker, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(blocker, []byte("x"), 0644); err == nil {
		t.Fatal("rename onto a directory must fail")
	}
	// ACCESS_DENIED is retried on renames (replace onto an open file), so a
	// permanent denial burns the whole budget — which must stay bounded.
	if len(slept) != fsRetryAttempts {
		t.Errorf("permanent rename failure slept %d times; want the full %d-attempt budget", len(slept), fsRetryAttempts)
	}
	var total time.Duration
	for _, d := range slept {
		total += d
	}
	if total > 500*time.Millisecond {
		t.Errorf("rename retry budget sums to %v; must stay under half a second", total)
	}

	// A read that is denied for good (a directory is not readable as a file)
	// must return immediately: ACCESS_DENIED is not retried on reads.
	slept = nil
	if _, err := readFileWithRetry(dir); err == nil {
		t.Fatal("reading a directory as a file must fail")
	}
	if len(slept) != 0 {
		t.Errorf("permanent read failure was retried %d times; it must not be retried", len(slept))
	}
	if isTransientReadError(os.ErrPermission) {
		t.Error("ACCESS_DENIED must not be a transient READ error")
	}
	if !isTransientRenameError(os.ErrPermission) {
		t.Error("ACCESS_DENIED must stay a transient RENAME error (replace onto an open file)")
	}
}

// TestSweepStaleAtomicTempsOnlyRemovesOurOldTemps: the crash-leftover sweep
// touches only our own ".<file>-*.tmp" names older than the cutoff.
func TestSweepStaleAtomicTempsOnlyRemovesOurOldTemps(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write(".settings.json-123.tmp", 3*time.Hour)
	oldHistory := write(".history.json-9.tmp", 3*time.Hour)
	fresh := write(".settings.json-456.tmp", 5*time.Minute)
	unrelated := write("settings.json", 3*time.Hour)
	foreign := write(".notes.md-1.tmp", 3*time.Hour)

	if n := sweepStaleAtomicTemps(dir, []string{"settings.json", "cache.json", "history.json"}, time.Hour); n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	for _, p := range []string{fresh, unrelated, foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should survive: %v", filepath.Base(p), err)
		}
	}
	for _, p := range []string{old, oldHistory} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s should be removed", filepath.Base(p))
		}
	}
	if sweepStaleAtomicTemps(filepath.Join(dir, "missing"), []string{"settings.json"}, time.Hour) != 0 {
		t.Error("missing dir must be a no-op")
	}
}

// TestSettingsFileWarningDistinguishesReadErrors: a file that cannot be READ
// is not reported as invalid JSON.
func TestSettingsFileWarningDistinguishesReadErrors(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })
	// A directory where the file should be: readable as a path, not as a file.
	if err := os.MkdirAll(configPath(), 0755); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings()
	if !settingsEqual(got, DefaultSettings()) {
		t.Errorf("unreadable file should yield defaults, got %+v", got)
	}
	d, rec := settingsFileWarning()
	if !strings.Contains(d, "could not be read") || strings.Contains(d, "invalid JSON") || rec == "" {
		t.Errorf("read error misreported: detail=%q rec=%q", d, rec)
	}
	if err := updateSettings(func(s *Settings) { s.Force = true }); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("delta write must refuse on an unreadable file, got %v", err)
	}
}

// TestRulesHoldUnionIsExclusive: hold is one explicit state on disk, in every
// flag order.
func TestRulesHoldUnionIsExclusive(t *testing.T) {
	setupRulesTest(t)
	set := func(opts rulesSetOptions) error {
		return runRulesSet(&bytes.Buffer{}, "Git.Git", "", opts, false)
	}
	// version hold → policy hold replaces it → policy ask must not resurrect it.
	if err := set(rulesSetOptions{IgnoreVersion: strPtr("2.48.0")}); err != nil {
		t.Fatal(err)
	}
	if err := set(rulesSetOptions{Policy: strPtr("hold")}); err != nil {
		t.Fatal(err)
	}
	if o := currentSettings().getOverride("Git.Git", "winget"); o.IgnoreVersion != "" || o.UpdatePolicy != PolicyHold {
		t.Errorf("policy hold must replace the version hold, got %+v", o)
	}
	if err := set(rulesSetOptions{Policy: strPtr("ask")}); err != nil {
		t.Fatal(err)
	}
	if o := currentSettings().getOverride("Git.Git", "winget"); !o.isEmpty() {
		t.Errorf("policy ask after a permanent hold must leave no hold behind, got %+v", o)
	}

	// version hold on top of a permanent hold: refused, nothing written.
	if err := set(rulesSetOptions{Policy: strPtr("hold")}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(configPath())
	err := set(rulesSetOptions{IgnoreVersion: strPtr("1.0")})
	if err == nil || !strings.Contains(err.Error(), "permanent hold") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if after, _ := os.ReadFile(configPath()); !bytes.Equal(before, after) {
		t.Error("refused set must not write")
	}
	// both in one command: refused.
	if err := set(rulesSetOptions{Policy: strPtr("hold"), IgnoreVersion: strPtr("1.0")}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got %v", err)
	}
	// the documented escape hatch: demote to ask together with the version.
	if err := set(rulesSetOptions{Policy: strPtr("ask"), IgnoreVersion: strPtr("1.0")}); err != nil {
		t.Fatal(err)
	}
	if o := currentSettings().getOverride("Git.Git", "winget"); o.IgnoreVersion != "1.0" || o.UpdatePolicy != PolicyAsk {
		t.Errorf("ask + version hold should be stored, got %+v", o)
	}
	var out bytes.Buffer
	_ = runRulesShow(&out, "Git.Git", "", false)
	if sq := squashSpaces(out.String()); !strings.Contains(sq, "hold version=1.0") {
		t.Errorf("show should render the version hold:\n%s", out.String())
	}
}

// TestRulesClearJSONWithoutRuleIsStillJSON: --json never prints prose.
func TestRulesClearJSONWithoutRuleIsStillJSON(t *testing.T) {
	setupRulesTest(t)
	for _, fields := range [][]string{nil, {"scope"}} {
		var out bytes.Buffer
		if err := runRulesClear(&out, "Nope.Pkg", "", fields, true); err != nil {
			t.Fatal(err)
		}
		var v struct {
			HasRule bool            `json:"has_rule"`
			Rule    json.RawMessage `json:"rule"`
		}
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatalf("clear --json (fields=%v) is not JSON: %v\n%s", fields, err, out.String())
		}
		if v.HasRule || string(v.Rule) != "null" {
			t.Errorf("clear --json without a rule: %s", out.String())
		}
	}
}

// TestRulesElevateAcceptsConfigBooleanAliases: one boolean vocabulary across
// config and rules; a real typo is still rejected.
func TestRulesElevateAcceptsConfigBooleanAliases(t *testing.T) {
	setupRulesTest(t)
	for in, want := range map[string]bool{"yes": true, "on": true, "1": true, "no": false, "off": false, "0": false} {
		if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Elevate: strPtr(in)}, false); err != nil {
			t.Fatalf("elevate %q: %v", in, err)
		}
		if o := currentSettings().getOverride("Git.Git", "winget"); o.Elevate == nil || *o.Elevate != want {
			t.Errorf("elevate %q stored %+v, want %v", in, o.Elevate, want)
		}
	}
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Elevate: strPtr("nevr")}, false); err == nil {
		t.Error("a typo must be rejected")
	}
}

// TestCleanupScanPartialTotalIsMarked: a lower-bound total is never reported
// as exact, in the table or in JSON.
func TestCleanupScanPartialTotalIsMarked(t *testing.T) {
	setupConfigTest(t)
	fakeCleanupRegistry(t, false)
	savedScan := cleanupScanFn
	cleanupScanFn = func(ctx context.Context, def cleanupTargetDef) cleanupTargetResult {
		res := cleanupScan(ctx, def)
		if def.id == "full_target" {
			res.unreadable = 1
		}
		return res
	}
	t.Cleanup(func() { cleanupScanFn = savedScan })

	var out, errOut bytes.Buffer
	if err := runCleanupScan(context.Background(), &out, &errOut, cleanupScanOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "≥ 160 B (some targets were only partly readable) reclaimable") {
		t.Errorf("summary must mark the lower bound:\n%s", out.String())
	}
	rep := scanReport(t, cleanupScanOptions{})
	if !rep.Partial {
		t.Error("JSON must carry partial=true")
	}
	cleanupScanFn = savedScan
	if rep := scanReport(t, cleanupScanOptions{}); rep.Partial {
		t.Error("JSON partial must be false when every walk was complete")
	}
}
