package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIsPortableInstalled(t *testing.T) {
	dirs := []string{
		"Anthropic.ClaudeCode_Microsoft.Winget.Source_8wekyb3d8bbwe",
		"Foo.BarBaz_Microsoft.Winget.Source_8wekyb3d8bbwe",
	}
	if !isPortableInstalled("Anthropic.ClaudeCode", dirs) {
		t.Error("expected Anthropic.ClaudeCode to match its package dir")
	}
	// Prefix-collision guard: "Foo.Bar" must NOT match "Foo.BarBaz_...".
	if isPortableInstalled("Foo.Bar", dirs) {
		t.Error("Foo.Bar must not match Foo.BarBaz dir (underscore separator)")
	}
	if isPortableInstalled("Not.Installed", dirs) {
		t.Error("unexpected match for a package with no dir")
	}
	if isPortableInstalled("", dirs) {
		t.Error("empty id must never match")
	}
}

// fixPortableHarness wires the three seams and returns the captured applies.
func fixPortableHarness(t *testing.T, installed []Package, dirs []string) *[]struct {
	id, source string
	o          PackageOverride
} {
	t.Helper()
	origInst := fixPortableInstalledFn
	origDirs := fixPortablePackageDirsFn
	origApply := fixPortableApplyFn
	fixPortableInstalledFn = func(ctx context.Context) ([]Package, error) { return installed, nil }
	fixPortablePackageDirsFn = func() []string { return dirs }
	var applied []struct {
		id, source string
		o          PackageOverride
	}
	fixPortableApplyFn = func(id, source string, o PackageOverride) error {
		applied = append(applied, struct {
			id, source string
			o          PackageOverride
		}{id, source, o})
		return nil
	}
	t.Cleanup(func() {
		fixPortableInstalledFn = origInst
		fixPortablePackageDirsFn = origDirs
		fixPortableApplyFn = origApply
	})
	return &applied
}

func TestRunFixPortablePinsPortableOnly(t *testing.T) {
	installed := []Package{
		{ID: "Anthropic.ClaudeCode", Name: "Claude Code", Source: "winget"},
		{ID: "Mozilla.Firefox", Name: "Firefox", Source: "winget"}, // MSI, not in Packages dir
	}
	dirs := []string{"Anthropic.ClaudeCode_Microsoft.Winget.Source_x"}
	applied := fixPortableHarness(t, installed, dirs)

	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{}, DefaultSettings(), &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	if len(*applied) != 1 {
		t.Fatalf("applied %d overrides, want 1 (portable only): %+v", len(*applied), *applied)
	}
	a := (*applied)[0]
	if a.id != "Anthropic.ClaudeCode" || a.source != "winget" {
		t.Errorf("applied to %s:%s, want winget:Anthropic.ClaudeCode", a.source, a.id)
	}
	if a.o.Scope != ScopeUser || a.o.Elevate == nil || *a.o.Elevate {
		t.Errorf("override = %+v, want scope=user elevate=false", a.o)
	}
	if !strings.Contains(buf.String(), "Claude Code") {
		t.Errorf("output should name the pinned package:\n%s", buf.String())
	}
}

func TestRunFixPortableMergesExistingOverride(t *testing.T) {
	installed := []Package{{ID: "Tool.CLI", Source: "winget"}}
	dirs := []string{"Tool.CLI_src"}
	applied := fixPortableHarness(t, installed, dirs)

	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Tool.CLI", "winget"): {UpdatePolicy: PolicyHold},
	}

	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{}, settings, &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	if len(*applied) != 1 {
		t.Fatalf("applied %d, want 1", len(*applied))
	}
	o := (*applied)[0].o
	if o.UpdatePolicy != PolicyHold {
		t.Errorf("merge dropped existing UpdatePolicy: %+v", o)
	}
	if o.Scope != ScopeUser || o.Elevate == nil || *o.Elevate {
		t.Errorf("override = %+v, want scope=user elevate=false plus the kept policy", o)
	}
}

