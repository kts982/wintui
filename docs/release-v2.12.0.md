# WinTUI v2.12.0 Release Notes

**CLI Control & Status.** The headless CLI becomes the day-to-day control
plane: `wintui config` edits any global setting, `wintui rules` manages
per-package rules, and `wintui cleanup scan` reports what the Cleanup tab
would reclaim — all without launching the TUI, and without adding any
destructive automation. Underneath, `settings.json` is now safe for two
writers (a running TUI plus CLI commands), and per-package rule keys are
matched case-insensitively like winget IDs. One behavior change: the Core
Temp cleanup age floor drops from 7 days to 1 day and becomes a setting
(`cleanup_min_age`). Pure Go throughout: the Win32 / syscall symbol surface
is identical to v2.11.2.

## `wintui config` — global settings from the CLI

- `config list | get <key> | set <key> <value> | unset <key>`, over the same
  16 keys the Settings tab edits, from one registry — no second table to
  drift. `list` groups them Common / Advanced / Appearance / Cleanup.
- Human-readable values, case-insensitive on input: `install_mode silent`,
  `source all`, `scope machine`, `auto_elevate off`, `cleanup_min_age 3d`,
  `theme nord`. Booleans accept `on`/`off`, `yes`/`no`, `1`/`0`.
- Strict validation: an invalid value is an error and writes nothing —
  nothing is normalized silently. `unset` restores WinTUI's real default,
  which is not always empty (`source` defaults to `winget`).
- A hand-edited value the registry does not recognise is shown raw with
  `(invalid)` (`"valid": false` in JSON) and turns the `wintui doctor`
  Settings row to WARN; `set` or `unset` clears it.
- `--json`: `list` is `{count, settings[]}`; `get`/`set`/`unset` return one
  self-describing object (`key`, `value`, `default`, `is_default`, `valid`,
  `type`, `values`, `group`, `description`). Keys and values complete in the
  shell.

## `wintui rules` — per-package rules from the CLI

- `rules list` — every explicit rule (ID / SOURCE / POLICY / HOLD / SCOPE /
  ARCH / ELEVATE). `rules show <id>` prints the explicit rule **and** the
  effective value of every field, so what a package inherits from the global
  settings is visible; no rule is not an error.
- `rules set <id> --policy ask|auto|hold --scope inherit|user|machine
  --architecture inherit|x64|x86|arm64 --elevate inherit|always|never
  --ignore-version <v>` changes only the fields you pass, in one write.
  `rules clear <id> [field…]` removes the whole rule or named fields.
- Holds are one explicit state — none, all (`--policy hold`), or a single
  version (`--ignore-version`). Changing the policy to `ask` or `auto` never
  touches a version hold, so a version-scoped hold cannot silently become a
  permanent one; conflicting combinations are refused rather than stored
  invisibly. A legacy `ignore: true` reads as a permanent hold and is
  rewritten in the canonical form the next time the rule is edited.
- `rules show` evaluates a version hold against the available version from
  the read-only package cache when it is warm (`active` / `inactive`) and
  says `cannot evaluate` when it is not. Read commands never call winget.
- `--json` with `rule` (`null` when none), `effective`, `conflicts`, and an
  explicit `hold` union (`kind: none|all|version`). `wintui show <id>` is
  unchanged and stays the argv-level diagnosis.

## `wintui cleanup scan` — read-only cleanup status

- Measures what every registered Cleanup-tab target would reclaim: TARGET /
  GROUP / ENABLED / SIZE / ITEMS / KEPT / ADMIN / STATUS. `--enabled` limits
  it to the set that starts checked in the TUI (default-checked plus opted-in
  targets — the set a TUI deletion would act on); `--target <id>` picks
  targets and completes in the shell.
- It never deletes and never elevates: deletion stays in the TUI, where each
  removal is reviewed and confirmed. Admin-only targets are reported as
  `needs_admin` instead of being walked unelevated (a partial walk would
  report a wrong size); run from an elevated terminal to measure them.
- Honest sizes: a walk that skipped unreadable entries is `partial` with a
  lower-bound size (`≥`), and a Core Temp target whose entries are all newer
  than the age floor is `recent` with the KEPT column saying what was left
  alone — never a bare `0 B` that contradicts Explorer.
- Bounded concurrency, a one-line progress note on stderr, pipeable stdout,
  exit 0 (a report, not a predicate). `--json` uses a stable scan shape with
  string statuses, explicit `null` for unmeasured sizes, zeros present, and
  arrays never `null`.

## Cleanup: configurable age floor (behavior change)

