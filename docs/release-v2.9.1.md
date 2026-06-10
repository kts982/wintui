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

VirusTotal scans of the published artifacts for v2.9.1 (run 2026-06-10):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.9.1_windows_amd64.exe` (7.4 MB) | `e33374535d58…` | 3/71 | [VT report](https://www.virustotal.com/gui/file/e33374535d58bd30f53273f43a746e527aea2119840b36b1f421bfdb2839f499) |
| `wintui_2.9.1_windows_amd64.zip` (2.7 MB) | `37ab7b2ef0e9…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/37ab7b2ef0e98a48724d022b21b799e7453fc3f51046a4e4f70ded78d777116a) |
| `wintui_2.9.1_windows_arm64.exe` (6.8 MB) | `58e6c759c645…` | 1/69 | [VT report](https://www.virustotal.com/gui/file/58e6c759c64524715e3e8052963457a0a4d48e9628137a7155632850ee7f43b4) |
| `wintui_2.9.1_windows_arm64.zip` (2.4 MB) | `0dbbe0baba8e…` | 0/67 | [VT report](https://www.virustotal.com/gui/file/0dbbe0baba8ea35771eb3174e1a0cd1a707f0912238dd03fb9de975cd6e506a3) |

Detections at scan time were low-signal noise on an unsigned Go binary. Bkav,
Trapmine, and McAfeeD are single-vendor ML/reputation engines that self-resolve
as the SHA256 gains reputation. The Microsoft `Wacatac.B!ml` hit on `amd64.exe`
is a VirusTotal-engine artifact — VirusTotal discloses that its Microsoft engine
"may differ from the commercial off-the-shelf product," running a more
aggressive ML configuration, and `Wacatac/Wacapew.C!ml` is a generic
machine-learning bucket. Live Windows Defender (the commercial product, engine
1.1.26050.11 / signatures 1.453.21.0) scans the published `amd64.exe`
(`e33374…`) **clean — "found no threats"**. The snapshot build of the same code
did not carry the Microsoft hit at all; the engine reacts to the per-file
version-string bytes, not to behavior.

Full SHA256 hashes:

- `wintui_2.9.1_windows_amd64.exe`: `e33374535d58bd30f53273f43a746e527aea2119840b36b1f421bfdb2839f499`
- `wintui_2.9.1_windows_amd64.zip`: `37ab7b2ef0e98a48724d022b21b799e7453fc3f51046a4e4f70ded78d777116a`
- `wintui_2.9.1_windows_arm64.exe`: `58e6c759c64524715e3e8052963457a0a4d48e9628137a7155632850ee7f43b4`
- `wintui_2.9.1_windows_arm64.zip`: `0dbbe0baba8ea35771eb3174e1a0cd1a707f0912238dd03fb9de975cd6e506a3`