func TestRunFixPortableIdempotent(t *testing.T) {
	installed := []Package{{ID: "Tool.CLI", Source: "winget"}}
	dirs := []string{"Tool.CLI_src"}
	applied := fixPortableHarness(t, installed, dirs)

	f := false
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Tool.CLI", "winget"): {Scope: ScopeUser, Elevate: &f},
	}

	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{}, settings, &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	if len(*applied) != 0 {
		t.Errorf("already-pinned package must not be re-applied, got %+v", *applied)
	}
	if !strings.Contains(buf.String(), "already pinned") {
		t.Errorf("expected 'already pinned' note:\n%s", buf.String())
	}
}

func TestRunFixPortableDryRunWritesNothing(t *testing.T) {
	installed := []Package{{ID: "Tool.CLI", Name: "CLI", Source: "winget"}}
	dirs := []string{"Tool.CLI_src"}
	applied := fixPortableHarness(t, installed, dirs)

	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{DryRun: true}, DefaultSettings(), &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	if len(*applied) != 0 {
		t.Errorf("--dry-run must not write overrides, got %+v", *applied)
	}
	out := buf.String()
	if !strings.Contains(out, "Would pin") || !strings.Contains(out, "Dry run") {
		t.Errorf("dry-run output wrong:\n%s", out)
	}
}

func TestRunFixPortableJSON(t *testing.T) {
	installed := []Package{
		{ID: "A.Portable", Source: "winget"},
		{ID: "B.MSI", Source: "winget"},
	}
	dirs := []string{"A.Portable_src"}
	fixPortableHarness(t, installed, dirs)

	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{JSON: true}, DefaultSettings(), &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	var payload fixPortableJSON
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(payload.Fixed) != 1 || payload.Fixed[0] != "A.Portable" {
		t.Errorf("fixed = %v, want [A.Portable]", payload.Fixed)
	}
}

func TestRunFixPortableNonePresent(t *testing.T) {
	installed := []Package{{ID: "Only.MSI", Source: "winget"}}
	applied := fixPortableHarness(t, installed, nil)

	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{}, DefaultSettings(), &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	if len(*applied) != 0 {
		t.Errorf("nothing portable, expected no applies")
	}
	if !strings.Contains(buf.String(), "No portable winget packages found") {
		t.Errorf("expected nothing-to-do message:\n%s", buf.String())
	}
}

// Regression: the advisory's "already pinned" check and fix --portable's must
// agree. A scope=user override with Elevate=nil must be treated as NOT-yet-pinned
// by BOTH (the advisory fires AND fix --portable completes the pin to elevate=false).
func TestPortablePredicateConsistencyScopeUserElevateNil(t *testing.T) {
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Half.Pinned", "winget"): {Scope: ScopeUser}, // Elevate nil
	}
	dirs := []string{"Half.Pinned_src"}

	// Advisory side: must flag it as at-risk (not silently "safe").
	origDirs := fixPortablePackageDirsFn
	fixPortablePackageDirsFn = func() []string { return dirs }
	t.Cleanup(func() { fixPortablePackageDirsFn = origDirs })

	risky := portableUnpinned([]Package{{ID: "Half.Pinned", Source: "winget"}}, settings)
	if len(risky) != 1 {
		t.Fatalf("portableUnpinned = %v, want it flagged (scope=user but elevate=nil)", risky)
	}

	// fix --portable side: must complete the pin (set elevate=false).
	applied := fixPortableHarness(t, []Package{{ID: "Half.Pinned", Source: "winget"}}, dirs)
	var buf bytes.Buffer
	if err := runFixPortable(fixPortableOptions{}, settings, &buf); err != nil {
		t.Fatalf("runFixPortable: %v", err)
	}
	if len(*applied) != 1 {
		t.Fatalf("expected fix to pin the half-pinned package, applied %+v", *applied)
	}
	if (*applied)[0].o.Elevate == nil || *(*applied)[0].o.Elevate {
		t.Errorf("fix should set elevate=false, got %+v", (*applied)[0].o)
	}
}

