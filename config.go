package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// UpdatePolicy controls how an upgradeable package is handled.
type UpdatePolicy string

const (
	PolicyAsk  UpdatePolicy = "" // default: show in Updates and let the user decide
	PolicyAuto UpdatePolicy = "auto"
	PolicyHold UpdatePolicy = "hold"
)

func normalizeUpdatePolicy(policy UpdatePolicy) UpdatePolicy {
	switch policy {
	case PolicyAuto:
		return PolicyAuto
	case PolicyHold:
		return PolicyHold
	default:
		return PolicyAsk
	}
}

// CleanupAutoScan is the tri-state for what the cleanup tab auto-scans on
// open. The persisted-on-disk default is "" (safe); any unknown value
// normalizes to safe so a hand-edited settings.json can't disable scanning
// silently.
type CleanupAutoScan string

const (
	CleanupAutoScanSafe CleanupAutoScan = ""    // default: scan core temp + caches + GPU vendor caches
	CleanupAutoScanAll  CleanupAutoScan = "all" // scan every present target on tab open
	CleanupAutoScanOff  CleanupAutoScan = "off" // never auto-scan; user presses s/r explicitly
)

func normalizeCleanupAutoScan(c CleanupAutoScan) CleanupAutoScan {
	switch c {
	case CleanupAutoScanAll:
		return CleanupAutoScanAll
	case CleanupAutoScanOff:
		return CleanupAutoScanOff
	default:
		return CleanupAutoScanSafe
	}
}

// ThemeBackground controls whether WinTUI paints the terminal
// background from the active palette via tea.View.BackgroundColor
// (OSC 11). Default is "terminal" — leave the user's terminal alone.
// "theme" opts into the palette's Background. Unknown values
// normalize back to "terminal" so a hand-edited settings.json can't
// silently override the safe default.
type ThemeBackground string

const (
	ThemeBackgroundTerminal ThemeBackground = ""      // default: don't touch terminal bg
	ThemeBackgroundTheme    ThemeBackground = "theme" // paint palette.Background via OSC 11
)

func normalizeThemeBackground(b ThemeBackground) ThemeBackground {
	if b == ThemeBackgroundTheme {
		return ThemeBackgroundTheme
	}
	return ThemeBackgroundTerminal
}

// normalizeTheme returns the theme ID if it's known, else "default".
// Used at apply time so a hand-edited settings.json with a garbage
// value can't crash startup.
func normalizeTheme(id string) string {
	if _, ok := themes[id]; ok {
		return id
	}
	return "default"
}

// PackageOverride holds per-package option overrides and ignore rules.
// Empty/nil fields mean "use the global default".
type PackageOverride struct {
	Scope         InstallScope    `json:"scope,omitempty"`
	Architecture  CPUArchitecture `json:"architecture,omitempty"`
	Elevate       *bool           `json:"elevate,omitempty"`
	UpdatePolicy  UpdatePolicy    `json:"update_policy,omitempty"`
	Ignore        bool            `json:"ignore,omitempty"`
	IgnoreVersion string          `json:"ignore_version,omitempty"`
}

func (o PackageOverride) isEmpty() bool {
	return o.Scope == "" && o.Architecture == "" && o.Elevate == nil &&
		normalizeUpdatePolicy(o.UpdatePolicy) == PolicyAsk &&
		!o.Ignore && o.IgnoreVersion == ""
}

func (o PackageOverride) displayedUpdatePolicy() UpdatePolicy {
	if o.Ignore || o.IgnoreVersion != "" {
		return PolicyHold
	}
	return normalizeUpdatePolicy(o.UpdatePolicy)
}

func (o PackageOverride) effectiveUpdatePolicy(availableVersion string) UpdatePolicy {
	if o.Ignore {
		return PolicyHold
	}
	if o.IgnoreVersion != "" && o.IgnoreVersion == availableVersion {
		return PolicyHold
	}
	return normalizeUpdatePolicy(o.UpdatePolicy)
}

