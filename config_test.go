package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestEffectiveSettingsMergesOverrides(t *testing.T) {
	s := DefaultSettings()
	s.Scope = "user"
	s.Architecture = "x64"
	s.AutoElevate = false
	elevTrue := true
	s.Packages = map[string]PackageOverride{
		"Mozilla.Firefox": {
			Scope:        "machine",
			Architecture: "x86",
			Elevate:      &elevTrue,
		},
	}

	eff := s.effectiveSettings("Mozilla.Firefox", "winget")
	if eff.Scope != "machine" {
		t.Fatalf("Scope = %q, want %q", eff.Scope, "machine")
	}
	if eff.Architecture != "x86" {
		t.Fatalf("Architecture = %q, want %q", eff.Architecture, "x86")
	}
	if !eff.AutoElevate {
		t.Fatal("AutoElevate = false, want true (from per-package elevate override)")
	}
}

func TestEffectiveSettingsNoOverride(t *testing.T) {
	s := DefaultSettings()
	s.Scope = "user"
	eff := s.effectiveSettings("Unknown.Pkg", "winget")
	if eff.Scope != "user" {
		t.Fatalf("Scope = %q, want %q (unchanged)", eff.Scope, "user")
	}
}

func TestEffectiveSettingsPartialOverride(t *testing.T) {
	s := DefaultSettings()
	s.Scope = "user"
	s.Architecture = "x64"
	s.Packages = map[string]PackageOverride{
		"Partial.Pkg": {Scope: "machine"},
	}
	eff := s.effectiveSettings("Partial.Pkg", "winget")
	if eff.Scope != "machine" {
		t.Fatalf("Scope = %q, want %q", eff.Scope, "machine")
	}
	if eff.Architecture != "x64" {
		t.Fatalf("Architecture = %q, want %q (should keep global)", eff.Architecture, "x64")
	}
}

func TestPackageOverrideIsEmpty(t *testing.T) {
	empty := PackageOverride{}
	if !empty.isEmpty() {
		t.Fatal("expected empty override to be isEmpty()")
	}

	withScope := PackageOverride{Scope: "user"}
	if withScope.isEmpty() {
		t.Fatal("expected override with scope to not be isEmpty()")
	}

	elevFalse := false
	withElev := PackageOverride{Elevate: &elevFalse}
	if withElev.isEmpty() {
		t.Fatal("expected override with elevate=false to not be isEmpty()")
	}
}

func TestSetOverrideCleansUpEmpty(t *testing.T) {
	s := DefaultSettings()
	s.setOverride("Test.Pkg", "winget", PackageOverride{Scope: "user"})
	if !s.hasOverride("Test.Pkg", "winget") {
		t.Fatal("expected override to exist")
	}

	s.setOverride("Test.Pkg", "winget", PackageOverride{})
	if s.hasOverride("Test.Pkg", "winget") {
		t.Fatal("expected empty override to be removed")
	}
	if s.Packages != nil {
		t.Fatal("expected Packages map to be nil when empty")
	}
}

func TestGetOverrideReturnsDefault(t *testing.T) {
	s := DefaultSettings()
	o := s.getOverride("Unknown.Pkg", "winget")
	if !o.isEmpty() {
		t.Fatalf("expected default override to be empty, got %#v", o)
	}
}

