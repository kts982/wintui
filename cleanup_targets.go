package main

import (
	"errors"
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

// cleanupSelfUpdateHandoffMinAge keeps a self-update kicked off mid-cleanup
// safe: a handoff script generated in the last 24 hours might still be the
// one running. Anything older has either finished or been abandoned.
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
		// Both targets glob inside %LOCALAPPDATA%\wintui\self-update\ — the
		// only WinTUI-owned location that accumulates files over time.
		// settings.json, cache.json, the toast Start-Menu shortcut and its
		// .aumid marker are deliberately out of scope: they are user state
		// or durable infra, not scratch.
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
			label: "WinTUI self-update handoff scripts",
			description: "Leftover PowerShell handoff scripts under %LOCALAPPDATA%\\wintui\\self-update. " +
				"Stale scripts older than 24 hours; in-flight handoffs are kept.",
			group:  cleanupGroupWinTUI,
			pathFn: selfUpdateStateDir,
			mode:   cleanupModeGlob,
			globs:  []string{selfUpdateScriptPrefix + "*.ps1"},
			minAge: cleanupSelfUpdateHandoffMinAge,
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

// cleanupValidateRoot rejects resolved roots we must never purge, regardless
// of where they came from. Both the in-process engine and the elevated helper
// call this before deleting; the helper does not trust the TUI's word for it.
func cleanupValidateRoot(p string) error {
	if p == "" {
		return errCleanupGuardedPath
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	abs = filepath.Clean(abs)

	vol := filepath.VolumeName(abs)
	if vol != "" {
		root := vol + string(filepath.Separator)
		if abs == vol || abs == root {
			return errCleanupGuardedPath
		}
	}

	guarded := []string{
		os.Getenv("WINDIR"),
		os.Getenv("USERPROFILE"),
		os.Getenv("LOCALAPPDATA"),
		os.Getenv("APPDATA"),
		os.Getenv("PROGRAMDATA"),
		os.Getenv("PROGRAMFILES"),
		os.Getenv("PROGRAMFILES(X86)"),
	}
	for _, g := range guarded {
		if g == "" {
			continue
		}
		gAbs, err := filepath.Abs(g)
		if err != nil {
			continue
		}
		if strings.EqualFold(filepath.Clean(gAbs), abs) {
			return errCleanupGuardedPath
		}
	}
	return nil
}
