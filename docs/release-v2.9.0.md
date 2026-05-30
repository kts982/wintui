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

## Verification

VirusTotal scans of the published artifacts for v2.9.0 (run 2026-05-31):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.9.0_windows_amd64.exe` (7.3 MB) | `a4cf712d45cc…` | 3/71 | [VT report](https://www.virustotal.com/gui/file/a4cf712d45ccc5c61e4cf64d8ad02f083228cb69585f06412a1afb1c0c1c8715) |
| `wintui_2.9.0_windows_amd64.zip` (2.7 MB) | `53317c1689f9…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/53317c1689f9ac8276c32ee6ddafdcd771898b63e4918adf9759e69872d8be44) |
| `wintui_2.9.0_windows_arm64.exe` (6.8 MB) | `fff900c7d343…` | 1/69 | [VT report](https://www.virustotal.com/gui/file/fff900c7d343d3a834766f5f87b999642a8641f72b55320057b0f1a9eaa4c218) |
| `wintui_2.9.0_windows_arm64.zip` (2.4 MB) | `2dc0a2ce06b9…` | 0/67 | [VT report](https://www.virustotal.com/gui/file/2dc0a2ce06b92b16f5e0205e68e078a24356e11992ae3f0b82441a85bbdc1406) |

Detections at scan time were low-signal noise on an unsigned Go binary. Bkav,
Trapmine, and McAfeeD are single-vendor ML/reputation engines that self-resolve
as the SHA256 gains reputation. The Microsoft `!ml` hit on `amd64.exe` is a
VirusTotal-engine artifact — VirusTotal discloses that its Microsoft engine
"may differ from the commercial off-the-shelf product," and the detection name
was itself unstable across rescans (`Wacapew.C!ml` → `Wacatac.C!ml`, both
generic machine-learning buckets). Live Windows Defender (the commercial
product, current signatures) scans the published `amd64.exe` **clean**.

Full SHA256 hashes:

- `wintui_2.9.0_windows_amd64.exe`: `a4cf712d45ccc5c61e4cf64d8ad02f083228cb69585f06412a1afb1c0c1c8715`
- `wintui_2.9.0_windows_amd64.zip`: `53317c1689f9ac8276c32ee6ddafdcd771898b63e4918adf9759e69872d8be44`
- `wintui_2.9.0_windows_arm64.exe`: `fff900c7d343d3a834766f5f87b999642a8641f72b55320057b0f1a9eaa4c218`
- `wintui_2.9.0_windows_arm64.zip`: `2dc0a2ce06b92b16f5e0205e68e078a24356e11992ae3f0b82441a85bbdc1406`
