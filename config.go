package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings holds user-configurable winget options.
type Settings struct {
	// Install/Upgrade scope: "user", "machine", or "" (winget default)
	Scope string `json:"scope"`

	// Install mode: "", "silent", or "interactive"
	InstallMode string `json:"install_mode"`

	// Force: skip non-security related issues
	Force bool `json:"force"`

	// Architecture preference: "x64", "x86", "arm64", or "" (auto)
	Architecture string `json:"architecture"`

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

// BuildInstallArgs returns extra winget flags based on current settings.
// Used for install, upgrade actions.
func (s Settings) BuildInstallArgs() []string {
	var args []string
	if s.Scope != "" {
		args = append(args, "--scope", s.Scope)
	}
	switch s.InstallMode {
	case "silent":
		args = append(args, "--silent")
	case "interactive":
		args = append(args, "--interactive")
	}
	if s.Architecture != "" {
		args = append(args, "--architecture", s.Architecture)
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
	case "silent":
		args = append(args, "--silent")
	case "interactive":
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
		return s.Scope
	case "install_mode":
		return s.InstallMode
	case "architecture":
		return s.Architecture
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
		s.Scope = val
	case "install_mode":
		s.InstallMode = val
	case "architecture":
		s.Architecture = val
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

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
