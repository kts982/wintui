# WinTUI

A terminal user interface for **winget** (Windows Package Manager), built with Go and the [Charmbracelet](https://charm.sh) TUI libraries.

```
██╗    ██╗██╗███╗   ██╗████████╗██╗   ██╗██╗
██║ █╗ ██║██║██╔██╗ ██║   ██║   ██║   ██║██║
╚███╔███╔╝██║██║ ╚████║   ██║   ╚██████╔╝██║
 ╚══╝╚══╝ ╚═╝╚═╝  ╚═══╝   ╚═╝    ╚═════╝ ╚═╝
```

## Features

**Package Management**
- **Upgrade** — scan for updates, upgrade all or select individual packages, what-if preview
- **Search** — search the winget repository with results in an interactive table
- **Installed** — browse all installed packages, select and uninstall with `[X]` checkboxes
- **Install** — search and install new packages with confirmation
- **Package Details** — view publisher, description, license, release notes, homepage (press `i`)

**System Utilities**
- **Health Check** — native Go checks for shells, dev tools, runtimes, package managers, disk space, Windows Defender, developer mode
- **Temp Cleanup** — scan and delete temp files older than 7 days
- **Settings** — persistent config for winget options (scope, architecture, silent/interactive, force, purge, etc.)

**UX**
- Tab-based navigation (click, number keys `1-7`, or `Tab`/`Shift+Tab` to cycle)
- Type-to-filter with `/` on package lists
- Mouse support (tab clicks, table navigation)
- Gradient progress bars (pink → mint) on all loading/executing states
- Package cache with 2-minute TTL (`r` to force refresh)
- Cancellable operations (`Esc` during loading)
- Export installed packages to JSON (`e`)
- Dynamic context-aware help bar
- `q` to quit

## Install

**Requirements:** Go 1.22+, Windows 10/11 with winget installed.

```bash
# Clone and build
git clone https://github.com/kts982/wintui.git
cd wintui
go build -o wintui.exe .

# Or install directly
go install github.com/kts982/wintui@latest
```

## Usage

```bash
./wintui.exe
```

## Keyboard Shortcuts

| Key | Action |
|---|---|
| `1-7` | Switch tabs |
| `Tab` / `Shift+Tab` | Cycle tabs |
| `↑↓` / `j/k` | Navigate |
| `Space` | Toggle selection |
| `Enter` | Select / confirm |
| `/` | Filter list |
| `i` | Package details |
| `o` | Open homepage (in detail view) |
| `r` | Refresh data |
| `e` | Export packages (Installed tab) |
| `u` | Uninstall selected (Installed tab) |
| `Esc` | Cancel / back |
| `q` | Quit |

## Settings

Settings are stored in `%APPDATA%\wintui\settings.json` and configurable from the Settings tab:

- **Install Scope** — user / machine / auto
- **Install Mode** — silent / interactive
- **Architecture** — x64 / x86 / arm64 / auto
- **Default Source** — winget / msstore / all
- **Force** — skip non-security issues
- **Allow Reboot** — permit reboots during install
- **Skip Dependencies** — don't process dependencies
- **Purge on Uninstall** — delete all package files
- **Include Unknown** — show packages with unknown versions

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components (table, spinner, progress, textinput, help)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling

## License

MIT