func TestFilterPackagesByIDs(t *testing.T) {
	raw := []Package{
		{ID: "Mozilla.Firefox", Source: "winget"},
		{ID: "Git.Git", Source: "winget"},
		{ID: "Other.Pkg", Source: "winget"},
	}
	got := filterPackagesByIDs(raw, []string{"mozilla.firefox", " Git.Git "})
	ids := map[string]bool{}
	for _, p := range got {
		ids[p.ID] = true
	}
	if len(got) != 2 || !ids["Mozilla.Firefox"] || !ids["Git.Git"] || ids["Other.Pkg"] {
		t.Fatalf("filtered = %+v, want Firefox+Git only (case-insensitive, trimmed)", got)
	}
}

// Regression: `upgrade --id <held portable>` must not get the advisory — a held
// package errors instead of upgrading, so it isn't "about to upgrade".
func TestAdvisoryPackagesForIDsExcludesHeld(t *testing.T) {
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Held.CLI", "winget"): {UpdatePolicy: PolicyHold},
	}
	raw := []Package{
		{ID: "Held.CLI", Source: "winget", Version: "1.0", Available: "2.0"},
		{ID: "Live.CLI", Source: "winget", Version: "1.0", Available: "2.0"},
		{ID: "Other.CLI", Source: "winget", Version: "1.0", Available: "2.0"},
	}
	got := advisoryPackagesForIDs(raw, []string{"Held.CLI", "Live.CLI"}, settings)
	if len(got) != 1 || got[0].ID != "Live.CLI" {
		t.Fatalf("advisory set = %v, want only [Live.CLI] (held requested id excluded)", got)
	}
}

// Regression: the advisory now receives the planned upgrade set, so a HELD
// portable package (excluded from plan.Visible) must not trigger the nudge.
func TestPortableAdvisoryNotShownForHeldPackages(t *testing.T) {
	origDirs := fixPortablePackageDirsFn
	fixPortablePackageDirsFn = func() []string { return []string{"Held.CLI_src", "Visible.CLI_src"} }
	t.Cleanup(func() { fixPortablePackageDirsFn = origDirs })

	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Held.CLI", "winget"): {UpdatePolicy: PolicyHold},
	}
	raw := []Package{
		{ID: "Held.CLI", Source: "winget", Version: "1.0", Available: "2.0"},
		{ID: "Visible.CLI", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	plan := planUpgrades(raw, settings)
	risky := portableUnpinned(plan.Visible, settings) // what runUpgradeAll now passes
	if len(risky) != 1 || risky[0].ID != "Visible.CLI" {
		t.Fatalf("advisory set = %v, want only [Visible.CLI] — the held portable must be excluded", risky)
	}
}

func TestPortableUpgradeAdvisory(t *testing.T) {
	origDirs := fixPortablePackageDirsFn
	fixPortablePackageDirsFn = func() []string { return []string{"Risky.CLI_src", "Pinned.CLI_src"} }
	t.Cleanup(func() { fixPortablePackageDirsFn = origDirs })

	f := false
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Pinned.CLI", "winget"): {Scope: ScopeUser, Elevate: &f},
	}
	pkgs := []Package{
		{ID: "Risky.CLI", Source: "winget"},  // portable, not pinned -> at risk
		{ID: "Pinned.CLI", Source: "winget"}, // portable, already pinned -> safe
		{ID: "Some.MSI", Source: "winget"},   // not portable -> safe
	}

	risky := portableUnpinned(pkgs, settings)
	if len(risky) != 1 || risky[0].ID != "Risky.CLI" {
		t.Fatalf("portableUnpinned = %v, want [Risky.CLI]", risky)
	}

	var buf bytes.Buffer
	printPortableUpgradeAdvisory(&buf, pkgs, settings)
	if !strings.Contains(buf.String(), "wintui fix --portable") {
		t.Errorf("advisory should point at the fix command:\n%s", buf.String())
	}

	// No advisory when nothing is at risk.
	buf.Reset()
	printPortableUpgradeAdvisory(&buf, []Package{{ID: "Pinned.CLI", Source: "winget"}}, settings)
	if buf.Len() != 0 {
		t.Errorf("expected no advisory when all portables are pinned, got %q", buf.String())
	}
}
