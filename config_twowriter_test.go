package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// useTempSettingsDir points configPath/diskCachePath/historyPath at a fresh
// temp dir for the duration of the test. os.UserConfigDir reads %APPDATA% on
// Windows, so this is the same seam cleanup_targets_test.go already uses; it
// also means these tests never touch the real settings.json.
func useTempSettingsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	return filepath.Join(dir, "wintui")
}

// seedSettingsFile writes s to the (temp) settings path and returns it.
func seedSettingsFile(t *testing.T, s Settings) Settings {
	t.Helper()
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSaveSettingsConcurrentWritersNeverPublishPartial is acceptance test #1
// of the v2.12 two-writer settings.json safety commit. Two writers saving
// distinct, complete snapshots concurrently must (a) both succeed and (b)
// never leave a file on disk that is not exactly one of the written
// snapshots — no truncated JSON, no defaults-filled partial. With a fixed
// shared temp name (settings.json.tmp) the writers collide on Windows
// (sharing violations / a rename of a half-written temp); unique temp files
// + rename make every published file complete.
func TestSaveSettingsConcurrentWritersNeverPublishPartial(t *testing.T) {
	useTempSettingsDir(t)

	const writers = 2
	const rounds = 80 // old code fails within the first few rounds; keep the suite fast

	snapshots := make([]Settings, writers)
	for i := range snapshots {
		s := DefaultSettings()
		s.Theme = fmt.Sprintf("writer-%d", i)
		s.Packages = map[string]PackageOverride{}
		// Make each snapshot large enough that a partial write is visible.
		for j := 0; j < 200; j++ {
			s.Packages[fmt.Sprintf("Writer%d.Package%03d", i, j)] = PackageOverride{UpdatePolicy: "hold"}
		}
		snapshots[i] = s
	}
	want := make([]string, writers)
	for i, s := range snapshots {
		b, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want[i] = string(b)
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers*rounds)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(s Settings) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				if err := SaveSettings(s); err != nil {
					errs <- err
					return
				}
			}
		}(snapshots[i])
	}

	// A concurrent reader: every successfully read file must be one of the
	// complete snapshots. Transient open errors during a rename are tolerated;
	// content that parses but is neither snapshot is not.
	stop := make(chan struct{})
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(configPath())
			if err != nil {
				continue
			}
			if got := string(b); got != want[0] && got != want[1] {
				errs <- fmt.Errorf("reader observed a file that is not a complete snapshot (%d bytes): %.80q…", len(b), got)
				return
			}
		}
	}()

	wg.Wait()
	close(stop)
	readerWG.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("two-writer collision: %v", err)
	}

	// Final state: complete and parseable, and no leftover temp files.
	got, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	if s := string(got); s != want[0] && s != want[1] {
		t.Errorf("final settings.json is not a complete snapshot: %.120q", s)
	}
	entries, _ := os.ReadDir(filepath.Dir(configPath()))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".settings") {
			t.Errorf("leftover temp file after SaveSettings: %s", e.Name())
		}
	}
}

// TestUpdateSettingsDisjointKeysFromStaleSnapshotsBothLand is acceptance test
// #2: two WinTUI processes that both loaded the same settings.json, then each
// change a DIFFERENT key, must both land on disk. Simulated in one process by
// resetting appSettings to the stale startup snapshot between the two writes.
func TestUpdateSettingsDisjointKeysFromStaleSnapshotsBothLand(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	stale := seedSettingsFile(t, DefaultSettings())

	// Process A starts, changes the theme.
	setAppSettings(LoadSettings())
	if err := updateSettings(func(s *Settings) { s.setValue("theme", "nord") }); err != nil {
		t.Fatal(err)
	}

	// Process B started earlier with the same stale snapshot and now changes
	// install_mode, knowing nothing about the theme write.
	setAppSettings(stale)
	if err := updateSettings(func(s *Settings) { s.InstallMode = ModeSilent }); err != nil {
		t.Fatal(err)
	}

	got, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "nord" {
		t.Errorf("theme write from process A was lost: theme=%q", got.Theme)
	}
	if got.InstallMode != ModeSilent {
		t.Errorf("install_mode write from process B was lost: install_mode=%q", got.InstallMode)
	}
}

