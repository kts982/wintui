package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func strPtr(s string) *string { return &s }

// squashSpaces collapses runs of spaces so table assertions do not depend on
// tabwriter column widths.
func squashSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// withRuleCache replaces the cache seam for the duration of the test.
func withRuleCache(t *testing.T, fn func(id string) (string, bool)) {
	t.Helper()
	saved := rulesAvailableFn
	rulesAvailableFn = fn
	t.Cleanup(func() { rulesAvailableFn = saved })
}

func coldCache(string) (string, bool) { return "", false }

func setupRulesTest(t *testing.T) {
	t.Helper()
	setupConfigTest(t)
	withRuleCache(t, coldCache)
}

// TestRulesSetIsFieldWiseAndNeverDestroysVersionHold is the critical Opus
// finding: changing the policy must not wipe a version-specific hold (the
// TUI editor's setValue does), and each flag touches only its own field.
func TestRulesSetIsFieldWiseAndNeverDestroysVersionHold(t *testing.T) {
	setupRulesTest(t)
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{IgnoreVersion: strPtr("2.48.0")}, false); err != nil {
		t.Fatal(err)
	}
	if err := runRulesSet(&bytes.Buffer{}, "git.git", "", rulesSetOptions{Policy: strPtr("auto"), Scope: strPtr("user")}, false); err != nil {
		t.Fatal(err)
	}
	disk, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	key, o, ok := disk.lookupOverride("Git.Git", "winget")
	if !ok || key != "winget:Git.Git" {
		t.Fatalf("rule not stored under the qualified key: %q %v", key, ok)
	}
	if o.IgnoreVersion != "2.48.0" || o.UpdatePolicy != PolicyAuto || o.Scope != ScopeUser || o.Architecture != "" || o.Elevate != nil {
		t.Errorf("field-wise set broke: %+v", o)
	}
	// Downstream effect through the real consumers, not just storage.
	if got := currentSettings().updatePolicy("Git.Git", "winget", "2.48.0"); got != PolicyHold {
		t.Errorf("version hold should apply for 2.48.0, got %q", got)
	}
	if got := currentSettings().updatePolicy("Git.Git", "winget", "2.49.0"); got != PolicyAuto {
		t.Errorf("policy should be auto for other versions, got %q", got)
	}
	if args := installCommandArgs("Git.Git", "winget", ""); !strings.Contains(strings.Join(args, " "), "--scope user") {
		t.Errorf("effective argv should carry --scope user: %v", args)
	}
	// Elevate vocabulary: never/always/inherit, and a typo is an error.
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Elevate: strPtr("never")}, false); err != nil {
		t.Fatal(err)
	}
	if o := currentSettings().getOverride("Git.Git", "winget"); o.Elevate == nil || *o.Elevate {
		t.Errorf("elevate never not stored: %+v", o)
	}
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Elevate: strPtr("inherit")}, false); err != nil {
		t.Fatal(err)
	}
	if o := currentSettings().getOverride("Git.Git", "winget"); o.Elevate != nil {
		t.Errorf("elevate inherit should clear the field: %+v", o)
	}
}

// TestRulesSetInvalidValuesNeverWrite: every bad flag is rejected before any
// write; the file bytes and memory stay untouched.
func TestRulesSetInvalidValuesNeverWrite(t *testing.T) {
	setupRulesTest(t)
	before, _ := os.ReadFile(configPath())
	mem := currentSettings()
	for _, opts := range []rulesSetOptions{
		{Policy: strPtr("maybe")},
		{Scope: strPtr("global")},
		{Architecture: strPtr("arm")},
		{Elevate: strPtr("nevr")}, // a typo must not mean "never"
		{IgnoreVersion: strPtr("  ")},
		{}, // nothing to set
	} {
		if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", opts, false); err == nil {
			t.Errorf("expected an error for %+v", opts)
		}
	}
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "chocolatey", rulesSetOptions{Policy: strPtr("auto")}, false); err == nil {
		t.Error("expected an error for an unknown source")
	}
	if after, _ := os.ReadFile(configPath()); !bytes.Equal(before, after) {
		t.Error("settings.json changed after invalid rules set")
	}
	if !settingsEqual(currentSettings(), mem) {
		t.Error("memory changed after invalid rules set")
	}
}

