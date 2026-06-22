# WinTUI v2.10.0 Release Notes

**Action history.** WinTUI now keeps a log of the install / upgrade / uninstall
operations it runs — including headless `wintui upgrade` and `wintui import`, so
scheduled and CLI-only runs are no longer invisible — and exposes it through a
new `wintui history` command. The release also ships `wintui fix --portable`, a
targeted fix for a winget footgun that drops portable CLIs (like Claude Code)
from your PATH on upgrade. Pure-Go throughout: no new Win32 imports and no new
dependencies, and `settings.json` / `cache.json` load unchanged.

## Action history (#4)

- **Operation log.** WinTUI records every batch it runs to a versioned
  `%APPDATA%\wintui\history.json` — TUI batches, `wintui upgrade --all/--auto/--id/--self`,
  and `wintui import` (both the CLI and the TUI overlay). Each record carries the
  trigger, action, per-package from→to versions, status (ok / error / pending /
  skipped), and an error message when one failed. The motivation: a scheduled
  `wintui upgrade --all` used to leave **zero trace**.
- **`wintui history`.** Lists recent batches newest-first; `wintui history <id>`
  shows a single package's timeline (a query over the log). Flags: `--limit`,
  `--since <dur>`, `--failed-only`, `--json`. Exit codes mirror `wintui list`:
  an unfiltered empty history exits `0` (a fresh install is normal), but a
  selector/filter that matches nothing exits `1`, so `wintui history <id>`
  doubles as a "did WinTUI ever touch this?" predicate (in `--json` mode too).
  Tab-completion of package ids reads the log directly, so it works even if
  you've only ever used the CLI.
- **WinTUI-originated only.** winget exposes no event feed, so history captures
  only what WinTUI itself dispatches — upgrades you run from plain `winget`,
  Microsoft Store auto-updates, and other tools are not recorded, and the log
  will not match `winget list` deltas. The newest 1000 batches are kept;
  self-upgrades are logged as `pending` (the handoff completes after restart).
  The file is independent of the cache and is preserved across WinTUI
  self-upgrades.

## Portable / user-scope PATH safety

- **`wintui fix --portable`.** winget mis-scopes **portable** packages on
  upgrade and strips their user PATH entry, so a CLI like `claude` suddenly
  "disappears" ([winget-cli #4044 / #5099](https://github.com/microsoft/winget-cli/issues/4044)).
  `fix --portable` pins each portable package with a per-package
  `scope: user` + `elevate: false` override, so WinTUI always passes
  `--scope user` for it and the PATH entry survives. It's idempotent, merges with
  any existing per-package rules, supports `--dry-run` / `--json`, and detects
  portability from a single listing of `%LOCALAPPDATA%\Microsoft\WinGet\Packages`
  (no `winget show` calls).
- **Upgrade advisory.** `wintui upgrade --all/--auto/--id` prints a one-line
  nudge toward `fix --portable` when the upgrade set contains a portable package
  that isn't pinned yet (elevation-agnostic — the winget bug bites non-elevated
  runs too).

## Smaller additions

- **doctor `winget MCP` row.** `wintui doctor` (and the TUI Health tab) gain a
  neutral INFO row reporting whether winget's MCP server binary is present —
  a pure file-stat, no winget call.
- **CLI upgrade heartbeat.** A slow installer that goes quiet (e.g. one stopping
  a service or replacing in-use files for minutes) now prints a periodic
  `… still working (Nm elapsed)` line so it isn't mistaken for a hang.

## Compatibility

- No breaking changes. `settings.json` and `cache.json` load unchanged;
  `history.json` is a new, versioned file (older binaries simply ignore it, and
  a newer-version file is left untouched rather than clobbered on a downgrade).
- Pure-Go: no new Win32 / COM / syscall imports and no new dependencies, so the
  binary surface is unchanged from v2.9.1 for clean AV-FP attribution.

## Deferred

- An in-**TUI** History tab (browse the log inside WinTUI) is planned for
  **v2.11.0**.
- A clickable **completion-toast deep-link** into history is deferred to a later
  release (the current toast is text-only; a reliable click target needs
  activation work that would add Win32/registration surface).

## Verification

VirusTotal scans of the published artifacts are added here after the GoReleaser
build, per `scripts/vt-scan.ps1` (pre-tag `-Path` + post-publish `-ReleaseTag`).

## Verification

VirusTotal scans of the published artifacts for v2.10.0 (run 2026-06-22):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.10.0_windows_amd64.exe` (7.5 MB) | `3b68527001d0…` | 2/69 | [VT report](https://www.virustotal.com/gui/file/3b68527001d07465717296c68489fe1108db9503c317bea567c14391c3772226) |
| `wintui_2.10.0_windows_amd64.zip` (2.7 MB) | `c71167311319…` | 1/66 | [VT report](https://www.virustotal.com/gui/file/c71167311319eac51f8a30415d9d679ccc8eb2d652b2613a053f2daef6477e03) |
| `wintui_2.10.0_windows_arm64.exe` (6.9 MB) | `a14f77b5136f…` | 1/67 | [VT report](https://www.virustotal.com/gui/file/a14f77b5136ff79fcc2a4302b84e4bbdb6fe6cd155fa8b625f82bab0b8d77ffb) |
| `wintui_2.10.0_windows_arm64.zip` (2.5 MB) | `35cba7fec843…` | 0/65 | [VT report](https://www.virustotal.com/gui/file/35cba7fec843eb0d11426295689a0840f7c3738ee247aa967af92ca2b5ea9eab) |

Detections at scan time were single-vendor low-signal ML/reputation noise. Microsoft Defender returned clean across all artifacts.

Full SHA256 hashes:

- `wintui_2.10.0_windows_amd64.exe`: `3b68527001d07465717296c68489fe1108db9503c317bea567c14391c3772226`
- `wintui_2.10.0_windows_amd64.zip`: `c71167311319eac51f8a30415d9d679ccc8eb2d652b2613a053f2daef6477e03`
- `wintui_2.10.0_windows_arm64.exe`: `a14f77b5136ff79fcc2a4302b84e4bbdb6fe6cd155fa8b625f82bab0b8d77ffb`
- `wintui_2.10.0_windows_arm64.zip`: `35cba7fec843eb0d11426295689a0840f7c3738ee247aa967af92ca2b5ea9eab`

