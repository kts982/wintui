# WinTUI v2.1.0 Release Notes

WinTUI v2.1.0 focuses on day-to-day package workflow polish and better in-app release visibility.

## Highlights

- Added a unified `g` apply flow on the Packages screen so staged installs, upgrades, and uninstalls can run in one mixed batch.
- Normalized Packages screen actions: `Space` stages, `g` applies, and `i` / `u` / `x` remain direct accelerators for focused actions.
- Added self-upgrade handoff for the installed WinGet build of WinTUI so `kts982.WinTUI` can replace its own running portable `.exe`.
- Improved package details with target-aware `What's New in <version>` headings and direct release-notes links via `n` when a package exposes `ReleaseNotesUrl`.
- Fixed detail overlay scrolling on smaller terminals so long descriptions and release notes remain reachable.

## Package Details

- WinTUI now parses and displays `Release Notes Url` metadata from `winget show`.
- `o` still opens the package homepage.
- `n` opens release notes directly when the package provides a release-notes URL.
- WinTUI's local winget manifest now includes inline release notes so the package can show shipped changes directly in its own details panel.

## Upgrade Notes

- The special self-upgrade flow only activates when WinTUI is running from the installed WinGet path.
- Repo builds and other non-installed binaries still treat `kts982.WinTUI` as a normal package entry.
