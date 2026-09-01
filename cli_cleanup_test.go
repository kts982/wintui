package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCleanupRegistry builds a registry over temp dirs so the scan runs the
// real engine without touching the machine.
func fakeCleanupRegistry(t *testing.T, elevated bool) (defs []cleanupTargetDef, dirs map[string]string) {
	t.Helper()
	base := t.TempDir()
	dirs = map[string]string{}
	mk := func(name string, files map[string]int) string {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for f, n := range files {
			if err := os.WriteFile(filepath.Join(dir, f), bytes.Repeat([]byte("x"), n), 0644); err != nil {
				t.Fatal(err)
			}
		}
		dirs[name] = dir
		return dir
	}
	full := mk("full", map[string]int{"a.tmp": 100, "b.tmp": 50})
	empty := mk("empty", nil)
	admin := mk("admin", map[string]int{"sys.tmp": 7})
	missing := filepath.Join(base, "does-not-exist")
	globDir := mk("glob", map[string]int{"thumbcache_1.db": 10, "keep.txt": 999})

	defs = []cleanupTargetDef{
		{id: "full_target", label: "Full target", group: cleanupGroupCoreTemp, pathFn: func() string { return full }, mode: cleanupModePurgeContents, defaultChecked: true},
		{id: "empty_target", label: "Empty target", group: cleanupGroupCaches, pathFn: func() string { return empty }, mode: cleanupModePurgeContents, defaultChecked: true},
		{id: "admin_target", label: "Admin target", group: cleanupGroupCoreTemp, pathFn: func() string { return admin }, mode: cleanupModePurgeContents, requiresAdmin: true, defaultChecked: true},
		{id: "missing_target", label: "Missing target", group: cleanupGroupDeveloper, pathFn: func() string { return missing }, mode: cleanupModePurgeContents, detectIfPresent: true},
		{id: "unresolved_target", label: "Unresolved target", group: cleanupGroupGPU, pathFn: func() string { return "" }, mode: cleanupModePurgeContents},
		{id: "glob_target", label: "Glob target", group: cleanupGroupCaches, pathFn: func() string { return globDir }, mode: cleanupModeGlob, globs: []string{"thumbcache_*.db"}},
	}
	saved := cleanupScanRegistryFn
	savedElev := cleanupScanElevatedFn
	cleanupScanRegistryFn = func() []cleanupTargetDef { return defs }
	cleanupScanElevatedFn = func() bool { return elevated }
	t.Cleanup(func() { cleanupScanRegistryFn = saved; cleanupScanElevatedFn = savedElev })
	return defs, dirs
}

func scanReport(t *testing.T, opts cleanupScanOptions) cleanupScanJSON {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := runCleanupScan(context.Background(), &out, &errOut, opts, true); err != nil {
		t.Fatal(err)
	}
	var rep cleanupScanJSON
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out.String())
	}
	return rep
}

func rowByID(rep cleanupScanJSON, id string) cleanupScanTargetJSON {
	for _, r := range rep.Targets {
		if r.ID == id {
			return r
		}
	}
	return cleanupScanTargetJSON{}
}

