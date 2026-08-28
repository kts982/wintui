package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cleanupGroup categorizes targets in the cleanup tab. The UI groups rows by
// this value; the engine uses it to decide which targets to auto-scan.
type cleanupGroup string

const (
	cleanupGroupCoreTemp  cleanupGroup = "core_temp"
	cleanupGroupCaches    cleanupGroup = "caches"
	cleanupGroupDeveloper cleanupGroup = "developer"
	cleanupGroupGPU       cleanupGroup = "gpu"
	cleanupGroupWinTUI    cleanupGroup = "wintui"
)

// cleanupSelfUpdateHandoffMinAge protects legacy pre-v2.11.2 script-file
// handoffs during the migration window. Anything older than 24 hours has
// either finished or been abandoned.
const cleanupSelfUpdateHandoffMinAge = 24 * time.Hour

// cleanupMode controls how the engine processes a target's resolved path.
type cleanupMode int

const (
	// cleanupModePurgeContents walks the resolved root and deletes its
	// children, never the root itself. Honors minAge per entry.
	cleanupModePurgeContents cleanupMode = iota
	// cleanupModeGlob deletes only entries directly inside the resolved root
	// whose name matches one of `globs`. Used for surgical scrubbing
	// (e.g. thumbcache_*.db) where the parent dir holds files we must keep.
	cleanupModeGlob
)

// cleanupTargetDef is a static, ID-addressable description of a cleanable
// location. Resolution happens at scan time; the engine never walks anything
// the registry didn't author. Settings persist user toggles by `id`, so IDs
// are part of the on-disk contract and must not change once shipped.
type cleanupTargetDef struct {
	id              string
	label           string
	description     string
	group           cleanupGroup
	pathFn          func() string
	mode            cleanupMode
	globs           []string      // only consulted when mode == cleanupModeGlob
	minAge          time.Duration // entries newer than this are kept; 0 = no age filter
	requiresAdmin   bool
	defaultChecked  bool
	detectIfPresent bool   // when true, hide row when pathFn resolves to a missing dir
	warning         string // optional caveat shown in the detail pane
}

// cleanupDefaultMinAge is the conservative cutoff we apply to anything that
// looks like a scratch directory (temp, crash dumps). New files are likely
// still in use; week-old ones almost never are.
const cleanupDefaultMinAge = 7 * 24 * time.Hour

