# WinTUI v2.8.0 Release Notes

The theme drop. Eight curated color palettes with a picker, adaptive
light/dark variant detection, and opt-in terminal background tinting
for terminals that support it.

No breaking changes, no new Win32 surface. `settings.json` and
`cache.json` formats remain backward-compatible — the two new keys
(`theme`, `theme_background`) are optional with safe defaults.

## Theme picker

Adds **Color Theme** in Settings (cycle with Left/Right/Space). The
curated set:

| Theme | Notes |
|---|---|
| Sweet Pink | WinTUI's original — pink/lavender/mint. Default. |
| WinTUI Midnight | App-native dark — calmer than Sweet Pink for daily use. |
| Catppuccin | Mocha + Latte variants from the published spec. |
| Nord | Frost blues over polar night. |
| Dracula | Bold pink/purple/cyan from the published spec. |
| Tokyo Night | Muted blue/purple, plus a Day variant. |
| Ember | Warm earth — burnt amber, copper, sage. Dusk + Dawn variants. |
| Monochrome | High-contrast greyscale for accessibility. |

Every theme ships matching Dark **and** Light variants. The variant
is picked automatically from the terminal's background color
(`tea.RequestBackgroundColor` → `BackgroundColorMsg.IsDark()`) at
startup, so the theme reads correctly on both dark and light
terminals without configuration.

## Theme Background (opt-in)

A second setting, **Theme Background**, controls whether WinTUI
paints the terminal background with the active palette's color via
OSC 11.

- Default `terminal` — leave your terminal's background untouched.
- Opt-in `theme` — tint with the palette's `Background` (recommended
  for the "immersive" look on Catppuccin/Nord/Dracula/Tokyo Night/
  Ember/Monochrome).

Off by default because OSC 11 support varies across terminals; on
unsupported terminals this setting is a no-op. The default Sweet
Pink theme does not define a background, so enabling Theme
Background without first picking a theme is also a graceful no-op.

## Architecture

The theme engine is built on a `Palette` + `Theme` model. A single
`setActiveTheme(id, bgIsDark)` rebinds every package-level color var
and rebuilds every cached `lipgloss.Style`. The ~100 inline
`lipgloss.NewStyle().Foreground(accent)` call sites across 14 files
pick up changes automatically.

Model-held styles (spinners, text inputs, viewport border, progress
bar, all six help model slots) ride a `themeAware` interface — every
screen returns an updated value via `applyTheme() screen` and cascades
into its sub-components. A conformance test asserts every screen
reachable via `createScreen` implements the interface, so future
screens can't silently miss the retheme path.

The animated header gradient is now generated via `lipgloss.Blend1D`
from two endpoint colors per theme, instead of eight hand-picked
ANSI codes.

`v.WindowTitle = "WinTUI"` sets the OS window title.

## Visible polish picked up along the way

- All six help bar slots are now styled (`Short/Full × Key/Desc/
  Separator`). Earlier versions left `FullKey`/`FullDesc`/
  `FullSeparator` at bubbles defaults.
- Help separator now sources from the palette `dim` slot instead of
  hardcoded ANSI 238, so Monochrome and Ember Dawn read correctly.


## Verification

VirusTotal scans of the published artifacts for v2.8.0 (run 2026-05-20):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.8.0_windows_amd64.exe` (6.8 MB) | `1d4bf22786a2…` | 2/75 | [VT report](https://www.virustotal.com/gui/file/1d4bf22786a2f6f0261c33509995bbb55933e4ebf93d037181f05798b2a6e2a5) |
| `wintui_2.8.0_windows_amd64.zip` (2.5 MB) | `cea069630a27…` | 1/75 | [VT report](https://www.virustotal.com/gui/file/cea069630a27510fb848690fb74abc9a13d259f7a847105aa2369dff5d753a39) |
| `wintui_2.8.0_windows_arm64.exe` (6.3 MB) | `4a9269ff526a…` | 1/75 | [VT report](https://www.virustotal.com/gui/file/4a9269ff526a81674010732918d5f1d4e0d52759b85ed1db94ae8ce2040d01ad) |
| `wintui_2.8.0_windows_arm64.zip` (2.2 MB) | `1a46e7d201d9…` | 0/75 | [VT report](https://www.virustotal.com/gui/file/1a46e7d201d9aaff34d9344e3eec2a302a746be1e8a7513e350da777f6c2b295) |

Detections at scan time were single-vendor low-signal ML/reputation noise (Bkav, Trapmine, McAfeeD). Microsoft Defender returned clean across all artifacts.

Full SHA256 hashes:

- `wintui_2.8.0_windows_amd64.exe`: `1d4bf22786a2f6f0261c33509995bbb55933e4ebf93d037181f05798b2a6e2a5`
- `wintui_2.8.0_windows_amd64.zip`: `cea069630a27510fb848690fb74abc9a13d259f7a847105aa2369dff5d753a39`
- `wintui_2.8.0_windows_arm64.exe`: `4a9269ff526a81674010732918d5f1d4e0d52759b85ed1db94ae8ce2040d01ad`
- `wintui_2.8.0_windows_arm64.zip`: `1a46e7d201d9aaff34d9344e3eec2a302a746be1e8a7513e350da777f6c2b295`

