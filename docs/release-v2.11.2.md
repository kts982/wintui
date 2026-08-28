# WinTUI v2.11.2 Release Notes

**Script-drop hardening.** Toast delivery, first-run toast shortcut setup, and
the self-update handoff now pass their PowerShell commands inline. WinTUI no
longer writes transient `.ps1` files for these operations, and no longer uses
`-ExecutionPolicy Bypass`.

## Hardening

- **No transient PowerShell scripts.** All three PowerShell call sites use
  `-NoLogo -NoProfile -NonInteractive -WindowStyle <style> -Command <script>`,
  with `-Command` as the final switch and the complete rendered script as one
  final argument.
- **No execution-policy override.** `-ExecutionPolicy Bypass` and `-File` are
  gone. Execution policy applies to script files, so an inline command does
  not need a process-level policy override.
- **No self-delete tail.** Rendered commands no longer reference
  `$PSCommandPath`; the self-update handoff also drops its former two-second
  pre-delete delay.
- **Process behavior preserved.** Toast PowerShell remains hidden with
  `CREATE_NO_WINDOW` and `HideWindow` (never `DETACHED_PROCESS`); self-update
  keeps `CREATE_NEW_CONSOLE`. Absolute PowerShell and winget resolution is
  unchanged.
- **Bounded command lines.** WinTUI validates the fully quoted Windows command
  line against the 32,767 UTF-16-unit `CreateProcess` ceiling before launch.
  The shipped toast, shortcut, and self-update templates remain far below it.
- **Safer diagnostics.** Toast shortcut failures record a fixed operation name
  and `transport=inline`; rendered commands and package-derived toast text are
  never written to the error log. The self-update log still records the winget
  command being attempted before the helper launches, preserving the
  post-mortem trail the script file used to provide.
- **Known tradeoff: command-line visibility.** While an inline helper runs, its
  rendered command (including package-derived toast text) is visible to local
  process enumeration — Task Manager, WMI, command-line auditing, EDR. The
  former transport exposed the same content as a transient world-readable
  `.ps1` under the user profile, so neither variant hides package metadata
  from the local machine. Documented in `docs/elevation.md`.

## Compatibility

- No settings, cache, history, CLI, or keybinding changes.
- No dependency or Go toolchain bump; release builds remain on Go 1.26.6 so
  the artifact delta stays attributable to the transport hardening.
- Cleanup of legacy `handoff-*.ps1` files created by releases before v2.11.2
  remains active for the migration window. The Cleanup tab labels these files
  as legacy data.

## Verification

Automated coverage asserts the inline argv contract, PowerShell parseability,
command-line size ceiling, exact process flags, absence of `$PSCommandPath`,
legacy cleanup, fail-closed winget resolution, and that self-update creates no
new handoff script.

The pre-release `go tool nm` comparison against v2.11.1 is identical: the same
set of unique `syscall.` / `windows.` symbols before and after (438 under the
release-playbook filter; absolute counts are filter-dependent), with no
additions or removals.

Manual local smoke testing passed for install, uninstall, update checks,
release notes, and toast delivery. The isolated-VM canary rehearsal also passed
the production-shaped `0.0.1` to `0.0.2` close/replace/manual-restart flow: the
log showed the inline handoff and `winget upgrade --manifest`, the target
version was installed, no `handoff-*.ps1` appeared, and the one-shot manifest
override was cleared.

In that run WinTUI exited before PowerShell reached `Wait-Process`, so the log
recorded that the parent PID was already gone and safely continued. This is the
normal fast-exit path and validates the v2.11.2 transport; a deliberately
slow-exiting parent was not exercised. The remaining optional environment
matrix is Restricted / RemoteSigned / AllSigned and an AppLocker or Constrained
Language Mode system.

VirusTotal, live Defender, WDSI, and final artifact hashes are added here after
the release build.
