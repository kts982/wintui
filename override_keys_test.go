package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

// TestLookupOverrideIsCaseInsensitive: the CLI accepts any ID casing, so the
// map must too — `rules set git.git` must not silently miss `Git.Git`.
func TestLookupOverrideIsCaseInsensitive(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"winget:Git.Git": {Scope: ScopeUser},
		"Legacy.Bare":    {Scope: ScopeMachine},
	}
	for _, tc := range []struct {
		id, source, wantKey string
		wantScope           InstallScope
	}{
		{"git.git", "winget", "winget:Git.Git", ScopeUser},
		{"GIT.GIT", "WINGET", "winget:Git.Git", ScopeUser},
		{"Git.Git", "winget", "winget:Git.Git", ScopeUser},
		{"legacy.bare", "winget", "Legacy.Bare", ScopeMachine},
		{"legacy.bare", "", "Legacy.Bare", ScopeMachine},
	} {
		key, o, ok := s.lookupOverride(tc.id, tc.source)
		if !ok || key != tc.wantKey || o.Scope != tc.wantScope {
			t.Errorf("lookup(%q,%q) = %q %+v %v, want %q scope=%q", tc.id, tc.source, key, o, ok, tc.wantKey, tc.wantScope)
		}
	}
	if _, _, ok := s.lookupOverride("Git.Git", "msstore"); ok {
		t.Error("qualified key of another source must not match")
	}
}

// TestLookupOverridePrecedenceIsTotalAndDeterministic pins the order: exact
// qualified → exact bare → case-insensitive qualified → case-insensitive bare,
// sorted ties. 100 lookups with conflicting case-variant duplicates present
// must answer identically every time (Go map order is random).
func TestLookupOverridePrecedenceIsTotalAndDeterministic(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"winget:git.git": {Scope: ScopeUser},    // ci qualified
		"winget:GIT.GIT": {Scope: ScopeMachine}, // ci qualified, sorts first (uppercase < lowercase)
		"git.git":        {Architecture: ArchX64},
		"Git.Git":        {Architecture: ArchX86}, // exact bare for "Git.Git"
	}

	// Exact bare beats case-insensitive qualified.
	if key, _, _ := s.lookupOverride("Git.Git", "winget"); key != "Git.Git" {
		t.Errorf("exact bare should beat ci qualified, got %q", key)
	}
	// With no exact match anywhere, the sorted-first ci qualified wins.
	for i := 0; i < 100; i++ {
		key, o, ok := s.lookupOverride("gIt.gIt", "winget")
		if !ok || key != "winget:GIT.GIT" || o.Scope != ScopeMachine {
			t.Fatalf("iteration %d: ci qualified tie not deterministic: %q %+v", i, key, o)
		}
	}
	// Exact qualified beats everything.
	s.Packages["winget:Git.Git"] = PackageOverride{Elevate: boolPtr(false)}
	for i := 0; i < 100; i++ {
		if key, _, _ := s.lookupOverride("Git.Git", "winget"); key != "winget:Git.Git" {
			t.Fatalf("iteration %d: exact qualified must win, got %q", i, key)
		}
	}
	// Bare-only lookups never see qualified keys; sorted-first ci bare wins.
	for i := 0; i < 100; i++ {
		if key, _, _ := s.lookupOverride("GIT.git", ""); key != "Git.Git" {
			t.Fatalf("iteration %d: sorted-first ci bare expected, got %q", i, key)
		}
	}
}