func TestPackageOverrideGetSetValue(t *testing.T) {
	var o PackageOverride

	o.setValue("update_policy", "auto")
	if o.getValue("update_policy") != "auto" {
		t.Fatalf("update_policy = %q, want auto", o.getValue("update_policy"))
	}

	o.setValue("update_policy", "hold")
	if o.getValue("update_policy") != "hold" {
		t.Fatalf("update_policy = %q, want hold", o.getValue("update_policy"))
	}

	o.setValue("update_policy", "")
	if o.getValue("update_policy") != "" {
		t.Fatalf("update_policy after clear = %q, want empty", o.getValue("update_policy"))
	}

	o.setValue("scope", "machine")
	if o.getValue("scope") != "machine" {
		t.Fatalf("scope = %q, want %q", o.getValue("scope"), "machine")
	}

	o.setValue("architecture", "arm64")
	if o.getValue("architecture") != "arm64" {
		t.Fatalf("architecture = %q, want %q", o.getValue("architecture"), "arm64")
	}

	o.setValue("elevate", "true")
	if o.getValue("elevate") != "true" {
		t.Fatalf("elevate = %q, want %q", o.getValue("elevate"), "true")
	}

	o.setValue("elevate", "")
	if o.getValue("elevate") != "" {
		t.Fatalf("elevate after clear = %q, want empty", o.getValue("elevate"))
	}
	if o.Elevate != nil {
		t.Fatal("Elevate should be nil after clearing")
	}
}

func TestSettingsJSONRoundTrip(t *testing.T) {
	elevTrue := true
	s := DefaultSettings()
	s.Scope = "user"
	s.Packages = map[string]PackageOverride{
		"Mozilla.Firefox": {
			Scope:        "machine",
			Architecture: "x64",
			Elevate:      &elevTrue,
		},
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !settingsEqual(s, loaded) {
		t.Fatalf("round-trip mismatch:\n  got:  %#v\n  want: %#v", loaded, s)
	}
}

func TestSettingsJSONOmitsEmptyPackages(t *testing.T) {
	s := DefaultSettings()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, ok := raw["packages"]; ok {
		t.Fatal("expected 'packages' key to be omitted when nil")
	}
}

func TestSettingsEqualWithPackages(t *testing.T) {
	elevTrue := true
	a := DefaultSettings()
	a.Packages = map[string]PackageOverride{
		"Test.Pkg": {Scope: "user", Elevate: &elevTrue},
	}
	b := DefaultSettings()
	b.Packages = map[string]PackageOverride{
		"Test.Pkg": {Scope: "user", Elevate: &elevTrue},
	}
	if !settingsEqual(a, b) {
		t.Fatal("expected settings with same packages to be equal")
	}

	b.Packages["Test.Pkg"] = PackageOverride{Scope: "machine"}
	if settingsEqual(a, b) {
		t.Fatal("expected settings with different packages to not be equal")
	}
}

func TestPackageElevateOverride(t *testing.T) {
	s := DefaultSettings()
	if s.packageElevateOverride("Any.Pkg", "winget") != nil {
		t.Fatal("expected nil for package with no override")
	}

	elevTrue := true
	s.Packages = map[string]PackageOverride{
		"Admin.Pkg": {Elevate: &elevTrue},
	}
	got := s.packageElevateOverride("Admin.Pkg", "winget")
	if got == nil || !*got {
		t.Fatal("expected elevate=true for Admin.Pkg")
	}
}

func TestInstallCommandArgsWithOverride(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Scope = "user"
	appSettings.Architecture = ""
	appSettings.Packages = map[string]PackageOverride{
		"Admin.Tool": {Scope: "machine", Architecture: "x64"},
	}

	got := installCommandArgs("Admin.Tool", "winget", "")
	want := []string{
		"install", "--id", "Admin.Tool", "--exact",
		"--accept-package-agreements",
		"--scope", "machine", "--architecture", "x64",
		"--source", "winget",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("installCommandArgs with override = %#v, want %#v", got, want)
	}

	gotNoOverride := installCommandArgs("Other.App", "winget", "")
	wantNoOverride := []string{
		"install", "--id", "Other.App", "--exact",
		"--accept-package-agreements",
		"--scope", "user",
		"--source", "winget",
	}
	if !reflect.DeepEqual(gotNoOverride, wantNoOverride) {
		t.Fatalf("installCommandArgs without override = %#v, want %#v", gotNoOverride, wantNoOverride)
	}
}

func TestIsIgnoredAll(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Ignored.Pkg": {Ignore: true},
	}
	if !s.isIgnored("Ignored.Pkg", "winget", "1.0") {
		t.Fatal("expected ignore-all to match any version")
	}
	if !s.isIgnored("Ignored.Pkg", "winget", "2.0") {
		t.Fatal("expected ignore-all to match any version")
	}
	if s.isIgnored("Other.Pkg", "winget", "1.0") {
		t.Fatal("expected non-ignored package to not be ignored")
	}
}

