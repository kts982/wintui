# WinTUI v2.12.1 Release Notes

**Elevation safety.** WinTUI no longer elevates packages that have to run as
you, and it now works on PCs where elevation runs as a different account:
Windows Administrator protection, or a standard user typing an
administrator's credentials. A batch costs at most one elevation prompt, the
TUI stays responsive while the prompt is open, and when an action runs
without elevation WinTUI says so and records why. winget's error codes are
now translated from winget's own reference. Pure Go throughout: the Win32 /
syscall symbol surface is identical to v2.12.0.

## User-scope packages are never elevated

- A package is user-scope when its rule says `scope: user`, the global scope
  setting is `user`, or winget's portable package folder exists for it.
  Running winget elevated for such a package goes wrong: winget can drop a
  portable package from your PATH
  ([winget-cli #4044](https://github.com/microsoft/winget-cli/issues/4044)),
  and it refuses to uninstall a user-scope package as administrator
  (`0x8A15007D`, now reported with a clear message).
- WinTUI never starts elevation for a user-scope package, or for one with an
  `elevate: never` rule: no upfront helper in silent mode, no automatic
  retry, no `Ctrl+E` retry. The result modal says why.
- In a WinTUI that is already elevated, such an action is skipped before
  winget runs, with "run WinTUI non-elevated" in the message. History records
  it as `skipped`. `upgrade --all` and `--auto` still exit 0, while
  `upgrade --id` exits 1, as for a held package. Import skips these packages
  the same way.
- An explicit `elevate: always` rule still elevates install and upgrade of
  that package, because it is the most specific setting; it never applies to
  uninstall. `wintui rules show`, `rules set` and the TUI detail panel label
  the combination as overriding the user-scope safety.
- `wintui fix --portable` leaves a package with an explicit `elevate: always`
  rule alone and lists it (`kept_elevate_always` in `--json`) instead of
  overwriting the rule.

## When elevation runs as a different account

Under Windows Administrator protection, elevation runs as a hidden
`ADMIN_<user>` account; with over-the-shoulder UAC it runs as the
administrator who typed the credentials. That account cannot see your
per-user installs, and anything per-user it installs lands in its own
profile, where you cannot see or run it.

- The elevated helper now connects in both cases (its pipe also admits
  Administrators, which grants nothing to unelevated processes) and reports
  which account it runs as. A helper that fails this check is dropped.
- Per-user actions stay off such a helper. Up front it takes only explicit
  machine-scope packages, and the requires-admin retry never takes user
  scope. An `elevate: always` rule elevates there only with `scope: machine`;
  otherwise the action runs as you, starting with a line that says why.
- The helper creates no profile state before its authenticated connection,
  so the other account's profile gets no WinTUI folders.
- winget is found even when that account has no `winget` alias on PATH yet
  (fallback to the App Installer package folder).
- The `wintui doctor` Privileges row says when elevation runs as a different
  account.

## One prompt per batch, and no silent fallbacks

- A batch costs at most one elevation prompt. A declined prompt, a helper
  that does not connect within 60 seconds, or one that stops responding
  counts for the rest of the batch instead of prompting again for every
  item; a `Ctrl+E` retry starts fresh. The TUI stays live while the prompt
  is open.
- When an action runs without elevation after all, its first output line
  says why ("Running without elevation: elevation was cancelled", "…: the
  elevated helper did not connect within 60 seconds"), the results modal
  shows it, and history keeps it in the item's notes. Routing a package
  around another account's helper is not a failure and adds no note.
- `Esc` during a running batch now marks every unfinished item as skipped
  ("cancelled"), matching history, and the title says "<Action> Cancelled".
  A batch where every item was skipped is titled "<Action> Skipped".

## winget error messages

- The winget code table is rebuilt from winget's `returnCodes.md`; several
  codes were mislabelled. `0x8A150019` means "this action requires
  administrator privileges", and `0x8A150056` means the installer refuses to
  run elevated. WinTUI used to offer an elevated retry for the latter, which
  could never help.
- WinTUI now matches winget's own exit code (which Go reports in decimal)
  before any code mentioned in the output, so failures get a readable
  message far more often. An unknown code is shown in hex.

## Fixes since v2.12.0

- Quitting no longer leaves `[?1u` at the PowerShell prompt in Windows
  Terminal 1.25 (a kitty keyboard query answered after WinTUI stopped
  reading input).
- Settings tab: reset moves to `Shift+R` and restores only the global
  settings, keeping per-package rules and cleanup opt-ins. Before, `r`
  dropped every rule in memory, so held packages were listed as updates
  after a refresh, and saving afterwards erased the rules on disk.

## Known issue

Packages that another updater installs can be listed by `winget upgrade`
but fail to upgrade by ID. Example: PowerShell with Microsoft Update turned
on, which installs it through a wrapper that winget matches only by name.
The upgrade fails with "no installed package found matching input criteria
(`0x8A150014`)" ([winget-cli #4718](https://github.com/microsoft/winget-cli/issues/4718)).
Update such a package through its own updater, or hold it in WinTUI
(`wintui rules set <id> --policy hold`). A clearer message and an automatic
hold are planned.

## Dependencies

- bubbletea 2.1.1: security fix
  [GHSA-qc58-pfg9-7pph](https://github.com/charmbracelet/bubbletea/security/advisories/GHSA-qc58-pfg9-7pph)
  (terminal size query). 2.1.0 fixed a shutdown deadlock that could leave the
  terminal unrestored.
- go-winio 0.6.4 (0.6.3 fixed a pipe-listener close race; the elevated
  helper talks over a named pipe).
- Go 1.26.10, including the 1.26.9 and 1.26.10 security releases; govulncheck
  reports no vulnerabilities.
- go-runewidth 0.0.31, x/sys 0.49.0, x/text 0.43.0; CI actions refreshed.
- The Win32 / syscall symbol set is identical to v2.12.0 on the same
  toolchain.

## Verification

VirusTotal scans of the published artifacts for v2.12.1 (run 2026-11-01):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.12.1_windows_amd64.exe` (7.9 MB) | `d1e3985756bf…` | 2/71 | [VT report](https://www.virustotal.com/gui/file/d1e3985756bfba184eb914db23a49b73fe65997f9f56eae04e7bf9ccca688f75) |
| `wintui_2.12.1_windows_amd64.zip` (2.9 MB) | `4dbbd4036fc9…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/4dbbd4036fc905764d0bffd725afaed3a061e72bc0332e9fe7210b6c6982a123) |
| `wintui_2.12.1_windows_arm64.exe` (7.3 MB) | `b30714212fff…` | 0/69 | [VT report](https://www.virustotal.com/gui/file/b30714212fff229362a181678c3de5764e70c2270eede4d58948295f16b4d354) |
| `wintui_2.12.1_windows_arm64.zip` (2.6 MB) | `97e73a5946c4…` | 0/67 | [VT report](https://www.virustotal.com/gui/file/97e73a5946c4dba1a4b498b401ff2bf510ea6f763ac42121fa61cdb20df8dd9d) |

Detections at scan time were single-vendor low-signal ML/reputation noise (Bkav, Trapmine). Microsoft Defender returned clean across all artifacts, on VirusTotal and in a local scan of both published executables (signatures 1.463.70.0).

Full SHA256 hashes:

- `wintui_2.12.1_windows_amd64.exe`: `d1e3985756bfba184eb914db23a49b73fe65997f9f56eae04e7bf9ccca688f75`
- `wintui_2.12.1_windows_amd64.zip`: `4dbbd4036fc905764d0bffd725afaed3a061e72bc0332e9fe7210b6c6982a123`
- `wintui_2.12.1_windows_arm64.exe`: `b30714212fff229362a181678c3de5764e70c2270eede4d58948295f16b4d354`
- `wintui_2.12.1_windows_arm64.zip`: `97e73a5946c4dba1a4b498b401ff2bf510ea6f763ac42121fa61cdb20df8dd9d`

