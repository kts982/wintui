# WinTUI v2.6.0 Release Notes

WinTUI v2.6.0 reframes the Health tab as a fast-loading WinTUI / winget
readiness panel, ships a verdict-first headless `wintui doctor` subcommand,
introduces opt-in Windows toast notifications, and absorbs the
auto-self-update flow originally planned as a standalone v2.5.1 release.

## Slim Health tab

The Health tab is now a flat seven-row readiness panel that loads
instantly — no fresh winget calls, no disk scans, no async machinery.

**Default rows (all cheap, all readiness-focused):**

| Row | Source |
|---|---|
| WinTUI | version, running path (installed vs. dev build), config-dir writeable |
| winget | `winget --version` (FAIL if the shim refuses to start) |
| Sources | configured source + last cached scan age |
| Updates | visible / auto / held counts from the upgradeable cache + cache age |
| Privileges | Administrator vs. Standard User · Auto Elevate context |
| Storage | system drive free space (system drive only) |
| Settings | neutral one-line summary (Auto Elevate · Self Update · Action Mode · Source) |

**Dropped from the tab** (still available via `wintui doctor --full` and
`--dev-tools`): RAM, uptime, Windows Defender, all extra fixed drives,
generic `ping 8.8.8.8`, PowerShell 7 row, the `f`-toggle full-diagnostics
panel, and the `d`-toggle developer-tools panel.

The connectivity signal that matters for package management is "did the
last winget call succeed?" — that lives in the cache layer and is now
surfaced directly. No more `http.Head` or `ping`.

## `wintui doctor` — verdict-first headless health

A new subcommand that prints a one-line verdict and exits with a status
code matching the worst row:

```text
$ wintui doctor
OK

$ wintui doctor          # WARN run
WARN: 2 issues

$ wintui doctor          # FAIL run
FAIL: 1 issue
```

| Exit code | Verdict |
|---|---|
| `0` | All rows PASS / INFO — `OK` |
| `1` | At least one row WARN, none FAIL — `WARN: N issues` |
| `2` | At least one row FAIL — `FAIL: N issues` |

**Flags:**

- `--verbose` — append the per-row table beneath the verdict
- `--full` — re-add the verbose system-diagnostics rows (RAM, Defender,
  internet ping, extra drives, OS / uptime, PATH, Windows PowerShell)
- `--dev-tools` — append a developer-tools detection group (Git, VS Code,
  Docker, Node.js, Python, Go, Rust, Java, dotnet, npm, OpenSSH, curl,
  PowerShell 7+); each missing tool is a WARN
- `--json` — emit `{ verdict, summary, exit_code, counts, checks }` for
  scripts and CI gates

The TUI Health tab and `wintui doctor` share a single check engine
(`runDoctorReport`), so adding a new check lights it up for both.
See [docs/cli.md](../docs/cli.md) for full reference and recipes.

## Auto-self-update (default on)

Originally planned as a standalone v2.5.1; folded into v2.6.0 to ship
together. When **WinTUI Auto Update** is enabled (default on for new
installs and existing configs that didn't explicitly disable it):

- Pre-TUI startup runs `winget list --upgrade-available --id kts982.WinTUI
  --exact --source winget` with an 8-second timeout. A
  `Checking for WinTUI updates…` banner prints to stdout while it runs.
- On a hit, WinTUI fires the existing PowerShell handoff and exits
  cleanly so winget can replace the released binary.
- The admin-gated `Ctrl+A` self-upgrade relaunch path is removed; manual
  self-upgrade now uses Enter-to-close + the same detached winget handoff.

Cross-PC reliability has been the main driver here — the stripped-down
flow avoids the Defender / PowerShell-variant edge cases the older
relaunch path hit on family machines.

## Opt-in toast notifications

A new `toast_notifications` setting (default **off**). When enabled,
WinTUI sends a single Windows toast on:

- TUI batch finish — `WinTUI · 2 of 3 upgraded · 1 failed`. Pending
  self-upgrade items are surfaced as `· N awaiting restart` so a mixed
  batch doesn't claim success while one item still needs the Enter
  handoff.
- `wintui upgrade --auto/--all/--id` finish — same body shape. Covers
  the scheduled-task case where the run is otherwise invisible.
- `wintui check` finding updates — `WinTUI · 5 updates available`.
  Silent when nothing's available, so a daily scheduled `check`
  produces zero noise on up-to-date machines.

**No batch-start toast** — the modal is right in front of you.

**Implementation:** PowerShell + raw WinRT
(`Windows.UI.Notifications.ToastNotificationManager`). On first toast
attempt, WinTUI drops a minimal Start Menu shortcut at
`%APPDATA%\Microsoft\Windows\Start Menu\Programs\WinTUI.lnk` and sets
`PKEY_AppUserModel_ID = kts982.WinTUI` on it via
`SHGetPropertyStoreFromParsingName`. Without this step, Windows
attributes raw toasts as "PowerShell" or suppresses them on Win11. A
sibling `WinTUI.aumid` marker file gates the early-return on subsequent
runs and triggers self-repair if a previous run crashed between
`Save()` and `SetAppId`.

**Suppression rules:** skipped when `CI` env var is set or when
`WINTUI_DISABLE_TOAST` is set. Elevation is **not** suppressed —
UAC-elevated processes share the user's session and notification
stream, and the scheduled `wintui check` use case (the headline reason
for these toasts) often runs elevated.

## Bug fixes

- **Broken winget shims now fail loudly.** Previously `checkWingetTool`
  swallowed `cmd.Run()` errors via `_ = cmd.Run()`, so a WindowsApps
  app-execution alias that's on `PATH` but refuses to start showed up as
  PASS in Health and `doctor`. Now the run error is captured and
  surfaced as a FAIL row with the trimmed error message and a
  recommendation pointing at App Installer reinstallation.
- **Toast first-run delivery.** A subtle path-quoting bug in the
  inline-C# AUMID setter (`%q` produced Go-style `\\` escapes that
  PowerShell read literally) caused
  `SHGetPropertyStoreFromParsingName` to reject the path with
  `E_INVALIDARG`, leaving the .lnk on disk without an AUMID and
  silently breaking toasts forever. Fixed via PowerShell single-quoted
  literal interpolation; the marker-file approach above also detects
  and self-heals shortcuts left poisoned by older builds.
- **Indexer race on first toast.** Action Center indexes Start Menu
  shortcuts asynchronously, so the very first toast after opt-in could
  fire before the AUMID was routable. WinTUI now waits 1.5 s after a
  fresh shortcut creation; subsequent toasts skip the wait via the
  marker-file early return.

## Notes

- No breaking changes to existing settings or keybindings.
- New settings:
  - `auto_self_update` — default true (existing configs inherit true
    unless explicitly turned off).
  - `toast_notifications` — default false (opt-in).
- The Health tab dropped its `f` (full diagnostics) and `d` (dev tools)
  keybindings. The same content is now reachable via
  `wintui doctor --full` and `wintui doctor --dev-tools`.
- `settings.json` is fully backward compatible.
- Headless upgrade commands (`--all`, `--auto`, `--id`) still skip the
  running WinTUI binary with a hint pointing at the startup
  self-update handoff. Self-upgrade goes through the TUI / launch path
  for the same reason as in v2.5.0.