func TestIsIgnoredVersion(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Partial.Pkg": {IgnoreVersion: "1.2.3"},
	}
	if !s.isIgnored("Partial.Pkg", "winget", "1.2.3") {
		t.Fatal("expected version-specific ignore to match")
	}
	if s.isIgnored("Partial.Pkg", "winget", "1.2.4") {
		t.Fatal("expected version-specific ignore to not match different version")
	}
	if s.isIgnored("Partial.Pkg", "winget", "") {
		t.Fatal("expected version-specific ignore to not match empty version")
	}
}

func TestUpdatePolicy(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Auto.Pkg": {UpdatePolicy: PolicyAuto},
		"Held.Pkg": {UpdatePolicy: PolicyHold},
	}
	if got := s.updatePolicy("Auto.Pkg", "winget", "2.0"); got != PolicyAuto {
		t.Fatalf("Auto.Pkg policy = %q, want auto", got)
	}
	if got := s.updatePolicy("Held.Pkg", "winget", "2.0"); got != PolicyHold {
		t.Fatalf("Held.Pkg policy = %q, want hold", got)
	}
	if got := s.updatePolicy("Ask.Pkg", "winget", "2.0"); got != PolicyAsk {
		t.Fatalf("Ask.Pkg policy = %q, want ask", got)
	}
}

func TestLegacyIgnoreForcesHoldPolicy(t *testing.T) {
	o := PackageOverride{UpdatePolicy: PolicyAuto, Ignore: true}
	if got := o.effectiveUpdatePolicy("2.0"); got != PolicyHold {
		t.Fatalf("effective policy = %q, want hold", got)
	}

	o = PackageOverride{UpdatePolicy: PolicyAuto, IgnoreVersion: "2.0"}
	if got := o.effectiveUpdatePolicy("2.0"); got != PolicyHold {
		t.Fatalf("effective version policy = %q, want hold", got)
	}
	if got := o.effectiveUpdatePolicy("2.1"); got != PolicyAuto {
		t.Fatalf("effective moved-version policy = %q, want auto", got)
	}
}

func TestIsIgnoredNilPackages(t *testing.T) {
	s := DefaultSettings()
	if s.isIgnored("Any.Pkg", "winget", "1.0") {
		t.Fatal("expected no ignore with nil packages map")
	}
}

func TestExpireVersionIgnoresClearsStale(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Stale.Pkg": {IgnoreVersion: "1.0.0"},
		"Fresh.Pkg": {IgnoreVersion: "2.0.0"},
	}
	upgradeable := []Package{
		{ID: "Stale.Pkg", Available: "1.1.0"},
		{ID: "Fresh.Pkg", Available: "2.0.0"},
	}
	changed := s.expireVersionIgnores(upgradeable)
	if !changed {
		t.Fatal("expected expiry to report changes")
	}
	if s.isIgnored("Stale.Pkg", "winget", "1.1.0") {
		t.Fatal("expected stale ignore to be cleared")
	}
	if !s.isIgnored("Fresh.Pkg", "winget", "2.0.0") {
		t.Fatal("expected fresh ignore to remain")
	}
}

func TestExpireVersionIgnoresPreservesOtherFields(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Mixed.Pkg": {Scope: "user", IgnoreVersion: "1.0.0"},
	}
	upgradeable := []Package{
		{ID: "Mixed.Pkg", Available: "2.0.0"},
	}
	s.expireVersionIgnores(upgradeable)
	o := s.getOverride("Mixed.Pkg", "winget")
	if o.IgnoreVersion != "" {
		t.Fatal("expected version ignore to be cleared")
	}
	if o.Scope != "user" {
		t.Fatal("expected scope to be preserved")
	}
}

