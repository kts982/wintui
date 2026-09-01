package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// `wintui config` — list / get / set / unset over every global setting, driven
// by the settingDefs registry (settings_registry.go). It is the CLI's
// day-to-day control plane for settings.json: gh/git-style positional
// values, human-readable enums, validation in front of the low-level setter,
// and delta writes through updateSettings so a running TUI never has its
// unrelated keys clobbered.

var configJSONFlag bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or change WinTUI settings",
	Long: `Show or change the global WinTUI settings stored in settings.json.

Values are human-readable: enums such as "default", "silent", "auto" or "all",
and true/false for switches (on/off are accepted as input). Invalid values are
rejected — nothing is normalized silently. "unset" restores WinTUI's default
for a key, which is not always empty (source defaults to winget).

Per-package rules live under "wintui rules"; the color theme also has the
friendlier "wintui theme".

Examples:
  wintui config list
  wintui config get install_mode
  wintui config set install_mode silent
  wintui config set auto_elevate off
  wintui config unset source
  wintui config list --json`,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every setting with its current value",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigList(cmd.OutOrStdout(), configJSONFlag)
	},
}

var configGetCmd = &cobra.Command{
	Use:               "get <key>",
	Short:             "Print one setting's value",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: configKeyCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigGet(cmd.OutOrStdout(), args[0], configJSONFlag)
	},
}

var configSetCmd = &cobra.Command{
	Use:               "set <key> <value>",
	Short:             "Change one setting",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: configSetCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigSet(cmd.OutOrStdout(), args[0], args[1], configJSONFlag)
	},
}

var configUnsetCmd = &cobra.Command{
	Use:               "unset <key>",
	Short:             "Restore one setting to WinTUI's default",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: configKeyCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigUnset(cmd.OutOrStdout(), args[0], configJSONFlag)
	},
}

func init() {
	configCmd.PersistentFlags().BoolVar(&configJSONFlag, "json", false, "Output JSON")
	configCmd.AddCommand(configListCmd, configGetCmd, configSetCmd, configUnsetCmd)
}

// ── JSON contract ──────────────────────────────────────────────────
//
// Self-describing elements (key, typed value, default, is_default, valid,
// type, values, group, description); `list` wraps them in a {count, settings}
// envelope like `history`, `get`/`set`/`unset` return one element. Booleans
// are JSON booleans, enums their CLI names, an invalid on-disk value its raw
// string with "valid": false. Arrays are never null.

type configEntryJSON struct {
	Key         string   `json:"key"`
	Value       any      `json:"value"`
	Default     any      `json:"default"`
	IsDefault   bool     `json:"is_default"`
	Valid       bool     `json:"valid"`
	Type        string   `json:"type"`
	Values      []string `json:"values"`
	Group       string   `json:"group"`
	Description string   `json:"description"`
}

type configListJSON struct {
	Count    int               `json:"count"`
	Settings []configEntryJSON `json:"settings"`
}

func configEntryFor(s Settings, d settingDef) configEntryJSON {
	stored := s.rawValue(d.key)
	def := d.storedDefault()
	values := d.cliChoices()
	if values == nil {
		values = []string{}
	}
	return configEntryJSON{
		Key:         d.key,
		Value:       d.jsonValue(stored),
		Default:     d.jsonValue(def),
		IsDefault:   stored == def,
		Valid:       d.isValidStored(stored),
		Type:        d.typeName(),
		Values:      values,
		Group:       string(d.group),
		Description: d.desc,
	}
}

// ── Runners (io.Writer for tests) ──────────────────────────────────

func runConfigList(out io.Writer, asJSON bool) error {
	settings := currentSettings()
	if asJSON {
		entries := make([]configEntryJSON, 0, len(settingDefs))
		for _, d := range settingDefs {
			entries = append(entries, configEntryFor(settings, d))
		}
		return writeJSON(out, configListJSON{Count: len(entries), Settings: entries})
	}
	for _, g := range settingGroupOrder {
		var defs []settingDef
		for _, d := range settingDefs {
			if d.group == g {
				defs = append(defs, d)
			}
		}
		if len(defs) == 0 {
			continue
		}
		fmt.Fprintln(out, cliAccent(g.title()))
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  KEY\tVALUE\tDESCRIPTION")
		for _, d := range defs {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.key, configHumanValue(settings, d), d.desc)
		}
		_ = tw.Flush()
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "Change a value with 'wintui config set <key> <value>'; 'wintui config unset <key>' restores the default.")
	return nil
}

// configHumanValue renders a value for the table, flagging an invalid on-disk
// value instead of pretending it is the default it falls back to.
func configHumanValue(s Settings, d settingDef) string {
	stored := s.rawValue(d.key)
	if !d.isValidStored(stored) {
		return fmt.Sprintf("%q (invalid)", stored)
	}
	return d.cliName(stored)
}

func runConfigGet(out io.Writer, key string, asJSON bool) error {
	d, ok := settingDefByKey(key)
	if !ok {
		return unknownSettingError(key)
	}
	settings := currentSettings()
	if asJSON {
		return writeJSON(out, configEntryFor(settings, d))
	}
	fmt.Fprintln(out, configHumanValue(settings, d))
	return nil
}

func runConfigSet(out io.Writer, key, value string, asJSON bool) error {
	d, ok := settingDefByKey(key)
	if !ok {
		return unknownSettingError(key)
	}
	stored, err := d.parseCLIValue(value)
	if err != nil {
		return err
	}
	if err := updateSettings(func(s *Settings) { s.setValue(d.key, stored) }); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	if asJSON {
		return writeJSON(out, configEntryFor(currentSettings(), d))
	}
	fmt.Fprintf(out, "%s set to %s.\n", d.key, d.cliName(stored))
	return nil
}

func runConfigUnset(out io.Writer, key string, asJSON bool) error {
	d, ok := settingDefByKey(key)
	if !ok {
		return unknownSettingError(key)
	}
	def := d.storedDefault()
	if err := updateSettings(func(s *Settings) { s.setValue(d.key, def) }); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	if asJSON {
		return writeJSON(out, configEntryFor(currentSettings(), d))
	}
	fmt.Fprintf(out, "%s reset to its default (%s).\n", d.key, d.cliName(def))
	return nil
}

func unknownSettingError(key string) error {
	return fmt.Errorf("unknown setting %q; run \"wintui config list\" to see the available keys", key)
}

// ── Completions ────────────────────────────────────────────────────

func configKeyCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, d := range settingDefs {
		if strings.HasPrefix(d.key, strings.ToLower(toComplete)) {
			out = append(out, d.key+"\t"+d.desc)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func configSetCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return configKeyCompletion(cmd, args, toComplete)
	case 1:
		d, ok := settingDefByKey(args[0])
		if !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, v := range d.cliChoices() {
			if strings.HasPrefix(strings.ToLower(v), strings.ToLower(toComplete)) {
				out = append(out, v)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
