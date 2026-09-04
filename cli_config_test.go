package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The v2.12 test invariants for the "silent no-op" class: assert on the
// resulting Settings / the file bytes, never on exit codes or printed text.

func setupConfigTest(t *testing.T) {
	t.Helper()
	useTempSettingsDir(t)
	saved := currentSettings()
	t.Cleanup(func() { setAppSettings(saved) })
	seedSettingsFile(t, DefaultSettings())
	setAppSettings(LoadSettings())
}

// TestSettingsRegistryIsCompleteAndSelfConsistent is the generated-table
// guard: every key has a group, its default is a valid stored value, and
// every choice round-trips through the CLI vocabulary. A new key that
// forgets any of this fails here, not in production.
func TestSettingsRegistryIsCompleteAndSelfConsistent(t *testing.T) {
	if len(settingDefs) != 16 {
		t.Fatalf("registry has %d keys, want 16 (update this test AND the docs when adding one)", len(settingDefs))
	}
	seen := map[settingGroup]int{}
	for _, d := range settingDefs {
		if d.group == "" {
			t.Errorf("%s: no group", d.key)
		}
		seen[d.group]++
		def := d.storedDefault()
		if !d.isValidStored(def) {
			t.Errorf("%s: default %q is not a valid stored value", d.key, def)
		}
		if _, ok := settingDefByKey(strings.ToUpper(d.key)); !ok {
			t.Errorf("%s: key lookup must be case-insensitive", d.key)
		}
		if got := d.cliChoices(); len(got) == 0 {
			t.Errorf("%s: no CLI choices", d.key)
		}
		if d.stype == settingToggle {
			for in, want := range map[string]string{"true": "true", "ON": "true", "yes": "true", "1": "true", "false": "false", "Off": "false", "no": "false", "0": "false"} {
				if got, err := d.parseCLIValue(in); err != nil || got != want {
					t.Errorf("%s: parse(%q) = %q %v, want %q", d.key, in, got, err, want)
				}
			}
			continue
		}
		for _, c := range d.choices {
			name := d.cliName(c)
			if name == "" {
				t.Errorf("%s: stored %q has an empty CLI name", d.key, c)
			}
			if got, err := d.parseCLIValue(strings.ToUpper(name)); err != nil || got != c {
				t.Errorf("%s: parse(cliName(%q)=%q) = %q %v", d.key, c, name, got, err)
			}
			if c != "" {
				if got, err := d.parseCLIValue(c); err != nil || got != c {
					t.Errorf("%s: parse(stored %q) = %q %v", d.key, c, got, err)
				}
			}
		}
		if _, err := d.parseCLIValue(""); err == nil {
			t.Errorf("%s: empty input must be rejected (use unset)", d.key)
		}
	}
	for _, g := range settingGroupOrder {
		if seen[g] == 0 {
			t.Errorf("group %s has no keys", g)
		}
	}
	// The vocabulary the plan pinned for the empty stored value.
	for key, want := range map[string]string{"scope": "default", "install_mode": "default", "architecture": "auto", "source": "all", "cleanup_auto_scan": "safe", "cleanup_min_age": "1d", "theme_background": "terminal", "theme": "default"} {
		d, _ := settingDefByKey(key)
		if got := d.cliName(""); got != want {
			t.Errorf("%s: cliName(\"\") = %q, want %q", key, got, want)
		}
	}
}

// TestConfigSetPersistsEveryValidValue is generated over the registry: each
// CLI name of each key lands in settings.json as the expected stored value,
// and in memory.
func TestConfigSetPersistsEveryValidValue(t *testing.T) {
	setupConfigTest(t)
	for _, d := range settingDefs {
		for _, name := range d.cliChoices() {
			want, err := d.parseCLIValue(name)
			if err != nil {
				t.Fatalf("%s %q: %v", d.key, name, err)
			}
			var out bytes.Buffer
			if err := runConfigSet(&out, strings.ToUpper(d.key), name, false); err != nil {
				t.Fatalf("set %s %s: %v", d.key, name, err)
			}
			disk, err := loadSettingsFile()
			if err != nil {
				t.Fatal(err)
			}
			if got := disk.rawValue(d.key); got != want {
				t.Errorf("set %s %s: disk=%q want %q", d.key, name, got, want)
			}
			if got := currentSettings().rawValue(d.key); got != want {
				t.Errorf("set %s %s: memory=%q want %q", d.key, name, got, want)
			}
		}
	}
}

