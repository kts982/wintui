package main

import (
	"encoding/json"
	"reflect"
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