// TestSetOverrideReusesExistingKeyCasingAndConsolidates: a write never re-keys
// an existing rule by the typed casing, and always leaves exactly one key.
func TestSetOverrideReusesExistingKeyCasingAndConsolidates(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"winget:Git.Git": {Scope: ScopeUser},
		"winget:git.git": {Scope: ScopeMachine},
		"git.git":        {Architecture: ArchX64},
	}
	s.setOverride("GIT.GIT", "winget", PackageOverride{Elevate: boolPtr(true)})
	if len(s.Packages) != 1 {
		t.Fatalf("expected exactly one key after write, got %v", keysOf(s.Packages))
	}
	o, ok := s.Packages["winget:Git.Git"]
	if !ok || o.Elevate == nil || !*o.Elevate {
		t.Fatalf("rule not written under the existing exact key casing: %v", keysOf(s.Packages))
	}

	// No existing qualified key: the sorted-first case variant is reused.
	s.Packages = map[string]PackageOverride{
		"winget:git.git": {Scope: ScopeUser},
		"winget:GIT.GIT": {Scope: ScopeMachine},
	}
	s.setOverride("Git.Git", "winget", PackageOverride{Scope: ScopeUser})
	if _, ok := s.Packages["winget:GIT.GIT"]; !ok || len(s.Packages) != 1 {
		t.Errorf("expected sorted-first variant reused, got %v", keysOf(s.Packages))
	}

	// Brand-new rule: keyed exactly as typed (callers canonicalize first).
	s.Packages = nil
	s.setOverride("Neovim.Neovim", "winget", PackageOverride{Scope: ScopeUser})
	if _, ok := s.Packages["winget:Neovim.Neovim"]; !ok {
		t.Errorf("new rule not keyed as typed: %v", keysOf(s.Packages))
	}

	// Empty rule deletes every alias, both tiers.
	s.Packages = map[string]PackageOverride{
		"winget:Git.Git": {Scope: ScopeUser},
		"winget:git.git": {Scope: ScopeMachine},
		"git.git":        {Architecture: ArchX64},
		"winget:Other":   {Scope: ScopeUser},
	}
	s.setOverride("git.GIT", "winget", PackageOverride{})
	if len(s.Packages) != 1 || s.Packages["winget:Other"].Scope != ScopeUser {
		t.Errorf("clear must delete all aliases and nothing else, got %v", keysOf(s.Packages))
	}
}

// TestSetOverrideCanonicalizesLegacyIgnore: `ignore: true` is written as
// `update_policy: hold` by any edit, so the CLI has one permanent-hold concept.
func TestSetOverrideCanonicalizesLegacyIgnore(t *testing.T) {
	s := DefaultSettings()
	s.setOverride("Git.Git", "winget", PackageOverride{Ignore: true, Scope: ScopeUser})
	o := s.Packages["winget:Git.Git"]
	if o.Ignore || o.UpdatePolicy != PolicyHold || o.Scope != ScopeUser {
		t.Errorf("legacy ignore not canonicalized: %+v", o)
	}
	if o.effectiveUpdatePolicy("1.0") != PolicyHold || o.displayedUpdatePolicy() != PolicyHold {
		t.Errorf("canonical hold must still hold: %+v", o)
	}
	// Version-specific holds are a distinct state and survive untouched.
	s.setOverride("Git.Git", "winget", PackageOverride{IgnoreVersion: "2.0"})
	if o := s.Packages["winget:Git.Git"]; o.IgnoreVersion != "2.0" || o.UpdatePolicy != PolicyAsk {
		t.Errorf("version hold altered: %+v", o)
	}
}

// TestDetailToggleIgnoreClearsCanonicalHold: the TUI ignore toggle must treat
// the canonical `update_policy: hold` exactly like the legacy flag it replaces
// — a second press clears the hold instead of stacking a version hold on top.
func TestDetailToggleIgnoreClearsCanonicalHold(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })
	setAppSettings(DefaultSettings())

	p := detailPanel{pkgID: "Git.Git", source: "winget", state: detailReady}
	p, _, _ = p.toggleIgnore() // no versions known → permanent hold
	if o := currentSettings().getOverride("Git.Git", "winget"); o.UpdatePolicy != PolicyHold || o.Ignore {
		t.Fatalf("first toggle should produce a canonical hold, got %+v", o)
	}
	p, _, _ = p.toggleIgnore()
	if o := currentSettings().getOverride("Git.Git", "winget"); !o.isEmpty() {
		t.Fatalf("second toggle should clear the hold, got %+v", o)
	}
	_ = p
}

