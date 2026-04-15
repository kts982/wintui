# WinTUI v2.3.0 Release Notes

WinTUI v2.3.0 adds visual override indicators, fixes multi-version uninstall, and includes a major codebase quality pass with new typed enums and 30+ new tests.

## Highlights

- **Override indicator (`⚙`)** — packages with per-package rules (scope, architecture, elevate, ignore) now show a warm-yellow gear glyph in the package list. The same override details appear in both the summary panel (while browsing) and the full detail panel, styled with a dedicated override color. Press `p` from the package list to jump straight into the rules editor.
- **Multi-version uninstall** — packages with multiple installed versions (e.g. side-by-side installs) are now handled correctly. WinTUI detects duplicate entries at staging time, collapses them into a single batch item, and passes `--all-versions` to winget. Previously the second uninstall command would fail with `0x8A150066`.
- **Search results dismissal** — press `Esc` to clear search results and return to the normal installed package list. Previously search results could only be cleared by entering an empty search or selecting every result.
- **Stale search error fix** — a successful search now clears any prior error banner. Previously a failed search would leave its error message stuck in the top bar even after a subsequent successful search.

## Code Quality

- **Workspace split** — the 1900-line `workspace.go` has been split into five focused files: `workspace.go` (types and update router), `workspace_view.go` (rendering), `workspace_actions.go` (user actions), `workspace_search.go` (search input), and `workspace_batch.go` (data loading and batch execution).
- **Typed enums** — string-based dispatches for action types, install scope, install mode, and CPU architecture are now typed string aliases (`retryOp`, `InstallScope`, `InstallMode`, `CPUArchitecture`) with named constants, providing compile-time safety.
- **Test coverage** — 30+ new tests covering workspace batch lifecycle (modal transitions, cancellation, incremental updates), search/queue flows (toggle, stale discard, display ordering), and navigation (cursor movement, data message state transitions, detail panel open/close).
- **Test safety** — `TestMain` now backs up and restores `settings.json` and `cache.json` so that `go test` no longer overwrites the developer's real configuration.

## Bug Fixes

- Fixed stale search error banner persisting after a successful search.
- Fixed uninstall of packages with multiple installed versions failing with `0x8A150066`.
- Fixed `go test` silently overwriting user's `settings.json` and `cache.json` with test defaults.
- Added friendly error message for winget error `0x8A150066` (multiple uninstall failed).

## Notes

- The `p` key now works from both the package list (opens detail panel directly into the rules editor) and the detail view (as before).
- The `Esc` key progressively backs out of state: search input → search results → normal list.
- No breaking changes to settings, CLI flags, or keybindings from v2.2.x.
