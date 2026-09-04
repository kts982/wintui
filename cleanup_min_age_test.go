package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cleanup_min_age (v2.12.0 dogfood follow-up): the Core Temp age floor is a
// setting (default 1 day, was a hard-coded 7), and every surface reports
// what the floor left alone instead of a bare "0 B" that contradicts
// Explorer.

// ── Setting ───────────────────────────────────────────────────────────

func TestCleanupMinAgeDefaultsToOneDay(t *testing.T) {
	s := DefaultSettings()
	if got := s.getValue("cleanup_min_age"); got != "" {
		t.Fatalf("default cleanup_min_age = %q, want empty (1 day)", got)
	}
	if got := s.cleanupMinAge(); got != 24*time.Hour {
		t.Fatalf("default cleanupMinAge() = %s, want 24h", got)
	}
}

func TestCleanupMinAgeRoundTripsAndNormalizes(t *testing.T) {
	s := DefaultSettings()
	for val, want := range map[string]time.Duration{"off": 0, "3d": 72 * time.Hour, "7d": 168 * time.Hour, "": 24 * time.Hour} {
		s.setValue("cleanup_min_age", val)
		if got := s.getValue("cleanup_min_age"); got != val {
			t.Errorf("after set %q, get returned %q", val, got)
		}
		if got := s.cleanupMinAge(); got != want {
			t.Errorf("after set %q, cleanupMinAge() = %s, want %s", val, got, want)
		}
	}
	// A hand-edited value can't disable the floor: unknown → default.
	s.setValue("cleanup_min_age", "0")
	if got := s.getValue("cleanup_min_age"); got != "" {
		t.Errorf("unknown value should normalize to the default (empty), got %q", got)
	}
	s.CleanupMinAge = "never"
	if got := s.cleanupMinAge(); got != 24*time.Hour {
		t.Errorf("unknown stored value should resolve to 24h, got %s", got)
	}
}

func TestCleanupMinAgeJSONOmitsDefaultAndKeepsChoice(t *testing.T) {
	s := DefaultSettings()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["cleanup_min_age"]; ok {
		t.Fatal("expected 'cleanup_min_age' to be omitted at the default")
	}
	s.CleanupMinAge = CleanupMinAge7d
	data, _ = json.MarshalIndent(s, "", "  ")
	raw = map[string]any{}
	_ = json.Unmarshal(data, &raw)
	if got, ok := raw["cleanup_min_age"]; !ok || got != "7d" {
		t.Errorf("expected cleanup_min_age=7d in JSON, got %v", raw["cleanup_min_age"])
	}
}

func TestSettingsEqualNormalizesCleanupMinAge(t *testing.T) {
	a := DefaultSettings()
	b := DefaultSettings()
	b.CleanupMinAge = "garbage" // normalizes to the default
	if !settingsEqual(a, b) {
		t.Error("garbage cleanup_min_age should compare equal to the default")
	}
	b.CleanupMinAge = CleanupMinAgeOff
	if settingsEqual(a, b) {
		t.Error("off must differ from the default")
	}
}

// ── Engine ────────────────────────────────────────────────────────────

// The engine measures what the floor leaves alone, in both modes, so the UI
// can say "0 B eligible · N kept" instead of a bare "0 B". Kept bytes are
// informational only — nothing under a kept entry is ever removed.
func TestCleanupRunCountsKeptEntries(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, "fresh.tmp"), 100, 0)
	makeFile(t, filepath.Join(root, "sub", "fresh-deep.tmp"), 30, 0) // the dir's mtime is fresh
	makeFile(t, filepath.Join(root, "old.tmp"), 200, 10*24*time.Hour)

	def := purgeDef("test", root, func(d *cleanupTargetDef) { d.minAge = 7 * 24 * time.Hour })

	scan := cleanupScan(context.Background(), def)
	if scan.files != 1 || scan.sizeBytes != 200 {
		t.Errorf("scan eligible: files=%d size=%d, want 1/200", scan.files, scan.sizeBytes)
	}
	if scan.kept != 2 || scan.keptBytes != 130 {
		t.Errorf("scan kept: kept=%d keptBytes=%d, want 2/130", scan.kept, scan.keptBytes)
	}

	del := cleanupDelete(context.Background(), def)
	if del.freedBytes != 200 || del.kept != 2 || del.keptBytes != 130 {
		t.Errorf("delete: freed=%d kept=%d keptBytes=%d, want 200/2/130", del.freedBytes, del.kept, del.keptBytes)
	}
	for _, keep := range []string{"fresh.tmp", filepath.Join("sub", "fresh-deep.tmp")} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Errorf("%s should have been kept: %v", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "old.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old.tmp should have been removed, got %v", err)
	}

	// No floor: nothing is kept.
	none := t.TempDir()
	makeFile(t, filepath.Join(none, "fresh.tmp"), 10, 0)
	if res := cleanupScan(context.Background(), purgeDef("none", none)); res.kept != 0 || res.keptBytes != 0 || res.files != 1 {
		t.Errorf("ageless target must not report kept entries: %+v", res)
	}
}

