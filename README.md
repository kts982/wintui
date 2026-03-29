# WinTUI

[![Go Report Card](https://goreportcard.com/badge/github.com/kts982/wintui)](https://goreportcard.com/report/github.com/kts982/wintui)
[![CI](https://github.com/kts982/wintui/actions/workflows/ci.yml/badge.svg)](https://github.com/kts982/wintui/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kts982/wintui)](https://github.com/kts982/wintui/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A terminal user interface for **winget** (Windows Package Manager), built with Go and the [Charmbracelet](https://charm.sh) TUI libraries.

Browse, install, upgrade, and manage Windows packages without leaving the terminal.
WinTUI also includes a headless CLI mode for scripts and a built-in auto-elevation helper that can keep a batch moving with a single UAC prompt.

![WinTUI demo](demo.gif)

## Install

**Requirements:** Windows 10/11 with winget installed.

```bash
# Run a release binary
.\wintui.exe

# Or build/install with Go 1.26+
go install github.com/kts982/wintui@latest

# Or clone and build from source
git clone https://github.com/kts982/wintui.git
cd wintui
go build -o wintui.exe .
```

Pre-built binaries are available on [GitHub Releases](https://github.com/kts982/wintui/releases) — both `.exe` and `.zip` for Windows amd64 and arm64:

```bash
gh release download --repo kts982/wintui --pattern '*windows_amd64.exe'
```

Portable `winget` manifest files live under [`packaging/winget`](./packaging/winget). A submission to the community `winget-pkgs` repository is pending approval.

## Features

**Package Management**
- **Upgrade** — open directly to the available updates list, select packages or upgrade all, with live streaming logs
- **Installed** — browse packages across `winget`, `msstore`, and `system` sources with selectable checkboxes
- **Install** — search and install new packages with live streaming output
- **Package Details** — inspect package metadata, choose an explicit target version, and compare installed vs. target upgrade details (`i`, `v`)
- **Export / Import** — export installed packages to JSON (`e`) and restore on another machine (`m`)
- **Headless CLI** — use `--check`, `--list`, and `--json` for scripts, Task Scheduler, or CI without launching the TUI

**System Utilities**
- **Health Check** — shells, dev tools, runtimes, package managers, disk space, Defender, developer mode
- **Temp Cleanup** — scan and delete temp files older than 7 days
- **Settings** — persistent config for winget options (scope, architecture, silent/interactive, force, purge, etc.)

**UX**
- Tab-based navigation with number keys, `Tab`/`Shift+Tab`, or mouse clicks
- Per-tab screen state preserved across tab switches
- Fuzzy filter (`/`) on package lists
- Gradient progress bars on all loading states
- Streaming execution view for install, upgrade, and uninstall operations
- Post-run log preview with `l` to expand/collapse execution output
- Cancellable operations (`Esc`)
- Built-in `Auto Elevate` support for hard admin-required actions, plus `Ctrl+e` to retry failed elevation-candidate actions; batch retries only rerun failed items
- Context-aware help bar

## Usage

```bash
.\wintui.exe
```

> **Tip:** Some operations require administrator privileges. Run in an elevated terminal for full functionality, or press `Ctrl+e` when WinTUI offers an elevated retry. The tab bar shows `● admin` / `● user` status.

### Headless CLI

```powershell
# Human-readable upgrade check
.\wintui.exe --check

# Exit code 1 when updates are available
.\wintui.exe --check || echo "Updates available"

# Machine-readable output
.\wintui.exe --check --json
.\wintui.exe --list --json > packages.json
```

Further documentation:
- [CLI reference](docs/cli.md)
- [Elevation and retry behavior](docs/elevation.md)

## Keyboard Shortcuts

| Key | Action |
|---|---|
| `1-6` | Switch tabs |
| `Tab` / `Shift+Tab` | Cycle tabs |
| `↑↓` / `j/k` | Navigate |
| `Space` | Toggle selection |
| `Enter` | Select / confirm |
| `/` | Filter list |
| `i` | Package details |
| `v` | Choose package version (detail view) / reveal skipped import entries |
| `c` | Reset selected package version to latest (detail view) |
| `o` | Open homepage (in detail view) |
| `r` | Refresh data |
| `Ctrl+e` | Retry elevated (when offered) |
| `l` | Expand / collapse saved execution log after a run |
| `e` | Export packages (Installed tab) |
| `m` | Import from export JSON (Installed tab) |
| `u` | Upgrade all (Upgrade tab) / uninstall selected (Installed tab) |
| `Esc` | Cancel / back |
| `q` | Quit |

## Settings

Configurable from the Settings tab, stored in `%APPDATA%\wintui\settings.json`:

| Setting | Options |
|---|---|
| Install Scope | user / machine / auto |
| Action Mode | auto / silent / interactive |
| Architecture | x64 / x86 / arm64 / auto |
| Default Source | winget / msstore / auto |
| Force | skip non-security issues |
| Allow Reboot | permit reboots during install |
| Skip Dependencies | don't process dependencies |
| Purge on Uninstall | delete all package files |
| Include Unknown Versions | show packages with unknown versions |
| Auto Elevate | automatically request admin rights for hard elevation errors |

`Action Mode` applies to install, upgrade, and uninstall requests where the underlying package supports it.

`Default Source` controls install/search preference only; uninstall works against the installed package database regardless of that setting.

`Auto Elevate` tries the built-in elevated helper automatically for hard permission errors. When a run still fails, `Ctrl+e` retries only the failed elevation-candidate items.

For deeper behavior details and examples, see:
- [CLI reference](docs/cli.md)
- [Elevation and retry behavior](docs/elevation.md)

## Development

```powershell
# Run the full validation suite
.\scripts\check.ps1 -Mode full

# Optional: enable git hooks
git config core.hooksPath .githooks
```

The validation suite runs `gofmt`, `go test`, `go vet`, `staticcheck`, and `go build`.

Optional Git hooks are included in `.githooks/pre-commit` and `.githooks/pre-push`.

Maintainers can regenerate `demo.gif` from `demo.cast` with `agg`.
Maintainers can validate the local WinGet manifest with `winget validate .\packaging\winget\manifests\k\kts982\WinTUI\0.1.0`.

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Harmonica](https://github.com/charmbracelet/harmonica) — Spring-physics animations

## License

MIT