- The five Core Temp targets (user Temp, Windows Temp, user crash dumps, the
  Windows Error Reporting queue, system minidumps) carried a hard-coded 7-day floor that no surface mentioned, so a week of
  churn survived every run and the resulting "0 B" read as a lie. The floor
  is now the `cleanup_min_age` setting — `off` / `1d` / `3d` / `7d` — in the
  Settings tab and `wintui config`, with **`1d` as the new default**. More
  gets deleted than before; `7d` restores the previous rule.
- Every surface reports what the floor kept: the Cleanup row shows
  `0 B · N recent`, the detail pane an `[older than 1 day]` chip and a kept
  line, the results screen "nothing older than 1 day; N entries kept", and
  the CLI a KEPT column plus `kept_items` / `kept_bytes` /
  `total_kept_bytes` / `cleanup_min_age` in JSON.
- Elevated-helper requests carry the floor, so an over-the-shoulder UAC
  elevation applies the current user's setting, not the admin account's.
- A non-elevated TUI no longer walks admin-only targets: the row says
  `needs admin` and the detail pane explains, matching `cleanup scan`
  (previously a failed walk rendered as an honest-looking `0 B`).

## Settings: safe for two writers

- Whole-file writes (`settings.json`, `cache.json`, `history.json`) use a
  unique temp file plus rename, with a bounded retry on Windows sharing
  errors, instead of a fixed `.tmp` name that concurrent TUI and CLI writers
  could collide on. Crash leftovers older than an hour are swept once per
  invocation.
- CLI commands and every TUI write point outside the Settings screen
  (per-package rule edits, cleanup toggles, version-hold expiry, `wintui
  theme`) now write a **delta**: reload the file, apply one change, save —
  so a TUI open in another window does not lose a key the CLI just set, and
  a stale TUI snapshot no longer reverts a CLI change on the next toggle. A
  file that cannot be read or parsed is never overwritten with defaults.
- A running TUI picks up external changes on explicit refresh (`r`) and when
  the Settings tab is opened, unless the Settings tab holds unsaved edits.
  The `wintui doctor` / Health Settings row warns while `settings.json` has
  changed on disk since the process started, when it does not parse, or when
  it holds conflicting rule keys.

## Case-insensitive package rules

- winget IDs are case-insensitive and every CLI path accepted any casing,
  but the stored rule lookup was effectively case-sensitive, so a typed
  `git.git` silently missed a `Git.Git` rule. Lookup now resolves under a
  total precedence (exact qualified → exact bare → case-insensitive
  qualified → case-insensitive bare) with deterministic tie-breaking.
- A write keeps the casing of the key it finds, takes winget's own casing
  for new rules from the package cache, and leaves exactly one key per
  package (legacy bare-ID and case-variant aliases are removed). Value-
  identical duplicates from hand edits are collapsed on load; conflicting
  ones are left in place, listed by `wintui doctor`, and refused by `rules
  set` until `rules clear <id>` removes them together.

## Hardening ride-alongs (zero surface)

- One constructor now builds all three inline PowerShell launches (toast
  send, toast shortcut setup, self-update handoff): it enforces the 32,767-
  unit `CreateProcess` ceiling and pins the absolute Windows PowerShell 5.1
  path, so no call site can skip the length check or fall back to PATH
  resolution.
- A real-argv transport test parses the exact command line handed to
  `CreateProcess` back through `CommandLineToArgvW` for all three rendered
  scripts (test binary only — the product binary gains no import).

## Dependencies (September refresh)

- **go-runewidth 0.0.28** — fixes a start-up and dirty-memory regression
  (an eagerly built lookup table introduced in 0.0.26/0.0.27) that v2.11.1
  and v2.11.2 shipped; every `wintui check` at logon paid it. No width
  changes.
- **bubbletea v2.0.9 / bubbles v2.2.1 / lipgloss v2.0.6** — routine refresh
  (bubbles: textarea word-left boundary fix; lipgloss 2.0.6 pins the
  ultraviolet 2026-08-11 snapshot).
- CI only: CodeQL action and `attest-build-provenance` v4.2.2 (SHA-pinned
  bumps; provenance subject handling unchanged).
- Go toolchain unchanged at **1.26.6**, so the artifact delta stays
  attributable to the feature work. The Go 1.27 directive bump ships in its
  own isolated maintenance release (it adds a new ws2_32 import).

## Compatibility

- `settings.json`, `cache.json`, and `history.json` formats are unchanged and
  load as before. Legacy plain-ID rule keys are still read; the only on-disk
  rewrites are the canonical `update_policy: hold` for a legacy
  `ignore: true` (on the next edit of that rule) and the one-shot collapse
  of value-identical case-variant duplicates.
- **Behavior change:** the Core Temp cleanup floor is 1 day instead of 7.
  Set `cleanup_min_age` to `7d` to keep the old rule.
- No keybinding changes. The Settings tab gains a Cleanup Min Age row.
- Zero new Win32 / COM / syscall surface: the `go tool nm`
  `syscall.`/`windows.` symbol set is **identical** to v2.11.2 (438 unique
  symbols under the release-playbook filter, before and after).

