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
| `wintui upgrade --all` | Upgrade every visible upgradeable package |

`wintui` (no subcommand) launches the TUI.

### Deprecated root flags

`--check` and `--list` at the root still work but print a deprecation
warning. They will be removed in v2.5.0; use the subcommands above.

| Deprecated | Replacement |
|---|---|
| `wintui --check` | `wintui check` |
| `wintui --check --json` | `wintui check --json` |
| `wintui --list` | `wintui list` |
| `wintui --list --json` | `wintui list --json` |

## Exit Codes

### `check`

| Exit code | Meaning |
|---|---|
| `0` | No updates available (or all available updates are hidden by ignore rules) |
| `1` | One or more visible updates available |

`check` honors the same per-package ignore rules the TUI uses, so a
hidden package will not flip the exit code.

### `list`

`list` exits with `0` on success.

### `show`

`show` exits with `0` on success and non-zero on argument errors
(missing id, unsupported `--source`).

### `upgrade --all`

| Exit code | Meaning |
|---|---|
| `0` | All visible upgrades succeeded (or no upgrades were available) |
| `1` | One or more package upgrades failed |

The running WinTUI binary is **not** upgraded by `upgrade --all`; it is
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

# Upgrade everything that is not on the ignore list
wintui upgrade --all
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
  winget install --id Mozilla.Firefox --exact --accept-package-agreements --silent --source winget

Effective upgrade command:
  winget upgrade --id Mozilla.Firefox --exact --accept-package-agreements --silent --source winget
```

If the package has overrides, a "Per-package overrides" block is
appended (scope, architecture, elevate, ignore, ignore_version).

### `upgrade --all`

`upgrade --all` streams winget output line-by-line under each package
header and prints a summary line on completion.

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

- `--check` and `--list` (root flags, deprecated) are mutually exclusive.
- `--json` is valid with `check`, `list`, and `show`.
- `wintui upgrade --all` honors per-package ignore rules from `settings.json`.
- The retry/relaunch flags below are internal and used by WinTUI's
  elevated retry flows; they are not intended for direct user use:
  - `--retry-op`
  - `--id`
  - `--name`
  - `--source`
  - `--package-version`
  - `--retry-batch`
