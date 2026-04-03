# WinTUI

[![Go Report Card](https://goreportcard.com/badge/github.com/kts982/wintui)](https://goreportcard.com/report/github.com/kts982/wintui)
[![CI](https://github.com/kts982/wintui/actions/workflows/ci.yml/badge.svg)](https://github.com/kts982/wintui/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kts982/wintui)](https://github.com/kts982/wintui/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A terminal user interface for **winget** (Windows Package Manager), built with Go and the [Charmbracelet](https://charm.sh) TUI libraries.

Browse, search, install, upgrade, and manage Windows packages without leaving the terminal.
WinTUI features a split-panel workspace, batch operations with a single UAC prompt, and a headless CLI mode for scripting.

![WinTUI demo](demo.gif)

## Install

**Requirements:** Windows 10/11 with winget installed.

```powershell
winget install kts982.WinTUI
```

Or install manually:

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

## Features

**Unified Packages Screen**
- **Split-panel layout** — package list on the left, detail summary on the right
- **Bordered sections** — Updates Available, Installed, Search Results, and Install Queue with package counts and active focus highlighting
- **Search & Install** — press `s` to search the winget catalog, `Space` to queue packages, `i` to batch install
- **Upgrade** — select upgradeable packages and press `u` to batch upgrade
- **Uninstall** — select installed packages and press `x` to batch uninstall
- **Package Details** — press `Enter` or `→` for a full detail overlay with version picker (`v`), homepage (`o`), and scrollable metadata
- **Batch Execution Modal** — review selected packages, watch live progress with per-package spinners, view compact results with `Ctrl+E` elevated retry
- **Version Selection** — pick a specific version to install or upgrade to from the detail panel
- **Headless CLI** — use `--check`, `--list`, and `--json` for scripts, Task Scheduler, or CI without launching the TUI

**System Utilities**
- **Health Check** — shells, dev tools, runtimes, package managers, disk space, Defender, developer mode
- **Temp Cleanup** — scan and delete temp files older than 7 days
- **Settings** — persistent config for winget options (scope, architecture, silent/interactive, force, purge, etc.)

**UX**
- 4-tab layout: Packages, Cleanup, Health, Settings
- Boxed tab bar with animated gradient ASCII logo
- Context-sensitive help bar that adapts to the active section
- Full help panel on `?` with grouped keybindings
- Fuzzy filter (`/`) on the installed package list
- Per-tab screen state preserved across tab switches
- Built-in elevated helper — silent + auto-elevate runs everything elevated upfront, avoiding UAC popups from installers
- `Ctrl+E` elevated retry on the result modal when auto-elevate is off
- Responsive layout — detail panel hides on narrow terminals, compact header on small screens

## Usage

```bash
.\wintui.exe
```

> **Tip:** Some operations require administrator privileges. The subtitle bar shows `● admin` / `● user` status. Enable **Silent** mode + **Auto Elevate** in Settings for hands-off elevated operations, or press `Ctrl+E` on the result modal when a package fails.

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

### Packages Screen

| Key | Action |
|---|---|
| `↑↓` / `j/k` | Navigate |
| `Space` | Select package / add to install queue |
| `Enter` / `→` / `l` | Open package details |
| `←` / `Esc` / `h` | Close details / cancel |
| `s` | Search & install (search winget catalog) |
| `/` | Filter installed packages |
| `u` | Upgrade selected |
| `x` | Uninstall selected |
| `i` | Install queued packages |
| `a` | Select all available updates |
| `v` | Pick version (in detail view) |
| `c` | Reset to latest version (in detail view) |
| `o` | Open homepage (in detail view) |
| `r` | Refresh package data |
| `Ctrl+E` | Retry failed packages elevated (result modal) |
| `?` | Toggle full help panel |

### Global

| Key | Action |
|---|---|
| `1-4` | Switch tabs |
| `Tab` / `Shift+Tab` | Cycle tabs |
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
| Auto Elevate | automatically request admin rights |

**Action Mode: Silent + Auto Elevate** runs all install/upgrade/uninstall operations through the elevated helper upfront, avoiding UAC popups from installers that elevate themselves.

**Auto Elevate** (without silent mode) retries automatically on hard permission errors. When a batch finishes with failures, the result modal offers `Ctrl+E` to retry only the failed elevation-candidate packages.

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

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Harmonica](https://github.com/charmbracelet/harmonica) — Spring-physics animations

## License

MIT
