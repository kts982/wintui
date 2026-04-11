# WinTUI v2.2.0 Release Notes

WinTUI v2.2.0 introduces per-package rules, an ignore toggle, and a live command preview so you can see exactly what winget is about to run.

## Highlights

- **Per-package rules editor (`p`)** in the detail view. Override `scope`, `architecture`, or `elevate` for a single package without changing your global Settings — rules are stored in `%APPDATA%\wintui\settings.json` under source-qualified keys (`winget:Git.Git`), with legacy plain-ID keys still honored on read.
- **Ignore toggle (`i`)** in the detail view. Hide a package — or just its current available version — from the Updates list. The Updates section shows a `(N hidden)` count on its header, and stale `ignore_version` entries expire automatically when a newer version ships.
- **Command preview** always visible in the detail view, showing the exact `winget install` or `winget upgrade` command that would run with all active overrides applied. The fastest way to verify that a rule like `scope=machine` is actually reaching the command line.
- **Batch confirm modal `?` toggle** expands the same per-item command preview for every staged package, so you can audit a full batch before pressing Enter.
- **Scrollable batch modal** — `↑↓`, `PgUp/PgDn`, `Home/End` scroll the body when large batches overflow the available height, with a footer hint showing the visible range. The actions line and bottom border always stay in view.

## Docs

- New [Per-package rules reference](../docs/package-rules.md) — full list of supported rules, the `i` toggle's state machine, an example `settings.json`, and behavior notes.

## Notes

- No workflow or keybinding regressions from v2.1.x. `i` still installs the focused queued package on the Packages screen — the detail-view binding is context-specific.
- Per-package rules and ignore filtering apply to the TUI only. The headless `--check` / `--list` CLI paths still report every upgradeable package straight from winget.