// cleanupTargetRegistry returns the immutable list of target definitions for
// the current build. Order is the user-visible order within each group.
func cleanupTargetRegistry() []cleanupTargetDef {
	return []cleanupTargetDef{
		// ── Core Temp ───────────────────────────────────────────────────
		{
			id:    "user_temp",
			label: "User temp directory",
			description: "Per-user %TEMP%. Browsers, installers, and tools drop scratch " +
				"files here; Windows does not auto-purge them.",
			group:          cleanupGroupCoreTemp,
			pathFn:         func() string { return os.TempDir() },
			mode:           cleanupModePurgeContents,
			minAge:         cleanupDefaultMinAge,
			defaultChecked: true,
		},
		{
			id:    "windows_temp",
			label: "Windows temp directory",
			description: "System-wide %WINDIR%\\Temp used by services and installers " +
				"running as SYSTEM. Cleaning requires admin.",
			group:          cleanupGroupCoreTemp,
			pathFn:         func() string { return cleanupResolveEnvDir("WINDIR", "Temp") },
			mode:           cleanupModePurgeContents,
			minAge:         cleanupDefaultMinAge,
			requiresAdmin:  true,
			defaultChecked: true,
		},
		{
			id:    "crash_dumps",
			label: "User crash dumps",
			description: "Per-user crash dumps written by Windows Error Reporting. " +
				"Useful only while actively debugging a recent crash.",
			group:          cleanupGroupCoreTemp,
			pathFn:         func() string { return cleanupResolveLocalAppData("CrashDumps") },
			mode:           cleanupModePurgeContents,
			minAge:         cleanupDefaultMinAge,
			defaultChecked: true,
		},
		{
			id:    "wer_reports",
			label: "Windows Error Reporting queue",
			description: "Queued and archived WER reports. Microsoft already received " +
				"the ones it cared about.",
			group:          cleanupGroupCoreTemp,
			pathFn:         func() string { return cleanupResolveLocalAppData("Microsoft", "Windows", "WER") },
			mode:           cleanupModePurgeContents,
			minAge:         cleanupDefaultMinAge,
			defaultChecked: true,
		},
		{
			id:    "minidump",
			label: "System minidumps",
			description: "Kernel-mode crash dumps in %WINDIR%\\Minidump. Cleaning " +
				"requires admin.",
			group:          cleanupGroupCoreTemp,
			pathFn:         func() string { return cleanupResolveEnvDir("WINDIR", "Minidump") },
			mode:           cleanupModePurgeContents,
			minAge:         cleanupDefaultMinAge,
			requiresAdmin:  true,
			defaultChecked: true,
		},

		// ── Caches ──────────────────────────────────────────────────────
		{
			id:    "d3dscache",
			label: "DirectX shader cache",
			description: "Compiled shader cache. Windows rebuilds it on demand; " +
				"clearing rarely costs more than a brief stutter.",
			group:          cleanupGroupCaches,
			pathFn:         func() string { return cleanupResolveLocalAppData("D3DSCache") },
			mode:           cleanupModePurgeContents,
			defaultChecked: true,
		},
		{
			id:             "thumbcache",
			label:          "Explorer thumbnail cache",
			description:    "Thumbnail and icon caches. Explorer rebuilds them on next view.",
			group:          cleanupGroupCaches,
			pathFn:         func() string { return cleanupResolveLocalAppData("Microsoft", "Windows", "Explorer") },
			mode:           cleanupModeGlob,
			globs:          []string{"thumbcache_*.db", "iconcache_*.db"},
			defaultChecked: true,
		},

		// ── Developer caches (detect-if-present, default unchecked) ─────
		{
			id:              "npm_cache",
			label:           "npm cache",
			description:     "`npm cache verify` recreates this on demand.",
			group:           cleanupGroupDeveloper,
			pathFn:          func() string { return cleanupResolveLocalAppData("npm-cache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
		},
		{
			id:    "go_build",
			label: "Go build cache",
			description: "Restored automatically on next `go build`. Long-running " +
				"test suites may slow until it warms back up.",
			group:           cleanupGroupDeveloper,
			pathFn:          func() string { return cleanupResolveLocalAppData("go-build") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
		},
		{
			id:              "pip_cache",
			label:           "pip wheel cache",
			description:     "Cleared cache means the next install hits PyPI.",
			group:           cleanupGroupDeveloper,
			pathFn:          func() string { return cleanupResolveLocalAppData("pip", "Cache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
		},
		{
			id:              "yarn_cache",
			label:           "Yarn cache",
			description:     "Yarn 1.x global package cache.",
			group:           cleanupGroupDeveloper,
			pathFn:          func() string { return cleanupResolveLocalAppData("Yarn", "Cache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
		},
		// winget's installer cache lives at %TEMP%\WinGet\cache (verified
		// against winget v1.28 on Windows 11). That path is already covered
		// by the user_temp target, so a dedicated entry here would
		// double-count. The %LOCALAPPDATA%\Microsoft\WinGet\Packages folder
		// holds installed portable packages — not a cache, never delete.

		// ── GPU vendor caches (detect-if-present, default unchecked) ────
		{
			id:              "nvidia_dx_cache",
			label:           "NVIDIA DX shader cache",
			description:     "Driver shader cache for DirectX titles. Rebuilds on next launch.",
			group:           cleanupGroupGPU,
			pathFn:          func() string { return cleanupResolveLocalAppData("NVIDIA", "DXCache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
			warning:         "Affected games may stutter once while the cache rebuilds.",
		},
		{
			id:              "nvidia_gl_cache",
			label:           "NVIDIA GL/Vulkan shader cache",
			description:     "Driver shader cache for OpenGL/Vulkan titles. Rebuilds on next launch.",
			group:           cleanupGroupGPU,
			pathFn:          func() string { return cleanupResolveLocalAppData("NVIDIA", "GLCache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
			warning:         "Affected games may stutter once while the cache rebuilds.",
		},
		{
			id:              "amd_dx_cache",
			label:           "AMD DX shader cache",
			description:     "Radeon driver DirectX shader cache.",
			group:           cleanupGroupGPU,
			pathFn:          func() string { return cleanupResolveLocalAppData("AMD", "DxCache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
			warning:         "Affected games may stutter once while the cache rebuilds.",
		},
		{
			id:              "amd_gl_cache",
			label:           "AMD GL/Vulkan shader cache",
			description:     "Radeon driver OpenGL/Vulkan shader cache.",
			group:           cleanupGroupGPU,
			pathFn:          func() string { return cleanupResolveLocalAppData("AMD", "GLCache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
			warning:         "Affected games may stutter once while the cache rebuilds.",
		},
		{
			id:              "intel_shader_cache",
			label:           "Intel shader cache",
			description:     "Intel graphics driver shader cache.",
			group:           cleanupGroupGPU,
			pathFn:          func() string { return cleanupResolveLocalAppData("Intel", "ShaderCache") },
			mode:            cleanupModePurgeContents,
			detectIfPresent: true,
			warning:         "Affected games may stutter once while the cache rebuilds.",
		},

		// ── WinTUI's own scratch (default unchecked) ────────────────────
		// The self-update targets glob inside %LOCALAPPDATA%\wintui\self-update\
		// — the only WinTUI-owned location that accumulates files over time.
		// settings.json, cache.json, the toast Start-Menu shortcut and its
		// .aumid marker are deliberately out of scope: they are user state
		// or durable infra, not scratch. history.json is also user state, but
		// it gets an entry anyway as a deliberate privacy reset ("clear what
		// WinTUI knows about my installs") — opt-in, with a warning.
		{
			id:    "wintui_self_update_log",
			label: "WinTUI self-update log",
			description: "Append-only log of WinTUI self-update probes and handoffs. " +
				"Grows by a few lines on every launch; safe to truncate.",
			group:  cleanupGroupWinTUI,
			pathFn: selfUpdateStateDir,
			mode:   cleanupModeGlob,
			globs:  []string{selfUpdateLogName},
		},
		{
			id:    "wintui_self_update_handoffs",
			label: "Legacy WinTUI handoff scripts",
			description: "Leftover PowerShell handoff scripts created by WinTUI versions before v2.11.2. " +
				"Stale scripts older than 24 hours; retained temporarily for migration cleanup.",
			group:  cleanupGroupWinTUI,
			pathFn: selfUpdateStateDir,
			mode:   cleanupModeGlob,
			globs:  []string{selfUpdateScriptPrefix + "*.ps1"},
			minAge: cleanupSelfUpdateHandoffMinAge,
		},
		{
			id:    "wintui_history",
			label: "WinTUI action history",
			description: "WinTUI's record of the install/upgrade/uninstall operations it ran — " +
				"what the History tab and 'wintui history' show. Bounded at 1000 batches, " +
				"so this is a privacy reset, not a disk-space win.",
			group:  cleanupGroupWinTUI,
			pathFn: wintuiConfigDir,
			mode:   cleanupModeGlob,
			globs:  []string{historyFileName},
			warning: "Deletes your audit trail of WinTUI-run installs. The History tab and " +
				"'wintui history' will start empty; cleared records are not recoverable.",
		},
	}
}

// cleanupTargetByID returns the registry entry with the given ID. Used by the
// engine, the elevated helper, and any settings-persisted toggle lookup.
func cleanupTargetByID(id string) (cleanupTargetDef, bool) {
	for _, t := range cleanupTargetRegistry() {
		if t.id == id {
			return t, true
		}
	}
	return cleanupTargetDef{}, false
}

// cleanupResolveLocalAppData joins parts under %LOCALAPPDATA%, returning ""
// if the env var is missing (the caller treats that as "skip").
func cleanupResolveLocalAppData(parts ...string) string {
	return cleanupResolveEnvDir("LOCALAPPDATA", parts...)
}

// cleanupResolveEnvDir joins parts under the named env var, returning "" if
// the var is unset or empty.
func cleanupResolveEnvDir(envVar string, parts ...string) string {
	base := os.Getenv(envVar)
	if base == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// errCleanupGuardedPath is returned by cleanupValidateRoot when a resolved
// path matches a critical filesystem location that must never be purged.
var errCleanupGuardedPath = errors.New("path is guarded against bulk delete")

// cleanupValidateRoot accepts only roots inside the locations the cleanup
// registry actually resolves to — an allowlist, not a denylist, so a bug
// elsewhere can't route an arbitrary directory through bulk delete. Both the
// in-process engine and the elevated helper call this before deleting; the
// helper does not trust the TUI's word for it.
//
// Allowed: %TEMP% itself or below (the user_temp target IS the temp root —
// the engine only deletes entries under it, never the root), strict
// descendants of %LOCALAPPDATA% and %WINDIR% (the bases themselves stay
// guarded), and %APPDATA%\wintui itself or below (the wintui_history target
// IS that root; the rest of %APPDATA% stays guarded — other apps' roaming
// state must never be routable through bulk delete).
func cleanupValidateRoot(p string) error {
	if p == "" {
		return errCleanupGuardedPath
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	abs = filepath.Clean(abs)

	if tmp := os.TempDir(); tmp != "" {
		if tmpAbs, err := filepath.Abs(tmp); err == nil {
			tmpAbs = filepath.Clean(tmpAbs)
			if strings.EqualFold(tmpAbs, abs) || pathIsDescendant(abs, tmpAbs) {
				return nil
			}
		}
	}
	if cfg := wintuiConfigDir(); cfg != "" {
		if cfgAbs, err := filepath.Abs(cfg); err == nil {
			cfgAbs = filepath.Clean(cfgAbs)
			if strings.EqualFold(cfgAbs, abs) || pathIsDescendant(abs, cfgAbs) {
				return nil
			}
		}
	}
	for _, base := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("WINDIR")} {
		if base == "" {
			continue
		}
		baseAbs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if pathIsDescendant(abs, baseAbs) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s is outside the allowed cleanup locations", errCleanupGuardedPath, abs)
}