func (o PackageOverride) getValue(key string) string {
	switch key {
	case "update_policy":
		return string(o.displayedUpdatePolicy())
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
	case "update_policy":
		o.UpdatePolicy = normalizeUpdatePolicy(UpdatePolicy(val))
		o.Ignore = false
		o.IgnoreVersion = ""
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
		o.UpdatePolicy = PolicyAsk
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

	// Check for and hand off WinTUI's own winget upgrade before launching the TUI
	AutoSelfUpdate bool `json:"auto_self_update"`

	// Send a Windows toast on TUI batch finish, headless upgrade --auto/--all
	// finish, and `wintui check` finding updates. Default off; opt-in.
	ToastNotifications bool `json:"toast_notifications"`

	// CleanupAutoScan controls what the cleanup tab scans on open:
	// "" (safe, default), "all", or "off". See CleanupAutoScan constants.
	CleanupAutoScan CleanupAutoScan `json:"cleanup_auto_scan,omitempty"`

	// Theme selects the active color palette. "" or "default" means the
	// built-in Sweet Pink theme. Curated set: wintui, catppuccin, nord,
	// dracula, tokyonight, ember, mono. Unknown values fall back to default at
	// apply time via lookupTheme.
	Theme string `json:"theme,omitempty"`

	// ThemeBackground controls whether the active palette tints the
	// terminal background (via tea.View.BackgroundColor / OSC 11).
	// Default "" (=terminal) leaves the user's terminal alone. "theme"
	// opts in. See ThemeBackground constants.
	ThemeBackground ThemeBackground `json:"theme_background,omitempty"`

	// CleanupEnabledTargets is the set of cleanup target IDs the user has
	// opted into beyond the registry's default-checked safe set. Default-on
	// targets are not persisted here (that's noise); only positive opt-ins
	// for advanced/admin targets like "go_build" or "yarn_cache".
	CleanupEnabledTargets []string `json:"cleanup_enabled_targets,omitempty"`

	// Per-package option overrides, keyed by source-qualified package key.
	// New writes use "<source>:<id>"; reads also support legacy plain-ID keys.
	Packages map[string]PackageOverride `json:"packages,omitempty"`
}

// DefaultSettings returns settings with sensible defaults.
func DefaultSettings() Settings {
	return Settings{
		Scope:          "",
		InstallMode:    "",
		Source:         "winget",
		AutoElevate:    true,
		AutoSelfUpdate: true,
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

// isIgnored returns true if the package should be held out of the upgrade list.
func (s Settings) isIgnored(pkgID, source, availableVersion string) bool {
	return s.updatePolicy(pkgID, source, availableVersion) == PolicyHold
}

func (s Settings) updatePolicy(pkgID, source, availableVersion string) UpdatePolicy {
	_, o, ok := s.lookupOverride(pkgID, source)
	if !ok {
		return PolicyAsk
	}
	return o.effectiveUpdatePolicy(availableVersion)
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

// cleanupTargetEnabled reports whether `def` should start checked when the
// cleanup tab opens. Default-checked registry entries (Core Temp, Caches)
// are always on; everything else is on iff its ID is in CleanupEnabledTargets.
func (s Settings) cleanupTargetEnabled(def cleanupTargetDef) bool {
	if def.defaultChecked {
		return true
	}
	return slices.Contains(s.CleanupEnabledTargets, def.id)
}

// setCleanupTargetEnabled persists the user's opt-in for non-default-checked
// targets. Default-checked entries are always-on by design and never
// persisted (that would be noise); this method silently ignores them.
func (s *Settings) setCleanupTargetEnabled(def cleanupTargetDef, enabled bool) {
	if def.defaultChecked {
		return
	}
	var out []string
	for _, id := range s.CleanupEnabledTargets {
		if id != def.id {
			out = append(out, id)
		}
	}
	if enabled {
		out = append(out, def.id)
	}
	s.CleanupEnabledTargets = out
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

// SaveSettings writes settings to disk atomically via temp file + rename.
func SaveSettings(s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := configPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// BuildListArgs returns extra winget flags for search queries.
// --include-unknown is intentionally excluded: it is only valid on
// `winget upgrade`; `winget search` rejects it.
func (s Settings) BuildListArgs() []string {
	var args []string
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
		detail:  "This affects searches and installs.\nUpgrades query all sources; uninstall works from the installed package database and does not depend on this setting.",
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
	{
		key:          "auto_self_update",
		label:        "WinTUI Auto Update",
		desc:         "Update WinTUI before launch",
		detail:       "When enabled and WinTUI is running from its winget install, startup checks for a WinTUI update and closes to let winget apply it before the TUI starts.",
		stype:        settingToggle,
		enabledHint:  "Startup will hand WinTUI updates to winget before the TUI starts.",
		disabledHint: "WinTUI will only update when you apply it from the Updates list.",
	},
	{
		key:          "toast_notifications",
		label:        "Toast Notifications",
		desc:         "Windows toast on batch / scheduled run finish",
		detail:       "When enabled, WinTUI sends a single Windows toast on TUI batch finish, on `wintui upgrade --auto/--all` finish, and when `wintui check` finds updates. A minimal Start Menu shortcut is dropped on first toast so notifications attribute as WinTUI rather than PowerShell. Skipped when running in CI or when WINTUI_DISABLE_TOAST is set.",
		stype:        settingToggle,
		enabledHint:  "Send a Windows toast on batch and scheduled-run finish.",
		disabledHint: "No Windows toasts will be sent.",
	},
	{
		key:     "cleanup_auto_scan",
		label:   "Cleanup Auto-Scan",
		desc:    "What the Cleanup tab scans on open",
		detail:  "Controls which cleanup targets are sized automatically when you open the Cleanup tab.\nSafe scans the default-checked safe set and any GPU vendor caches present, leaving developer caches alone until you check them.\nAll scans every present target on tab open — slower but gives you a complete picture.\nOff disables auto-scan; press s to size the focused target or r to rescan.",
		stype:   settingChoice,
		choices: []string{"", "all", "off"},
		choiceLabels: map[string]string{
			"":    "safe",
			"all": "all",
			"off": "off",
		},
		choiceHints: map[string]string{
			"":    "Scan the safe set and any GPU vendor caches present.",
			"all": "Scan every present target — slower, but fully populated.",
			"off": "Never auto-scan. Press s to size focused row, r to rescan.",
		},
	},
	themeSettingDef,
	{
		key:     "theme_background",
		label:   "Theme Background",
		desc:    "Tint the terminal background from the active theme",
		detail:  "When set to \"theme\", WinTUI tints the terminal background with the active palette's color via OSC 11.\nDefault \"terminal\" leaves your terminal background untouched (recommended on most terminals).\nThe default Sweet Pink theme does not define a background — switch to WinTUI Midnight, Catppuccin, Nord, Dracula, Tokyo Night, Ember, or Monochrome before enabling.\nSupport varies across terminals; if your terminal doesn't honor OSC 11, this setting is a no-op.",
		stype:   settingChoice,
		choices: []string{"", "theme"},
		choiceLabels: map[string]string{
			"":      "terminal",
			"theme": "theme",
		},
		choiceHints: map[string]string{
			"":      "Leave the terminal's background unchanged.",
			"theme": "Paint the active palette's background color via OSC 11.",
		},
	},
}

// themeSettingDef is built lazily so settingDefs can stay a var. The
// choice list, labels, and hints are derived from the themes map +
// themeOrder so adding a new palette in theme.go automatically lands
// in the picker without an entry in two places.
var themeSettingDef = buildThemeSettingDef()

func buildThemeSettingDef() settingDef {
	choices := make([]string, 0, len(themeOrder))
	labels := make(map[string]string, len(themeOrder))
	hints := make(map[string]string, len(themeOrder))
	for _, id := range themeOrder {
		t, ok := themes[id]
		if !ok {
			continue
		}
		key := id
		if id == "default" {
			key = "" // align with normalizeTheme("") → "default"
		}
		choices = append(choices, key)
		labels[key] = t.Label
		hints[key] = "Use the " + t.Label + " palette."
	}
	return settingDef{
		key:          "theme",
		label:        "Color Theme",
		desc:         "Active color palette",
		detail:       "Cycles through the curated theme set. Each theme includes a light and dark variant; WinTUI switches variants automatically when the terminal reports a light background.",
		stype:        settingChoice,
		choices:      choices,
		choiceLabels: labels,
		choiceHints:  hints,
	}
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
	case "auto_self_update":
		return boolStr(s.AutoSelfUpdate)
	case "toast_notifications":
		return boolStr(s.ToastNotifications)
	case "cleanup_auto_scan":
		return string(normalizeCleanupAutoScan(s.CleanupAutoScan))
	case "theme":
		// Normalize "default" back to "" so the picker shows the
		// canonical default-slot label rather than a literal "default"
		// entry the user never picked.
		id := normalizeTheme(s.Theme)
		if id == "default" {
			return ""
		}
		return id
	case "theme_background":
		return string(normalizeThemeBackground(s.ThemeBackground))
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
	case "auto_self_update":
		s.AutoSelfUpdate = val == "true"
	case "toast_notifications":
		s.ToastNotifications = val == "true"
	case "cleanup_auto_scan":
		s.CleanupAutoScan = normalizeCleanupAutoScan(CleanupAutoScan(val))
	case "theme":
		s.Theme = normalizeTheme(val)
		if s.Theme == "default" {
			s.Theme = "" // persist the canonical "I haven't picked" value
		}
	case "theme_background":
		s.ThemeBackground = normalizeThemeBackground(ThemeBackground(val))
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
		a.AutoSelfUpdate == b.AutoSelfUpdate &&
		a.ToastNotifications == b.ToastNotifications &&
		normalizeCleanupAutoScan(a.CleanupAutoScan) == normalizeCleanupAutoScan(b.CleanupAutoScan) &&
		stringSetsEqual(a.CleanupEnabledTargets, b.CleanupEnabledTargets) &&
		normalizeTheme(a.Theme) == normalizeTheme(b.Theme) &&
		normalizeThemeBackground(a.ThemeBackground) == normalizeThemeBackground(b.ThemeBackground) &&
		packagesEqual(a.Packages, b.Packages)
}

// stringSetsEqual reports whether two string slices represent the same set.
// Order is not significant; duplicates are treated as one occurrence.
func stringSetsEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			return false
		}
		delete(seen, s)
	}
	return len(seen) == 0
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
			normalizeUpdatePolicy(va.UpdatePolicy) != normalizeUpdatePolicy(vb.UpdatePolicy) ||
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
		key:     "update_policy",
		label:   "Update Policy",
		desc:    "Auto, ask, or hold upgrades",
		stype:   settingChoice,
		choices: []string{"", "auto", "hold"},
		choiceLabels: map[string]string{
			"":     "ask",
			"auto": "auto",
			"hold": "hold",
		},
		choiceHints: map[string]string{
			"":     "Show this package in Updates and let you choose when to upgrade.",
			"auto": "Upgrade this package automatically on launch or via wintui upgrade --auto.",
			"hold": "Keep this package out of normal upgrade actions.",
		},
	},
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
