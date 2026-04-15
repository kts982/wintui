package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// InstallScope constrains the install/upgrade scope.
type InstallScope string

const (
	ScopeDefault InstallScope = ""
	ScopeUser    InstallScope = "user"
	ScopeMachine InstallScope = "machine"
)

// InstallMode constrains the installer UI behaviour.
type InstallMode string

const (
	ModeDefault     InstallMode = ""
	ModeSilent      InstallMode = "silent"
	ModeInteractive InstallMode = "interactive"
)

// CPUArchitecture constrains the preferred CPU architecture.
type CPUArchitecture string

const (
	ArchDefault CPUArchitecture = ""
	ArchX64     CPUArchitecture = "x64"
	ArchX86     CPUArchitecture = "x86"
	ArchARM64   CPUArchitecture = "arm64"
)

// PackageOverride holds per-package option overrides and ignore rules.
// Empty/nil fields mean "use the global default".
type PackageOverride struct {
	Scope         InstallScope    `json:"scope,omitempty"`
	Architecture  CPUArchitecture `json:"architecture,omitempty"`
	Elevate       *bool           `json:"elevate,omitempty"`
	Ignore        bool            `json:"ignore,omitempty"`
	IgnoreVersion string          `json:"ignore_version,omitempty"`
}

func (o PackageOverride) isEmpty() bool {
	return o.Scope == "" && o.Architecture == "" && o.Elevate == nil &&
		!o.Ignore && o.IgnoreVersion == ""
}

func (o PackageOverride) getValue(key string) string {
	switch key {
	case "scope":
		return string(o.Scope)
	case "architecture":
		return string(o.Architecture)
	case "elevate":
		if o.Elevate == nil {
			return ""
		}
		return boolStr(*o.Elevate)
	case "ignore":
		if o.Ignore {
			return "all"
		}
		if o.IgnoreVersion != "" {
			return o.IgnoreVersion
		}
		return ""
	}
	return ""
}

func (o *PackageOverride) setValue(key, val string) {
	switch key {
	case "scope":
		o.Scope = InstallScope(val)
	case "architecture":
		o.Architecture = CPUArchitecture(val)
	case "elevate":
		if val == "" {
			o.Elevate = nil
		} else {
			b := val == "true"
			o.Elevate = &b
		}
	case "ignore":
		switch val {
		case "":
			o.Ignore = false
			o.IgnoreVersion = ""
		case "all":
			o.Ignore = true
			o.IgnoreVersion = ""
		default:
			o.Ignore = false
			o.IgnoreVersion = val
		}
	}
}

// Settings holds user-configurable winget options.
type Settings struct {
	// Install/Upgrade scope: ScopeUser, ScopeMachine, or ScopeDefault (winget default)
	Scope InstallScope `json:"scope"`

	// Install mode: ModeDefault, ModeSilent, or ModeInteractive
	InstallMode InstallMode `json:"install_mode"`

	// Force: skip non-security related issues
	Force bool `json:"force"`

	// Architecture preference: ArchX64, ArchX86, ArchARM64, or ArchDefault (auto)
	Architecture CPUArchitecture `json:"architecture"`

	// Allow reboot during install/upgrade
	AllowReboot bool `json:"allow_reboot"`

	// Skip dependency processing
	SkipDependencies bool `json:"skip_dependencies"`

	// Purge all files on uninstall (portable packages)
	PurgeOnUninstall bool `json:"purge_on_uninstall"`

	// Include packages with unknown versions in upgrade list
	IncludeUnknown bool `json:"include_unknown"`

	// Default source: "winget", "msstore", or "" (all)
	Source string `json:"source"`

	// Attempt to automatically elevate commands that require admin
	AutoElevate bool `json:"auto_elevate"`

	// Per-package option overrides, keyed by source-qualified package key.
	// New writes use "<source>:<id>"; reads also support legacy plain-ID keys.
	Packages map[string]PackageOverride `json:"packages,omitempty"`
}

// DefaultSettings returns settings with sensible defaults.
func DefaultSettings() Settings {
	return Settings{
		Scope:       "",
		InstallMode: "",
		Source:      "winget",
		AutoElevate: true,
	}
}

var appSettings = DefaultSettings()

