# WinTUI v2.9.1 Release Notes

**Hardening & correctness.** A security and robustness pass over the paths
that elevate, self-update, run external commands, and mutate shared settings.
No new features and no breaking changes — `settings.json` and `cache.json` are
unchanged, and normal install / upgrade / uninstall behavior is identical. The
binary surface is unchanged from v2.9.0 (no new Win32 imports, no new
dependencies).

## Security hardening

- **Elevated-helper contract.** The long-lived elevated helper now requires a
  per-session token on every request and validates winget arguments against a
  strict allowlist — a known verb plus the exact flag shapes WinTUI itself
  emits. Unknown actions and installer-escalation flags (`--override`,
  `--custom`, `--location`, `--manifest`, …) are rejected. Least privilege for
  the privileged process.
- **Pinned binaries.** `winget` and `powershell.exe` are resolved to absolute
  system paths before every spawn, and `winget` is validated to live under a
  known WindowsApps location. The elevated path errors rather than running an
  unpinned binary picked up from `PATH`.
- **Self-update fails closed.** The self-upgrade handoff aborts with an
  actionable error if winget can't be resolved to a trusted path, instead of
  falling back to a bare name that PowerShell would re-resolve off `PATH`. The
  canary-only rehearsal manifest override is now compiled out of release builds
  entirely.

## Correctness & robustness

- **No more 1000-package cap** — `wintui list` and the installed view no longer
  silently truncate inventories larger than 1000 packages.
- **Settings mouse clicks** route to the correct row regardless of header
  height (full vs compact layout).
- **Deterministic winget error messages** — when winget output mentions more
  than one known error code, the friendly mapping is now stable across runs.
- **`wintui doctor`** gains a *State dirs* row that surfaces config / cache
  directory-creation failures that were previously silent (the symptom used to
  be settings or cache simply not saving).
- Internal robustness: a data race between the UI loop and background refresh
  over shared settings is fixed; the elevation pipe is serialized behind a
  single reader with cancellable contexts (a stuck elevated action can now be
  cancelled, and a dropped connection no longer leaks a handle); import
  ids/versions and cleanup target roots are validated before use.

## Verification

<!-- VirusTotal scan table appended at release time by scripts/vt-scan.ps1 -->
