# WinTUI v2.7.0 Release Notes

WinTUI v2.7.0 rebuilds the Cleanup tab around a registry-driven target
model, adds first-class export / import for moving package lists between
machines, and fixes a long-standing toast-body cosmetic bug.

## Cleanup tab — registry-driven multi-target cleanup

The previous Cleanup tab was a single scanner over `%TEMP%` with a
global success/failure summary. v2.7.0 replaces it with a workspace
that mirrors the Packages tab: grouped bordered panels on the left,
per-target detail pane on the right, persistent per-row toggle state.

### Targets

Seventeen registered targets across four groups, each with a stable ID
that's part of the on-disk contract (settings persist user opt-ins by
ID). Detect-if-present targets only appear when their resolved path
exists on the machine.

| Group | Default | What's in it |
|---|---|---|
| Core Temp | checked | `%TEMP%`, `%WINDIR%\Temp`, user crash dumps, WER queue, system minidumps |
| Caches | checked | DirectX shader cache, Explorer thumbnail cache (`thumbcache_*.db` glob) |
| Developer | unchecked, opt-in | npm cache, Go build cache, pip wheel cache, Yarn cache |
| GPU | unchecked, opt-in | NVIDIA DX/GL caches, AMD DX/GL caches, Intel shader cache |

`%WINDIR%\Temp` and system minidumps are flagged `[suggests admin]` —
they require elevation to clean cleanly. Cleanup routes them through
the existing elevated helper-pipe rather than spawning a fresh
elevation channel, so UAC fires once and Defender doesn't see a new
static surface.

### Auto-scan policy

A new tri-state setting `cleanup_auto_scan` controls what gets sized
when you open the tab:

- `safe` (default) — scan Core Temp, Caches, and any present GPU
  vendor caches. Developer caches stay quiet until you check them.
- `all` — scan every present target on tab open.
- `off` — never auto-scan. Press `s` to size the focused row, `r` to
  rescan everything visible.

Per-target scans run as background goroutines. You can cursor and
toggle freely while sizes land. Tabbing away cancels in-flight scans;
already-completed results persist for the session.

### Confirm gate + admin framing

Pressing `enter` on the cleanup tab opens a centered confirm modal
listing only the **checked targets that have a non-zero size**. Slow
or unwanted scans don't block the action. When admin targets are in
the queue, the modal pre-emptively explains the UAC prompt and
includes the AV-FP disclosure: "If your antivirus flags this, that's
a known false positive on bulk file delete — WinTUI only touches the
listed paths."

Cleanup state machine: `ready → confirming → executing → done` with
an explicit `partialFailure` substate when any target reported
failures. The `done` view sorts results by freed-bytes desc so the
most impactful target appears first, and a generation token guards
against late-arriving stale scan results from cancelled goroutines.

### Settings persistence

Default-checked targets are always on by design and never persisted —
that would be on-disk noise. Only positive opt-ins for non-default
rows go to `settings.json` under a new `cleanup_enabled_targets`
list. Toggling a Developer or GPU row writes the change immediately;
the next session opens with your selections intact.

### Toast on completion

When toast notifications are enabled, a single Windows toast fires on
cleanup finish: "Freed X across N target(s)" or, on partial failure,
"Freed X; N targets had locked files."

## Export / Import — portable package lists

Two new CLI commands move installed package lists across machines:

```powershell
wintui export [--output PATH] [--with-versions]
wintui import <path> [--dry-run] [--all] [--json]
```

`wintui export` writes a versioned JSON envelope (top-level object with
`version`, `generator`, `created`, `host`, `packages` fields). Versions
are excluded by default — restoring exact versions on a fresh machine
is a footgun. Stdout is reserved for clean JSON when no `--output` is
given, so `wintui export | jq` works as documented; warnings about
unrecoverable truncated IDs go to stderr.

The new envelope is the default writer; `wintui import` accepts both
the envelope and the legacy flat-array form for backward compatibility
with anything users hand-rolled before this shipped.

### Three-category install plan

`--dry-run` prints the install plan without touching anything:

- **Will install** — packages not currently on the machine, canonical
  IDs, no name collisions
- **Already installed** — same ID match against the local list
- **Review needed** — entries whose normalized name matches a
  *different* installed package (e.g. exporting `Git.Git` when
  `Microsoft.Git` is already installed). Default-skipped; pass
  `--all` to install them anyway
- **Non-canonical** — MSIX hashes, ARP entries, and GUIDs that
  `winget --id --exact` can't restore

The `Review needed` category is the locked design's safety net for the
common cross-machine case where the same software exists under two
different package IDs. Preview with `--dry-run` and decide.

### TUI integration on the Packages tab

- `e` — exports the installed list to a dated, time-stamped file on
  your Desktop (`wintui-packages-2026-05-05_143022.json`). Two presses
  produce distinct files. Fires a toast and shows a transient status
  line on success.
- `Shift+I` — opens the import overlay. It scans Desktop, home, and
  cwd for `*.json` files, lets you toggle which entries to install
  (collisions stay unchecked by default), then runs the install
  through the existing batch flow. The package list reloads when the
  overlay closes.

The import overlay was a half-built feature in earlier versions —
v2.7.0 finishes the wiring and brings the visual style up to the
current theme (titled panels, mint-cyan for intent, pink for
structure, centered confirm modal).

### Truncated-ID safety

Long package IDs are sometimes truncated by `winget list` when the
output column hits its width limit. v2.7.0's export resolves any
truncated canonical IDs upfront via `resolveTruncatedPackage`, so
they don't round-trip into the export and fail at import time with
`winget --id --exact` returning `0x8a150014`. Non-canonical IDs
(MSIX/GUID/`Foo_hash`) that report truncated are kept verbatim — the
import side classifies them as `Non-canonical (can't restore)`
rather than dropping them silently from the inventory.

## Bug fixes

- **Toast verb on uninstall batches said "upgraded"** —
  `notifyBatchFinish` hardcoded "upgraded" regardless of the batch's
  operation. The toast now picks `installed` / `uninstalled` /
  `upgraded` from the batch action and falls back to `completed`
  on heterogeneous batches.

## Settings

Two new keys in `settings.json`, both with sensible omit-empty
defaults so existing config files stay diff-clean:

| Key | Type | Default | Behavior |
|---|---|---|---|
| `cleanup_auto_scan` | `safe` / `all` / `off` | `safe` (omitted) | Controls what the Cleanup tab scans on open |
| `cleanup_enabled_targets` | string array | `[]` (omitted) | Persisted opt-in list of advanced cleanup target IDs |

Both are exposed in the Settings tab UI as well as editable directly
in `settings.json`.

## Notes

- **No breaking changes.** Existing `settings.json` files, exported
  package lists in the legacy flat-array format, and prior tab key
  bindings all continue to work.
- **No new Win32 surface.** Source-level import diff vs v2.6.1
  confirms zero new `syscall` / `windows` / `golang.org/x/sys/windows`
  imports across the entire dev branch. The cleanup tab and
  helper-pipe `cleanup_delete` action both ride on top of the v2.6.1
  elevation channel rather than opening a new one. Empirical
  validation on a real Windows host confirmed UAC fires once and
  Defender stays quiet on the new bulk-delete-via-elevated-helper
  pattern.
- **WinTUI itself is included in `wintui export`.** On import it gets
  marked already-installed automatically — by definition you must
  have WinTUI installed to run import in the first place.