// TestCleanupScanListsEveryTargetWithHonestStatus: the bare scan lists all
// registered targets, present or not, with the status vocabulary; admin
// targets short-circuit in a non-elevated process and are never walked.
func TestCleanupScanListsEveryTargetWithHonestStatus(t *testing.T) {
	setupConfigTest(t)
	fakeCleanupRegistry(t, false)
	walked := map[string]bool{}
	savedScan := cleanupScanFn
	cleanupScanFn = func(ctx context.Context, def cleanupTargetDef) cleanupTargetResult {
		walked[def.id] = true
		return cleanupScan(ctx, def)
	}
	t.Cleanup(func() { cleanupScanFn = savedScan })

	rep := scanReport(t, cleanupScanOptions{})
	if rep.Elevated || rep.Selection != "all" || rep.Count != 6 {
		t.Fatalf("header: %+v", rep)
	}
	want := map[string]string{
		"full_target": cleanupStatusOK, "empty_target": cleanupStatusEmpty, "admin_target": cleanupStatusNeedsAdmin,
		"missing_target": cleanupStatusMissing, "unresolved_target": cleanupStatusUnresolved, "glob_target": cleanupStatusOK,
	}
	for id, status := range want {
		if got := rowByID(rep, id).Status; got != status {
			t.Errorf("%s: status %q, want %q", id, got, status)
		}
	}
	if walked["admin_target"] {
		t.Error("admin target must not be walked in a non-elevated process")
	}
	if walked["missing_target"] || walked["unresolved_target"] {
		t.Error("absent targets must not be walked")
	}
	full := rowByID(rep, "full_target")
	if full.SizeBytes == nil || *full.SizeBytes != 150 || full.Items == nil || *full.Items != 2 || !full.Scanned || !full.Present {
		t.Errorf("full_target row wrong: %+v", full)
	}
	admin := rowByID(rep, "admin_target")
	if admin.SizeBytes != nil || admin.Items != nil || admin.Scanned || !admin.Present || !admin.RequiresAdmin {
		t.Errorf("admin row must be present but unmeasured: %+v", admin)
	}
	if empty := rowByID(rep, "empty_target"); empty.SizeBytes == nil || *empty.SizeBytes != 0 || empty.Items == nil || *empty.Items != 0 {
		t.Errorf("empty row must carry explicit zeros: %+v", empty)
	}
	if glob := rowByID(rep, "glob_target"); glob.SizeBytes == nil || *glob.SizeBytes != 10 || glob.Mode != "glob" || strings.Join(glob.Globs, ",") != "thumbcache_*.db" {
		t.Errorf("glob row wrong: %+v", glob)
	}
	if rep.TotalSizeBytes != 160 || rep.Scanned != 3 || rep.NeedsAdmin != 1 {
		t.Errorf("totals wrong: scanned=%d needs_admin=%d total=%d", rep.Scanned, rep.NeedsAdmin, rep.TotalSizeBytes)
	}

	// Elevated: the admin target is scanned normally.
	cleanupScanElevatedFn = func() bool { return true }
	rep = scanReport(t, cleanupScanOptions{Targets: []string{"ADMIN_TARGET"}})
	if rep.Selection != "targets" || rep.Count != 1 || !rep.Elevated {
		t.Fatalf("targets selection header: %+v", rep)
	}
	if admin := rowByID(rep, "admin_target"); admin.Status != cleanupStatusOK || admin.SizeBytes == nil || *admin.SizeBytes != 7 {
		t.Errorf("elevated admin row: %+v", admin)
	}
}

// TestCleanupScanEnabledMeansTheTUICheckedSet pins the selection vocabulary:
// --enabled = default-checked ∪ opted-in (Settings.cleanupTargetEnabled), the
// set a TUI deletion acts on — not the auto-scan "safe" set.
func TestCleanupScanEnabledMeansTheTUICheckedSet(t *testing.T) {
	setupConfigTest(t)
	fakeCleanupRegistry(t, false)

	rep := scanReport(t, cleanupScanOptions{Enabled: true})
	ids := make([]string, 0, len(rep.Targets))
	for _, r := range rep.Targets {
		ids = append(ids, r.ID)
	}
	if rep.Selection != "enabled" || strings.Join(ids, ",") != "full_target,empty_target,admin_target" {
		t.Errorf("--enabled should list exactly the default-checked targets, got %v", ids)
	}

	// Opting a non-default target in (the TUI toggle) adds it.
	s := currentSettings().clone()
	s.CleanupEnabledTargets = []string{"glob_target"}
	setAppSettings(s)
	rep = scanReport(t, cleanupScanOptions{Enabled: true})
	if row := rowByID(rep, "glob_target"); row.ID == "" || !row.Enabled {
		t.Errorf("opted-in target missing from --enabled: %+v", rep.Targets)
	}
	if row := rowByID(rep, "missing_target"); row.ID != "" {
		t.Error("non-enabled target must not be listed under --enabled")
	}
	// The bare listing still carries the enabled flag per row.
	rep = scanReport(t, cleanupScanOptions{})
	if !rowByID(rep, "glob_target").Enabled || rowByID(rep, "missing_target").Enabled {
		t.Error("enabled flag wrong in bare listing")
	}
}