// TestConfigSetInvalidValueNeverWrites: an invalid value must leave the file
// bytes and the in-memory settings untouched and name the accepted values.
func TestConfigSetInvalidValueNeverWrites(t *testing.T) {
	setupConfigTest(t)
	before, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatal(err)
	}
	mem := currentSettings()
	for _, tc := range [][2]string{
		{"install_mode", "loud"},
		{"auto_elevate", "maybe"},
		{"source", "default"}, // "default" is not source vocabulary; unset is
		{"scope", ""},
		{"architecture", "arm"},
		{"theme", "purple"},
		{"theme_background", "on"},
		{"cleanup_auto_scan", "true"},
		{"cleanup_min_age", "2d"}, // only the registry's choices, never an arbitrary duration
		{"cleanup_min_age", "1"},
	} {
		var out bytes.Buffer
		err := runConfigSet(&out, tc[0], tc[1], false)
		if err == nil {
			t.Errorf("set %s %q: expected an error", tc[0], tc[1])
			continue
		}
		if !strings.Contains(err.Error(), "invalid value") || !strings.Contains(err.Error(), "expected") {
			t.Errorf("set %s %q: error should name the accepted values, got %q", tc[0], tc[1], err)
		}
	}
	if err := runConfigSet(&bytes.Buffer{}, "no_such_key", "x", false); err == nil || !strings.Contains(err.Error(), "unknown setting") {
		t.Errorf("unknown key should error, got %v", err)
	}
	after, _ := os.ReadFile(configPath())
	if !bytes.Equal(before, after) {
		t.Error("settings.json changed after invalid set commands")
	}
	if !settingsEqual(currentSettings(), mem) {
		t.Error("in-memory settings changed after invalid set commands")
	}
}

// TestConfigUnsetRestoresRealDefaults pins the sentinel traps: source → winget
// (not ""), auto_elevate → true, scope → "" rendered as "default", theme → "".
func TestConfigUnsetRestoresRealDefaults(t *testing.T) {
	setupConfigTest(t)
	for _, set := range [][2]string{{"source", "all"}, {"auto_elevate", "off"}, {"scope", "machine"}, {"theme", "nord"}, {"auto_self_update", "false"}} {
		if err := runConfigSet(&bytes.Buffer{}, set[0], set[1], false); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"source", "auto_elevate", "scope", "theme", "auto_self_update"} {
		if err := runConfigUnset(&bytes.Buffer{}, key, false); err != nil {
			t.Fatal(err)
		}
	}
	disk, err := loadSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	if disk.Source != "winget" || !disk.AutoElevate || disk.Scope != ScopeDefault || disk.Theme != "" || !disk.AutoSelfUpdate {
		t.Errorf("unset did not restore defaults: %+v", disk)
	}
	var out bytes.Buffer
	if err := runConfigGet(&out, "scope", false); err != nil || strings.TrimSpace(out.String()) != "default" {
		t.Errorf("get scope after unset = %q %v, want default", out.String(), err)
	}
	out.Reset()
	if err := runConfigGet(&out, "source", false); err != nil || strings.TrimSpace(out.String()) != "winget" {
		t.Errorf("get source after unset = %q %v, want winget", out.String(), err)
	}
}

// TestConfigGetJSONGolden pins the element contract byte-for-byte: an object
// (never a bare scalar), typed value, 2-space indent, trailing newline.
func TestConfigGetJSONGolden(t *testing.T) {
	setupConfigTest(t)
	var out bytes.Buffer
	if err := runConfigGet(&out, "auto_elevate", true); err != nil {
		t.Fatal(err)
	}
	want := `{
  "key": "auto_elevate",
  "value": true,
  "default": true,
  "is_default": true,
  "valid": true,
  "type": "bool",
  "values": [
    "true",
    "false"
  ],
  "group": "common",
  "description": "Automatically request administrator rights"
}
`
	if out.String() != want {
		t.Errorf("get auto_elevate --json:\n%s\nwant:\n%s", out.String(), want)
	}

	if err := runConfigSet(&bytes.Buffer{}, "scope", "user", false); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runConfigGet(&out, "scope", true); err != nil {
		t.Fatal(err)
	}
	want = `{
  "key": "scope",
  "value": "user",
  "default": "default",
  "is_default": false,
  "valid": true,
  "type": "enum",
  "values": [
    "default",
    "user",
    "machine"
  ],
  "group": "common",
  "description": "Default, user-only, or machine-wide"
}
`
	if out.String() != want {
		t.Errorf("get scope --json:\n%s\nwant:\n%s", out.String(), want)
	}
}

