# CLI Reference

WinTUI exposes a small headless CLI for scripts, CI, and scheduled checks.
If no CLI flag is provided, WinTUI launches the interactive TUI.

## Supported Flags

| Flag | Behavior |
|---|---|
| `--check` | Print upgradeable packages and exit |
| `--list` | Print installed packages and exit |
| `--json` | Emit JSON instead of a human-readable table |

## Exit Codes

### `--check`

| Exit code | Meaning |
|---|---|
| `0` | No updates available |
| `1` | One or more updates available |

### `--list`

`--list` exits with `0` on success.

## Examples

```powershell
# Human-readable upgrade check
.\wintui.exe --check

# Use the exit code in Task Scheduler, PowerShell, or CI
.\wintui.exe --check || echo "Updates available"

# JSON output for scripting
.\wintui.exe --check --json

# Export installed packages to stdout as JSON
.\wintui.exe --list --json > packages.json
```

## Human-Readable Output

### `--check`

```text
Name       ID                   Version  Available
Git        Git.Git              2.44.0   2.45.0
Notepad++  Notepad++.Notepad++  8.6.4    8.7.1

2 package(s) have updates available.
```

### `--list`

```text
Name       ID                   Version  Source
Git        Git.Git              2.45.0   winget
PowerToys  Microsoft.PowerToys  0.91.0   winget

2 package(s) installed.
```

## JSON Output

JSON output uses lowercase keys:

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

## Notes

- `--check` and `--list` are mutually exclusive.
- `--json` is valid with either `--check` or `--list`.
- No TUI is launched in CLI mode; output goes directly to stdout.
- Existing retry startup flags still work for internal elevated retry flows:
  - `--retry-op`
  - `--id`
  - `--name`
  - `--source`
  - `--package-version`
  - `--retry-batch`