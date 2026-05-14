# WinTUI v2.7.3 Release Notes

A small polish release: Cleanup-tab readability on short terminals,
a new WinTUI cleanup group for self-update scratch, a third theme
tier so meaningful secondary data reads cleanly, and the routine
toolchain bump.

No bugfixes, no breaking changes, no new Win32 surface.

## Cleanup tab now fits short terminals

`renderGroupPanels` rendered every group panel at its natural
height with no viewport, so any terminal shorter than the total
group stack clipped the bottom panels off-screen — visible
immediately after the new WinTUI group landed as a fifth panel.

The renderer now lays out every group into a single line buffer,
finds the cursor's absolute line position, and slides a window
over the buffer so the focused row is always visible. Short
terminals show one continuous slice of the grouped list, which
reads cleaner for a destructive action than per-panel internal
scrolling — there's no ambiguity about what "select group"
affects when some rows are hidden inside a panel.

`selectedSummary` also now counts every checked row instead of
only rows with non-zero scanned size. Count tracks the user's
action (checkboxes), bytes track the consequence; they're
allowed to disagree — "5 selected, 0 B" tells the user
immediately that picked rows scanned empty, where the previous
"0 selected" after 5 toggles looked broken. The delete path
itself is unchanged — it still only touches non-empty targets.

## New WinTUI cleanup group

Adds a fifth Cleanup group ("WinTUI") with two glob-mode targets
rooted at `%LOCALAPPDATA%\wintui\self-update\`:

- `wintui-self-update.log` — opened `O_CREATE|O_WRONLY|O_APPEND`
  in `appendSelfUpdateLogf` and grows by a few lines on every
  startup probe + handoff invocation. The only WinTUI-owned
  file that grows monotonically.
- `handoff-*.ps1` with a 24h `minAge` floor — exposes a manual
  purge alongside the existing best-effort stale-script sweep.
  The 24h floor keeps any in-flight self-update spawned during
  cleanup safe.

Both targets default-unchecked. No new admin surface — both are
per-user; the elevated-helper pipe protocol is untouched.
Settings persistence rides on the existing
`cleanup_enabled_targets` list.

Deliberately out of scope: `settings.json` / `cache.json` (user
state), the toast Start Menu shortcut + `.aumid` marker (durable
infra), the currently-running self-update script (the 24h
`minAge` covers that case).

## Subtle gray tier across screens

The palette had two tiers — bright (252) for primary text, dim
(240) for chrome — and dim was carrying six different semantic
roles, so version columns, free GB, package IDs, and other
data users actually scan were rendering as background.

Adds a third tier: subtle (247), used for "meaningful but not
headline" data. Per-screen substitutions:

- Settings: active choice chip switched from accent (pink) to
  state (cyan), keeping pink reserved for structural focus.
  Inactive alternatives use subtle.
- Health: right-column detail (version, free GB, last scan)
  switched from dim → subtle.
- Workspace: version columns (upgrade transition, installed
  version, queue/search version) switched from dim → subtle.
- Summary panel: package ID under the detail title switched
  from dim → subtle (it's a copy-paste identifier, not chrome).

Description text, `[winget]` / `[suggests admin]` / `[default on]`
chips, and the Cleanup tab itself were left untouched — they
were already using the two-axis system correctly.

## Toolchain + dependency refresh

- Go toolchain 1.26.1 → 1.26.3 (11 stdlib security fixes across
  `crypto/x509`, `crypto/tls`, `archive/tar`, `html/template`,
  `os`, compiler).
- `charm.land/bubbletea/v2` 2.0.2 → 2.0.6 (wide-char CPU-spin
  fix, extended keyboard reports, width-calc fix).
- `charm.land/lipgloss/v2` 2.0.2 → 2.0.3 (avoids bg-color query
  hang on non-cooperative terminals).
- `golang.org/x/sys` 0.42.0 → 0.44.0 (cleaner Windows surface;
  drops unused `Syscall9/12/15` + `windows.itoa`).
- `github.com/mattn/go-runewidth` 0.0.21 → 0.0.23 (Unicode 17.0.0
  data; required by bubbletea 2.0.6).

Static-surface check vs the pre-bump tree (`go tool nm | grep
syscall/windows/__imp_`): zero new symbols, four removed.
AV-FP risk unchanged-to-lower.