// A target that opts into the setting follows it at run time: the same
// files are eligible or kept depending on cleanup_min_age, and the registry
// fallback is never what decides.
func TestCleanupRunTakesMinAgeFromSettings(t *testing.T) {
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	root := t.TempDir()
	makeFile(t, filepath.Join(root, "fresh.tmp"), 100, 0)
	makeFile(t, filepath.Join(root, "mid.tmp"), 50, 3*24*time.Hour+time.Hour)
	makeFile(t, filepath.Join(root, "old.tmp"), 200, 10*24*time.Hour)
	def := purgeDef("test", root, func(d *cleanupTargetDef) {
		d.minAge = 365 * 24 * time.Hour // the static fallback must be ignored
		d.minAgeFromSettings = true
	})

	for _, tc := range []struct {
		stored    CleanupMinAge
		wantSize  int64
		wantFiles int
		wantKept  int
	}{
		{CleanupMinAgeDefault, 250, 2, 1}, // 1 day: mid + old
		{CleanupMinAgeOff, 350, 3, 0},
		{CleanupMinAge3d, 250, 2, 1},
		{CleanupMinAge7d, 200, 1, 2},
	} {
		s := DefaultSettings()
		s.CleanupMinAge = tc.stored
		setAppSettings(s)
		res := cleanupScan(context.Background(), def)
		if res.sizeBytes != tc.wantSize || res.files != tc.wantFiles || res.kept != tc.wantKept {
			t.Errorf("%q: size=%d files=%d kept=%d, want %d/%d/%d",
				tc.stored, res.sizeBytes, res.files, res.kept, tc.wantSize, tc.wantFiles, tc.wantKept)
		}
	}
}

// ── Elevated helper ───────────────────────────────────────────────────

// applyHelperMinAge: the TUI's resolved floor is honored only for targets
// that opt into the setting, "off" (0) is a real value, and a negative
// value is a malformed request.
func TestApplyHelperMinAge(t *testing.T) {
	settingsDef := cleanupTargetDef{id: "t", minAge: 24 * time.Hour, minAgeFromSettings: true}
	staticDef := cleanupTargetDef{id: "s", minAge: 24 * time.Hour}
	secs := func(n int64) *int64 { return &n }

	if got, err := applyHelperMinAge(settingsDef, helperRequest{}); err != nil || got.minAge != 24*time.Hour || !got.minAgeFromSettings {
		t.Errorf("no value sent: def must be untouched, got %+v err=%v", got, err)
	}
	if got, err := applyHelperMinAge(settingsDef, helperRequest{MinAgeSeconds: secs(0)}); err != nil || got.minAge != 0 || got.minAgeFromSettings {
		t.Errorf("off: want minAge 0 with the settings flag cleared, got %+v err=%v", got, err)
	}
	if got, err := applyHelperMinAge(settingsDef, helperRequest{MinAgeSeconds: secs(7 * 86400)}); err != nil || got.minAge != 7*24*time.Hour {
		t.Errorf("7d: got %+v err=%v", got, err)
	}
	if got, err := applyHelperMinAge(staticDef, helperRequest{MinAgeSeconds: secs(0)}); err != nil || got.minAge != 24*time.Hour {
		t.Errorf("static floor must ignore the request, got %+v err=%v", got, err)
	}
	if _, err := applyHelperMinAge(settingsDef, helperRequest{MinAgeSeconds: secs(-1)}); err == nil {
		t.Error("negative min_age_seconds must be rejected")
	}
}