func TestExpireVersionIgnoresCleanupEmptyOverride(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Only.Ignore": {IgnoreVersion: "1.0.0"},
	}
	s.expireVersionIgnores([]Package{{ID: "Only.Ignore", Available: "2.0.0"}})
	if s.hasOverride("Only.Ignore", "winget") {
		t.Fatal("expected empty override to be removed after expiry")
	}
	if s.Packages != nil {
		t.Fatal("expected Packages map to be nil when empty")
	}
}

func TestExpireVersionIgnoresNoChangeReturnsFalse(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Same.Pkg": {IgnoreVersion: "1.0.0"},
	}
	changed := s.expireVersionIgnores([]Package{{ID: "Same.Pkg", Available: "1.0.0"}})
	if changed {
		t.Fatal("expected no changes when version matches")
	}
}

func TestPackageOverrideIgnoreGetSetValue(t *testing.T) {
	var o PackageOverride

	o.setValue("ignore", "all")
	if o.getValue("ignore") != "all" {
		t.Fatalf("ignore = %q, want %q", o.getValue("ignore"), "all")
	}
	if !o.Ignore {
		t.Fatal("Ignore should be true")
	}

	o.setValue("ignore", "1.2.3")
	if o.getValue("ignore") != "1.2.3" {
		t.Fatalf("ignore = %q, want %q", o.getValue("ignore"), "1.2.3")
	}
	if o.Ignore {
		t.Fatal("Ignore should be false when version-specific")
	}
	if o.IgnoreVersion != "1.2.3" {
		t.Fatalf("IgnoreVersion = %q, want %q", o.IgnoreVersion, "1.2.3")
	}

	o.setValue("ignore", "")
	if o.getValue("ignore") != "" {
		t.Fatalf("ignore = %q, want empty", o.getValue("ignore"))
	}
	if o.Ignore || o.IgnoreVersion != "" {
		t.Fatal("both Ignore and IgnoreVersion should be cleared")
	}
}

func TestPackageOverrideIsEmptyWithIgnore(t *testing.T) {
	o := PackageOverride{Ignore: true}
	if o.isEmpty() {
		t.Fatal("expected override with Ignore to not be empty")
	}

	o = PackageOverride{IgnoreVersion: "1.0"}
	if o.isEmpty() {
		t.Fatal("expected override with IgnoreVersion to not be empty")
	}

	o = PackageOverride{UpdatePolicy: PolicyAuto}
	if o.isEmpty() {
		t.Fatal("expected override with UpdatePolicy Auto to not be empty")
	}
}

func TestSettingsJSONRoundTripWithIgnore(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Ignored.All":     {Ignore: true},
		"Ignored.Version": {IgnoreVersion: "3.2.1", Scope: "user"},
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !settingsEqual(s, loaded) {
		t.Fatalf("round-trip mismatch:\n  got:  %#v\n  want: %#v", loaded, s)
	}
}

func TestBuildItemsFiltersIgnored(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		"Ignored.All":     {Ignore: true},
		"Ignored.Version": {IgnoreVersion: "2.0.0"},
	}

	installed := []Package{
		{ID: "Visible.Pkg", Name: "Visible", Version: "1.0", Source: "winget"},
		{ID: "Ignored.All", Name: "Ignored All", Version: "1.0", Source: "winget"},
		{ID: "Ignored.Version", Name: "Ignored Version", Version: "1.0", Source: "winget"},
	}
	upgradeable := []Package{
		{ID: "Visible.Pkg", Name: "Visible", Version: "1.0", Available: "2.0.0", Source: "winget"},
		{ID: "Ignored.All", Name: "Ignored All", Version: "1.0", Available: "2.0.0", Source: "winget"},
		{ID: "Ignored.Version", Name: "Ignored Version", Version: "1.0", Available: "2.0.0", Source: "winget"},
	}

	items, hidden := buildItems(installed, upgradeable)
	if hidden != 2 {
		t.Fatalf("hidden = %d, want 2", hidden)
	}

	upgradeCount := 0
	for _, item := range items {
		if item.upgradeable {
			upgradeCount++
			if item.pkg.ID != "Visible.Pkg" {
				t.Fatalf("unexpected upgradeable item: %s", item.pkg.ID)
			}
		}
	}
	if upgradeCount != 1 {
		t.Fatalf("upgradeCount = %d, want 1", upgradeCount)
	}
}

