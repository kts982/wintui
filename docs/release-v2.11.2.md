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

Post-build verification of the published artifacts (2026-08-28):

- **Live Windows Defender clean** on both published exes: `MpCmdRun -Scan
  -ScanType 3 -DisableRemediation`, platform 4.18.26070.9, signatures
  1.457.375.0, exit 0 / "found no threats" for amd64 and arm64.
- **Build provenance verified**: `gh attestation verify` exits 0 for both
  published exes, and the `wintui_provenance.intoto.jsonl` subjects cover all
  four artifacts with digests matching the downloaded bytes.
- **VirusTotal Microsoft engine clean on every artifact** — the first
  zero-detection arm64 exe to date; amd64 carries only the usual
  single-vendor ML noise (see table below).
- A WDSI "check latest detections" submission of the published amd64 bytes is
  filed at release time to preempt a delayed FastPath verdict; the published
  exe is re-scanned locally around T+3d.

## Verification

VirusTotal scans of the published artifacts for v2.11.2 (run 2026-08-28):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.11.2_windows_amd64.exe` (7.6 MB) | `7b6b20aff8ad…` | 2/71 | [VT report](https://www.virustotal.com/gui/file/7b6b20aff8ad2a647a3df2a958b01efdd4172bd41ddf82fa384ffcd5dd3e0cc1) |
| `wintui_2.11.2_windows_amd64.zip` (2.7 MB) | `eeae59b7ffe2…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/eeae59b7ffe24a072485f03ea16ac650c021eb7bda23eaac87ef82944c1de60a) |
| `wintui_2.11.2_windows_arm64.exe` (7 MB) | `e36701160cd3…` | 0/69 | [VT report](https://www.virustotal.com/gui/file/e36701160cd3ff0d21e7b50a692db765d831c6fb4d09d9624cf1ab0ede976a9b) |
| `wintui_2.11.2_windows_arm64.zip` (2.5 MB) | `bf307bdded0e…` | 0/67 | [VT report](https://www.virustotal.com/gui/file/bf307bdded0e168b97cd0c1d5099952e7f6d0e1fbbe41a9dc7ce0dd3f6318377) |

Detections at scan time were single-vendor low-signal ML/reputation noise. Microsoft Defender returned clean across all artifacts.

Full SHA256 hashes:

- `wintui_2.11.2_windows_amd64.exe`: `7b6b20aff8ad2a647a3df2a958b01efdd4172bd41ddf82fa384ffcd5dd3e0cc1`
- `wintui_2.11.2_windows_amd64.zip`: `eeae59b7ffe24a072485f03ea16ac650c021eb7bda23eaac87ef82944c1de60a`
- `wintui_2.11.2_windows_arm64.exe`: `e36701160cd3ff0d21e7b50a692db765d831c6fb4d09d9624cf1ab0ede976a9b`
- `wintui_2.11.2_windows_arm64.zip`: `bf307bdded0e168b97cd0c1d5099952e7f6d0e1fbbe41a9dc7ce0dd3f6318377`

