package main

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// `wintui rules` — per-package rules (update policy, holds, scope,
// architecture, elevation) as a first-class CLI surface.
//
// Design points carried from the v2.12 design review:
//   - Rules are constructed FIELD-WISE from the stored PackageOverride, never
//     round-tripped through the TUI editor's get/setValue: setValue for
//     update_policy unconditionally clears a version hold, so the obvious
//     read-modify-write would silently turn a version-scoped hold into a
//     permanent one. Here `--policy` and `--ignore-version` are independent.
//   - Hold is an explicit union: none | all | version=<v>. Legacy
//     `ignore: true` on disk renders as policy=hold / hold=all and is
//     canonicalized by the shared write layer on the next edit.
//   - `rules show` prints BOTH the explicit rule and the effective value so
//     inheritance is visible; a version hold is evaluated against the
//     available version from the read-only package cache when it is warm,
//     and honestly reported as "unknown" when it is not — never a winget
//     call from a read command.
//   - Package IDs are case-insensitive (override_keys.go); new rules pick up
//     winget's casing from the cache; conflicting duplicate keys make
//     mutating forms refuse instead of guessing.
//   - No history records for rule edits, by design (settings, not actions).
//   - `wintui show` keeps its argv-diagnosis contract unchanged.

var rulesJSONFlag bool

var (
	rulesSourceFlag        string
	rulesPolicyFlag        string
	rulesScopeFlag         string
	rulesArchitectureFlag  string
	rulesElevateFlag       string
	rulesIgnoreVersionFlag string
)

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Show or change per-package rules (policy, holds, scope, elevation)",
	Long: `Show or change the per-package rules WinTUI applies on top of the global
settings: update policy (ask / auto / hold), version-specific holds, install
scope, architecture, and elevation.

"rules show" prints the explicit rule next to the effective value, so what a
package inherits from the global settings is visible. "rules set" changes only
the fields you pass, in one write. "rules clear" removes a whole rule or named
fields. Package IDs are case-insensitive.

Examples:
  wintui rules list
  wintui rules show Git.Git
  wintui rules set Git.Git --policy auto --scope user
  wintui rules set Mozilla.Firefox --ignore-version 155.0
  wintui rules set Anthropic.ClaudeCode --elevate never
  wintui rules clear Git.Git scope
  wintui rules clear Git.Git`,
}

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every explicit per-package rule",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRulesList(cmd.OutOrStdout(), rulesJSONFlag)
	},
}

var rulesShowCmd = &cobra.Command{
	Use:               "show <id>",
	Short:             "Show a package's explicit rule and its effective values",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeRuleIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRulesShow(cmd.OutOrStdout(), args[0], rulesSourceFlag, rulesJSONFlag)
	},
}

var rulesSetCmd = &cobra.Command{
	Use:               "set <id>",
	Short:             "Change one or more fields of a package's rule in one write",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeRuleIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var opts rulesSetOptions
		flags := cmd.Flags()
		if flags.Changed("policy") {
			opts.Policy = &rulesPolicyFlag
		}
		if flags.Changed("scope") {
			opts.Scope = &rulesScopeFlag
		}
		if flags.Changed("architecture") {
			opts.Architecture = &rulesArchitectureFlag
		}
		if flags.Changed("elevate") {
			opts.Elevate = &rulesElevateFlag
		}
		if flags.Changed("ignore-version") {
			opts.IgnoreVersion = &rulesIgnoreVersionFlag
		}
		return runRulesSet(cmd.OutOrStdout(), args[0], rulesSourceFlag, opts, rulesJSONFlag)
	},
}

var rulesClearCmd = &cobra.Command{
	Use:               "clear <id> [field...]",
	Short:             "Remove a package's rule, or only the named fields",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeRuleClearArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRulesClear(cmd.OutOrStdout(), args[0], rulesSourceFlag, args[1:], rulesJSONFlag)
	},
}