func TestBuildItemsVersionIgnoreDoesNotMatchDifferentVersion(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		"Partial.Pkg": {IgnoreVersion: "1.5.0"},
	}

	installed := []Package{
		{ID: "Partial.Pkg", Name: "Partial", Version: "1.0", Source: "winget"},
	}
	upgradeable := []Package{
		{ID: "Partial.Pkg", Name: "Partial", Version: "1.0", Available: "2.0.0", Source: "winget"},
	}

	items, hidden := buildItems(installed, upgradeable)
	if hidden != 0 {
		t.Fatalf("hidden = %d, want 0 (version mismatch)", hidden)
	}
	upgradeCount := 0
	for _, item := range items {
		if item.upgradeable {
			upgradeCount++
		}
	}
	if upgradeCount != 1 {
		t.Fatalf("upgradeCount = %d, want 1", upgradeCount)
	}
}

func TestBuildItemsIgnoreMatchesSourceOnly(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("Shared.Pkg", "winget"): {Ignore: true},
	}

	installed := []Package{
		{ID: "Shared.Pkg", Name: "Shared", Version: "1.0", Source: "winget"},
		{ID: "Shared.Pkg", Name: "Shared", Version: "1.0", Source: "msstore"},
	}
	upgradeable := []Package{
		{ID: "Shared.Pkg", Name: "Shared", Version: "1.0", Available: "2.0.0", Source: "winget"},
		{ID: "Shared.Pkg", Name: "Shared", Version: "1.0", Available: "2.0.0", Source: "msstore"},
	}

	items, hidden := buildItems(installed, upgradeable)
	if hidden != 1 {
		t.Fatalf("hidden = %d, want 1", hidden)
	}

	upgradeSources := map[string]bool{}
	for _, item := range items {
		if item.upgradeable {
			upgradeSources[item.pkg.Source] = true
		}
	}
	if upgradeSources["winget"] {
		t.Fatal("winget source should be hidden by source-qualified ignore")
	}
	if !upgradeSources["msstore"] {
		t.Fatal("msstore source should remain visible")
	}
}

func TestBuildItemsPolicyHoldAndAuto(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("Auto.Pkg", "winget"): {UpdatePolicy: PolicyAuto},
		packageRuleKey("Held.Pkg", "winget"): {UpdatePolicy: PolicyHold},
	}

	installed := []Package{
		{ID: "Auto.Pkg", Name: "Auto", Version: "1.0", Source: "winget"},
		{ID: "Held.Pkg", Name: "Held", Version: "1.0", Source: "winget"},
		{ID: "Ask.Pkg", Name: "Ask", Version: "1.0", Source: "winget"},
	}
	upgradeable := []Package{
		{ID: "Auto.Pkg", Name: "Auto", Version: "1.0", Available: "2.0", Source: "winget"},
		{ID: "Held.Pkg", Name: "Held", Version: "1.0", Available: "2.0", Source: "winget"},
		{ID: "Ask.Pkg", Name: "Ask", Version: "1.0", Available: "2.0", Source: "winget"},
	}

	items, hidden := buildItems(installed, upgradeable)
	if hidden != 1 {
		t.Fatalf("hidden = %d, want 1 held package", hidden)
	}
	upgradeIDs := map[string]bool{}
	for _, item := range items {
		if item.upgradeable {
			upgradeIDs[item.pkg.ID] = true
		}
	}
	if !upgradeIDs["Auto.Pkg"] || !upgradeIDs["Ask.Pkg"] {
		t.Fatalf("upgradeable IDs = %v, want Auto.Pkg and Ask.Pkg", upgradeIDs)
	}
	if upgradeIDs["Held.Pkg"] {
		t.Fatalf("upgradeable IDs = %v, did not want Held.Pkg", upgradeIDs)
	}
}

