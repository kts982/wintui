package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeFile creates a file with the given size and (optionally) backdated mtime.
func makeFile(t *testing.T, path string, size int, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if age > 0 {
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}
}

func purgeDef(id string, root string, opts ...func(*cleanupTargetDef)) cleanupTargetDef {
	def := cleanupTargetDef{
		id:     id,
		label:  id,
		group:  cleanupGroupCoreTemp,
		pathFn: func() string { return root },
		mode:   cleanupModePurgeContents,
	}
	for _, opt := range opts {
		opt(&def)
	}
	return def
}

// ── Scan ──────────────────────────────────────────────────────────────

func TestCleanupScanSumsTopLevelFilesAndDirs(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, "a.tmp"), 100, 0)
	makeFile(t, filepath.Join(root, "sub", "b.tmp"), 200, 0)
	makeFile(t, filepath.Join(root, "sub", "deep", "c.tmp"), 50, 0)

	res := cleanupScan(context.Background(), purgeDef("test", root))
	if res.skipped != cleanupSkipNone {
		t.Fatalf("unexpected skip: %v", res.skipped)
	}
	if res.sizeBytes != 350 {
		t.Errorf("sizeBytes = %d, want 350", res.sizeBytes)
	}
	if res.files != 2 { // top-level: "a.tmp" and "sub"
		t.Errorf("files = %d, want 2", res.files)
	}
	if res.freedBytes != 0 {
		t.Errorf("scan must not free bytes; got %d", res.freedBytes)
	}

	// And nothing was removed.
	if _, err := os.Stat(filepath.Join(root, "a.tmp")); err != nil {
		t.Errorf("scan deleted a.tmp: %v", err)
	}
}

func TestCleanupScanSkipsUnresolvedPath(t *testing.T) {
	def := purgeDef("test", "")
	res := cleanupScan(context.Background(), def)
	if res.skipped != cleanupSkipUnresolved {
		t.Errorf("skipped = %v, want unresolved", res.skipped)
	}
}

func TestCleanupScanSkipsMissingPath(t *testing.T) {
	def := purgeDef("test", filepath.Join(t.TempDir(), "does-not-exist"))
	res := cleanupScan(context.Background(), def)
	if res.skipped != cleanupSkipMissing {
		t.Errorf("skipped = %v, want missing", res.skipped)
	}
	if len(res.errors) != 0 {
		t.Errorf("missing path should not produce errors: %v", res.errors)
	}
}

// ── Delete ────────────────────────────────────────────────────────────

func TestCleanupDeleteRemovesContentsButNotRoot(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, "a.tmp"), 100, 0)
	makeFile(t, filepath.Join(root, "sub", "b.tmp"), 200, 0)

	res := cleanupDelete(context.Background(), purgeDef("test", root))
	if res.skipped != cleanupSkipNone {
		t.Fatalf("unexpected skip: %v", res.skipped)
	}
	if res.failed != 0 {
		t.Errorf("failed = %d (errors=%v)", res.failed, res.errors)
	}
	if res.freedBytes != 300 {
		t.Errorf("freedBytes = %d, want 300", res.freedBytes)
	}

	// Root must survive — engine never RemoveAlls the registered path.
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("registered root was removed: %v", err)
	}
	// But its contents must be gone.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("root not purged; still contains %d entries", len(entries))
	}
}

func TestCleanupDeleteHonorsMinAge(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, "fresh.tmp"), 100, 0)             // brand new
	makeFile(t, filepath.Join(root, "old.tmp"), 200, 10*24*time.Hour) // 10 days

	def := purgeDef("test", root, func(d *cleanupTargetDef) {
		d.minAge = 7 * 24 * time.Hour
	})
	res := cleanupDelete(context.Background(), def)

	if res.freedBytes != 200 {
		t.Errorf("freedBytes = %d, want 200 (only old.tmp)", res.freedBytes)
	}
	if _, err := os.Stat(filepath.Join(root, "fresh.tmp")); err != nil {
		t.Errorf("fresh.tmp should have been kept, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old.tmp should have been removed, got %v", err)
	}
}

func TestCleanupDeleteGlobModeOnlyTouchesMatchingNames(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, "thumbcache_32.db"), 100, 0)
	makeFile(t, filepath.Join(root, "thumbcache_256.db"), 200, 0)
	makeFile(t, filepath.Join(root, "iconcache_idx.db"), 50, 0)
	makeFile(t, filepath.Join(root, "explorer.bag"), 999, 0) // must survive
	makeFile(t, filepath.Join(root, "shell32.dll"), 777, 0)  // must survive

	def := purgeDef("test", root, func(d *cleanupTargetDef) {
		d.mode = cleanupModeGlob
		d.globs = []string{"thumbcache_*.db", "iconcache_*.db"}
	})
	res := cleanupDelete(context.Background(), def)

	if res.freedBytes != 350 {
		t.Errorf("freedBytes = %d, want 350", res.freedBytes)
	}
	// Bystanders must survive.
	for _, name := range []string{"explorer.bag", "shell32.dll"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("glob mode wrongly removed %q: %v", name, err)
		}
	}
}