// TestRulesSetUsesCacheCasingForNewRulesOnly: a new rule takes winget's casing
// from the warm cache; an existing rule keeps its key even when typed
// differently.
func TestRulesSetUsesCacheCasingForNewRulesOnly(t *testing.T) {
	setupRulesTest(t)
	data := diskCacheData{
		Installed:   toCachedPackages([]Package{{ID: "Neovim.Neovim", Name: "Neovim", Source: "winget"}}),
		Upgradeable: toCachedPackages(nil),
		SavedAt:     time.Now(),
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(diskCachePath(), b, 0644); err != nil {
		t.Fatal(err)
	}
	if err := runRulesSet(&bytes.Buffer{}, "neovim.neovim", "", rulesSetOptions{Policy: strPtr("hold")}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := currentSettings().Packages["winget:Neovim.Neovim"]; !ok {
		t.Errorf("new rule should use cache casing, got %v", keysOf(currentSettings().Packages))
	}
	// Existing rule with its own casing (hand-edited) is reused, not re-keyed.
	s := currentSettings().clone()
	s.Packages["winget:git.git"] = PackageOverride{Scope: ScopeUser}
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	setAppSettings(s)
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Policy: strPtr("auto")}, false); err != nil {
		t.Fatal(err)
	}
	got := currentSettings()
	if _, ok := got.Packages["winget:git.git"]; !ok || len(got.overrideKeyAliases("Git.Git", "winget")) != 1 {
		t.Errorf("existing key should be reused: %v", keysOf(got.Packages))
	}
	if o := got.Packages["winget:git.git"]; o.Scope != ScopeUser || o.UpdatePolicy != PolicyAuto {
		t.Errorf("merge lost a field: %+v", o)
	}
}

// TestRulesConflictsRefuseSetButClearAllResolves pins the collision contract.
func TestRulesConflictsRefuseSetButClearAllResolves(t *testing.T) {
	setupRulesTest(t)
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"winget:Git.Git": {Scope: ScopeUser},
		"winget:git.git": {Scope: ScopeMachine},
	}
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	setAppSettings(LoadSettings())

	err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Policy: strPtr("auto")}, false)
	if err == nil || !strings.Contains(err.Error(), "conflicting duplicate rules") {
		t.Fatalf("set should refuse on conflict, got %v", err)
	}
	if err := runRulesClear(&bytes.Buffer{}, "Git.Git", "", []string{"scope"}, false); err == nil {
		t.Fatal("field clear should refuse on conflict")
	}
	var out bytes.Buffer
	if err := runRulesShow(&out, "git.git", "", false); err != nil || !strings.Contains(out.String(), "conflicting duplicate rules") {
		t.Errorf("show should still answer and warn: %v %q", err, out.String())
	}
	if err := runRulesClear(&bytes.Buffer{}, "GIT.GIT", "", nil, false); err != nil {
		t.Fatal(err)
	}
	if got := currentSettings(); len(got.Packages) != 0 {
		t.Errorf("clear-all should remove every alias, got %v", keysOf(got.Packages))
	}
	disk, _ := loadSettingsFile()
	if len(disk.Packages) != 0 {
		t.Errorf("clear-all not persisted: %v", keysOf(disk.Packages))
	}
}

// TestRulesClearFields: named fields are cleared, others survive; an empty
// rule disappears entirely.
func TestRulesClearFields(t *testing.T) {
	setupRulesTest(t)
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{Policy: strPtr("auto"), Scope: strPtr("user"), IgnoreVersion: strPtr("1.0"), Elevate: strPtr("always"), Architecture: strPtr("x64")}, false); err != nil {
		t.Fatal(err)
	}
	if err := runRulesClear(&bytes.Buffer{}, "Git.Git", "", []string{"policy", "ignore-version"}, false); err != nil {
		t.Fatal(err)
	}
	o := currentSettings().getOverride("Git.Git", "winget")
	if o.UpdatePolicy != PolicyAsk || o.IgnoreVersion != "" || o.Scope != ScopeUser || o.Architecture != ArchX64 || o.Elevate == nil {
		t.Errorf("field clear wrong: %+v", o)
	}
	if err := runRulesClear(&bytes.Buffer{}, "Git.Git", "", []string{"bogus"}, false); err == nil {
		t.Error("unknown field should error")
	}
	if err := runRulesClear(&bytes.Buffer{}, "Git.Git", "", []string{"scope", "architecture", "elevate"}, false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := currentSettings().lookupOverride("Git.Git", "winget"); ok {
		t.Error("a rule with every field cleared should be removed")
	}
	var out bytes.Buffer
	if err := runRulesClear(&out, "Git.Git", "", nil, false); err != nil || !strings.Contains(out.String(), "nothing to clear") {
		t.Errorf("clearing a missing rule should be a no-op with exit 0: %v %q", err, out.String())
	}
}

