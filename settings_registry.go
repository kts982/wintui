package main

import (
	"fmt"
	"slices"
	"strings"
)

// ── Semantic settings registry ─────────────────────────────────────
//
// settingDefs (config.go) is the single table describing every global
// setting. The TUI settings screen has always driven its rows from it; this
// file promotes the same table into the vocabulary `wintui config` and the
// doctor rows speak, so a key, its choices, its default, and its human names
// exist in exactly one place.
//
// Design points that are easy to get wrong:
//   - The default is derived from DefaultSettings(), never a literal in the
//     table: choices[0] is NOT the default (source → "winget", scope → ""),
//     and a second literal could drift from what the app really uses.
//   - Validation sits IN FRONT of Settings.setValue, which remains the TUI's
//     low-level mutator: the CLI never lets garbage through to a winget
//     invocation hours later, and never normalizes a typo into a default.
//   - Human vocabulary comes from choiceLabels / cliValues, so "" renders as
//     default / auto / all / safe / terminal / default(theme) depending on
//     the key, and unset means "restore WinTUI's default", not "".

type settingGroup string

const (
	settingGroupCommon     settingGroup = "common"
	settingGroupAdvanced   settingGroup = "advanced"
	settingGroupAppearance settingGroup = "appearance"
	settingGroupCleanup    settingGroup = "cleanup"
)

// settingGroupOrder is the display order of `wintui config list`.
var settingGroupOrder = []settingGroup{
	settingGroupCommon, settingGroupAdvanced, settingGroupAppearance, settingGroupCleanup,
}

func (g settingGroup) title() string {
	switch g {
	case settingGroupCommon:
		return "Common"
	case settingGroupAdvanced:
		return "Advanced (use with care)"
	case settingGroupAppearance:
		return "Appearance"
	case settingGroupCleanup:
		return "Cleanup"
	}
	return string(g)
}

// settingDefByKey looks a setting up by its snake_case key, case-insensitively.
func settingDefByKey(key string) (settingDef, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, d := range settingDefs {
		if d.key == key {
			return d, true
		}
	}
	return settingDef{}, false
}

// typeName is the JSON-facing type: "bool" for toggles, "enum" for choices.
func (d settingDef) typeName() string {
	if d.stype == settingToggle {
		return "bool"
	}
	return "enum"
}

// storedDefault is the canonical stored value of the key's default, derived
// from DefaultSettings() so it can never disagree with the app.
func (d settingDef) storedDefault() string {
	return DefaultSettings().getValue(d.key)
}

// cliName renders a stored value in the CLI vocabulary. Unknown (invalid)
// stored values are returned verbatim so callers can show them honestly.
func (d settingDef) cliName(stored string) string {
	if d.stype == settingToggle {
		return stored
	}
	if n, ok := d.cliValues[stored]; ok {
		return n
	}
	if n, ok := d.choiceLabels[stored]; ok {
		return n
	}
	return stored
}

// cliChoices lists the accepted CLI names in choice order.
func (d settingDef) cliChoices() []string {
	if d.stype == settingToggle {
		return []string{"true", "false"}
	}
	out := make([]string, 0, len(d.choices))
	for _, c := range d.choices {
		out = append(out, d.cliName(c))
	}
	return out
}

// isValidStored reports whether a stored value is one the registry knows.
func (d settingDef) isValidStored(v string) bool {
	if d.stype == settingToggle {
		return v == "true" || v == "false"
	}
	return slices.Contains(d.choices, v)
}

// parseCLIValue maps user input to the stored value. Case-insensitive; accepts
// the CLI name, the stored value itself (non-empty), the TUI label (so
// `config set theme Nord` works), and on/off yes/no 1/0 for booleans. Anything
// else is an error — the CLI never normalizes an invalid value silently.
func (d settingDef) parseCLIValue(input string) (string, error) {
	in := strings.ToLower(strings.TrimSpace(input))
	if d.stype == settingToggle {
		switch in {
		case "true", "on", "yes", "1":
			return "true", nil
		case "false", "off", "no", "0":
			return "false", nil
		}
		return "", fmt.Errorf("invalid value %q for %s: expected true or false", input, d.key)
	}
	for _, c := range d.choices {
		if in == strings.ToLower(d.cliName(c)) {
			return c, nil
		}
		if c != "" && in == strings.ToLower(c) {
			return c, nil
		}
		if lbl, ok := d.choiceLabels[c]; ok && in == strings.ToLower(lbl) {
			return c, nil
		}
	}
	return "", fmt.Errorf("invalid value %q for %s: expected one of %s", input, d.key, strings.Join(d.cliChoices(), ", "))
}

// jsonValue is the semantic typed value for --json: a real boolean for
// toggles, the CLI name for enums, the raw string for an invalid stored value.
func (d settingDef) jsonValue(stored string) any {
	if d.stype == settingToggle {
		return stored == "true"
	}
	return d.cliName(stored)
}

// settingCLIValue renders the current value of key in s using the registry
// vocabulary — the helper doctor rows use instead of hand-rolled mappings.
func settingCLIValue(s Settings, key string) string {
	d, ok := settingDefByKey(key)
	if !ok {
		return s.getValue(key)
	}
	return d.cliName(s.rawValue(key))
}

// invalidStoredSettings lists keys whose on-disk value the registry does not
// recognise, with the raw value, in table order.
func invalidStoredSettings(s Settings) []string {
	var out []string
	for _, d := range settingDefs {
		if v := s.rawValue(d.key); !d.isValidStored(v) {
			out = append(out, fmt.Sprintf("%s=%q", d.key, v))
		}
	}
	return out
}