func packageRuleKey(pkgID, source string) string {
	if source == "" {
		return pkgID
	}
	return source + ":" + pkgID
}

func packageRuleKeys(pkgID, source string) []string {
	if source == "" {
		return []string{pkgID}
	}
	return []string{packageRuleKey(pkgID, source), pkgID}
}

func (s Settings) lookupOverride(pkgID, source string) (string, PackageOverride, bool) {
	if s.Packages == nil {
		return "", PackageOverride{}, false
	}
	for _, key := range packageRuleKeys(pkgID, source) {
		if o, ok := s.Packages[key]; ok {
			return key, o, true
		}
	}
	return "", PackageOverride{}, false
}

// effectiveSettings returns a copy of s with per-package overrides applied.
func (s Settings) effectiveSettings(pkgID, source string) Settings {
	_, o, ok := s.lookupOverride(pkgID, source)
	if !ok {
		return s
	}
	eff := s
	if o.Scope != "" {
		eff.Scope = o.Scope
	}
	if o.Architecture != "" {
		eff.Architecture = o.Architecture
	}
	if o.Elevate != nil {
		eff.AutoElevate = *o.Elevate
	}
	return eff
}

func (s Settings) packageElevateOverride(pkgID, source string) *bool {
	_, o, ok := s.lookupOverride(pkgID, source)
	if !ok {
		return nil
	}
	return o.Elevate
}

func (s *Settings) setOverride(pkgID, source string, o PackageOverride) {
	primaryKey := packageRuleKey(pkgID, source)
	legacyKey := pkgID
	if o.isEmpty() {
		if s.Packages != nil {
			delete(s.Packages, primaryKey)
			if primaryKey != legacyKey {
				delete(s.Packages, legacyKey)
			}
			if len(s.Packages) == 0 {
				s.Packages = nil
			}
		}
		return
	}
	if s.Packages == nil {
		s.Packages = make(map[string]PackageOverride)
	}
	if primaryKey != legacyKey {
		delete(s.Packages, legacyKey)
	}
	s.Packages[primaryKey] = o
}

func (s Settings) getOverride(pkgID, source string) PackageOverride {
	_, o, ok := s.lookupOverride(pkgID, source)
	if !ok {
		return PackageOverride{}
	}
	return o
}

func (s Settings) hasOverride(pkgID, source string) bool {
	_, o, ok := s.lookupOverride(pkgID, source)
	return ok && !o.isEmpty()
}

// isIgnored returns true if the package should be hidden from the upgrade list.
func (s Settings) isIgnored(pkgID, source, availableVersion string) bool {
	_, o, ok := s.lookupOverride(pkgID, source)
	if !ok {
		return false
	}
	if o.Ignore {
		return true
	}
	return o.IgnoreVersion != "" && o.IgnoreVersion == availableVersion
}

// expireVersionIgnores clears version-specific ignores where the available
// version has moved past the ignored version. Returns true if any were cleared.
func (s *Settings) expireVersionIgnores(upgradeable []Package) bool {
	if s.Packages == nil {
		return false
	}
	changed := false
	for _, pkg := range upgradeable {
		_, o, ok := s.lookupOverride(pkg.ID, pkg.Source)
		if !ok || o.IgnoreVersion == "" {
			continue
		}
		if pkg.Available != "" && pkg.Available != o.IgnoreVersion {
			o.IgnoreVersion = ""
			s.setOverride(pkg.ID, pkg.Source, o)
			changed = true
		}
	}
	if changed && len(s.Packages) == 0 {
		s.Packages = nil
	}
	return changed
}

// configPath returns the path to the settings JSON file.
func configPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	dir := filepath.Join(configDir, "wintui")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "settings.json")
}

// LoadSettings reads settings from disk, falling back to defaults.
func LoadSettings() Settings {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return DefaultSettings()
	}
	s := DefaultSettings()
	json.Unmarshal(data, &s)
	return s
}

// SaveSettings writes settings to disk.
func SaveSettings(s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0644)
}

func persistSettings(next Settings) error {
	if err := SaveSettings(next); err != nil {
		return err
	}
	appSettings = next
	return nil
}

