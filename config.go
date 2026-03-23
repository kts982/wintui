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

	// Install mode: "silent" (default), "interactive", or "" (winget default)
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
}

// DefaultSettings returns settings with sensible defaults.
func DefaultSettings() Settings {
	return Settings{
		Scope:       "",
		InstallMode: "silent",
		Source:      "winget",
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
func (s Settings) BuildUninstallArgs() []string {
	var args []string
	if s.PurgeOnUninstall {
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
	key     string
	label   string
	desc    string
	stype   settingType
	choices []string // for settingChoice
}

var settingDefs = []settingDef{
	{
		key:   "scope",
		label: "Install Scope",
		desc:  "Where packages are installed",
		stype: settingChoice,
		choices: []string{"", "user", "machine"},
	},
	{
		key:   "install_mode",
		label: "Install Mode",
		desc:  "How installers run",
		stype: settingChoice,
		choices: []string{"silent", "interactive", ""},
	},
	{
		key:   "architecture",
		label: "Architecture",
		desc:  "Preferred CPU architecture",
		stype: settingChoice,
		choices: []string{"", "x64", "x86", "arm64"},
	},
	{
		key:   "source",
		label: "Default Source",
		desc:  "Package repository to use",
		stype: settingChoice,
		choices: []string{"winget", "msstore", ""},
	},
	{
		key:   "force",
		label: "Force",
		desc:  "Skip non-security related issues",
		stype: settingToggle,
	},
	{
		key:   "allow_reboot",
		label: "Allow Reboot",
		desc:  "Permit system reboots during install",
		stype: settingToggle,
	},
	{
		key:   "skip_dependencies",
		label: "Skip Dependencies",
		desc:  "Don't process package dependencies",
		stype: settingToggle,
	},
	{
		key:   "purge_on_uninstall",
		label: "Purge on Uninstall",
		desc:  "Delete all package files when uninstalling",
		stype: settingToggle,
	},
	{
		key:   "include_unknown",
		label: "Include Unknown Versions",
		desc:  "Show packages with unknown versions in upgrade list",
		stype: settingToggle,
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
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