// TestRulesShowEffectiveAndHoldEvaluation: no rule → inherited values, exit
// 0; a version hold is active / inactive / unknown depending on the cache.
func TestRulesShowEffectiveAndHoldEvaluation(t *testing.T) {
	setupRulesTest(t)
	if err := runConfigSet(&bytes.Buffer{}, "scope", "machine", false); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRulesShow(&out, "Git.Git", "", false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if sq := squashSpaces(text); !strings.Contains(sq, "no explicit rule") || !strings.Contains(sq, "scope inherit machine") || !strings.Contains(sq, "elevate inherit true") {
		t.Errorf("show without rule should print inherited effective values:\n%s", text)
	}

	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{IgnoreVersion: strPtr("2.48.0"), Policy: strPtr("auto")}, false); err != nil {
		t.Fatal(err)
	}
	// Cold cache: cannot evaluate.
	out.Reset()
	_ = runRulesShow(&out, "Git.Git", "", false)
	if !strings.Contains(out.String(), "version=2.48.0 (cannot evaluate)") || !strings.Contains(out.String(), "cache is cold") {
		t.Errorf("cold cache should be reported honestly:\n%s", out.String())
	}
	// Warm cache, hold active.
	withRuleCache(t, func(string) (string, bool) { return "2.48.0", true })
	out.Reset()
	_ = runRulesShow(&out, "Git.Git", "", false)
	if sq := squashSpaces(out.String()); !strings.Contains(sq, "version=2.48.0 (active)") || !strings.Contains(sq, "policy auto hold") {
		t.Errorf("active hold should flip effective policy to hold:\n%s", out.String())
	}
	// Warm cache, newer version pending: inactive, policy auto.
	withRuleCache(t, func(string) (string, bool) { return "2.49.0", true })
	out.Reset()
	_ = runRulesShow(&out, "Git.Git", "", false)
	if sq := squashSpaces(out.String()); !strings.Contains(sq, "version=2.48.0 (inactive)") || !strings.Contains(sq, "policy auto auto") {
		t.Errorf("inactive hold should leave policy auto:\n%s", out.String())
	}
}