// TestConfigListJSONEnvelope: {count, settings[]} with every key in table
// order, snake_case fields, arrays never null.
func TestConfigListJSONEnvelope(t *testing.T) {
	setupConfigTest(t)
	var out bytes.Buffer
	if err := runConfigList(&out, true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "}\n") || strings.Contains(out.String(), "null") {
		t.Errorf("envelope must end with a newline and contain no null: %q", out.String())
	}
	var env struct {
		Count    int               `json:"count"`
		Settings []json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Count != len(settingDefs) || len(env.Settings) != len(settingDefs) {
		t.Fatalf("count=%d settings=%d want %d", env.Count, len(env.Settings), len(settingDefs))
	}
	for i, raw := range env.Settings {
		var e map[string]any
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		if e["key"] != settingDefs[i].key {
			t.Errorf("settings[%d].key = %v, want %s (table order)", i, e["key"], settingDefs[i].key)
		}
		for _, f := range []string{"key", "value", "default", "is_default", "valid", "type", "values", "group", "description"} {
			if _, ok := e[f]; !ok {
				t.Errorf("settings[%d] (%s) missing %q", i, settingDefs[i].key, f)
			}
		}
	}
}

// TestConfigSurfacesInvalidOnDiskValue: an unrecognised stored value is shown
// raw with valid:false in list/get, and doctor's Settings row warns —
// nothing invents a semantic value for it.
func TestConfigSurfacesInvalidOnDiskValue(t *testing.T) {
	setupConfigTest(t)
	s := DefaultSettings()
	s.ThemeBackground = "purple"
	s.Theme = "neon"
	if err := SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	setAppSettings(LoadSettings())

	var out bytes.Buffer
	if err := runConfigList(&out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"purple" (invalid)`) || !strings.Contains(out.String(), `"neon" (invalid)`) {
		t.Errorf("list should flag invalid values:\n%s", out.String())
	}
	out.Reset()
	if err := runConfigGet(&out, "theme_background", true); err != nil {
		t.Fatal(err)
	}
	var e struct {
		Value any  `json:"value"`
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Value != "purple" || e.Valid {
		t.Errorf("get --json should expose the raw value with valid=false, got %+v", e)
	}
	if row := checkSettingsSummary(); row.Status != "WARN" || !strings.Contains(row.Details, `theme_background="purple"`) || !strings.Contains(row.Details, `theme="neon"`) {
		t.Errorf("doctor row should warn about invalid values: %+v", row)
	}
	// Fixing it through the CLI clears the warning.
	if err := runConfigSet(&bytes.Buffer{}, "theme_background", "terminal", false); err != nil {
		t.Fatal(err)
	}
	if err := runConfigUnset(&bytes.Buffer{}, "theme", false); err != nil {
		t.Fatal(err)
	}
	if row := checkSettingsSummary(); row.Status != "INFO" {
		t.Errorf("doctor row should be INFO after the fix: %+v", row)
	}
}

// TestThemeCommandUsesRegistryValidator: `wintui theme` shares the registry
// validator — IDs and labels in any casing work, garbage errors, and the
// stored value is the canonical ID ("" for default).
func TestThemeCommandUsesRegistryValidator(t *testing.T) {
	setupConfigTest(t)
	for _, tc := range []struct{ in, wantStored string }{{"Nord", "nord"}, {"NORD", "nord"}, {"default", ""}, {"Default", ""}} {
		var out bytes.Buffer
		if err := runTheme(tc.in, false, &out); err != nil {
			t.Fatalf("theme %q: %v", tc.in, err)
		}
		disk, err := loadSettingsFile()
		if err != nil {
			t.Fatal(err)
		}
		if disk.Theme != tc.wantStored {
			t.Errorf("theme %q stored %q, want %q", tc.in, disk.Theme, tc.wantStored)
		}
	}
	before, _ := os.ReadFile(configPath())
	if err := runTheme("neon", false, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "unknown theme") {
		t.Errorf("garbage theme should error, got %v", err)
	}
	if after, _ := os.ReadFile(configPath()); !bytes.Equal(before, after) {
		t.Error("invalid theme must not write")
	}
	// Both entry points agree.
	if err := runConfigSet(&bytes.Buffer{}, "theme", "Catppuccin", false); err != nil {
		t.Fatal(err)
	}
	if disk, _ := loadSettingsFile(); disk.Theme != "catppuccin" {
		t.Errorf("config set theme stored %q, want catppuccin", disk.Theme)
	}
}

// TestDoctorSettingsRowSpeaksRegistryVocabulary: the refactored doctor row
// renders enums exactly as `wintui config` does.
func TestDoctorSettingsRowSpeaksRegistryVocabulary(t *testing.T) {
	setupConfigTest(t)
	row := checkSettingsSummary()
	if !strings.Contains(row.Details, "Action Mode: default") || !strings.Contains(row.Details, "Source: winget") {
		t.Errorf("defaults row = %q", row.Details)
	}
	for _, set := range [][2]string{{"source", "all"}, {"install_mode", "silent"}, {"theme_background", "theme"}} {
		if err := runConfigSet(&bytes.Buffer{}, set[0], set[1], false); err != nil {
			t.Fatal(err)
		}
	}
	row = checkSettingsSummary()
	if !strings.Contains(row.Details, "Action Mode: silent") || !strings.Contains(row.Details, "Source: all") || row.Status != "INFO" {
		t.Errorf("row after set = %+v", row)
	}
	if theme := checkThemeSummary(); !strings.Contains(theme.Details, "background: theme") {
		t.Errorf("theme row = %+v", theme)
	}
}

// TestConfigCompletionsFollowRegistry: keys complete with descriptions and
// values complete from the CLI vocabulary.
func TestConfigCompletionsFollowRegistry(t *testing.T) {
	keys, _ := configKeyCompletion(nil, nil, "auto_")
	if len(keys) != 2 || !strings.HasPrefix(keys[0], "auto_elevate\t") {
		t.Errorf("key completion = %v", keys)
	}
	vals, _ := configSetCompletion(nil, []string{"install_mode"}, "")
	if strings.Join(vals, ",") != "default,silent,interactive" {
		t.Errorf("value completion = %v", vals)
	}
	if none, _ := configSetCompletion(nil, []string{"nope"}, ""); len(none) != 0 {
		t.Errorf("unknown key should complete nothing, got %v", none)
	}
}
