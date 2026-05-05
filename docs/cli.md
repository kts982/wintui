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
| `wintui doctor [--verbose] [--full] [--dev-tools] [--json]` | Verdict-first readiness check: `OK` / `WARN: N issues` / `FAIL: N issues` and exit 0/1/2 |
| `wintui export [--output PATH] [--with-versions]` | Write the installed package list to a portable JSON file (stdout by default) |
| `wintui import <path> [--dry-run] [--all] [--json]` | Install packages from a `wintui export` file (with optional preflight) |

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

### `doctor`

| Exit code | Meaning |
|---|---|
| `0` | All readiness rows are PASS / INFO — `OK` |
| `1` | At least one row is WARN, none are FAIL — `WARN: N issues` |
| `2` | At least one row is FAIL — `FAIL: N issues` |

The slim default check set is identical to the TUI Health tab. `--full`
re-adds the verbose system-diagnostics rows (RAM, Defender, internet ping,
extra fixed drives, OS / uptime, PATH, Windows PowerShell). `--dev-tools`
appends a developer-tools detection group (Git, VS Code, Docker, Node,
Python, Go, etc.) — every missing tool is a WARN, so this flag generally
flips the verdict to WARN unless your machine has the full dev stack
installed. `--verbose` prints the per-row table beneath the verdict line.

`--all` upgrades every non-held package. `--auto` upgrades only packages
whose per-package update policy is Auto. `--id` upgrades one or more named
packages — the flag is repeatable (`--id A --id B`), packages without an
available update are reported with no error, and naming a held package is
an error (so the user notices their hold instead of silently skipping).

`--all`, `--auto`, and `--id` are mutually exclusive.

The running WinTUI binary is **not** upgraded by headless upgrade commands; it is
skipped with a hint pointing at the TUI startup self-update handoff. To
upgrade WinTUI itself, run `wintui`; the default-on WinTUI Auto Update
setting checks before the TUI starts and exits to let winget replace the
released binary when an update is available.

### `export`

`wintui export` writes the current installed package list as a versioned
JSON envelope you can move to another machine and feed to `wintui import`.

By default the JSON is printed to stdout (so it pipes cleanly to anything
expecting JSON on a stream). Pass `--output PATH` to write to a file with
a status summary on stdout instead.

Versions are excluded by default — restoring exact versions on a fresh
machine is a footgun (the registered version may have aged out of winget).
Pass `--with-versions` if you genuinely need the snapshot pinned.

WinTUI itself is included in the export. On import it gets marked
already-installed automatically, since by definition you must have WinTUI
to run import in the first place.

### `import`

`wintui import <path>` reads an export file and installs its packages
headlessly. Default behavior installs the **safe subset**:

- packages that aren't already installed,
- packages that aren't raw / non-canonical identifiers (MSIX hashes, GUIDs),
- packages that don't share a name with a different installed package
  (e.g. exporting `Git.Git` when `Microsoft.Git` is installed locally —
  this is flagged as a possible duplicate and skipped by default).

Flags:

| Flag | Behavior |
|---|---|
| `--dry-run` | Print the install plan (will-install / already-installed / review-needed / non-restorable) without touching anything |
| `--all` | Also install entries flagged as possible name matches |
| `--json` | Print the plan as JSON; implies `--dry-run` |

`wintui import` accepts both the new envelope format (top-level JSON
object) and the legacy flat-array form (top-level JSON array) for
backward compatibility.

For row-level toggling of the install set, use the TUI:
press `I` (capital) on the Packages tab to open the import overlay,
which scans your Desktop, home, and current directory for `*.json` files.

| Exit code | Meaning |
|---|---|
| `0` | Plan shown (dry-run) or every selected install succeeded |
| `1` | At least one install failed |

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

# Export and re-import on a new machine
wintui export --output \\share\backup\packages.json
wintui import \\share\backup\packages.json --dry-run     # preview
wintui import \\share\backup\packages.json               # install safe subset
wintui import \\share\backup\packages.json --all         # also install name-collision rows

# Pipe from check (bash, e.g. Git Bash):
wintui check --json | jq -r '.[].id' | xargs -r -n1 wintui upgrade --id
```

## Recipes

PowerShell snippets for common automation patterns. Each one assumes
`wintui.exe` is on `PATH`.

### Daily toast when updates are available

Enable **Toast Notifications** in the Settings tab once (or set
`"toast_notifications": true` in `%APPDATA%\wintui\settings.json`), then
pair `wintui check` with Task Scheduler (`schtasks /Create ... /SC DAILY`).
WinTUI fires the toast itself when the scan finds at least one update —
no PowerShell module dependency, AUMID-attributed as "WinTUI" in Action
Center:

```powershell
# Once Toast Notifications is on, this is all the schedule needs:
wintui check
```

The toast is silent when nothing is available, so a daily scheduled run
produces zero noise on up-to-date machines.

### Verdict-driven scheduled health check

`wintui doctor` exits 0/1/2 based on readiness state, so it slots into
Task Scheduler / CI gates the same way `check` does. Combined with toast
notifications, a scheduled `doctor` becomes a "tell me when something
needs attention" probe:

```powershell
# Daily readiness verdict — pair with Task Scheduler for a quiet probe
wintui doctor
if ($LASTEXITCODE -ne 0) { wintui doctor --verbose }
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

### `doctor`

```json
{
  "verdict": "WARN",
  "summary": "WARN: 1 issue",
  "exit_code": 1,
  "counts": { "pass": 4, "warn": 1, "fail": 0, "info": 2 },
  "checks": [
    {
      "check": "WinTUI",
      "status": "PASS",
      "details": "v2.6.0 · installed · config writeable"
    },
    {
      "check": "Sources",
      "status": "WARN",
      "details": "winget · no cached scan yet",
      "recommendation": "Run wintui or winget upgrade to populate the cache."
    }
  ]
}
```

`verdict` is one of `OK` / `WARN` / `FAIL`; `exit_code` always matches.
`recommendation` is omitted on rows that don't have one.

## Notes

- `--json` is valid with `check`, `list`, `show`, and `doctor`.
- `wintui upgrade --all` and `wintui upgrade --auto` honor per-package update policy from `settings.json`.
- `wintui upgrade --id` is the user-facing single-package mode. The
  identically-named `--id` on the root command (alongside `--retry-op`,
  `--name`, etc.) is internal to WinTUI's elevated retry flow and is not
  intended for direct user use.
