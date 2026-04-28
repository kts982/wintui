# CLI Reference

WinTUI exposes a small headless CLI for scripts, CI, and scheduled checks.
Running `wintui` with no arguments launches the interactive TUI; subcommands
run headlessly and exit.

## Subcommands

| Command | Behavior |
|---|---|
| `wintui check [--json]` | Print upgradeable packages and exit |
| `wintui list [--json]` | Print installed packages and exit |
| `wintui show <id> [--source winget\|msstore] [--json]` | Print effective install/upgrade args and overrides for a single package (read-only; does not call winget) |
| `wintui upgrade --all` | Upgrade every non-held upgradeable package |
| `wintui upgrade --auto` | Upgrade only packages marked Auto |
| `wintui upgrade --id <pkg>` | Upgrade one or more named packages (repeatable) |

`wintui` (no subcommand) launches the TUI.

The old root `--check` and `--list` flags have been removed. Use
`wintui check` and `wintui list`.

## Exit Codes

### `check`

| Exit code | Meaning |
|---|---|
| `0` | No updates available (or all available updates are held by policy) |
| `1` | One or more visible updates available |

`check` honors the same per-package update policy the TUI uses, so a held
package will not flip the exit code.

### `list`

`list` exits with `0` on success.

### `show`

`show` exits with `0` on success and non-zero on argument errors
(missing id, unsupported `--source`).

### `upgrade --all` / `upgrade --auto` / `upgrade --id`

| Exit code | Meaning |
|---|---|
| `0` | All selected upgrades succeeded, no matching upgrades were available, or only the running WinTUI binary was skipped |
| `1` | One or more package upgrades failed, or `--id` named a held package |

`--all` upgrades every non-held package. `--auto` upgrades only packages
whose per-package update policy is Auto. `--id` upgrades one or more named
packages — the flag is repeatable (`--id A --id B`), packages without an
available update are reported with no error, and naming a held package is
an error (so the user notices their hold instead of silently skipping).

`--all`, `--auto`, and `--id` are mutually exclusive.

The running WinTUI binary is **not** upgraded by headless upgrade commands; it is
skipped with a hint pointing at the TUI, where the self-upgrade handoff
is verified. To upgrade WinTUI itself, run `wintui` and use the TUI.

## Examples

```powershell
# Human-readable upgrade check
wintui check

# Use the exit code in Task Scheduler, PowerShell, or CI
wintui check ; if ($LASTEXITCODE -eq 1) { "Updates available" }

# JSON output for scripting
wintui check --json

# Export installed packages as JSON
wintui list --json > packages.json

# Inspect what WinTUI would pass to winget for a given package
wintui show Mozilla.Firefox
wintui show Mozilla.Firefox --json

# Upgrade everything that is not held
wintui upgrade --all

# Upgrade only packages marked Auto
wintui upgrade --auto

# Upgrade one or more specific packages by ID
wintui upgrade --id Mozilla.Firefox --id Microsoft.VisualStudioCode

# Pipe from check (PowerShell):
wintui check --json | ConvertFrom-Json | ForEach-Object { wintui upgrade --id $_.id }

# Pipe from check (bash, e.g. Git Bash):
wintui check --json | jq -r '.[].id' | xargs -r -n1 wintui upgrade --id
```

## Recipes

PowerShell snippets for common automation patterns. Each one assumes
`wintui.exe` is on `PATH`.

### Daily toast when updates are available

Pair with Task Scheduler (`schtasks /Create ... /SC DAILY`) so the toast
fires once per day:

```powershell
# Requires the BurntToast module: Install-Module -Name BurntToast
$updates = wintui check --json | ConvertFrom-Json
if ($updates.Count -gt 0) {
    New-BurntToastNotification -Text "WinTUI", "$($updates.Count) update(s) available"
}
```

### Upgrade everything matching a pattern

Selectively upgrade by package ID prefix without touching anything else:

```powershell
wintui check --json | ConvertFrom-Json |
    Where-Object { $_.id -like 'Microsoft.*' } |
    ForEach-Object { wintui upgrade --id $_.id }
```

### Exit-code gate for CI / Task Scheduler

`wintui check` exits 1 when visible updates exist — drop straight into a
build step or scheduled job to short-circuit when patches are pending:

```powershell
wintui check
if ($LASTEXITCODE -eq 1) {
    Write-Host "Pending updates — failing build" -ForegroundColor Yellow
    exit 1
}
```

### Inspect-before-upgrade

Print the exact `winget` command WinTUI would run for every pending
update — useful for reviewing per-package overrides before pulling the
trigger:

```powershell
wintui check --json | ConvertFrom-Json |
    ForEach-Object { wintui show $_.id }
```

### Save an installed-package snapshot

Until `wintui export` / `wintui import` lands, JSON snapshots make a fine
manual baseline. Re-apply on a fresh machine via `winget install` per ID:

```powershell
wintui list --json | Out-File -Encoding utf8 packages.json
# On the new machine:
Get-Content packages.json | ConvertFrom-Json |
    ForEach-Object { winget install --id $_.id --exact --accept-package-agreements }
```

## Human-Readable Output

### `check`

```text
Name       ID                   Version  Available
Git        Git.Git              2.44.0   2.45.0
Notepad++  Notepad++.Notepad++  8.6.4    8.7.1

2 package(s) have updates available.
```

### `list`

```text
Name       ID                   Version  Source
Git        Git.Git              2.45.0   winget
PowerToys  Microsoft.PowerToys  0.91.0   winget

2 package(s) installed.
```

### `show`

```text
ID:     Mozilla.Firefox
Source: winget

Effective install command:
  winget install --id Mozilla.Firefox --exact --accept-package-agreements --source winget

Effective upgrade command:
  winget upgrade --id Mozilla.Firefox --exact --accept-package-agreements --source winget
```

If the package has overrides, a "Per-package overrides" block is
appended (update_policy, scope, architecture, elevate, ignore, ignore_version).
Global and per-package action settings can add flags such as `--silent`,
`--scope`, or `--architecture` to the command preview.

### `upgrade --all` / `upgrade --auto`

Headless upgrade commands stream winget output line-by-line under each
package header and print a summary line on completion.

## JSON Output

### `check`, `list`

JSON output is an array of package objects with lowercase keys:

```json
[
  {
    "name": "Git",
    "id": "Git.Git",
    "version": "2.44.0",
    "available": "2.45.0",
    "source": "winget"
  }
]
```

### `show`

```json
{
  "id": "Mozilla.Firefox",
  "source": "winget",
  "install_args": ["install", "--id", "Mozilla.Firefox", "--exact", "..."],
  "upgrade_args": ["upgrade", "--id", "Mozilla.Firefox", "--exact", "..."],
  "override": {
    "scope": "user"
  }
}
```

The `override` field is omitted when no per-package rules exist.

## Notes

- `--json` is valid with `check`, `list`, and `show`.
- `wintui upgrade --all` and `wintui upgrade --auto` honor per-package update policy from `settings.json`.
- `wintui upgrade --id` is the user-facing single-package mode. The
  identically-named `--id` on the root command (alongside `--retry-op`,
  `--name`, etc.) is internal to WinTUI's elevated retry flow and is not
  intended for direct user use.
