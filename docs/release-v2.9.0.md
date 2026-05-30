# WinTUI v2.9.0 Release Notes

**CLI Polish.** The headless CLI becomes a first-class, on-brand surface:
styled help and errors, shell completions, a themed release-notes viewer,
CLI theme switching, self-upgrade, and package filtering.

No breaking changes — `settings.json` is unchanged, and `cache.json` stays
backward-compatible (it gains optional `id_truncated` / `name_truncated`
metadata so truncated package IDs survive the cache round-trip).

## Styled CLI (fang)

The CLI is now wrapped with [fang](https://github.com/charmbracelet/fang), so
`wintui --help`, usage, errors, and `--version` render to match your active
v2.8.0 color theme. The bubbletea TUI is unaffected (fang installs no signal
handler), and `--json` output for every subcommand stays byte-for-byte plain.

**Shell completions** ship via `wintui completion <bash|zsh|fish|powershell>`.
Once loaded, `wintui upgrade --id <TAB>` and `wintui show <TAB>` complete
package IDs from the local cache (no winget call). See
[docs/cli.md → Enabling shell completions](https://github.com/kts982/wintui/blob/main/docs/cli.md#enabling-shell-completions).

## `wintui notes <id>` and `check --notes`

`wintui notes <id>` fetches a package's latest-version release notes via
`winget show` and renders the markdown, themed to your palette. Many winget
manifests ship only a release-notes URL (or nothing) — in that case `notes`
shows the URL or reports none, and a genuine unknown id gives a clear
not-found error (ids are matched case-insensitively).

`wintui check --notes` renders the notes for **every pending update** inline —
the built-in form of `wintui check --json | … | wintui notes`, so you can
review what you're about to install before upgrading.

## `wintui theme` and `wintui upgrade --self`

- **`wintui theme [name] [--list]`** — show, list, or set the color theme from
  the CLI. The active theme also appears as a row in `wintui doctor`.
- **`wintui upgrade --self`** — upgrade WinTUI itself via the startup
  self-update handoff. `--all` / `--auto` / `--id` deliberately skip the
  running binary, so CLI-only users needed a way to keep WinTUI current
  without launching the TUI. `--self` ignores the Auto Update setting (you
  asked for it explicitly) and requires the winget-installed build.

## `wintui list [query]`

`wintui list` now takes an optional query that filters installed packages by
**name or id** (case-insensitive substring, like `winget list`):
`wintui list firefox` answers "is it installed?" and exits 1 when nothing
matches — usable as a predicate. A bare `wintui list` still lists everything.

## Themed CLI output and scroll-wheel

- Release notes render with accent-colored headers and bullets, word-wrapped
  and themed to your palette. Upgrade `✓`/`✗` markers, per-package headers, and
  check/list summary lines are accent-colored too. **All color is gated on an
  interactive terminal and respects `NO_COLOR`** — piped output and `--json`
  stay plain, so scripts are unaffected.
- **Scroll-wheel** support in the package list, detail panel, and cleanup tab.

The release-notes renderer is a small in-house markdown formatter, so the
binary stays lean (close to v2.8.0) with no new Win32 surface.