func init() {
	rulesCmd.PersistentFlags().BoolVar(&rulesJSONFlag, "json", false, "Output JSON")
	for _, c := range []*cobra.Command{rulesShowCmd, rulesSetCmd, rulesClearCmd} {
		c.Flags().StringVar(&rulesSourceFlag, "source", "", "Package source: winget (default) or msstore")
	}
	rulesSetCmd.Flags().StringVar(&rulesPolicyFlag, "policy", "", "Update policy: ask, auto, or hold")
	rulesSetCmd.Flags().StringVar(&rulesScopeFlag, "scope", "", "Install scope: inherit, user, or machine")
	rulesSetCmd.Flags().StringVar(&rulesArchitectureFlag, "architecture", "", "Architecture: inherit, x64, x86, or arm64")
	rulesSetCmd.Flags().StringVar(&rulesElevateFlag, "elevate", "", "Elevation: inherit, always, or never")
	rulesSetCmd.Flags().StringVar(&rulesIgnoreVersionFlag, "ignore-version", "", "Hold only this available version (a version-specific hold)")
	rulesCmd.AddCommand(rulesListCmd, rulesShowCmd, rulesSetCmd, rulesClearCmd)
}

// ── Vocabulary ─────────────────────────────────────────────────────

const (
	ruleInherit     = "inherit"
	ruleHoldNone    = "none"
	ruleHoldAll     = "all"
	ruleHoldVersion = "version"
)

var (
	rulePolicyNames       = []string{"ask", "auto", "hold"}
	ruleScopeNames        = []string{ruleInherit, "user", "machine"}
	ruleArchitectureNames = []string{ruleInherit, "x64", "x86", "arm64"}
	ruleElevateNames      = []string{ruleInherit, "always", "never"}
	ruleClearFields       = []string{"policy", "scope", "architecture", "elevate", "ignore-version"}
)

func parseRulePolicy(in string) (UpdatePolicy, error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "ask":
		return PolicyAsk, nil
	case "auto":
		return PolicyAuto, nil
	case "hold":
		return PolicyHold, nil
	}
	return "", fmt.Errorf("invalid --policy %q: expected one of %s", in, strings.Join(rulePolicyNames, ", "))
}

func parseRuleScope(in string) (InstallScope, error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case ruleInherit, "":
		return ScopeDefault, nil
	case "user":
		return ScopeUser, nil
	case "machine":
		return ScopeMachine, nil
	}
	return "", fmt.Errorf("invalid --scope %q: expected one of %s", in, strings.Join(ruleScopeNames, ", "))
}

func parseRuleArchitecture(in string) (CPUArchitecture, error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case ruleInherit, "":
		return ArchDefault, nil
	case "x64":
		return ArchX64, nil
	case "x86":
		return ArchX86, nil
	case "arm64":
		return ArchARM64, nil
	}
	return "", fmt.Errorf("invalid --architecture %q: expected one of %s", in, strings.Join(ruleArchitectureNames, ", "))
}

// parseRuleElevate returns nil for inherit. A typo must never mean "never":
// only the listed words and the same boolean aliases `wintui config` accepts
// (true/false, yes/no, on/off, 1/0) are accepted.
func parseRuleElevate(in string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case ruleInherit, "":
		return nil, nil
	case "always", "true", "yes", "on", "1":
		b := true
		return &b, nil
	case "never", "false", "no", "off", "0":
		b := false
		return &b, nil
	}
	return nil, fmt.Errorf("invalid --elevate %q: expected one of %s", in, strings.Join(ruleElevateNames, ", "))
}

// ── Views (shared builder) ─────────────────────────────────────────

// ruleHold is the explicit hold union.
type ruleHold struct {
	Kind    string `json:"kind"`              // none | all | version
	Version string `json:"version,omitempty"` // kind=version only
	// Active reports whether a version hold currently applies (the available
	// version equals the held one). nil when the available version is unknown
	// (package cache cold) or for kinds other than version.
	Active *bool `json:"active"`
}

// ruleSpec is the explicit rule as stored, in CLI vocabulary. Tri-state
// fields read "inherit" when the rule does not set them.
type ruleSpec struct {
	Policy       string   `json:"policy"`
	Hold         ruleHold `json:"hold"`
	Scope        string   `json:"scope"`
	Architecture string   `json:"architecture"`
	Elevate      string   `json:"elevate"`
}

// ruleEffective is what WinTUI will actually do for the package once the
// rule is layered over the global settings.
type ruleEffective struct {
	Policy           string   `json:"policy"`
	Hold             ruleHold `json:"hold"`
	Scope            string   `json:"scope"`
	Architecture     string   `json:"architecture"`
	Elevate          bool     `json:"elevate"`
	AvailableVersion *string  `json:"available_version"` // nil when unknown
}

type ruleShowJSON struct {
	ID        string        `json:"id"`
	Source    string        `json:"source"`
	Key       string        `json:"key"`
	HasRule   bool          `json:"has_rule"`
	Rule      *ruleSpec     `json:"rule"`
	Effective ruleEffective `json:"effective"`
	Conflicts []string      `json:"conflicts"`
}

