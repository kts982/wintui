# WinTUI

A terminal user interface for **winget** (Windows Package Manager), built with Go and the [Charmbracelet](https://charm.sh) TUI libraries.

```
██╗    ██╗ ██╗ ███╗   ██╗ ████████╗ ██╗   ██╗ ██╗
██║    ██║ ██║ ████╗  ██║ ╚══██╔══╝ ██║   ██║ ██║
██║ █╗ ██║ ██║ ██╔██╗ ██║    ██║    ██║   ██║ ██║
██║███╗██║ ██║ ██║╚██╗██║    ██║    ██║   ██║ ██║
╚███╔███╔╝ ██║ ██║ ╚████║    ██║    ╚██████╔╝ ██║
 ╚══╝╚══╝  ╚═╝ ╚═╝  ╚═══╝    ╚═╝     ╚═════╝  ╚═╝
```

## Features

**Package Management**
- **Upgrade** — scan for updates, upgrade all or select individual packages, what-if preview
- **Installed** — browse installed packages across `winget`, `msstore`, and system entries, select and uninstall with `[X]` checkboxes
- **Install** — search and install new packages with live streaming output and source-aware results
- **Package Details** — view publisher, description, license, release notes, homepage (press `i`)
- **Export / Restore** — export selected or installed packages to JSON and restore from Desktop exports with review before install

**System Utilities**
- **Health Check** — native Go checks for shells, dev tools, runtimes, package managers, disk space, Windows Defender, developer mode
- **Temp Cleanup** — scan and delete temp files older than 7 days
- **Settings** — persistent config for winget options (scope, architecture, silent/interactive, force, purge, etc.)

**UX**
- Tab-based navigation (click, number keys `1-6`, or `Tab`/`Shift+Tab` to cycle)
- Per-tab screen state is preserved across tab switches
- Fuzzy filter with `/` on package lists
- Mouse support (tab clicks, table navigation, scroll)
- Gradient progress bars (pink → mint) on all loading/executing states
- Package cache with 2-minute TTL (`r` to force refresh)
- Cancellable operations (`Esc` during loading)
- Export selected packages to JSON with `e` (or all installed when nothing is selected) and restore from export with `m`
- Dynamic context-aware help bar
- `q` to quit

## Install

**Requirements:** Go 1.24.2+, Windows 10/11 with winget installed.

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

> **Tip:** Some packages (e.g. MSIX/Appx installers) require administrator privileges to upgrade. Run wintui in an elevated terminal for full functionality, or press `Ctrl+e` when WinTUI offers an elevated retry. The app shows a `● admin` / `● user` indicator in the tab bar and flags this in the Health Check.

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
| `Ctrl+e` | Retry current action elevated (when offered) |
| `e` | Export packages (Installed tab) |
| `m` | Import packages from export JSON (Installed tab) |
| `u` | Uninstall selected (Installed tab) |
| `Esc` | Cancel / back |
| `q` | Quit |

## Settings

Settings are stored in `%APPDATA%\wintui\settings.json` and configurable from the Settings tab:

- **Install Scope** — user / machine / auto
- **Install Mode** — auto / silent / interactive
- **Architecture** — x64 / x86 / arm64 / auto
- **Default Source** — winget / msstore / auto
- `Default Source` controls install/search preference; the Installed tab reflects the real installed state
- **Force** — skip non-security issues
- **Allow Reboot** — permit reboots during install
- **Skip Dependencies** — don't process dependencies
- **Purge on Uninstall** — delete all package files
- **Include Unknown** — show packages with unknown versions

## Development

Run the full local validation suite before pushing:

```powershell
.\scripts\check.ps1 -Mode full
```

This checks:
- `gofmt`
- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`
- `go build .`

Optional Git hooks are included in `.githooks/pre-commit` and `.githooks/pre-push`. To enable them:

```powershell
git config core.hooksPath .githooks
```

Recommended workflow:
- `pre-commit` stays fast and only checks formatting on staged Go files
- `pre-push` runs the full validation suite

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components (table, spinner, progress, textinput, viewport, help)
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Harmonica](https://github.com/charmbracelet/harmonica) — Spring-physics animations

## License

MIT