// TestPersistPackageOverrideDoesNotClobberOtherWritersKeys pins the rewired
// per-package override path (fix --portable, the TUI detail editor): it must
// write a delta over the file on disk, not the snapshot held by the caller.
// This is the regression test for the "persistPackageOverride is the pattern
// NOT to copy" finding — it fails against the old clone-the-global-and-save-all
// code.
func TestPersistPackageOverrideDoesNotClobberOtherWritersKeys(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	stale := seedSettingsFile(t, DefaultSettings())
	setAppSettings(stale)

	// Another process changed the theme after we loaded.
	other := stale.clone()
	other.Theme = "dracula"
	if err := SaveSettings(other); err != nil {
		t.Fatal(err)
	}

	if err := persistPackageOverride("Git.Git", "winget", PackageOverride{UpdatePolicy: "hold"}); err != nil {
		t.Fatal(err)
	}

	got, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "dracula" {
		t.Errorf("override write clobbered the theme set by the other writer: theme=%q", got.Theme)
	}
	if _, o, ok := got.lookupOverride("Git.Git", "winget"); !ok || o.UpdatePolicy != "hold" {
		t.Errorf("override not persisted: ok=%v override=%+v", ok, o)
	}
	if _, o, ok := currentSettings().lookupOverride("Git.Git", "winget"); !ok || o.UpdatePolicy != "hold" {
		t.Errorf("override not published to memory: ok=%v override=%+v", ok, o)
	}
}

// TestUpdateSettingsKeepsUnsavedInMemoryEdits pins the "delta applied twice"
// contract: an edit the running TUI has not saved yet must survive a delta
// write in memory, and must NOT be flushed to disk by it.
func TestUpdateSettingsKeepsUnsavedInMemoryEdits(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	seedSettingsFile(t, DefaultSettings())
	mem := LoadSettings()
	mem.Force = true // unsaved settings-screen edit
	setAppSettings(mem)

	if err := updateSettings(func(s *Settings) { s.setValue("theme", "nord") }); err != nil {
		t.Fatal(err)
	}

	if got := currentSettings(); !got.Force || got.Theme != "nord" {
		t.Errorf("memory after delta: force=%v theme=%q, want force=true theme=nord", got.Force, got.Theme)
	}
	disk, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if disk.Force {
		t.Error("unsaved in-memory edit (force) was flushed to disk by a delta write")
	}
	if disk.Theme != "nord" {
		t.Errorf("delta not on disk: theme=%q", disk.Theme)
	}
}

