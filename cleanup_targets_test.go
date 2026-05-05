package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupRegistryIDsAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, def := range cleanupTargetRegistry() {
		if def.id == "" {
			t.Fatalf("registry entry %q has empty id", def.label)
		}
		if seen[def.id] {
			t.Fatalf("registry entry id %q is duplicated; IDs are persisted in settings and must be unique", def.id)
		}
		seen[def.id] = true
	}
}

func TestCleanupRegistryEntriesWellFormed(t *testing.T) {
	validGroups := map[cleanupGroup]bool{
		cleanupGroupCoreTemp:  true,
		cleanupGroupCaches:    true,
		cleanupGroupDeveloper: true,
		cleanupGroupGPU:       true,
	}

	for _, def := range cleanupTargetRegistry() {
		if def.label == "" {
			t.Errorf("%s: label is empty", def.id)
		}
		if def.description == "" {
			t.Errorf("%s: description is empty (detail pane needs it)", def.id)
		}
		if !validGroups[def.group] {
			t.Errorf("%s: group %q is not one of the four valid groups", def.id, def.group)
		}
		if def.pathFn == nil {
			t.Errorf("%s: pathFn is nil", def.id)
		}
		if def.mode == cleanupModeGlob && len(def.globs) == 0 {
			t.Errorf("%s: glob mode requires at least one pattern", def.id)
		}
		if def.mode == cleanupModePurgeContents && len(def.globs) != 0 {
			t.Errorf("%s: globs are only meaningful with cleanupModeGlob", def.id)
		}
	}
}

// The locked v2.7.0 design fixes group-level defaults: Core Temp + Caches
// auto-check on tab open; Developer + GPU stay off until the user opts in.
func TestCleanupRegistryDefaultCheckedFollowsGroup(t *testing.T) {
	for _, def := range cleanupTargetRegistry() {
		switch def.group {
		case cleanupGroupCoreTemp, cleanupGroupCaches:
			if !def.defaultChecked {
				t.Errorf("%s: group %q members must default-check", def.id, def.group)
			}
			if def.detectIfPresent {
				t.Errorf("%s: group %q members are always shown, never detect-if-present", def.id, def.group)
			}
		case cleanupGroupDeveloper, cleanupGroupGPU:
			if def.defaultChecked {
				t.Errorf("%s: group %q members must default-uncheck (user opts in)", def.id, def.group)
			}
			if !def.detectIfPresent {
				t.Errorf("%s: group %q members must be detect-if-present", def.id, def.group)
			}
		}
	}
}

// requiresAdmin must only be set on entries the design actually marks as
// system-owned. Misclassifying a per-user target would suggest an unnecessary
// elevation prompt.
func TestCleanupRegistryAdminTargets(t *testing.T) {
	wantAdmin := map[string]bool{
		"windows_temp": true,
		"minidump":     true,
	}
	for _, def := range cleanupTargetRegistry() {
		if def.requiresAdmin && !wantAdmin[def.id] {
			t.Errorf("%s: requiresAdmin=true is unexpected — confirm the path actually needs SYSTEM access", def.id)
		}
		if !def.requiresAdmin && wantAdmin[def.id] {
			t.Errorf("%s: should be marked requiresAdmin", def.id)
		}
	}
}

func TestCleanupTargetByIDFound(t *testing.T) {
	def, ok := cleanupTargetByID("user_temp")
	if !ok {
		t.Fatal("expected user_temp to resolve")
	}
	if def.group != cleanupGroupCoreTemp {
		t.Errorf("user_temp.group = %q, want core_temp", def.group)
	}
}

func TestCleanupTargetByIDMissing(t *testing.T) {
	if _, ok := cleanupTargetByID("does_not_exist"); ok {
		t.Fatal("expected lookup miss to return ok=false")
	}
}

func TestCleanupResolveEnvDirMissing(t *testing.T) {
	t.Setenv("WINTUI_TEST_MISSING", "")
	if got := cleanupResolveEnvDir("WINTUI_TEST_MISSING", "sub"); got != "" {
		t.Errorf("missing env should return empty, got %q", got)
	}
}