func TestLegacyPlainIDOverrideStillReads(t *testing.T) {
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"Legacy.Pkg": {Scope: "machine"},
	}

	o := s.getOverride("Legacy.Pkg", "winget")
	if o.Scope != "machine" {
		t.Fatalf("legacy scope = %q, want machine", o.Scope)
	}
}

// ── Cleanup settings ──────────────────────────────────────────────────

func TestCleanupAutoScanDefaultsToSafe(t *testing.T) {
	s := DefaultSettings()
	if got := s.getValue("cleanup_auto_scan"); got != "" {
		t.Fatalf("default cleanup_auto_scan = %q, want empty (safe)", got)
	}
}

func TestCleanupAutoScanRoundTripsThroughGetSet(t *testing.T) {
	s := DefaultSettings()
	for _, val := range []string{"all", "off", ""} {
		s.setValue("cleanup_auto_scan", val)
		if got := s.getValue("cleanup_auto_scan"); got != val {
			t.Errorf("after set %q, get returned %q", val, got)
		}
	}
}

func TestCleanupAutoScanNormalizesUnknownValues(t *testing.T) {
	s := DefaultSettings()
	s.setValue("cleanup_auto_scan", "garbage")
	if got := s.getValue("cleanup_auto_scan"); got != "" {
		t.Errorf("unknown value should normalize to safe (empty), got %q", got)
	}
}

func TestCleanupAutoScanJSONOmitsSafeDefault(t *testing.T) {
	s := DefaultSettings()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["cleanup_auto_scan"]; ok {
		t.Fatal("expected 'cleanup_auto_scan' key to be omitted at safe default")
	}
}

func TestCleanupAutoScanJSONIncludesNonDefault(t *testing.T) {
	s := DefaultSettings()
	s.CleanupAutoScan = CleanupAutoScanOff
	data, _ := json.MarshalIndent(s, "", "  ")
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if got, ok := raw["cleanup_auto_scan"]; !ok || got != "off" {
		t.Errorf("expected cleanup_auto_scan=off in JSON, got %v", raw["cleanup_auto_scan"])
	}
}

func TestSettingsEqualNormalizesCleanupAutoScan(t *testing.T) {
	a := DefaultSettings()                       // CleanupAutoScan = ""
	b := DefaultSettings()                       // also ""
	b.CleanupAutoScan = CleanupAutoScan("bogus") // normalizes to safe
	if !settingsEqual(a, b) {
		t.Fatal("settings with unknown vs empty cleanup_auto_scan should compare equal (both safe)")
	}

	c := DefaultSettings()
	c.CleanupAutoScan = CleanupAutoScanOff
	if settingsEqual(a, c) {
		t.Fatal("safe vs off should not compare equal")
	}
}

func TestCleanupTargetEnabledDefaultChecked(t *testing.T) {
	s := DefaultSettings()
	def := cleanupTargetDef{id: "user_temp", defaultChecked: true}
	if !s.cleanupTargetEnabled(def) {
		t.Fatal("default-checked target should always be enabled, even with empty list")
	}
	// Even if the list explicitly excludes (or doesn't mention) it.
	s.CleanupEnabledTargets = []string{"unrelated"}
	if !s.cleanupTargetEnabled(def) {
		t.Fatal("default-checked target should not depend on the persisted list")
	}
}

func TestCleanupTargetEnabledOptIn(t *testing.T) {
	def := cleanupTargetDef{id: "yarn_cache", defaultChecked: false}
	s := DefaultSettings()
	if s.cleanupTargetEnabled(def) {
		t.Fatal("non-default target with empty list should be off")
	}
	s.CleanupEnabledTargets = []string{"yarn_cache"}
	if !s.cleanupTargetEnabled(def) {
		t.Fatal("non-default target should be on when ID is in list")
	}
}