// TestSettingsFileWarningDetectsExternalChangeNotOwnSave covers the doctor
// WARN row: silent after our own load/save, loud after another writer touched
// the file, silent again once we re-read it.
func TestSettingsFileWarningDetectsExternalChangeNotOwnSave(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	seedSettingsFile(t, DefaultSettings())
	setAppSettings(LoadSettings())
	if d, _ := settingsFileWarning(); d != "" {
		t.Fatalf("unexpected warning right after load: %q", d)
	}

	if err := SaveSettings(currentSettings()); err != nil {
		t.Fatal(err)
	}
	if d, _ := settingsFileWarning(); d != "" {
		t.Fatalf("own save must not trigger the drift warning: %q", d)
	}

	// Another process writes the file. Push the mtime forward explicitly so
	// the test does not depend on filesystem timestamp resolution.
	other := DefaultSettings()
	other.Theme = "dracula"
	b, _ := json.MarshalIndent(other, "", "  ")
	if err := os.WriteFile(configPath(), b, 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(configPath(), future, future); err != nil {
		t.Fatal(err)
	}
	d, rec := settingsFileWarning()
	if !strings.Contains(d, "changed on disk") || rec == "" {
		t.Errorf("external change not flagged: detail=%q rec=%q", d, rec)
	}
	if row := checkSettingsSummary(); row.Status != "WARN" || !strings.Contains(row.Details, "changed on disk") {
		t.Errorf("doctor Settings row did not surface drift: %+v", row)
	}

	setAppSettings(LoadSettings())
	if d, _ := settingsFileWarning(); d != "" {
		t.Errorf("warning must clear after reload: %q", d)
	}
}

// TestLoadSettingsSurfacesInvalidJSON: a corrupt file still yields usable
// defaults (unchanged behaviour) but is no longer silent — doctor warns.
func TestLoadSettingsSurfacesInvalidJSON(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	if err := os.MkdirAll(filepath.Dir(configPath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(), []byte("{ \"theme\": \"nord\", "), 0644); err != nil {
		t.Fatal(err)
	}
	got := LoadSettings()
	if !settingsEqual(got, DefaultSettings()) {
		t.Errorf("syntax error should yield defaults, got %+v", got)
	}
	d, rec := settingsFileWarning()
	if !strings.Contains(d, "invalid JSON") || rec == "" {
		t.Errorf("invalid JSON not flagged: detail=%q rec=%q", d, rec)
	}
	if row := checkSettingsSummary(); row.Status != "WARN" {
		t.Errorf("doctor Settings row did not surface invalid JSON: %+v", row)
	}

	// Missing file: defaults, no warning.
	if err := os.Remove(configPath()); err != nil {
		t.Fatal(err)
	}
	_ = LoadSettings()
	if d, _ := settingsFileWarning(); d != "" {
		t.Errorf("missing file must not warn: %q", d)
	}
}

// TestUpdateSettingsRefusesToOverwriteUnreadableFile: a delta write must never
// replace a settings.json it could not parse with defaults-plus-delta. The
// user keeps their (fixable) file and gets an explicit error.
func TestUpdateSettingsRefusesToOverwriteUnreadableFile(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	corrupt := []byte("{ \"theme\": \"nord\", ")
	if err := os.MkdirAll(filepath.Dir(configPath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(), corrupt, 0644); err != nil {
		t.Fatal(err)
	}
	before := currentSettings()

	err := updateSettings(func(s *Settings) { s.InstallMode = ModeSilent })
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("expected an explicit refusal, got err=%v", err)
	}
	got, readErr := os.ReadFile(configPath())
	if readErr != nil || string(got) != string(corrupt) {
		t.Errorf("corrupt file was replaced: %q (%v)", got, readErr)
	}
	if !settingsEqual(currentSettings(), before) {
		t.Error("in-memory settings changed although the write was refused")
	}
	if d, _ := settingsFileWarning(); !strings.Contains(d, "invalid JSON") {
		t.Errorf("doctor should now flag the file: %q", d)
	}

	// Missing file is fine: defaults + delta are created.
	if err := os.Remove(configPath()); err != nil {
		t.Fatal(err)
	}
	if err := updateSettings(func(s *Settings) { s.InstallMode = ModeSilent }); err != nil {
		t.Fatalf("missing file must not block a delta write: %v", err)
	}
	if disk, err := loadSettingsFile(); err != nil || disk.InstallMode != ModeSilent {
		t.Errorf("delta not written to fresh file: %+v %v", disk, err)
	}
}

// TestWriteFileAtomicLeavesNoTempOnFailure: a failed publish must not litter
// the state dir with temp files.
func TestWriteFileAtomicLeavesNoTempOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.json")
	if err := writeFileAtomic(target, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Rename onto a directory fails on every platform; the temp must be gone.
	blocker := filepath.Join(dir, "blocked.json")
	if err := os.Mkdir(blocker, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(blocker, []byte(`{"a":2}`), 0644); err == nil {
		t.Fatal("expected rename onto a directory to fail")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != `{"a":1}` {
		t.Errorf("successful write not intact: %q %v", b, err)
	}
}
