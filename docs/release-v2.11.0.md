# WinTUI v2.11.0 Release Notes

**History tab.** The action-history log introduced in v2.10.0 gets its browsing
surface inside the TUI: a new **History** tab shows every batch WinTUI has run —
including headless `wintui upgrade` runs from Task Scheduler — with per-package
drill-down and a cross-batch timeline. The release also ships an opt-in Cleanup
target to clear that log, a `wintui doctor` detection fix, and the July
toolchain/dependency refresh (Go 1.26.5 security release included). Pure-Go
throughout: the Win32 symbol surface is byte-identical to v2.10.0.

## History tab

- **Batch list (Tier 1).** A newest-first list of recorded batches — trigger
  (`[tui]`, `[cli-all]`, …), action, package summary, ok/fail counts — with a
  detail pane for the selected batch. The same `%APPDATA%\wintui\history.json`
  the CLI writes; the tab is read-only over it.
- **Drill-down (Tier 2).** `Enter` opens a batch's per-package items: each
  item's record (action, from → to version, status, error text when a step
  failed) plus that package's **cross-batch timeline** — every time WinTUI has
  touched it, across all recorded batches.
- **Filter and refresh.** `f` toggles a failed-only filter on both tiers; `r`
  reloads from disk. The tab also reloads when you switch to it, so batches
  written by a concurrent `wintui upgrade` in another terminal appear without
  restarting the TUI.
- **Robust states.** Empty history (fresh install), corrupt file, and
  newer-schema (`future-version`) files each render a friendly notice instead
  of an error — and the log is never written by the tab, only read.
- **Tab layout.** History slots in as tab **4**, before Settings (now **5**),
  keeping `1`–`3` muscle memory intact; tab switching is `1`–`5` / `Tab`.

## Cleanup: opt-in "WinTUI action history" target

- A new entry in the Cleanup tab's WinTUI group deletes
  `%APPDATA%\wintui\history.json` — a **privacy reset** ("clear what WinTUI
  knows about my installs"), not a disk-space win. It ships
  **default-unchecked** with an explicit detail-pane warning that it erases the
  audit trail the History tab (and `wintui history`) renders.
- The cleanup path-validation allowlist was widened surgically: only
  `%APPDATA%\wintui` itself (or below) is eligible; the rest of `%APPDATA%`
  stays guarded, so no registry bug can route another app's roaming state
  through bulk delete.

## Fixes

- **doctor: winget MCP detection.** The `winget MCP` row in `wintui doctor` and
  the Health tab now also finds the MCP server binary via its
  `%LOCALAPPDATA%` alias path, fixing a false "not present" on standard
  installs.

## Toolchain and dependencies (July refresh)

- **Go 1.26.5** — the 2026-07-07 security release. Both CVEs were evaluated
  and are unreachable in WinTUI: CVE-2026-39822 (`os.Root` symlink escape) —
  WinTUI does not use `os.Root`, and the issue is Unix-only; CVE-2026-42505
  (`crypto/tls` ECH identity leak) — WinTUI's only TLS use is the public
  GitHub API self-update check. `govulncheck` on the release tree: clean.
- **bubbletea v2.0.8 / lipgloss v2.0.5** — includes the lipgloss 2.0.4 fix for
  a crash when writing to a closed wrap writer, plus grapheme/emoji width
  handling improvements.
- **sahilm/fuzzy v0.1.3** — fixes an index-out-of-bounds panic on NUL runes in
  match data and a `FindFrom` ordering bug; WinTUI fuzzy-matches
  winget-derived strings in the `/` filter.
- **golang.org/x/sys v0.46.0** — routine refresh.

## Compatibility

- No breaking changes. `settings.json`, `cache.json`, and `history.json` load
  unchanged; the history schema is untouched (the tab reads the v2.10.0
  format as-is).
- The only muscle-memory change: **Settings moved from key `4` to key `5`** to
  make room for History.
- Zero new Win32 / COM / syscall surface: the `go tool nm`
  `syscall.`/`windows.` symbol set is **identical** to v2.10.0 (380 symbols in
  both), despite the dependency refresh — clean AV-FP attribution.

## Deferred

- A clickable **completion-toast deep-link** into the History tab remains
  deferred (a reliable click target needs activation work that would add
  Win32/registration surface).

## Verification

VirusTotal scans of the published artifacts are added here after the GoReleaser
build, per `scripts/vt-scan.ps1` (pre-tag `-Path` + post-publish `-ReleaseTag`).

## Verification

VirusTotal scans of the published artifacts for v2.11.0 (run 2026-07-08):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.11.0_windows_amd64.exe` (7.5 MB) | `a40737bf6633…` | 1/69 | [VT report](https://www.virustotal.com/gui/file/a40737bf663314172e9aa9a9e75627b177b3ac34172bd4e7617c1d83a02835aa) |
| `wintui_2.11.0_windows_amd64.zip` (2.7 MB) | `f1fdd19f7c7d…` | 1/67 | [VT report](https://www.virustotal.com/gui/file/f1fdd19f7c7d9b2370af9a1fe55a6967585f85070ca7848ff7aa0954c6ec3b42) |
| `wintui_2.11.0_windows_arm64.exe` (7 MB) | `754b99170bdf…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/754b99170bdf628b137053297099459fd85592e7eb80594dc73680ab1eee54c2) |
| `wintui_2.11.0_windows_arm64.zip` (2.5 MB) | `1ba442e797c9…` | 0/66 | [VT report](https://www.virustotal.com/gui/file/1ba442e797c96941d732188d882a1c2f0072d2360cf3fbd3556f239441eb3280) |

Detections at scan time were single-vendor low-signal ML/reputation noise. Microsoft Defender returned clean across all artifacts.

Full SHA256 hashes:

- `wintui_2.11.0_windows_amd64.exe`: `a40737bf663314172e9aa9a9e75627b177b3ac34172bd4e7617c1d83a02835aa`
- `wintui_2.11.0_windows_amd64.zip`: `f1fdd19f7c7d9b2370af9a1fe55a6967585f85070ca7848ff7aa0954c6ec3b42`
- `wintui_2.11.0_windows_arm64.exe`: `754b99170bdf628b137053297099459fd85592e7eb80594dc73680ab1eee54c2`
- `wintui_2.11.0_windows_arm64.zip`: `1ba442e797c96941d732188d882a1c2f0072d2360cf3fbd3556f239441eb3280`

