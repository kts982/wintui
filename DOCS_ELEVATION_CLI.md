# WinTUI CLI & Elevation Strategy

This note describes the delivered shape of the headless CLI (`#17`) and the elevated helper approach (`#21`).

## Headless CLI

WinTUI keeps Cobra as the entrypoint, but the scripting surface is exposed as **root flags**, not top-level action subcommands.

### Supported flags

| Flag | Behavior |
|---|---|
| `--check` | Print upgradeable packages and exit. Exit code `0` = up to date, `1` = updates available |
| `--list` | Print installed packages and exit |
| `--json` | Emit machine-readable JSON for `--check` or `--list` |

### Notes

- Launching the TUI remains the default when neither `--check` nor `--list` is provided.
- `--check` and `--list` are mutually exclusive.
- Existing retry startup flags still work:
  - `--retry-op`
  - `--id`
  - `--name`
  - `--source`
  - `--package-version`
  - `--retry-batch`

### Output contract

- Human-readable mode prints a tabular view plus a summary count.
- JSON mode writes package objects to stdout using lowercase keys (`name`, `id`, `version`, `available`, `source`).
- No TUI is launched in CLI mode.

## Elevated Helper

WinTUI uses a built-in elevated companion process for batch-friendly retries without relaunching the whole app for every package.

### Flow

1. The non-elevated TUI detects a hard elevation error from `winget`.
2. If `Auto Elevate` is enabled, it starts a named-pipe listener and launches:
   - `wintui helper --pipe <pipe_name>`
3. The elevated helper connects back over a Windows named pipe.
4. Winget actions stream line-by-line through the helper so the existing execution log UI stays intact.
5. The helper stays alive for subsequent elevated commands in the same session, so a batch needs at most one UAC prompt.

### Security

- The named pipe is created with an SDDL restricted to the current user SID.
- The helper itself must be elevated before it will serve requests.

### Fallbacks

- If helper startup fails, WinTUI keeps the original package failure visible and still offers manual `Ctrl+e`.
- Manual `Ctrl+e` first tries the helper path.
- If the helper still cannot start, WinTUI falls back to the older full-process relaunch with retry arguments.

### Retry behavior

- Retries only target the failed/current items, never already-successful batch items.
- Explicit target versions are preserved across retry startup.
- Hard elevation errors and softer installer/MSI cases such as `1603` are both retry candidates, but the messaging differs:
  - hard: administrator privileges are required
  - soft: retrying elevated may help, but may change installer behavior

## Libraries

### `spf13/cobra`

- Role: entrypoint and flag parsing
- Why: stable Go CLI framework with help/version support

### `github.com/Microsoft/go-winio`

- Role: Windows named pipes
- Why: `net.Conn`/`net.Listener` compatible IPC with Windows security descriptor support