func persistPackageOverride(pkgID, source string, o PackageOverride) error {
	next := appSettings
	next.setOverride(pkgID, source, o)
	return persistSettings(next)
}

// BuildInstallArgs returns extra winget flags based on current settings.
// Used for install, upgrade actions.
func (s Settings) BuildInstallArgs() []string {
	var args []string
	if s.Scope != ScopeDefault {
		args = append(args, "--scope", string(s.Scope))
	}
	switch s.InstallMode {
	case ModeSilent:
		args = append(args, "--silent")
	case ModeInteractive:
		args = append(args, "--interactive")
	}
	if s.Architecture != ArchDefault {
		args = append(args, "--architecture", string(s.Architecture))
	}
	if s.Force {
		args = append(args, "--force")
	}
	if s.AllowReboot {
		args = append(args, "--allow-reboot")
	}
	if s.SkipDependencies {
		args = append(args, "--skip-dependencies")
	}
	return args
}

// BuildUninstallArgs returns extra winget flags for uninstall.
func (s Settings) BuildUninstallArgs(includePurge bool) []string {
	var args []string
	switch s.InstallMode {
	case ModeSilent:
		args = append(args, "--silent")
	case ModeInteractive:
		args = append(args, "--interactive")
	}
	if includePurge && s.PurgeOnUninstall {
		args = append(args, "--purge")
	}
	if s.Force {
		args = append(args, "--force")
	}
	return args
}

// BuildListArgs returns extra winget flags for listing/upgrade queries.
func (s Settings) BuildListArgs() []string {
	var args []string
	if s.IncludeUnknown {
		args = append(args, "--include-unknown")
	}
	if s.Source != "" {
		args = append(args, "--source", s.Source)
	}
	return args
}

// ── Setting definitions for the UI ─────────────────────────────────

type settingType int

const (
	settingToggle settingType = iota
	settingChoice
)

type settingDef struct {
	key          string
	label        string
	desc         string
	detail       string
	stype        settingType
	choices      []string // for settingChoice
	choiceLabels map[string]string
	choiceHints  map[string]string
	enabledHint  string
	disabledHint string
}