func TestCleanupDeleteRefusesGuardedRoot(t *testing.T) {
	// Point LOCALAPPDATA at a sandbox dir, then ask the engine to clean the
	// sandbox dir directly — the allowlist must refuse the base itself.
	// TEMP is pointed elsewhere because t.TempDir() lives under the real
	// %TEMP%, which would make the sandbox an allowed TEMP descendant.
	guarded := t.TempDir()
	t.Setenv("LOCALAPPDATA", guarded)
	t.Setenv("TMP", filepath.Join(guarded, "redirected-temp"))
	t.Setenv("TEMP", filepath.Join(guarded, "redirected-temp"))
	makeFile(t, filepath.Join(guarded, "a.tmp"), 100, 0)

	res := cleanupDelete(context.Background(), purgeDef("test", guarded))
	if res.skipped != cleanupSkipGuarded {
		t.Fatalf("skipped = %v, want guarded", res.skipped)
	}
	// The file must still be there — guard list refusal is pre-walk.
	if _, err := os.Stat(filepath.Join(guarded, "a.tmp")); err != nil {
		t.Errorf("guarded refusal still touched the filesystem: %v", err)
	}
}

func TestCleanupScanIgnoresGuardList(t *testing.T) {
	// Scans must work even on guarded roots — cleanupValidateRoot only fires
	// when we're about to delete. Otherwise we couldn't show sizes for
	// %TEMP% etc. without resolving them outside the guard set.
	guarded := t.TempDir()
	t.Setenv("LOCALAPPDATA", guarded)
	makeFile(t, filepath.Join(guarded, "a.tmp"), 100, 0)

	res := cleanupScan(context.Background(), purgeDef("test", guarded))
	if res.skipped != cleanupSkipNone {
		t.Errorf("scan was skipped on guarded root: %v", res.skipped)
	}
	if res.sizeBytes != 100 {
		t.Errorf("sizeBytes = %d, want 100", res.sizeBytes)
	}
}

func TestCleanupRunRespectsContextCancel(t *testing.T) {
	root := t.TempDir()
	for i := range 50 {
		makeFile(t, filepath.Join(root, "f"+string(rune('a'+i%26))+".tmp"), 32, 0)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	res := cleanupDelete(ctx, purgeDef("test", root))
	if res.freedBytes != 0 {
		t.Errorf("cancelled context still freed %d bytes", res.freedBytes)
	}
	// And the directory should be largely intact (no entries iterated).
	entries, _ := os.ReadDir(root)
	if len(entries) == 0 {
		t.Errorf("cancelled delete still purged the directory")
	}
}

// ── Helpers ───────────────────────────────────────────────────────────

func TestCleanupMatchesAnyGlob(t *testing.T) {
	cases := []struct {
		name  string
		globs []string
		want  bool
	}{
		{"thumbcache_32.db", []string{"thumbcache_*.db"}, true},
		{"thumbcache.dat", []string{"thumbcache_*.db"}, false},
		{"iconcache_idx.db", []string{"thumbcache_*.db", "iconcache_*.db"}, true},
		{"anything.txt", nil, false},
	}
	for _, tc := range cases {
		if got := cleanupMatchesAnyGlob(tc.name, tc.globs); got != tc.want {
			t.Errorf("matches(%q, %v) = %v, want %v", tc.name, tc.globs, got, tc.want)
		}
	}
}

func TestCleanupAppendErrCapsAtMax(t *testing.T) {
	var r cleanupTargetResult
	for range cleanupMaxErrors * 2 {
		r.appendErr(errors.New("boom"))
	}
	if len(r.errors) != cleanupMaxErrors {
		t.Errorf("len(errors) = %d, want %d", len(r.errors), cleanupMaxErrors)
	}
}

func TestCleanupAppendErrIgnoresNil(t *testing.T) {
	var r cleanupTargetResult
	r.appendErr(nil)
	if len(r.errors) != 0 {
		t.Errorf("nil error should not be retained, got %v", r.errors)
	}
}

// Sanity: registered roots that pass the guard list (typical user-facing
// targets) must not collide with our guarded set on a real machine.
func TestCleanupRegistryPathsAreNotSelfGuarded(t *testing.T) {
	for _, def := range cleanupTargetRegistry() {
		path := def.pathFn()
		if path == "" {
			continue // env not set in this environment, skip
		}
		err := cleanupValidateRoot(path)
		if err == nil {
			continue
		}
		// Allow %TEMP% to land *inside* a guarded ancestor — that's fine,
		// only equality with a guarded path is forbidden. The guard list
		// returning an error here would mean the path equals e.g.
		// %LOCALAPPDATA% itself, which is a registry bug.
		if strings.Contains(err.Error(), "guarded") {
			t.Errorf("%s: registered path %q is itself in the guard list", def.id, path)
		}
	}
}