// TestCleanupScanRejectsBadSelection: unknown IDs and conflicting flags are
// errors before any walk.
func TestCleanupScanRejectsBadSelection(t *testing.T) {
	setupConfigTest(t)
	fakeCleanupRegistry(t, false)
	var out, errOut bytes.Buffer
	if err := runCleanupScan(context.Background(), &out, &errOut, cleanupScanOptions{Targets: []string{"nope"}}, false); err == nil || !strings.Contains(err.Error(), "unknown cleanup target") {
		t.Errorf("unknown target: %v", err)
	}
	if err := runCleanupScan(context.Background(), &out, &errOut, cleanupScanOptions{Enabled: true, Targets: []string{"full_target"}}, false); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("mutually exclusive: %v", err)
	}
}

// TestCleanupScanJSONGolden pins the per-target contract at the byte level:
// explicit null for unmeasured size/items, zeros present, arrays never null,
// elevated at top level.
func TestCleanupScanJSONGolden(t *testing.T) {
	setupConfigTest(t)
	_, dirs := fakeCleanupRegistry(t, false)
	var out, errOut bytes.Buffer
	if err := runCleanupScan(context.Background(), &out, &errOut, cleanupScanOptions{Targets: []string{"empty_target", "admin_target"}}, true); err != nil {
		t.Fatal(err)
	}
	emptyPath, _ := json.Marshal(dirs["empty"])
	adminPath, _ := json.Marshal(dirs["admin"])
	want := `{
  "elevated": false,
  "selection": "targets",
  "count": 2,
  "scanned": 1,
  "needs_admin": 1,
  "total_size_bytes": 0,
  "partial": false,
  "targets": [
    {
      "id": "empty_target",
      "label": "Empty target",
      "group": "caches",
      "group_label": "Caches",
      "path": ` + string(emptyPath) + `,
      "mode": "purge_contents",
      "globs": [],
      "min_age_seconds": 0,
      "requires_admin": false,
      "default_checked": true,
      "enabled": true,
      "present": true,
      "scanned": true,
      "status": "empty",
      "size_bytes": 0,
      "items": 0,
      "unreadable": 0,
      "errors": []
    },
    {
      "id": "admin_target",
      "label": "Admin target",
      "group": "core_temp",
      "group_label": "Core Temp",
      "path": ` + string(adminPath) + `,
      "mode": "purge_contents",
      "globs": [],
      "min_age_seconds": 0,
      "requires_admin": true,
      "default_checked": true,
      "enabled": true,
      "present": true,
      "scanned": false,
      "status": "needs_admin",
      "size_bytes": null,
      "items": null,
      "unreadable": 0,
      "errors": []
    }
  ]
}
`
	if out.String() != want {
		t.Errorf("cleanup scan --json:\n%s\nwant:\n%s", out.String(), want)
	}
	if errOut.Len() != 0 {
		t.Errorf("JSON mode must keep stderr quiet, got %q", errOut.String())
	}
}