func TestCleanupResolveEnvDirJoins(t *testing.T) {
	t.Setenv("WINTUI_TEST_BASE", `C:\base`)
	got := cleanupResolveEnvDir("WINTUI_TEST_BASE", "child", "leaf")
	want := filepath.Join(`C:\base`, "child", "leaf")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCleanupValidateRootRejectsGuardedPaths(t *testing.T) {
	t.Setenv("WINDIR", `C:\Windows`)
	t.Setenv("USERPROFILE", `C:\Users\test`)
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	t.Setenv("PROGRAMDATA", `C:\ProgramData`)
	t.Setenv("PROGRAMFILES", `C:\Program Files`)
	t.Setenv("PROGRAMFILES(X86)", `C:\Program Files (x86)`)

	guarded := []string{
		"",
		`C:\`,
		`C:`,
		`C:\Windows`,
		`c:\windows`, // case-insensitive
		`C:\Users\test`,
		`C:\Users\test\AppData\Local`,
		`C:\Users\test\AppData\Roaming`,
		`C:\ProgramData`,
		`C:\Program Files`,
		`C:\Program Files (x86)`,
	}
	for _, p := range guarded {
		if err := cleanupValidateRoot(p); err == nil {
			t.Errorf("expected guarded path %q to be rejected", p)
		}
	}
}

func TestCleanupValidateRootAcceptsNormalPaths(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)

	tmp := t.TempDir()
	ok := []string{
		tmp,
		filepath.Join(tmp, "child"),
		`C:\Users\test\AppData\Local\Temp`,
		`C:\Users\test\AppData\Local\NVIDIA\DXCache`,
	}
	for _, p := range ok {
		if err := cleanupValidateRoot(p); err != nil {
			t.Errorf("expected %q to be accepted, got %v", p, err)
		}
	}
}

// minAge defaults: Core Temp entries must filter by age (week-old cutoff).
// Caches and vendor caches are owned by the producing app — age filtering
// adds no safety, just clutter.
func TestCleanupRegistryMinAgePolicy(t *testing.T) {
	for _, def := range cleanupTargetRegistry() {
		switch def.group {
		case cleanupGroupCoreTemp:
			if def.minAge != cleanupDefaultMinAge {
				t.Errorf("%s: core_temp must use the default min age (got %s)", def.id, def.minAge)
			}
		case cleanupGroupCaches, cleanupGroupDeveloper, cleanupGroupGPU:
			if def.minAge != 0 {
				t.Errorf("%s: %q targets should not age-filter (got %s)", def.id, def.group, def.minAge)
			}
		}
	}

	// Sanity: the default we picked is on the conservative side, not minutes.
	if cleanupDefaultMinAge < 24*time.Hour {
		t.Errorf("cleanupDefaultMinAge looks too aggressive: %s", cleanupDefaultMinAge)
	}
}

// Sanity-check that a few well-known IDs the helper and settings code will
// reference are actually present, so a typo in the registry breaks tests
// instead of silently dropping a target.
func TestCleanupRegistryHasKnownIDs(t *testing.T) {
	for _, want := range []string{
		"user_temp", "windows_temp", "crash_dumps", "wer_reports", "minidump",
		"d3dscache", "thumbcache",
		"npm_cache", "go_build", "pip_cache", "yarn_cache",
		"nvidia_dx_cache", "amd_dx_cache", "intel_shader_cache",
	} {
		if _, ok := cleanupTargetByID(want); !ok {
			t.Errorf("registry is missing expected id %q", want)
		}
	}
}

// Locked default min-age value, asserted directly so a future tweak shows up
// in the diff rather than slipping in silently.
func TestCleanupDefaultMinAgeIsSevenDays(t *testing.T) {
	if cleanupDefaultMinAge != 7*24*time.Hour {
		t.Errorf("cleanupDefaultMinAge = %s, want 7 days", cleanupDefaultMinAge)
	}
}

// Make sure os.TempDir() never returns empty (Windows always has a temp dir);
// without this guarantee, the user_temp registry entry could vanish at
// runtime.
func TestUserTempPathFnAlwaysResolves(t *testing.T) {
	def, _ := cleanupTargetByID("user_temp")
	if got := def.pathFn(); got == "" {
		t.Fatal("user_temp pathFn returned empty — os.TempDir contract broken")
	}
	// And the default temp dir is never one of our guarded roots.
	if err := cleanupValidateRoot(os.TempDir()); err != nil {
		t.Errorf("os.TempDir() is itself guarded: %v", err)
	}
}