func TestSetCleanupTargetEnabledSkipsDefaultChecked(t *testing.T) {
	s := DefaultSettings()
	def := cleanupTargetDef{id: "user_temp", defaultChecked: true}

	s.setCleanupTargetEnabled(def, true)
	if len(s.CleanupEnabledTargets) != 0 {
		t.Errorf("default-checked opt-in should not be persisted, got %v", s.CleanupEnabledTargets)
	}
	s.setCleanupTargetEnabled(def, false)
	if len(s.CleanupEnabledTargets) != 0 {
		t.Errorf("default-checked opt-out should not be persisted, got %v", s.CleanupEnabledTargets)
	}
}

func TestSetCleanupTargetEnabledTogglesOptIn(t *testing.T) {
	s := DefaultSettings()
	def := cleanupTargetDef{id: "go_build", defaultChecked: false}

	s.setCleanupTargetEnabled(def, true)
	if !slices.Contains(s.CleanupEnabledTargets, "go_build") {
		t.Errorf("expected go_build to be added, got %v", s.CleanupEnabledTargets)
	}

	// Idempotent: enabling twice doesn't duplicate.
	s.setCleanupTargetEnabled(def, true)
	count := 0
	for _, id := range s.CleanupEnabledTargets {
		if id == "go_build" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected go_build to appear once, got %d times", count)
	}

	s.setCleanupTargetEnabled(def, false)
	if slices.Contains(s.CleanupEnabledTargets, "go_build") {
		t.Errorf("expected go_build to be removed, got %v", s.CleanupEnabledTargets)
	}
	if s.CleanupEnabledTargets != nil {
		t.Errorf("expected nil slice when last entry removed, got %v", s.CleanupEnabledTargets)
	}
}

func TestSetCleanupTargetEnabledPreservesOthers(t *testing.T) {
	s := DefaultSettings()
	s.CleanupEnabledTargets = []string{"npm_cache", "go_build", "yarn_cache"}

	s.setCleanupTargetEnabled(cleanupTargetDef{id: "go_build"}, false)
	want := []string{"npm_cache", "yarn_cache"}
	if !reflect.DeepEqual(s.CleanupEnabledTargets, want) {
		t.Errorf("got %v, want %v", s.CleanupEnabledTargets, want)
	}
}

func TestCleanupSettingsJSONRoundTrip(t *testing.T) {
	s := DefaultSettings()
	s.CleanupAutoScan = CleanupAutoScanAll
	s.CleanupEnabledTargets = []string{"go_build", "yarn_cache"}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !settingsEqual(s, loaded) {
		t.Fatalf("round-trip mismatch:\n  got:  %#v\n  want: %#v", loaded, s)
	}
}

func TestCleanupSettingsJSONOmitsEmptyTargets(t *testing.T) {
	s := DefaultSettings()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, ok := raw["cleanup_enabled_targets"]; ok {
		t.Fatal("empty CleanupEnabledTargets should be omitted from JSON")
	}
}

func TestStringSetsEqualOrderInsensitive(t *testing.T) {
	if !stringSetsEqual([]string{"a", "b", "c"}, []string{"c", "a", "b"}) {
		t.Fatal("order should not affect equality")
	}
	if !stringSetsEqual(nil, []string{}) {
		t.Fatal("nil and empty slice should compare equal")
	}
	if stringSetsEqual([]string{"a"}, []string{"b"}) {
		t.Fatal("disjoint sets should not be equal")
	}
	if stringSetsEqual([]string{"a", "b"}, []string{"a"}) {
		t.Fatal("different sizes should not be equal")
	}
}

// ── End cleanup settings ──────────────────────────────────────────────

func TestUpgradeCommandArgsWithOverride(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		"Scoped.Pkg": {Scope: "user"},
	}

	got := upgradeCommandArgs("Scoped.Pkg", "winget", "1.2.3")
	want := []string{
		"upgrade", "--id", "Scoped.Pkg", "--exact",
		"--accept-package-agreements", "--version", "1.2.3",
		"--scope", "user",
		"--source", "winget",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upgradeCommandArgs with override = %#v, want %#v", got, want)
	}
}