// TestLoadSettingsCollapsesEquivalentDuplicatesLeavesConflicts: the one-shot
// load-time migration folds value-equivalent case variants and keeps
// conflicting ones (deterministic precedence still answers; doctor warns).
func TestLoadSettingsCollapsesEquivalentDuplicatesLeavesConflicts(t *testing.T) {
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })

	raw := map[string]any{
		"packages": map[string]any{
			"winget:Git.Git":       map[string]any{"scope": "user"},
			"winget:git.git":       map[string]any{"scope": "user"},
			"winget:GIT.GIT":       map[string]any{"ignore": true, "scope": "user"}, // equivalent? no: hold ≠ ask
			"winget:Neovim.Neovim": map[string]any{"scope": "machine"},
			"winget:neovim.neovim": map[string]any{"scope": "machine"},
			"Legacy.Bare":          map[string]any{"architecture": "x64"},
			"legacy.bare":          map[string]any{"architecture": "x64"},
		},
	}
	b, _ := json.Marshal(raw)
	if err := os.MkdirAll(filepath.Dir(configPath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(), b, 0644); err != nil {
		t.Fatal(err)
	}

	s := LoadSettings()
	if _, ok := s.Packages["winget:neovim.neovim"]; ok {
		t.Error("equivalent duplicate not collapsed (neovim)")
	}
	if _, ok := s.Packages["winget:Neovim.Neovim"]; !ok {
		t.Error("sorted-first key must survive (Neovim)")
	}
	if _, ok := s.Packages["legacy.bare"]; ok {
		t.Error("equivalent bare duplicate not collapsed")
	}
	// The Git group conflicts (one member is a hold): all three stay.
	for _, k := range []string{"winget:Git.Git", "winget:git.git", "winget:GIT.GIT"} {
		if _, ok := s.Packages[k]; !ok {
			t.Errorf("conflicting duplicate %q must be left in place", k)
		}
	}
	conflicts := s.overrideConflicts()
	if len(conflicts) != 1 || strings.Join(conflicts[0], ",") != "winget:GIT.GIT,winget:Git.Git,winget:git.git" {
		t.Errorf("conflict groups = %v", conflicts)
	}
	if group, ok := s.overrideConflictFor("git.git", "winget"); !ok || len(group) != 3 {
		t.Errorf("overrideConflictFor should flag the Git group, got %v %v", group, ok)
	}
	if _, ok := s.overrideConflictFor("Neovim.Neovim", "winget"); ok {
		t.Error("collapsed group must not be reported as a conflict")
	}
	setAppSettings(s)
	if row := checkSettingsSummary(); row.Status != "WARN" || !strings.Contains(row.Details, "winget:GIT.GIT / winget:Git.Git / winget:git.git") {
		t.Errorf("doctor row should list the conflicting group: %+v", row)
	}

	// A write to the conflicting package consolidates it under the exact key.
	s.setOverride("Git.Git", "winget", PackageOverride{Scope: ScopeMachine})
	if len(s.overrideConflicts()) != 0 || len(s.overrideKeyAliases("git.git", "winget")) != 1 {
		t.Errorf("write did not consolidate the conflicting group: %v", keysOf(s.Packages))
	}
}

// TestCanonicalPackageIDUsesWarmCacheCasing: user typing picks up winget's
// own casing from the read-only disk cache when it is warm, and is returned
// unchanged when the cache is cold or ignorant.
func TestCanonicalPackageIDUsesWarmCacheCasing(t *testing.T) {
	useTempSettingsDir(t)

	if got, ok := canonicalPackageID("git.git"); ok || got != "git.git" {
		t.Errorf("cold cache should pass input through, got %q %v", got, ok)
	}
	data := diskCacheData{
		Installed:   toCachedPackages([]Package{{ID: "Git.Git", Name: "Git", Source: "winget"}}),
		Upgradeable: toCachedPackages([]Package{{ID: "Neovim.Neovim", Name: "Neovim", Source: "winget"}}),
		SavedAt:     time.Now(),
	}
	b, _ := json.Marshal(data)
	if err := os.MkdirAll(filepath.Dir(diskCachePath()), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diskCachePath(), b, 0644); err != nil {
		t.Fatal(err)
	}
	if got, ok := canonicalPackageID("GIT.GIT"); !ok || got != "Git.Git" {
		t.Errorf("installed casing not applied: %q %v", got, ok)
	}
	if got, ok := canonicalPackageID("neovim.neovim"); !ok || got != "Neovim.Neovim" {
		t.Errorf("upgradeable casing not applied: %q %v", got, ok)
	}
	if got, ok := canonicalPackageID("Unknown.Pkg"); ok || got != "Unknown.Pkg" {
		t.Errorf("unknown package must pass through: %q %v", got, ok)
	}
}

func keysOf(m map[string]PackageOverride) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
