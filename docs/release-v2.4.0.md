# WinTUI v2.4.0 Release Notes

WinTUI v2.4.0 reshapes the headless CLI into proper subcommands, adds two
new ones (`show` and `upgrade --all`), fixes a long-standing post-batch
cursor bug, and adds page-style navigation in the package list.

## Headless CLI: subcommands

The CLI is moving from two read-only flags into a real verb surface.
v2.4.0 ships the hybrid root+subcommands shape:

- `wintui` (no arguments) keeps launching the TUI — unchanged.
- `wintui check [--json]` — print upgradeable packages, exit 1 if any
  *visible* updates exist. Honors per-package ignore rules, so a hidden
  package no longer flips the exit code (this was a bug in v2.3.x).
- `wintui list [--json]` — print installed packages.
- `wintui show <id> [--source winget|msstore] [--json]` — read-only
  inspector. Prints the install and upgrade arguments WinTUI would pass
  to winget for the given package, plus any per-package overrides. Does
  not call winget; useful for debugging settings and for scripts that
  need a machine-readable view of WinTUI's effective config.
- `wintui upgrade --all` — upgrade every visible upgradeable package
  without launching the TUI. Streams winget output line-by-line under
  each package header and prints a per-package summary on completion.
  Exits 1 if any upgrade failed.

The old root flags `--check` and `--list` still work for one minor
release with a deprecation warning. They will be removed in v2.5.0.

`wintui upgrade --all` does **not** upgrade the running WinTUI binary;
it is skipped with a hint pointing at the TUI, where the v2.3.x
self-upgrade handoff is verified. To upgrade WinTUI itself, run
`wintui` and use the TUI.

See [docs/cli.md](../docs/cli.md) for the full reference.

## Package list navigation

`PgUp` / `PgDn` jump the cursor 10 rows. `Home` / `End` jump to the top
/ bottom of the cursor's *current section* (Updates or Installed) so
scanning a long Installed list does not snap the cursor back into
Updates.

## Bug fixes

- **Cursor no longer jumps to the upgraded package after a batch
  completes.** Repro: focus an upgrade in Updates (e.g. Mozilla.Firefox),
  press `u`, let the batch finish, dismiss. Previously the cursor
  followed the package across to its new row in Installed; now the
  just-upgraded item is moved to the bottom of Installed and the
  cursor resets to the top of the list. The eventual background
  refresh re-sorts to winget's natural order.
- **`--check` (and `wintui check`) now honor per-package ignore
  rules**, matching the TUI. A package the TUI hides via `i` will no
  longer flip the `--check` exit code to 1.

## Internal

- Shared upgrade planner extracted to `planner.go::selectUpgrades`;
  both `buildItems` (TUI) and the headless `check` / `upgrade --all`
  paths route through it.
- The hand-rolled `--check` + `--list` mutual-exclusion guard is
  replaced by Cobra's `MarkFlagsMutuallyExclusive` so the rule scales
  as more action flags / subcommands land.
- `runCheck` no longer calls `os.Exit` directly; the exit code flows
  through `main` via a package-level `cliExitCode`. This was the
  prerequisite that made the new subcommands testable.

## Notes

- No breaking changes to settings, keybindings, or the v2.3.x
  self-upgrade handoff from v2.3.3.
- `wintui --check` / `--list` keep working but print a deprecation
  warning; migrate scripts to `wintui check` / `wintui list`.