var settingDefs = []settingDef{
	{
		key:     "scope",
		label:   "Install Scope",
		desc:    "Default, user-only, or machine-wide",
		detail:  "Scope affects install and upgrade actions.\nMachine scope may require administrator privileges.",
		stype:   settingChoice,
		choices: []string{"", "user", "machine"},
		choiceLabels: map[string]string{
			"":        "default",
			"user":    "user",
			"machine": "machine",
		},
		choiceHints: map[string]string{
			"":        "Let winget and the package choose the normal scope.",
			"user":    "Install only for the current Windows account when supported.",
			"machine": "Install system-wide when supported.",
		},
	},
	{
		key:     "install_mode",
		label:   "Action Mode",
		desc:    "UI behavior for install, upgrade, uninstall",
		detail:  "Default uses the package's normal flow.\nSilent requests no installer UI.\nInteractive allows prompts and windows.\nPackages may ignore the request if their installer does not support it.",
		stype:   settingChoice,
		choices: []string{"", "silent", "interactive"},
		choiceLabels: map[string]string{
			"":            "default",
			"silent":      "silent",
			"interactive": "interactive",
		},
		choiceHints: map[string]string{
			"":            "Use the package's normal installer behavior.",
			"silent":      "Request a quiet run with no UI. Combined with Auto Elevate, all actions run elevated upfront.",
			"interactive": "Allow installer dialogs and prompts.",
		},
	},
	{
		key:     "architecture",
		label:   "Architecture",
		desc:    "Preferred CPU architecture",
		detail:  "Auto lets winget choose the best installer for this machine.\nOnly change this when you intentionally need a non-default architecture.",
		stype:   settingChoice,
		choices: []string{"", "x64", "x86", "arm64"},
		choiceLabels: map[string]string{
			"":      "auto",
			"x64":   "x64",
			"x86":   "x86",
			"arm64": "arm64",
		},
		choiceHints: map[string]string{
			"":      "Choose the installer that best matches the current machine.",
			"x64":   "Prefer 64-bit packages.",
			"x86":   "Prefer 32-bit packages.",
			"arm64": "Prefer ARM64 packages.",
		},
	},
	{
		key:     "source",
		label:   "Default Source",
		desc:    "Preferred source for search and install",
		detail:  "This affects searches, installs, and upgrade queries.\nUninstall works from the installed package database and does not depend on this setting.",
		stype:   settingChoice,
		choices: []string{"winget", "msstore", ""},
		choiceLabels: map[string]string{
			"winget":  "winget",
			"msstore": "msstore",
			"":        "all",
		},
		choiceHints: map[string]string{
			"winget":  "Prefer the winget community repository.",
			"msstore": "Prefer Microsoft Store packages only.",
			"":        "Search across all available sources.",
		},
	},
	{
		key:          "force",
		label:        "Force",
		desc:         "Continue past non-security warnings",
		detail:       "Useful for stubborn packages, but it can bypass normal guardrails.\nLeave this off unless you know why a package needs it.",
		stype:        settingToggle,
		enabledHint:  "Winget will continue through non-security warnings.",
		disabledHint: "Use winget's normal warning and safety behavior.",
	},
	{
		key:          "allow_reboot",
		label:        "Allow Reboot",
		desc:         "Permit package-triggered reboots",
		detail:       "Some installers request a reboot to finish.\nKeep this off unless you are okay with WinTUI allowing that automatically.",
		stype:        settingToggle,
		enabledHint:  "Package actions may reboot the machine if required.",
		disabledHint: "Package actions will not opt into automatic reboot behavior.",
	},
	{
		key:          "skip_dependencies",
		label:        "Skip Dependencies",
		desc:         "Do not install package dependencies",
		detail:       "This is mainly for advanced cases.\nTurning it on can leave packages partially installed or unusable.",
		stype:        settingToggle,
		enabledHint:  "Dependencies will be skipped when supported by winget.",
		disabledHint: "Winget will install required dependencies normally.",
	},
	{
		key:          "purge_on_uninstall",
		label:        "Purge on Uninstall",
		desc:         "Delete package files for portable apps",
		detail:       "This is most useful for portable packages.\nMany normal installers ignore purge, and WinTUI will retry without it if purge causes a failure.",
		stype:        settingToggle,
		enabledHint:  "Request full removal of portable package files when possible.",
		disabledHint: "Use the package's standard uninstall behavior.",
	},
	{
		key:          "include_unknown",
		label:        "Include Unknown Versions",
		desc:         "Show unknown-version packages in upgrades",
		detail:       "Some packages do not report a local version cleanly.\nTurn this on if you still want those entries in the upgrade list.",
		stype:        settingToggle,
		enabledHint:  "Upgrade scans will include packages with unknown installed versions.",
		disabledHint: "Upgrade scans will hide packages whose installed version is unknown.",
	},
	{
		key:          "auto_elevate",
		label:        "Auto Elevate",
		desc:         "Automatically request administrator rights",
		detail:       "When enabled, WinTUI automatically handles elevation.\nIn silent mode, all actions run elevated upfront to avoid UAC popups.\nIn other modes, elevation is retried automatically on failure.\nTurn this off to stay non-elevated and use Ctrl+E manually.",
		stype:        settingToggle,
		enabledHint:  "WinTUI will handle elevation automatically. In silent mode, all actions run elevated.",
		disabledHint: "All actions run non-elevated. Use Ctrl+E to retry on failure.",
	},
}

// getValue returns the current value for a setting key.
func (s Settings) getValue(key string) string {
	switch key {
	case "scope":
		return string(s.Scope)
	case "install_mode":
		return string(s.InstallMode)
	case "architecture":
		return string(s.Architecture)
	case "source":
		return s.Source
	case "force":
		return boolStr(s.Force)
	case "allow_reboot":
		return boolStr(s.AllowReboot)
	case "skip_dependencies":
		return boolStr(s.SkipDependencies)
	case "purge_on_uninstall":
		return boolStr(s.PurgeOnUninstall)
	case "include_unknown":
		return boolStr(s.IncludeUnknown)
	case "auto_elevate":
		return boolStr(s.AutoElevate)
	}
	return ""
}