// TestCleanupScanTableAndProgress: the human table carries every column, the
// "needs admin" wording, an explicit partial marker, and progress goes to
// stderr only.
func TestCleanupScanTableAndProgress(t *testing.T) {
	setupConfigTest(t)
	fakeCleanupRegistry(t, false)
	savedScan := cleanupScanFn
	cleanupScanFn = func(ctx context.Context, def cleanupTargetDef) cleanupTargetResult {
		res := cleanupScan(ctx, def)
		if def.id == "full_target" {
			res.unreadable = 3 // simulate access-denied entries during the size walk
		}
		return res
	}
	t.Cleanup(func() { cleanupScanFn = savedScan })

	var out, errOut bytes.Buffer
	if err := runCleanupScan(context.Background(), &out, &errOut, cleanupScanOptions{}, false); err != nil {
		t.Fatal(err)
	}
	text := squashSpaces(out.String())
	for _, want := range []string{
		"TARGET GROUP ENABLED SIZE ITEMS ADMIN STATUS",
		"full_target Core Temp yes ≥ 150 B 2 - partial",
		"admin_target Core Temp yes - - needs admin needs_admin",
		"missing_target Developer no - - - missing",
		"unresolved_target GPU no - - - unresolved",
		"1 need admin (run elevated to measure)",
		"not elevated",
		"Deletion stays in the TUI",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("table missing %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "Scanning 3 targets") {
		t.Errorf("progress line should go to stderr, got %q", errOut.String())
	}
}

// TestCleanupScanStatusDerivation covers the pure status mapping, including
// the partial/lower-bound case the engine can now report.
func TestCleanupScanStatusDerivation(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  cleanupTargetResult
		want string
	}{
		{"ok", cleanupTargetResult{files: 2, sizeBytes: 10}, cleanupStatusOK},
		{"empty", cleanupTargetResult{}, cleanupStatusEmpty},
		{"partial-unreadable", cleanupTargetResult{files: 2, unreadable: 1}, cleanupStatusPartial},
		{"partial-failed", cleanupTargetResult{files: 2, failed: 1}, cleanupStatusPartial},
		{"error", cleanupTargetResult{errors: []error{os.ErrPermission}}, cleanupStatusError},
		{"missing", cleanupTargetResult{skipped: cleanupSkipMissing}, cleanupStatusMissing},
		{"unresolved", cleanupTargetResult{skipped: cleanupSkipUnresolved}, cleanupStatusUnresolved},
		{"guarded", cleanupTargetResult{skipped: cleanupSkipGuarded}, cleanupStatusSkipped},
		{"not-elevated", cleanupTargetResult{skipped: cleanupSkipNotElevated}, cleanupStatusNeedsAdmin},
	} {
		if got := cleanupScanStatus(tc.res); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCleanupEntrySizeCountsUnreadableAndWireRoundTrips: the size walk
// reports evidence of skipped entries, and the helper wire carries it.
func TestCleanupEntrySizeCountsUnreadableAndWireRoundTrips(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f"), []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(dir)
	total, unreadable := cleanupEntrySize(context.Background(), dir, info)
	if total != 5 || unreadable != 0 {
		t.Errorf("clean walk: total=%d unreadable=%d", total, unreadable)
	}
	// A cancelled context aborts the walk without inventing unreadable entries.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, u := cleanupEntrySize(ctx, dir, info); u != 0 {
		t.Errorf("cancelled walk should not count unreadable entries, got %d", u)
	}

	w := cleanupResultToWire(cleanupTargetResult{id: "x", sizeBytes: 5, files: 1, unreadable: 2})
	if w.Unreadable != 2 {
		t.Errorf("wire lost unreadable: %+v", w)
	}
	if back := cleanupResultFromWire(w); back.unreadable != 2 {
		t.Errorf("wire round trip lost unreadable: %+v", back)
	}
}

// TestCleanupTargetVisibleSharedWithTUI: the extracted present-filter behaves
// exactly like the old computeVisible inline logic.
func TestCleanupTargetVisibleSharedWithTUI(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		def  cleanupTargetDef
		want bool
	}{
		{"always-listed", cleanupTargetDef{pathFn: func() string { return "" }}, true},
		{"detect-present-dir", cleanupTargetDef{detectIfPresent: true, pathFn: func() string { return dir }}, true},
		{"detect-missing", cleanupTargetDef{detectIfPresent: true, pathFn: func() string { return filepath.Join(dir, "nope") }}, false},
		{"detect-unresolved", cleanupTargetDef{detectIfPresent: true, pathFn: func() string { return "" }}, false},
		{"detect-file-not-dir", cleanupTargetDef{detectIfPresent: true, pathFn: func() string { return file }}, false},
		{"nil-pathfn", cleanupTargetDef{detectIfPresent: true}, false},
	} {
		if got := cleanupTargetVisible(tc.def); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
