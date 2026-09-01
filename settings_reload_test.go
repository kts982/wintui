package main

import (
	"os"
	"testing"
	"time"
)

// externalSettingsWrite simulates another WinTUI process changing
// settings.json after this one loaded it: it writes the file (pushing the
// mtime forward explicitly so the test does not depend on timestamp
// resolution) WITHOUT touching this process's recorded mtime or memory.
func externalSettingsWrite(t *testing.T, mutate func(*Settings)) {
	t.Helper()
	disk, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	// Remember what this process believes, then write as a stranger would.
	settingsFileMu.Lock()
	seen, mtime := settingsFileSeen, settingsFileMtime
	settingsFileMu.Unlock()
	mutate(&disk)
	if err := SaveSettings(disk); err != nil {
		t.Fatal(err)
	}
	settingsFileMu.Lock()
	settingsFileSeen, settingsFileMtime = seen, mtime
	settingsFileMu.Unlock()
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(configPath(), future, future); err != nil {
		t.Fatal(err)
	}
}

func TestReloadSettingsIfChangedOnDisk(t *testing.T) {
	setupConfigTest(t)
	t.Cleanup(func() { settingsScreenDirty = false })
	settingsScreenDirty = false

	if reloadSettingsIfChangedOnDisk() {
		t.Fatal("nothing changed on disk; must not reload")
	}
	externalSettingsWrite(t, func(s *Settings) {
		s.setOverride("Notepad++.Notepad++", "winget", PackageOverride{UpdatePolicy: PolicyHold})
		s.Theme = "nord"
	})
	if _, _, ok := currentSettings().lookupOverride("Notepad++.Notepad++", "winget"); ok {
		t.Fatal("memory must still be stale before the reload")
	}

	// Unsaved Settings-screen edits block the reload.
	settingsScreenDirty = true
	if reloadSettingsIfChangedOnDisk() {
		t.Fatal("must not reload over unsaved Settings-screen edits")
	}
	settingsScreenDirty = false

	if !reloadSettingsIfChangedOnDisk() {
		t.Fatal("expected a reload after an external change")
	}
	got := currentSettings()
	if got.updatePolicy("Notepad++.Notepad++", "winget", "8.9.8") != PolicyHold || got.Theme != "nord" {
		t.Errorf("reload did not pick up the external change: %+v", got)
	}
	if reloadSettingsIfChangedOnDisk() {
		t.Error("a second call right after the reload must be a no-op")
	}
}

// TestWorkspaceRefreshPicksUpCLIRuleChange: pressing `r` on the Packages
// screen re-reads settings.json when a CLI command changed it, so the
// rebuilt list honours the new rule without restarting the TUI.
func TestWorkspaceRefreshPicksUpCLIRuleChange(t *testing.T) {
	setupConfigTest(t)
	t.Cleanup(func() { settingsScreenDirty = false })
	settingsScreenDirty = false

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	externalSettingsWrite(t, func(s *Settings) {
		s.setOverride("Notepad++.Notepad++", "winget", PackageOverride{UpdatePolicy: PolicyHold})
	})

	next, _ := ws.update(keyMsg("r"))
	if _, ok := next.(workspaceScreen); !ok {
		t.Fatalf("unexpected screen type %T", next)
	}
	if got := currentSettings().updatePolicy("Notepad++.Notepad++", "winget", "8.9.8"); got != PolicyHold {
		t.Errorf("refresh did not reload the CLI rule change: policy=%q", got)
	}
	// The rebuilt list must hide the held package like a fresh start would.
	_, hidden := buildItems(nil, []Package{{ID: "Notepad++.Notepad++", Name: "Notepad++", Source: "winget", Version: "8.9.7", Available: "8.9.8"}})
	if hidden == 0 {
		t.Error("held package should be hidden from the upgrade list after the reload")
	}
}

// TestSettingsScreenReloadResetsBaseline: the Settings screen created after an
// external change does not misreport it as edits, and a reload message resets
// the dirty baseline of an existing screen.
func TestSettingsScreenReloadResetsBaseline(t *testing.T) {
	setupConfigTest(t)
	t.Cleanup(func() { settingsScreenDirty = false })

	externalSettingsWrite(t, func(s *Settings) { s.InstallMode = ModeSilent })
	sc := newSettingsScreen()
	if sc.dirty || settingsScreenDirty {
		t.Fatal("a screen created after an external change must not start dirty")
	}
	if currentSettings().InstallMode != ModeSilent {
		t.Fatal("creating the Settings screen should have reloaded the external change")
	}

	// An existing screen with the reload message: baseline reset.
	sc.markDirty(true)
	next, _ := sc.update(settingsReloadedMsg{})
	got := next.(settingsScreen)
	if got.dirty || settingsScreenDirty || !settingsEqual(got.diskState, currentSettings()) {
		t.Errorf("reload message did not reset the baseline: dirty=%v global=%v", got.dirty, settingsScreenDirty)
	}
}