## Known gaps and what is next

- In a non-elevated TUI, admin-only cleanup targets are never queued for
  deletion (only measured targets with a size are queued), so the elevated-
  helper delete path is unreachable from a default run. Pre-existing since
  v2.7.0, not a regression; the fix pairs admin-target queueing with a
  persistent opt-out for default-checked rows and is scheduled for v2.12.x.
- Headless cleanup deletion stays deferred until `cleanup scan` usage proves
  demand.
- **v2.12.1** follows promptly with the elevation-safety tier split out of
  this release: under an elevated WinTUI the `fix --portable` pins cannot
  de-elevate, so the refusal tier completes that feature.

## Verification

Automated coverage for this release: concurrent-writer and stale-snapshot
acceptance tests for `settings.json`; generated registry tests (every key has
a group and a valid default, every choice round-trips, every CLI value
persists); byte-golden JSON for `config`, `rules`, and `cleanup scan`;
field-wise `rules set` reaching the real consumers; hold exclusivity in every
flag order; case-insensitive lookup precedence and collapse; every cleanup
scan status over temp-dir targets with the real engine; the TUI reload
semantics; and the real-argv PowerShell transport round trip.

The pre-release `go tool nm` comparison against v2.11.2 is identical: the same
set of unique `syscall.` / `windows.` symbols before and after (438 under the
release-playbook filter), with no additions or removals. The release build ran
on Go 1.26.6 as pinned by `go.mod`.

Post-build verification of the published artifacts (2026-09-12):

- **Live Windows Defender clean** on both published exes: `MpCmdRun -Scan
  -ScanType 3 -DisableRemediation`, engine 1.1.26080.3, signatures
  1.459.171.0, exit 0 / "found no threats" for amd64 and arm64.
- **Build provenance verified**: `gh attestation verify` exits 0 for both
  published exes, and the `wintui_provenance.intoto.jsonl` subjects cover all
  four artifacts with digests matching the downloaded bytes and
  `checksums.txt`.
- **VirusTotal**: arm64 exe and zip are 0 detections. The amd64 exe carries
  the usual single-vendor ML noise plus a VirusTotal Microsoft-engine
  `Wacatac.B!ml` verdict; VirusTotal runs that engine in a configuration that
  differs from shipping Defender, and the live Defender scan of the identical
  bytes above is clean, so it is treated as an engine false positive per the
  release playbook. The published exe is re-scanned locally around T+3d.
- A WDSI "check latest detections" submission of the published amd64 bytes is
  filed at release time to preempt a delayed FastPath verdict.

## Verification

VirusTotal scans of the published artifacts for v2.12.0 (run 2026-09-12):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.12.0_windows_amd64.exe` (7.8 MB) | `310743fd020c…` | 3/71 | [VT report](https://www.virustotal.com/gui/file/310743fd020c6861917fd64aee98790617e4f0b7962c13dc2431262170f1a079) |
| `wintui_2.12.0_windows_amd64.zip` (2.8 MB) | `6e45f1f6f47a…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/6e45f1f6f47ae0345a593361da1f0ef4ac30d15f6ebd6688024aa7490022d3b2) |
| `wintui_2.12.0_windows_arm64.exe` (7.2 MB) | `5bc0c75c82de…` | 0/69 | [VT report](https://www.virustotal.com/gui/file/5bc0c75c82de885434559fec698930afe53798f7aa35efae2620d9ec5c5a2c5f) |
| `wintui_2.12.0_windows_arm64.zip` (2.6 MB) | `11d8bcc953a6…` | 0/67 | [VT report](https://www.virustotal.com/gui/file/11d8bcc953a63774b2d167b3830187101048f1c38f98f7b9e65a0fae8741c6ef) |

Detections at scan time were single-vendor low-signal ML/reputation noise plus a VirusTotal Microsoft-engine `Wacatac.B!ml` verdict on the amd64 exe only; live Windows Defender on the identical bytes is clean (see above), so it is treated as an engine false positive.

Full SHA256 hashes:

- `wintui_2.12.0_windows_amd64.exe`: `310743fd020c6861917fd64aee98790617e4f0b7962c13dc2431262170f1a079`
- `wintui_2.12.0_windows_amd64.zip`: `6e45f1f6f47ae0345a593361da1f0ef4ac30d15f6ebd6688024aa7490022d3b2`
- `wintui_2.12.0_windows_arm64.exe`: `5bc0c75c82de885434559fec698930afe53798f7aa35efae2620d9ec5c5a2c5f`
- `wintui_2.12.0_windows_arm64.zip`: `11d8bcc953a63774b2d167b3830187101048f1c38f98f7b9e65a0fae8741c6ef`