// ── CLI ───────────────────────────────────────────────────────────────

// A Core-Temp-style target with only fresh files is "recent", not "empty":
// the KEPT column, the summary line, and the JSON all say what the floor
// left alone, and the per-target min_age_seconds is the EFFECTIVE floor
// (the setting), not the registry fallback.
func TestCleanupScanReportsKeptEntries(t *testing.T) {
	setupConfigTest(t)
	base := t.TempDir()
	recent := filepath.Join(base, "recent")
	mixed := filepath.Join(base, "mixed")
	for _, d := range []string{recent, mixed} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	makeFile(t, filepath.Join(recent, "today.tmp"), 100, 0)
	makeFile(t, filepath.Join(mixed, "today.tmp"), 40, 0)
	makeFile(t, filepath.Join(mixed, "old.tmp"), 60, 10*24*time.Hour)
	defs := []cleanupTargetDef{
		{id: "recent_target", label: "Recent", group: cleanupGroupCoreTemp, pathFn: func() string { return recent }, mode: cleanupModePurgeContents, minAge: 365 * 24 * time.Hour, minAgeFromSettings: true, defaultChecked: true},
		{id: "mixed_target", label: "Mixed", group: cleanupGroupCoreTemp, pathFn: func() string { return mixed }, mode: cleanupModePurgeContents, minAge: 365 * 24 * time.Hour, minAgeFromSettings: true, defaultChecked: true},
	}
	saved, savedElev := cleanupScanRegistryFn, cleanupScanElevatedFn
	cleanupScanRegistryFn = func() []cleanupTargetDef { return defs }
	cleanupScanElevatedFn = func() bool { return false }
	t.Cleanup(func() { cleanupScanRegistryFn = saved; cleanupScanElevatedFn = savedElev })

	rep := scanReport(t, cleanupScanOptions{})
	if rep.CleanupMinAge != "1d" || rep.TotalSizeBytes != 60 || rep.TotalKeptBytes != 140 {
		t.Errorf("header: min_age=%q total=%d kept=%d", rep.CleanupMinAge, rep.TotalSizeBytes, rep.TotalKeptBytes)
	}
	r := rowByID(rep, "recent_target")
	if r.Status != cleanupStatusRecent || r.MinAgeSeconds != 86400 || r.SizeBytes == nil || *r.SizeBytes != 0 ||
		r.KeptItems == nil || *r.KeptItems != 1 || r.KeptBytes == nil || *r.KeptBytes != 100 {
		t.Errorf("recent row: %+v", r)
	}
	m := rowByID(rep, "mixed_target")
	if m.Status != cleanupStatusOK || *m.SizeBytes != 60 || *m.Items != 1 || *m.KeptItems != 1 || *m.KeptBytes != 40 {
		t.Errorf("mixed row: %+v", m)
	}

	var out, errOut bytes.Buffer
	if err := runCleanupScan(context.Background(), &out, &errOut, cleanupScanOptions{}, false); err != nil {
		t.Fatal(err)
	}
	text := squashSpaces(out.String())
	for _, want := range []string{
		"recent_target Core Temp yes 0 B 0 100 B (1) - recent",
		"mixed_target Core Temp yes 60 B 1 40 B (1) - ok",
		"140 B in 2 entries kept as newer than 1 day",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("table missing %q:\n%s", want, out.String())
		}
	}

	// Turning the floor off from the setting makes everything eligible.
	s := currentSettings().clone()
	s.CleanupMinAge = CleanupMinAgeOff
	setAppSettings(s)
	rep = scanReport(t, cleanupScanOptions{})
	if r := rowByID(rep, "recent_target"); r.Status != cleanupStatusOK || *r.SizeBytes != 100 || *r.KeptItems != 0 || r.MinAgeSeconds != 0 {
		t.Errorf("off: recent row %+v", r)
	}
	if rep.CleanupMinAge != "off" || rep.TotalKeptBytes != 0 {
		t.Errorf("off: header min_age=%q kept=%d", rep.CleanupMinAge, rep.TotalKeptBytes)
	}
}