type ruleListEntryJSON struct {
	ID     string   `json:"id"`
	Source string   `json:"source"`
	Key    string   `json:"key"`
	Rule   ruleSpec `json:"rule"`
}

type ruleListJSON struct {
	Count int                 `json:"count"`
	Rules []ruleListEntryJSON `json:"rules"`
}

// holdOf derives the explicit hold union from a stored rule. Legacy
// `ignore: true` is a permanent hold.
func holdOf(o PackageOverride) ruleHold {
	switch {
	case o.Ignore || normalizeUpdatePolicy(o.UpdatePolicy) == PolicyHold:
		return ruleHold{Kind: ruleHoldAll}
	case o.IgnoreVersion != "":
		return ruleHold{Kind: ruleHoldVersion, Version: o.IgnoreVersion}
	}
	return ruleHold{Kind: ruleHoldNone}
}

func rulePolicyName(o PackageOverride) string {
	if o.Ignore {
		return "hold"
	}
	switch normalizeUpdatePolicy(o.UpdatePolicy) {
	case PolicyAuto:
		return "auto"
	case PolicyHold:
		return "hold"
	}
	return "ask"
}

func ruleSpecOf(o PackageOverride) ruleSpec {
	spec := ruleSpec{
		Policy:       rulePolicyName(o),
		Hold:         holdOf(o),
		Scope:        ruleInherit,
		Architecture: ruleInherit,
		Elevate:      ruleInherit,
	}
	if o.Scope != "" {
		spec.Scope = string(o.Scope)
	}
	if o.Architecture != "" {
		spec.Architecture = string(o.Architecture)
	}
	if o.Elevate != nil {
		if *o.Elevate {
			spec.Elevate = "always"
		} else {
			spec.Elevate = "never"
		}
	}
	return spec
}

// availableVersionFromCache returns the cached available version for id when
// the read-only disk cache is warm. known=false means "cannot evaluate";
// known=true with an empty version means "no update pending".
func availableVersionFromCache(id string) (version string, known bool) {
	_, upgradeable, _, ok := cache.loadFromDisk()
	if !ok {
		return "", false
	}
	for _, p := range upgradeable {
		if strings.EqualFold(p.ID, id) {
			return p.Available, true
		}
	}
	return "", true
}

// effectiveRuleView is the ONE builder both CLI views consume: the explicit
// rule (nil when none), the effective values, and any conflicting duplicate
// keys. available/known come from availableVersionFromCache (or a test seam).
func effectiveRuleView(s Settings, id, source string, available string, known bool) (key string, spec *ruleSpec, eff ruleEffective, conflicts []string) {
	key, o, has := s.lookupOverride(id, source)
	if has {
		v := ruleSpecOf(o)
		spec = &v
	} else {
		key = packageRuleKey(id, source)
	}
	if group, ok := s.overrideConflictFor(id, source); ok {
		conflicts = group
	}

	global := s.effectiveSettings(id, source)
	scopeDef, _ := settingDefByKey("scope")
	archDef, _ := settingDefByKey("architecture")
	eff = ruleEffective{
		Scope:        scopeDef.cliName(string(global.Scope)),
		Architecture: archDef.cliName(string(global.Architecture)),
		Elevate:      global.AutoElevate,
		Hold:         ruleHold{Kind: ruleHoldNone},
		Policy:       "ask",
	}
	if known {
		av := available
		eff.AvailableVersion = &av
	}
	if !has {
		return key, spec, eff, conflicts
	}

	base := rulePolicyName(o)
	eff.Policy = base
	eff.Hold = holdOf(o)
	if eff.Hold.Kind == ruleHoldVersion {
		if known {
			active := available != "" && available == eff.Hold.Version
			eff.Hold.Active = &active
			if active {
				eff.Policy = "hold"
			}
		}
	}
	return key, spec, eff, conflicts
}

// ── Target normalization ───────────────────────────────────────────

func normalizeRuleTarget(id, source string) (string, string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", fmt.Errorf("package id is required")
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "":
		return id, "winget", nil
	case "winget", "msstore":
		return id, strings.ToLower(strings.TrimSpace(source)), nil
	}
	return "", "", fmt.Errorf("invalid --source %q: must be 'winget' or 'msstore'", source)
}