// setValue sets a value for a setting key.
func (s *Settings) setValue(key, val string) {
	switch key {
	case "scope":
		s.Scope = InstallScope(val)
	case "install_mode":
		s.InstallMode = InstallMode(val)
	case "architecture":
		s.Architecture = CPUArchitecture(val)
	case "source":
		s.Source = val
	case "force":
		s.Force = val == "true"
	case "allow_reboot":
		s.AllowReboot = val == "true"
	case "skip_dependencies":
		s.SkipDependencies = val == "true"
	case "purge_on_uninstall":
		s.PurgeOnUninstall = val == "true"
	case "include_unknown":
		s.IncludeUnknown = val == "true"
	case "auto_elevate":
		s.AutoElevate = val == "true"
	}
}

func settingsEqual(a, b Settings) bool {
	return a.Scope == b.Scope &&
		a.InstallMode == b.InstallMode &&
		a.Force == b.Force &&
		a.Architecture == b.Architecture &&
		a.AllowReboot == b.AllowReboot &&
		a.SkipDependencies == b.SkipDependencies &&
		a.PurgeOnUninstall == b.PurgeOnUninstall &&
		a.IncludeUnknown == b.IncludeUnknown &&
		a.Source == b.Source &&
		a.AutoElevate == b.AutoElevate &&
		packagesEqual(a.Packages, b.Packages)
}

func packagesEqual(a, b map[string]PackageOverride) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if va.Scope != vb.Scope || va.Architecture != vb.Architecture ||
			va.Ignore != vb.Ignore || va.IgnoreVersion != vb.IgnoreVersion {
			return false
		}
		if (va.Elevate == nil) != (vb.Elevate == nil) {
			return false
		}
		if va.Elevate != nil && *va.Elevate != *vb.Elevate {
			return false
		}
	}
	return true
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

var overrideDefs = []settingDef{
	{
		key:     "ignore",
		label:   "Ignore",
		desc:    "Hide from upgrade list",
		stype:   settingChoice,
		choices: []string{"", "all"},
		choiceLabels: map[string]string{
			"":    "none",
			"all": "all versions",
		},
		choiceHints: map[string]string{
			"":    "Show upgrade notifications normally.",
			"all": "Permanently hide this package from the upgrade list.",
		},
	},
	{
		key:     "scope",
		label:   "Scope",
		desc:    "Install scope override",
		stype:   settingChoice,
		choices: []string{"", "user", "machine"},
		choiceLabels: map[string]string{
			"":        "global",
			"user":    "user",
			"machine": "machine",
		},
		choiceHints: map[string]string{
			"":        "Use the global scope setting.",
			"user":    "Always install this package for the current user only.",
			"machine": "Always install this package system-wide.",
		},
	},
	{
		key:     "architecture",
		label:   "Architecture",
		desc:    "CPU architecture override",
		stype:   settingChoice,
		choices: []string{"", "x64", "x86", "arm64"},
		choiceLabels: map[string]string{
			"":      "global",
			"x64":   "x64",
			"x86":   "x86",
			"arm64": "arm64",
		},
		choiceHints: map[string]string{
			"":      "Use the global architecture setting.",
			"x64":   "Always prefer the 64-bit installer for this package.",
			"x86":   "Always prefer the 32-bit installer for this package.",
			"arm64": "Always prefer the ARM64 installer for this package.",
		},
	},
	{
		key:     "elevate",
		label:   "Elevate",
		desc:    "Admin elevation override",
		stype:   settingChoice,
		choices: []string{"", "true", "false"},
		choiceLabels: map[string]string{
			"":      "global",
			"true":  "always",
			"false": "never",
		},
		choiceHints: map[string]string{
			"":      "Use the global auto-elevate setting.",
			"true":  "Always run this package elevated (admin).",
			"false": "Never auto-elevate this package.",
		},
	},
}

func (d settingDef) choiceLabel(val string) string {
	if label, ok := d.choiceLabels[val]; ok {
		return label
	}
	if val == "" {
		return "auto"
	}
	return val
}

func (d settingDef) currentHint(val string) string {
	switch d.stype {
	case settingChoice:
		return d.choiceHints[val]
	case settingToggle:
		if val == "true" {
			return d.enabledHint
		}
		return d.disabledHint
	default:
		return ""
	}
}