// TestRulesShowJSONGolden pins the element contract byte-for-byte.
func TestRulesShowJSONGolden(t *testing.T) {
	setupRulesTest(t)
	withRuleCache(t, func(string) (string, bool) { return "2.49.0", true })
	if err := runRulesSet(&bytes.Buffer{}, "Git.Git", "", rulesSetOptions{IgnoreVersion: strPtr("2.48.0"), Scope: strPtr("user"), Elevate: strPtr("never")}, false); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRulesShow(&out, "git.git", "", true); err != nil {
		t.Fatal(err)
	}
	want := `{
  "id": "Git.Git",
  "source": "winget",
  "key": "winget:Git.Git",
  "has_rule": true,
  "rule": {
    "policy": "ask",
    "hold": {
      "kind": "version",
      "version": "2.48.0",
      "active": null
    },
    "scope": "user",
    "architecture": "inherit",
    "elevate": "never"
  },
  "effective": {
    "policy": "ask",
    "hold": {
      "kind": "version",
      "version": "2.48.0",
      "active": false
    },
    "scope": "user",
    "architecture": "auto",
    "elevate": false,
    "available_version": "2.49.0"
  },
  "conflicts": []
}
`
	if out.String() != want {
		t.Errorf("rules show --json:\n%s\nwant:\n%s", out.String(), want)
	}

	// No rule: rule is null, effective inherited, conflicts [] (never null).
	out.Reset()
	if err := runRulesShow(&out, "Nope.Pkg", "msstore", true); err != nil {
		t.Fatal(err)
	}
	var v struct {
		HasRule   bool            `json:"has_rule"`
		Rule      json.RawMessage `json:"rule"`
		Conflicts []string        `json:"conflicts"`
		Effective struct {
			Elevate bool    `json:"elevate"`
			Avail   *string `json:"available_version"`
		} `json:"effective"`
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.HasRule || string(v.Rule) != "null" || v.Conflicts == nil || !v.Effective.Elevate {
		t.Errorf("no-rule JSON wrong: %s", out.String())
	}
}

// TestRulesListRendersLegacyIgnoreAsHold: list shows every explicit rule,
// legacy `ignore: true` as policy hold / hold all, bare keys as source (any).
func TestRulesListRendersLegacyIgnoreAsHold(t *testing.T) {
	setupRulesTest(t)
	s := DefaultSettings()
	s.Packages = map[string]PackageOverride{
		"winget:Zed.Zed":  {UpdatePolicy: PolicyAuto, Architecture: ArchX64},
		"Legacy.Bare":     {Ignore: true},
		"msstore:App.One": {IgnoreVersion: "3.0"},
	}
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	setAppSettings(LoadSettings())

	var out bytes.Buffer
	if err := runRulesList(&out, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"App.One msstore ask version=3.0 inherit inherit inherit", "Legacy.Bare (any) hold all inherit inherit inherit", "Zed.Zed winget auto none inherit x64 inherit", "3 rule(s)"} {
		if !strings.Contains(squashSpaces(text), want) {
			t.Errorf("list missing %q:\n%s", want, text)
		}
	}
	out.Reset()
	if err := runRulesList(&out, true); err != nil {
		t.Fatal(err)
	}
	var env ruleListJSON
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Count != 3 || len(env.Rules) != 3 || env.Rules[1].Rule.Policy != "hold" || env.Rules[1].Rule.Hold.Kind != "all" || env.Rules[1].Source != "" {
		t.Errorf("list JSON wrong: %s", out.String())
	}

	// Empty state is a listing, not a query: exit 0, friendly line.
	setAppSettings(DefaultSettings())
	out.Reset()
	if err := runRulesList(&out, false); err != nil || !strings.Contains(out.String(), "No per-package rules") {
		t.Errorf("empty list: %v %q", err, out.String())
	}
	out.Reset()
	_ = runRulesList(&out, true)
	if !strings.Contains(out.String(), `"rules": []`) {
		t.Errorf("empty list JSON must be [] not null: %s", out.String())
	}
}

// TestRulesCompletionIncludesRuledPackagesBeyondCache: completion is the
// installed ∪ upgradeable cache plus every package with a rule.
func TestRulesCompletionIncludesRuledPackagesBeyondCache(t *testing.T) {
	setupRulesTest(t)
	if err := runRulesSet(&bytes.Buffer{}, "Only.Ruled", "", rulesSetOptions{Policy: strPtr("hold")}, false); err != nil {
		t.Fatal(err)
	}
	out, _ := completeRuleIDs(nil, nil, "only")
	if len(out) != 1 || !strings.HasPrefix(out[0], "Only.Ruled\t") {
		t.Errorf("completion = %v", out)
	}
	fields, _ := completeRuleClearArgs(nil, []string{"Only.Ruled", "scope"}, "")
	if strings.Join(fields, ",") != "policy,architecture,elevate,ignore-version" {
		t.Errorf("field completion = %v", fields)
	}
}

func TestSplitRuleKey(t *testing.T) {
	for _, tc := range []struct{ key, id, source string }{
		{"winget:Git.Git", "Git.Git", "winget"},
		{"msstore:9NKSQGP7F2NH", "9NKSQGP7F2NH", "msstore"},
		{"Git.Git", "Git.Git", ""},
		{"Weird.Id:With.Colon", "Weird.Id:With.Colon", ""},
	} {
		id, source := splitRuleKey(tc.key)
		if id != tc.id || source != tc.source {
			t.Errorf("splitRuleKey(%q) = %q,%q want %q,%q", tc.key, id, source, tc.id, tc.source)
		}
	}
}