// splitRuleKey turns a stored map key back into (id, source). Sources never
// contain a dot; package IDs always do, so the first ':' is the separator
// only when the prefix looks like a source.
func splitRuleKey(key string) (id, source string) {
	if i := strings.Index(key, ":"); i > 0 && !strings.Contains(key[:i], ".") {
		return key[i+1:], key[:i]
	}
	return key, ""
}

func conflictError(id string, group []string) error {
	return fmt.Errorf("conflicting duplicate rules for %s: %s — keep one key in settings.json \"packages\" (IDs are case-insensitive), or run \"wintui rules clear %s\" to remove them all", id, strings.Join(group, " / "), id)
}

// ── Runners ────────────────────────────────────────────────────────

func runRulesList(out io.Writer, asJSON bool) error {
	s := currentSettings()
	keys := make([]string, 0, len(s.Packages))
	for k := range s.Packages {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ai, as := splitRuleKey(keys[i])
		bi, bs := splitRuleKey(keys[j])
		if !strings.EqualFold(ai, bi) {
			return strings.ToLower(ai) < strings.ToLower(bi)
		}
		if as != bs {
			return as < bs
		}
		return keys[i] < keys[j]
	})

	entries := make([]ruleListEntryJSON, 0, len(keys))
	for _, k := range keys {
		id, source := splitRuleKey(k)
		entries = append(entries, ruleListEntryJSON{ID: id, Source: source, Key: k, Rule: ruleSpecOf(s.Packages[k])})
	}
	if asJSON {
		return writeJSON(out, ruleListJSON{Count: len(entries), Rules: entries})
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "No per-package rules. Add one with 'wintui rules set <id> --policy auto|hold ...'.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSOURCE\tPOLICY\tHOLD\tSCOPE\tARCH\tELEVATE")
	for _, e := range entries {
		src := e.Source
		if src == "" {
			src = "(any)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.ID, src, e.Rule.Policy, holdLabel(e.Rule.Hold), e.Rule.Scope, e.Rule.Architecture, e.Rule.Elevate)
	}
	_ = tw.Flush()
	fmt.Fprintf(out, "\n%s\n", cliAccent(fmt.Sprintf("%d rule(s). 'wintui rules show <id>' prints the effective values.", len(entries))))
	if conflicts := s.overrideConflicts(); len(conflicts) > 0 {
		for _, g := range conflicts {
			fmt.Fprintf(out, "Warning: conflicting duplicate rules: %s\n", strings.Join(g, " / "))
		}
	}
	return nil
}

func holdLabel(h ruleHold) string {
	switch h.Kind {
	case ruleHoldVersion:
		return "version=" + h.Version
	case ruleHoldAll:
		return ruleHoldAll
	}
	return ruleHoldNone
}

// rulesAvailableFn is the cache seam tests replace.
var rulesAvailableFn = availableVersionFromCache

func runRulesShow(out io.Writer, id, source string, asJSON bool) error {
	id, source, err := normalizeRuleTarget(id, source)
	if err != nil {
		return err
	}
	return printRuleView(out, currentSettings(), id, source, asJSON, "")
}

