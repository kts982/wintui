# WinTUI v2.3.1 Release Notes

WinTUI v2.3.1 is a patch release focused on execution feedback and recovery ergonomics — live progress while packages install, clearer guidance when an app blocks its own upgrade, and cleanup of the self-upgrade helper's visible-window paper cut.

## Highlights

- **Live execution feedback** — the batch modal no longer shows just a spinner for long installs. Each running item now displays the latest winget output line (download URLs, verification status, installer output) plus elapsed time. When winget emits an explicit percent, a progress bar is shown instead.
- **Process-in-use guidance and retry** — when winget reports that an app is blocked because the app is still running (errors `0x80073d02`, `0x8a150052`, `0x8a150066`, or messages containing "is in use"), the result modal now shows a clear hint: _"Close the running application and press ctrl+e to retry."_ `Ctrl+E` is the same key used for elevation retries — it now covers process-blocked items too, adapting its label (`retry` vs `retry elevated`) to what the batch needs.
- **Self-upgrade blank window fixed** — during a portable-package self-upgrade, the helper process was spawning winget.exe in a new visible console that could linger briefly. The helper now hides winget's console explicitly, and the fix extends to the elevated helper path so self-upgrades that elevate don't reintroduce the blank window either.

## Bug Fixes

- `Ctrl+E` now fires on the result modal when only process-blocked items need retrying; previously the shortcut was advertised but the handler and help bar only offered it when elevation candidates were present.
- `Ctrl+E` no longer forces a UAC prompt for process-blocked-only retries — those stay non-elevated. Mixed batches (elevation + process-blocked) still request elevation as before.
- `0x8A150066` (multi-uninstall failure) now maps to a friendly message instead of a raw exit code.

## Test coverage

- Three new handler-level tests around `Ctrl+E`: blocked-only (no UAC), elevation-only (UAC), mixed (UAC + both items collected).
- New tests for process-in-use detection, progress percent extraction, and the progress sentinel JSON round-trip.

## Notes

- No breaking changes to settings, CLI flags, or keybindings from v2.3.0.
- Automatic process-kill (issue #26) remains deferred to a future release — this patch gives clear guidance and a one-keystroke retry, leaving the process-matching heuristics for when they've had more real-world testing.
