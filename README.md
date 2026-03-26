# WinTUI

[![Go Report Card](https://goreportcard.com/badge/github.com/kts982/wintui)](https://goreportcard.com/report/github.com/kts982/wintui)
[![CI](https://github.com/kts982/wintui/actions/workflows/ci.yml/badge.svg)](https://github.com/kts982/wintui/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kts982/wintui)](https://github.com/kts982/wintui/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A terminal user interface for **winget** (Windows Package Manager), built with Go and the [Charmbracelet](https://charm.sh) TUI libraries.

Browse, install, upgrade, and manage Windows packages without leaving the terminal.

![WinTUI demo](demo.gif)

## Install

**Requirements:** Windows 10/11 with winget installed.

```bash
# Run a release binary
.\wintui.exe

# Or build/install with Go 1.24.2+
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

Portable `winget` manifest files now live under [`packaging/winget`](./packaging/winget), and submission to the community `winget-pkgs` repository is the next distribution step.

## Features

**Package Management**
- **Upgrade** — scan for updates, upgrade all or select individual packages
- **Installed** — browse packages across `winget`, `msstore`, and `system` sources with selectable checkboxes
- **Install** — search and install new packages with live streaming output
- **Package Details** — view publisher, description, license, release notes, homepage (`i`)
- **Export / Import** — export installed packages to JSON (`e`) and restore on another machine (`m`)

**System Utilities**
- **Health Check** — shells, dev tools, runtimes, package managers, disk space, Defender, developer mode
- **Temp Cleanup** — scan and delete temp files older than 7 days
- **Settings** — persistent config for winget options (scope, architecture, silent/interactive, force, purge, etc.)

**UX**
- Tab-based navigation with number keys, `Tab`/`Shift+Tab`, or mouse clicks
- Per-tab screen state preserved across tab switches
- Fuzzy filter (`/`) on package lists
- Gradient progress bars on all loading states
- Cancellable operations (`Esc`)
- `Ctrl+e` to retry with admin elevation when needed
- Context-aware help bar

## Usage

```bash
.\wintui.exe
```

> **Tip:** Some operations require administrator privileges. Run in an elevated terminal for full functionality, or press `Ctrl+e` when WinTUI offers an elevated retry. The tab bar shows `● admin` / `● user` status.

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
| `o` | Open homepage (in detail view) |
| `r` | Refresh data |
| `Ctrl+e` | Retry elevated (when offered) |
| `e` | Export packages (Installed tab) |
| `m` | Import from export JSON (Installed tab) |
| `u` | Uninstall selected (Installed tab) |
| `Esc` | Cancel / back |
| `q` | Quit |

## Settings

Configurable from the Settings tab, stored in `%APPDATA%\wintui\settings.json`:

| Setting | Options |
|---|---|
| Install Scope | user / machine / auto |
| Install Mode | auto / silent / interactive |
| Architecture | x64 / x86 / arm64 / auto |
| Default Source | winget / msstore / auto |
| Force | skip non-security issues |
| Allow Reboot | permit reboots during install |
| Skip Dependencies | don't process dependencies |
| Purge on Uninstall | delete all package files |
| Include Unknown | show packages with unknown versions |

`Default Source` controls install/search preference; the Installed tab reflects the real installed state.

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