func printRuleView(out io.Writer, s Settings, id, source string, asJSON bool, headline string) error {
	available, known := rulesAvailableFn(id)
	key, spec, eff, conflicts := effectiveRuleView(s, id, source, available, known)
	// Echo the stored casing once a rule exists, not whatever was typed.
	if spec != nil {
		if rid, _ := splitRuleKey(key); rid != "" {
			id = rid
		}
	}
	if asJSON {
		if conflicts == nil {
			conflicts = []string{}
		}
		return writeJSON(out, ruleShowJSON{ID: id, Source: source, Key: key, HasRule: spec != nil, Rule: spec, Effective: eff, Conflicts: conflicts})
	}
	if headline != "" {
		fmt.Fprintln(out, cliAccent(headline))
	}
	if spec == nil {
		fmt.Fprintf(out, "%s (%s) — no explicit rule; effective values are inherited from the global settings.\n\n", id, source)
	} else {
		fmt.Fprintf(out, "%s (%s) — rule %s\n\n", id, source, key)
	}
	inherit := ruleSpec{Policy: "ask", Hold: ruleHold{Kind: ruleHoldNone}, Scope: ruleInherit, Architecture: ruleInherit, Elevate: ruleInherit}
	if spec == nil {
		spec = &inherit
	}
	elevate := "false"
	if eff.Elevate {
		elevate = "true"
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tRULE\tEFFECTIVE")
	fmt.Fprintf(tw, "policy\t%s\t%s\n", spec.Policy, eff.Policy)
	fmt.Fprintf(tw, "hold\t%s\t%s\n", holdLabel(spec.Hold), effectiveHoldLabel(eff.Hold, known))
	fmt.Fprintf(tw, "scope\t%s\t%s\n", spec.Scope, eff.Scope)
	fmt.Fprintf(tw, "architecture\t%s\t%s\n", spec.Architecture, eff.Architecture)
	fmt.Fprintf(tw, "elevate\t%s\t%s\n", spec.Elevate, elevate)
	_ = tw.Flush()
	switch {
	case !known:
		fmt.Fprintln(out, "\nAvailable version: unknown (package cache is cold — open the TUI or run 'wintui check' to refresh).")
	case available == "":
		fmt.Fprintln(out, "\nAvailable version: none pending (per the package cache).")
	default:
		fmt.Fprintf(out, "\nAvailable version: %s (per the package cache).\n", available)
	}
	if len(conflicts) > 0 {
		fmt.Fprintf(out, "Warning: conflicting duplicate rules: %s\n", strings.Join(conflicts, " / "))
	}
	return nil
}

func effectiveHoldLabel(h ruleHold, known bool) string {
	label := holdLabel(h)
	if h.Kind != ruleHoldVersion {
		return label
	}
	switch {
	case !known:
		return label + " (cannot evaluate)"
	case h.Active != nil && *h.Active:
		return label + " (active)"
	}
	return label + " (inactive)"
}

// rulesSetOptions carries only the flags the user passed; nil = untouched.
type rulesSetOptions struct {
	Policy        *string
	Scope         *string
	Architecture  *string
	Elevate       *string
	IgnoreVersion *string
}

func (o rulesSetOptions) empty() bool {
	return o.Policy == nil && o.Scope == nil && o.Architecture == nil && o.Elevate == nil && o.IgnoreVersion == nil
}

func runRulesSet(out io.Writer, id, source string, opts rulesSetOptions, asJSON bool) error {
	id, source, err := normalizeRuleTarget(id, source)
	if err != nil {
		return err
	}
	if opts.empty() {
		return fmt.Errorf("nothing to set: pass at least one of --policy, --scope, --architecture, --elevate, --ignore-version")
	}

	// Validate everything before touching disk.
	var (
		policy  UpdatePolicy
		scope   InstallScope
		arch    CPUArchitecture
		elevate *bool
	)
	if opts.Policy != nil {
		if policy, err = parseRulePolicy(*opts.Policy); err != nil {
			return err
		}
	}
	if opts.Scope != nil {
		if scope, err = parseRuleScope(*opts.Scope); err != nil {
			return err
		}
	}
	if opts.Architecture != nil {
		if arch, err = parseRuleArchitecture(*opts.Architecture); err != nil {
			return err
		}
	}
	if opts.Elevate != nil {
		if elevate, err = parseRuleElevate(*opts.Elevate); err != nil {
			return err
		}
	}
	if opts.IgnoreVersion != nil && strings.TrimSpace(*opts.IgnoreVersion) == "" {
		return fmt.Errorf("--ignore-version needs a version; use \"wintui rules clear %s ignore-version\" to remove a version hold", id)
	}

	// Hold is ONE explicit state: none, all, or version. The two flags may
	// not produce "all + version" in one write, and a version hold on top of
	// an existing permanent hold would be invisible (holdOf reports all), so
	// that is refused rather than silently stored.
	if opts.Policy != nil && opts.IgnoreVersion != nil && policy == PolicyHold {
		return fmt.Errorf("--policy hold and --ignore-version are mutually exclusive: a permanent hold already covers every version")
	}

	s := currentSettings()
	if group, ok := s.overrideConflictFor(id, source); ok {
		return conflictError(id, group)
	}
	_, existing, exists := s.lookupOverride(id, source)
	if !exists {
		if canonical, ok := canonicalPackageID(id); ok {
			id = canonical
		}
	}
	if opts.IgnoreVersion != nil && opts.Policy == nil && exists && holdOf(existing).Kind == ruleHoldAll {
		return fmt.Errorf("%s is on a permanent hold, so a version hold would have no effect; pass --policy ask (or auto) together with --ignore-version, or clear the hold first", id)
	}

	apply := func(st *Settings) {
		_, o, _ := st.lookupOverride(id, source)
		if opts.Policy != nil {
			o.UpdatePolicy = policy
			o.Ignore = false // legacy flag is the same state as policy=hold
		}
		if opts.Scope != nil {
			o.Scope = scope
		}
		if opts.Architecture != nil {
			o.Architecture = arch
		}
		if opts.Elevate != nil {
			o.Elevate = elevate
		}
		if opts.IgnoreVersion != nil {
			o.IgnoreVersion = strings.TrimSpace(*opts.IgnoreVersion)
		}
		// A permanent hold supersedes a version hold: `--policy hold` drops
		// the version so a later `--policy ask` cannot resurrect it.
		if normalizeUpdatePolicy(o.UpdatePolicy) == PolicyHold {
			o.IgnoreVersion = ""
		}
		st.setOverride(id, source, o)
	}
	if err := updateSettings(apply); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	return printRuleView(out, settingsAfterWrite(), id, source, asJSON, fmt.Sprintf("Rule for %s (%s) updated.", id, source))
}

func runRulesClear(out io.Writer, id, source string, fields []string, asJSON bool) error {
	id, source, err := normalizeRuleTarget(id, source)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if !slices.Contains(ruleClearFields, strings.ToLower(f)) {
			return fmt.Errorf("unknown field %q: expected one of %s", f, strings.Join(ruleClearFields, ", "))
		}
	}

	s := currentSettings()
	_, _, exists := s.lookupOverride(id, source)
	if len(fields) == 0 {
		// Whole-rule clear deletes every alias, including a conflicting
		// group — it IS the resolution path for conflicts.
		if !exists {
			if asJSON {
				return printRuleView(out, s, id, source, true, "")
			}
			fmt.Fprintf(out, "No rule for %s (%s); nothing to clear.\n", id, source)
			return nil
		}
		if err := updateSettings(func(st *Settings) { st.setOverride(id, source, PackageOverride{}) }); err != nil {
			return fmt.Errorf("could not save settings: %w", err)
		}
		if asJSON {
			return printRuleView(out, settingsAfterWrite(), id, source, true, "")
		}
		fmt.Fprintf(out, "Rule for %s (%s) removed.\n", id, source)
		return nil
	}

	if group, ok := s.overrideConflictFor(id, source); ok {
		return conflictError(id, group)
	}
	if !exists {
		if asJSON {
			return printRuleView(out, s, id, source, true, "")
		}
		fmt.Fprintf(out, "No rule for %s (%s); nothing to clear.\n", id, source)
		return nil
	}
	apply := func(st *Settings) {
		_, o, _ := st.lookupOverride(id, source)
		for _, f := range fields {
			switch strings.ToLower(f) {
			case "policy":
				o.UpdatePolicy = PolicyAsk
				o.Ignore = false
			case "scope":
				o.Scope = ScopeDefault
			case "architecture":
				o.Architecture = ArchDefault
			case "elevate":
				o.Elevate = nil
			case "ignore-version":
				o.IgnoreVersion = ""
			}
		}
		st.setOverride(id, source, o)
	}
	if err := updateSettings(apply); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	return printRuleView(out, settingsAfterWrite(), id, source, asJSON, fmt.Sprintf("Rule for %s (%s) updated.", id, source))
}

// ── Completions ────────────────────────────────────────────────────

// completeRuleIDs offers installed and upgradeable packages from the cache
// plus every package that already has a rule — rules matter for packages
// that are currently up to date, so the upgradeable-only completer of
// `upgrade --id` is the wrong one here.
func completeRuleIDs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	installed, upgradeable, _, _ := cache.loadFromDisk()
	pkgs := append(append([]Package{}, installed...), upgradeable...)
	seen := map[string]bool{}
	for _, p := range pkgs {
		seen[strings.ToLower(p.ID)] = true
	}
	for k := range currentSettings().Packages {
		id, _ := splitRuleKey(k)
		if !seen[strings.ToLower(id)] {
			seen[strings.ToLower(id)] = true
			pkgs = append(pkgs, Package{ID: id, Name: "(rule)"})
		}
	}
	return packageIDCompletions(pkgs, toComplete)
}

func completeRuleClearArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeRuleIDs(cmd, args, toComplete)
	}
	var out []string
	for _, f := range ruleClearFields {
		if strings.HasPrefix(f, strings.ToLower(toComplete)) && !slices.Contains(args[1:], f) {
			out = append(out, f)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
