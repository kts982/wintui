# CLI Reference

WinTUI exposes a small headless CLI for scripts, CI, and scheduled checks.
Running `wintui` with no arguments launches the interactive TUI; subcommands
run headlessly and exit.

## Subcommands

| Command | Behavior |
|---|---|
| `wintui check [--json] [--notes]` | Print upgradeable packages and exit (`--notes` renders each pending update's release notes inline) |
| `wintui list [query] [--json]` | Print installed packages and exit; with a query, show only packages whose name or id matches (like `winget list`) |
| `wintui show <id> [--source winget\|msstore] [--json]` | Print effective install/upgrade args and overrides for a single package (read-only; does not call winget) |
| `wintui upgrade --all` | Upgrade every non-held upgradeable package |
| `wintui upgrade --auto` | Upgrade only packages marked Auto |
| `wintui upgrade --id <pkg>` | Upgrade one or more named packages (repeatable) |
| `wintui upgrade --self` | Upgrade WinTUI itself via the startup self-update handoff |
| `wintui doctor [--verbose] [--full] [--dev-tools] [--json]` | Verdict-first readiness check: `OK` / `WARN: N issues` / `FAIL: N issues` and exit 0/1/2 |
| `wintui notes <id> [--source winget\|msstore] [--json]` | Show a package's release notes (rendered markdown), when the winget manifest has them |
| `wintui theme [name] [--list]` | Show, list, or set the color theme |
| `wintui config list\|get <key>\|set <key> <value>\|unset <key> [--json]` | Show or change any global setting in `settings.json` with validated, human-readable values |
| `wintui rules list\|show <id>\|set <id> --policy … \|clear <id> [field…] [--source winget\|msstore] [--json]` | Show or change per-package rules (update policy, version holds, scope, architecture, elevation), with explicit and effective values side by side |
| `wintui cleanup scan [--enabled] [--target <id>…] [--json]` | Measure what each Cleanup-tab target would reclaim, read-only; deletion stays in the TUI |
| `wintui export [--output PATH] [--with-versions]` | Write the installed package list to a portable JSON file (stdout by default) |
| `wintui import <path> [--dry-run] [--all] [--json]` | Install packages from a `wintui export` file (with optional preflight) |
| `wintui history [id] [--limit N] [--since DUR] [--failed-only] [--json]` | Show WinTUI-originated install/upgrade/uninstall operations; with an id, that package's timeline |
| `wintui fix --portable [--dry-run] [--json]` | Pin portable winget packages to user scope so upgrades can't drop them from PATH |

`wintui` (no subcommand) launches the TUI.

The old root `--check` and `--list` flags have been removed. Use
`wintui check` and `wintui list`.

The CLI is wrapped with [fang](https://github.com/charmbracelet/fang), so
`wintui --help`, usage, errors, and `--version` are styled to match your
active theme. Shell completions are available via `wintui completion
<bash|zsh|fish|powershell>` — once enabled, `wintui upgrade --id <TAB>` and
`wintui show <TAB>` complete package IDs from the local cache (no winget call).
See [Enabling shell completions](#enabling-shell-completions) for setup.

## Exit Codes

### `check`

| Exit code | Meaning |
|---|---|
| `0` | No updates available (or all available updates are held by policy) |
| `1` | One or more visible updates available |

`check` honors the same per-package update policy the TUI uses, so a held
package will not flip the exit code.

`--notes` renders the **target version's** release notes for each pending update
inline — the built-in form of `wintui check --json | … | wintui notes` (see
[Inspect-before-upgrade](#inspect-before-upgrade)). It fetches one package at a
time (each is a winget call), so it's heavier than a plain `check`; it's meant
for the "review what I'm about to install" flow, not scripting. `--notes` and
`--json` are mutually exclusive.

### `list`

`wintui list` prints every installed package. With a **query** argument it
shows only packages whose **name or id** contains the query
(case-insensitive substring, matching `winget list`):

```powershell
wintui list firefox        # is Firefox installed?
wintui list Microsoft      # everything from Microsoft
wintui list git --json     # matches as JSON
```

| Exit code | Meaning |
|---|---|
| `0` | Listed successfully (no query, or the query matched at least one package) |
| `1` | A query was given and **nothing matched** — so `wintui list firefox` doubles as an "is it installed?" predicate |

A bare `wintui list` (no query) always exits `0`. Short queries match broadly,
just like winget — e.g. `git` also matches `Logitech` and `Digital` because
those names contain the substring "git".

### `show`

Note on rule values in `show --json`: the `override` object is the rule as
stored. Since v2.12 the shared write layer stores a legacy `ignore: true` as
`update_policy: hold` the next time that rule is edited (by the CLI or the
TUI), so a migrated rule reports `"update_policy": "hold"` where it used to
report `"ignore": true` — same meaning, same field names, different
representation. `wintui rules show <id>` is the semantic view.

`show` exits with `0` on success and non-zero on argument errors
(missing id, unsupported `--source`).

### `upgrade --all` / `upgrade --auto` / `upgrade --id`

| Exit code | Meaning |
|---|---|
| `0` | All selected upgrades succeeded, no matching upgrades were available, or only the running WinTUI binary was skipped |
| `1` | One or more package upgrades failed, or `--id` named a held package |

### `doctor`

| Exit code | Meaning |
|---|---|
| `0` | All readiness rows are PASS / INFO — `OK` |
| `1` | At least one row is WARN, none are FAIL — `WARN: N issues` |
| `2` | At least one row is FAIL — `FAIL: N issues` |

The slim default check set is identical to the TUI Health tab and includes a
neutral `winget MCP` INFO row reporting whether winget's MCP server binary is
present (detected / not found). `--full`
re-adds the verbose system-diagnostics rows (RAM, Defender, internet ping,
extra fixed drives, OS / uptime, PATH, Windows PowerShell). `--dev-tools`
appends a developer-tools detection group (Git, VS Code, Docker, Node,
Python, Go, etc.) — every missing tool is a WARN, so this flag generally
flips the verdict to WARN unless your machine has the full dev stack
installed. `--verbose` prints the per-row table beneath the verdict line.

`--all` upgrades every non-held package. `--auto` upgrades only packages
whose per-package update policy is Auto. `--id` upgrades one or more named
packages — the flag is repeatable (`--id A --id B`), packages without an
available update are reported with no error, and naming a held package is
an error (so the user notices their hold instead of silently skipping).

`--all`, `--auto`, and `--id` are mutually exclusive.

The running WinTUI binary is **not** upgraded by `--all` / `--auto` / `--id`;
it is skipped with a hint pointing at `wintui upgrade --self`. `--self` runs
the same PowerShell handoff the TUI uses at startup: it checks winget for a
WinTUI update and, if one is available, exits so winget can replace the
released binary. Unlike the default-on startup check, `--self` ignores the
WinTUI Auto Update setting (you asked for it explicitly), but it still
requires the winget-installed build — a dev or portable build prints a hint
and does nothing. This closes the gap for CLI-only users who rarely launch
the TUI: `wintui upgrade --all; wintui upgrade --self` keeps everything,
including WinTUI, current.

### `export`

`wintui export` writes the current installed package list as a versioned
JSON envelope you can move to another machine and feed to `wintui import`.

By default the JSON is printed to stdout (so it pipes cleanly to anything
expecting JSON on a stream). Pass `--output PATH` to write to a file with
a status summary on stdout instead.

Versions are excluded by default — restoring exact versions on a fresh
machine is a footgun (the registered version may have aged out of winget).
Pass `--with-versions` if you genuinely need the snapshot pinned.

WinTUI itself is included in the export. On import it gets marked
already-installed automatically, since by definition you must have WinTUI
to run import in the first place.

### `import`

`wintui import <path>` reads an export file and installs its packages
headlessly. Default behavior installs the **safe subset**:

- packages that aren't already installed,
- packages that aren't raw / non-canonical identifiers (MSIX hashes, GUIDs),
- packages that don't share a name with a different installed package
  (e.g. exporting `Git.Git` when `Microsoft.Git` is installed locally —
  this is flagged as a possible duplicate and skipped by default).

Flags:

| Flag | Behavior |
|---|---|
| `--dry-run` | Print the install plan (will-install / already-installed / review-needed / non-restorable) without touching anything |
| `--all` | Also install entries flagged as possible name matches |
| `--json` | Print the plan as JSON; implies `--dry-run` |

`wintui import` accepts both the new envelope format (top-level JSON
object) and the legacy flat-array form (top-level JSON array) for
backward compatibility.

For row-level toggling of the install set, use the TUI:
press `I` (capital) on the Packages tab to open the import overlay,
which scans your Desktop, home, and current directory for `*.json` files.

| Exit code | Meaning |
|---|---|
| `0` | Plan shown (dry-run) or every selected install succeeded |
| `1` | At least one install failed |

### notes

`wintui notes <id>` fetches a package's latest-version release notes via
`winget show` and renders the markdown for the terminal.

Caveats worth knowing:

- Many winget manifests ship only a release-notes **URL**, or nothing at all.
  When there is no notes text, `notes` prints the URL (or reports that none
  are available) instead.
- The manifest carries only the **latest** version's notes — not a changelog
  spanning the versions you may have skipped.

`--source` accepts `winget` or `msstore` (defaults to `winget`). `--json`
emits `{id, source, version, release_notes, release_notes_url}`. When stdout is
piped, notes render as plain text (no ANSI); on a terminal they render dark.

### theme

`wintui theme` prints the active theme and background mode. `wintui theme
--list` lists every palette (the active one marked). `wintui theme <name>`
sets and persists the theme to `settings.json`; the TUI and CLI help pick it
up on the next run. An unknown name is an error — run `wintui theme --list`
to see valid IDs. The active theme also appears as an INFO row in
`wintui doctor`.

### config

`wintui config` is the CLI's control plane for the global settings the TUI's
Settings tab edits — the same 15 keys, the same vocabulary, one registry.

| Form | Behavior |
|---|---|
| `wintui config list` | Every key with its current value, grouped Common / Advanced / Appearance / Cleanup |
| `wintui config get <key>` | Print one value (`--json`: a self-describing object, never a bare scalar) |
| `wintui config set <key> <value>` | Validate and persist one value; invalid values are an error and write nothing |
| `wintui config unset <key>` | Restore WinTUI's default for the key (not always empty — `source` defaults to `winget`) |

Values are gh/git-style positionals with human-readable names, case-insensitive
on input: `scope default|user|machine`, `install_mode default|silent|interactive`,
`architecture auto|x64|x86|arm64`, `source all|winget|msstore`,
`cleanup_auto_scan safe|all|off`, `theme_background terminal|theme`,
`theme <id>` (same names as `wintui theme`), and `true|false` for switches
(`on`/`off`, `yes`/`no`, `1`/`0` accepted as input). Anything else is
rejected — nothing is normalized silently. `set` and `unset` write a delta
over the file on disk rather than a whole snapshot, so a TUI running in
another window does not lose unrelated keys, and never overwrite a
`settings.json` they cannot parse.

A TUI that is already running picks up `config` and `rules` changes made
from the CLI on its next explicit refresh (`r` on the Packages tab) and when
the Settings tab is first opened — unless the Settings tab holds unsaved
edits, in which case the change waits until they are saved or the TUI is
restarted. The Health tab's Settings row warns while a change is pending.

An unrecognised value already on disk (a hand edit) is shown raw with
`"valid": false` in `--json` and `(invalid)` in the table, and turns the
`wintui doctor` Settings row to WARN; `set` or `unset` clears it.

`--json` for `list` is `{"count": N, "settings": [...]}`; `get`/`set`/`unset`
return one element:

```json
{
  "key": "install_mode",
  "value": "silent",
  "default": "default",
  "is_default": false,
  "valid": true,
  "type": "enum",
  "values": ["default", "silent", "interactive"],
  "group": "common",
  "description": "UI behavior for install, upgrade, uninstall"
}
```

Booleans are JSON booleans. Per-package rules are not part of `config`; see
`wintui rules`. Keys and values complete in the shell once completions are
enabled.

### rules

`wintui rules` manages the per-package rules the TUI's detail panel edits
(`p` / `t` / `i`): update policy, version-specific holds, install scope,
architecture, and elevation. Package IDs are case-insensitive; `--source`
defaults to `winget`.

| Form | Behavior |
|---|---|
| `wintui rules list` | Every explicit rule: ID / SOURCE / POLICY / HOLD / SCOPE / ARCH / ELEVATE (`(any)` = a legacy rule without a source) |
| `wintui rules show <id>` | The explicit rule **and** the effective value for each field, so inheritance from the global settings is visible. No rule is not an error (exit 0) |
| `wintui rules set <id> [--policy ask\|auto\|hold] [--scope inherit\|user\|machine] [--architecture inherit\|x64\|x86\|arm64] [--elevate inherit\|always\|never] [--ignore-version <v>]` | Change only the fields you pass, in one write; everything else on the rule is untouched |
| `wintui rules clear <id> [policy\|scope\|architecture\|elevate\|ignore-version …]` | Remove the whole rule (no fields) or just the named fields |

Holds are one explicit state: **none**, **all** (`--policy hold`), or
**version=X** (`--ignore-version X`, held only while that exact version is
the available one). `--policy auto` and `--policy ask` never touch a version
hold, so a version-scoped hold cannot silently become a permanent one;
`--policy hold` replaces it (a permanent hold covers every version, and a
later `--policy ask` will not resurrect the old version hold). Asking for
`--ignore-version` on a package that is already on a permanent hold is
refused rather than stored invisibly — pass `--policy ask --ignore-version X`
to demote it in one write. A legacy `ignore: true` in `settings.json`
reads as policy `hold` / hold `all` and is rewritten in the canonical form the
next time the rule is edited (by the CLI or the TUI).

`rules show` evaluates a version hold against the available version in the
read-only package cache when it is warm (`active` / `inactive`) and says so
honestly when it cannot (`cannot evaluate` — open the TUI or run
`wintui check` to refresh the cache). It never calls winget.

Invalid values are errors and write nothing; a typo in `--elevate` never
means "never". New rules take winget's ID casing from the cache; an existing
rule keeps its key. If `settings.json` contains conflicting duplicate keys for
one package (`winget:Git.Git` and `winget:git.git` with different values —
only possible by hand editing), `set` and field-level `clear` refuse with the
keys listed, `rules clear <id>` removes them all, and `wintui doctor` warns.

`--json`: `list` is `{"count": N, "rules": [...]}`; `show`/`set`/`clear` return
one object with `rule` (the explicit rule, `null` when none), `effective`, and
`conflicts` (`[]` when clean). `hold` is `{"kind": "none|all|version",
"version": "…", "active": true|false|null}` — `null` when the cache cannot
evaluate it; `available_version` is likewise `null` when unknown.

Rule edits are settings changes, not actions: they do not appear in
`wintui history`. `wintui show <id>` remains the argv-level diagnosis (the
exact `winget` arguments) and its output is unchanged.

### cleanup scan

`wintui cleanup scan` is the read-only half of the Cleanup tab: it measures
what every registered target would reclaim and prints TARGET / GROUP /
ENABLED / SIZE / ITEMS / ADMIN / STATUS. It never deletes anything and never
routes through the elevated helper — deleting stays in the TUI, where each
removal is reviewed and confirmed.

| Form | Behavior |
|---|---|
| `wintui cleanup scan` | Every registered target, present or not, so `missing` / `unresolved` reasons are visible |
| `wintui cleanup scan --enabled` | Only the targets that start **checked** in the TUI (default-checked plus the ones you opted in) — the set a TUI deletion would act on. This is deliberately not the set the Cleanup tab *auto-scans* on open: the default `safe` auto-scan measures every present target outside the Developer group, including GPU caches that are not checked by default, so a GPU cache can show `enabled: no` here while the TUI still shows its size |
| `wintui cleanup scan --target <id>` | Only the named target(s); repeatable, IDs complete in the shell |

Statuses: `ok` (reclaimable entries found), `empty`, `missing` (path not on
disk), `unresolved` (environment variable missing), `needs_admin` (the target
requires elevation and this process is not elevated — it is **not** walked,
because a non-elevated walk would report a partial, wrong size; run the same
command from an elevated terminal to measure it), `partial` (scanned, but
some entries were unreadable — the size is a lower bound, shown as `≥`),
`skipped` (engine guard, e.g. a reparse-point root), `error`.

Targets are walked with bounded concurrency; a one-line progress note goes to
stderr so stdout stays pipeable. Exit code is 0 — this is a report, not a
predicate.

`--json` is `{"elevated", "selection", "count", "scanned", "needs_admin",
"total_size_bytes", "targets": [...]}`. Each target carries `id`, `label`,
`group`, `group_label`, `path`, `mode`, `globs`, `min_age_seconds`,
`requires_admin`, `default_checked`, `enabled`, `present`, `scanned`,
`status`, `size_bytes`, `items`, `unreadable`, `errors`. Unmeasured
`size_bytes` / `items` are an explicit `null` (never omitted); measured zeros
are `0`; arrays are never `null`.

### history

`wintui history` lists the operations WinTUI itself has run — TUI batches and
the headless `wintui upgrade` / `wintui import` commands — newest first, so a
scheduled `wintui upgrade --all` leaves a trace. Pass a package id for that
package's timeline (a query over the recorded batches):

```powershell
wintui history                  # recent batches (default --limit 20)
wintui history Mozilla.Firefox  # that package's upgrade/install/uninstall timeline
wintui history --failed-only --since 168h
```

| Flag | Behavior |
|---|---|
| `[id]` | Tier-2: show only the named package's timeline |
| `--limit N` | Cap rows shown (default `20`; `0` = all) |
| `--since DUR` | Only records newer than a Go duration (e.g. `168h`, `30m`) |
| `--failed-only` | Only records / items that failed |
| `--json` | Structured output (`{view, count, …}`) |

| Exit code | Meaning |
|---|---|
| `0` | Records shown, or an **unfiltered** history is empty (a fresh install is normal) |
| `1` | A **selector / filter** (an id, `--since`, or `--failed-only`) matched nothing — so `wintui history <id>` doubles as a "did WinTUI ever touch this?" predicate |

The exit-code predicate holds in `--json` mode too (corrupt / unsupported-version
files are a hard error). **History records only WinTUI-originated actions.**
winget exposes no event feed, so upgrades you run from plain `winget`, Microsoft
Store auto-updates, and other tools are **not** captured — history will not match
`winget list` deltas. It lives at `%APPDATA%\wintui\history.json`, independent of
the cache (so completion and queries work even if you've only ever used the CLI).
The newest 1000 batches are kept; WinTUI self-upgrades are recorded as `pending`
(the handoff finishes after restart).

### fix

`wintui fix --portable` protects **portable** winget packages — CLIs like
`claude`, `gum`, or `ffmpeg` that winget extracts to
`%LOCALAPPDATA%\Microsoft\WinGet\Packages\` and adds to your user PATH. winget
can mis-scope these on upgrade and strip them from PATH ([winget-cli #4044 /
#5099](https://github.com/microsoft/winget-cli/issues/4044)), so the command
suddenly "disappears". `fix --portable` pins each one with a per-package
`scope: user` + `elevate: false` override, so WinTUI always passes
`--scope user` for them and the PATH entry survives future upgrades:

```powershell
wintui fix --portable --dry-run   # preview which packages would be pinned
wintui fix --portable             # apply (idempotent; merges with existing rules)
```

It's safe to re-run — already-pinned packages are skipped. Detection is a single
directory listing, with no `winget show` calls. When `wintui upgrade
--all/--auto/--id` is about to upgrade a portable package that isn't pinned yet,
it prints a one-line nudge toward `fix --portable`.

## Examples

```powershell
# Human-readable upgrade check
wintui check

# Use the exit code in Task Scheduler, PowerShell, or CI
wintui check ; if ($LASTEXITCODE -eq 1) { "Updates available" }

# JSON output for scripting
wintui check --json

# Export installed packages as JSON
wintui list --json > packages.json

# Check whether a package is installed (exit 1 if not)
wintui list firefox
wintui list firefox ; if ($LASTEXITCODE -eq 0) { "Firefox is installed" }

# Inspect what WinTUI would pass to winget for a given package
wintui show Mozilla.Firefox
wintui show Mozilla.Firefox --json

# Upgrade everything that is not held
wintui upgrade --all

# Upgrade only packages marked Auto
wintui upgrade --auto

# Upgrade one or more specific packages by ID
wintui upgrade --id Mozilla.Firefox --id Microsoft.VisualStudioCode

# Upgrade WinTUI itself from the CLI (the other modes skip it)
wintui upgrade --self

# Read a package's release notes (rendered markdown)
wintui notes Mozilla.Firefox
wintui notes Git.Git --json

# Show, list, or switch the color theme
wintui theme
wintui theme --list
wintui theme nord

# Show or change global settings without opening the TUI
wintui config list
wintui config set install_mode silent
wintui config set auto_elevate off
wintui config unset source
wintui config get scope --json

# Per-package rules: what applies, and why
wintui rules list
wintui rules show Git.Git
wintui rules set Git.Git --policy auto --scope user
wintui rules set Mozilla.Firefox --ignore-version 155.0   # hold just this version
wintui rules set Anthropic.ClaudeCode --elevate never
wintui rules clear Git.Git scope
wintui rules clear Git.Git                                # remove the whole rule

# How much junk has piled up? (read-only; deleting stays in the TUI)
wintui cleanup scan
wintui cleanup scan --enabled
wintui cleanup scan --target user_temp --target npm_cache --json

# Show WinTUI's action history (and one package's timeline)
wintui history
wintui history Mozilla.Firefox
wintui history --failed-only --since 168h --json

# Pin portable packages to user scope so a future upgrade can't drop them from PATH
wintui fix --portable --dry-run
wintui fix --portable

# Install shell completions (PowerShell shown; bash/zsh/fish also supported)
wintui completion powershell | Out-String | Invoke-Expression

# Pipe from check (PowerShell):
wintui check --json | ConvertFrom-Json | ForEach-Object { wintui upgrade --id $_.id }

# Export and re-import on a new machine
wintui export --output \\share\backup\packages.json
wintui import \\share\backup\packages.json --dry-run     # preview
wintui import \\share\backup\packages.json               # install safe subset
wintui import \\share\backup\packages.json --all         # also install name-collision rows

# Pipe from check (bash, e.g. Git Bash):
wintui check --json | jq -r '.[].id' | xargs -r -n1 wintui upgrade --id
```

## Recipes

PowerShell snippets for common automation patterns. Each one assumes
`wintui.exe` is on `PATH`.

### Enabling shell completions

Completion is **not** automatic — PowerShell can't see inside an external
`.exe`, so the completer has to be registered once per session. `wintui`
generates the registration script via `wintui completion <shell>`
(`powershell`, `bash`, `zsh`, `fish`).

Activate for the **current session**:

```powershell
wintui completion powershell | Out-String | Invoke-Expression
```

Make it **permanent** — add that line to your PowerShell `$PROFILE`. To avoid
regenerating on every shell start, cache the script and dot-source it instead:

```powershell
# one-time: write the cache (re-run after upgrading wintui)
$cache = "$env:LOCALAPPDATA\wintui\completion-pwsh.ps1"
New-Item -ItemType Directory -Force (Split-Path $cache) | Out-Null
wintui completion powershell | Out-String | Set-Content -Encoding UTF8 $cache

# in $PROFILE: load the cache if present
$wintuiCompletion = "$env:LOCALAPPDATA\wintui\completion-pwsh.ps1"
if (Test-Path $wintuiCompletion) { . $wintuiCompletion }
```

Once loaded, `wintui <TAB>` completes subcommands and flags, and `wintui
upgrade --id <TAB>` / `wintui show <TAB>` complete package IDs from the local
cache — completion never calls winget. That cache (`%LOCALAPPDATA%\wintui\
cache.json`) is written when you **launch the TUI** (`wintui`); the headless
subcommands (`check`, `list`, …) don't write it. So if you've only ever used the
CLI, package-ID completion stays empty until you've run the TUI at least once —
subcommand and flag completion work regardless. The registered completer
re-invokes `wintui` by name through `PATH`, so the IDs reflect whichever
`wintui` is on your `PATH`. For a menu that shows the package descriptions, bind
Tab to menu completion:

```powershell
Set-PSReadLineKeyHandler -Key Tab -Function MenuComplete
```

### Daily toast when updates are available

Enable **Toast Notifications** in the Settings tab once (or set
`"toast_notifications": true` in `%APPDATA%\wintui\settings.json`), then
pair `wintui check` with Task Scheduler (`schtasks /Create ... /SC DAILY`).
WinTUI fires the toast itself when the scan finds at least one update —
no PowerShell module dependency, AUMID-attributed as "WinTUI" in Action
Center:

```powershell
# Once Toast Notifications is on, this is all the schedule needs:
wintui check
```

The toast is silent when nothing is available, so a daily scheduled run
produces zero noise on up-to-date machines.

### Verdict-driven scheduled health check

`wintui doctor` exits 0/1/2 based on readiness state, so it slots into
Task Scheduler / CI gates the same way `check` does. Combined with toast
notifications, a scheduled `doctor` becomes a "tell me when something
needs attention" probe:

```powershell
# Daily readiness verdict — pair with Task Scheduler for a quiet probe
wintui doctor
if ($LASTEXITCODE -ne 0) { wintui doctor --verbose }
```

### Audit what a scheduled upgrade did

A scheduled `wintui upgrade --all` runs unattended, but it now records every
batch, so you can review what happened after the fact (or check a single
package's history):

```powershell
wintui history                       # what did the last runs upgrade?
wintui history --failed-only         # only batches with a failure
wintui history Mozilla.Firefox       # Firefox's upgrade timeline
```

### Upgrade everything matching a pattern

Selectively upgrade by package ID prefix without touching anything else:

```powershell
wintui check --json | ConvertFrom-Json |
    Where-Object { $_.id -like 'Microsoft.*' } |
    ForEach-Object { wintui upgrade --id $_.id }
```

### Exit-code gate for CI / Task Scheduler

`wintui check` exits 1 when visible updates exist — drop straight into a
build step or scheduled job to short-circuit when patches are pending:

```powershell
wintui check
if ($LASTEXITCODE -eq 1) {
    Write-Host "Pending updates — failing build" -ForegroundColor Yellow
    exit 1
}
```

### Inspect-before-upgrade

**Read the release notes for everything you're about to install** — the
built-in form:

```powershell
wintui check --notes
```

That's equivalent to fetching each pending update's notes by hand, which still
works if you want to compose it:

```powershell
wintui check --json | ConvertFrom-Json |
    ForEach-Object { "`n=== $($_.id)  $($_.version) -> $($_.available) ==="; wintui notes $_.id }
```

Or print the exact `winget` command WinTUI would run for every pending update —
useful for reviewing per-package overrides before pulling the trigger:

```powershell
wintui check --json | ConvertFrom-Json |
    ForEach-Object { wintui show $_.id }
```

## Human-Readable Output

### `check`

```text
Name       ID                   Version  Available
Git        Git.Git              2.44.0   2.45.0
Notepad++  Notepad++.Notepad++  8.6.4    8.7.1

2 package(s) have updates available.
```

### `list`

```text
Name       ID                   Version  Source
Git        Git.Git              2.45.0   winget
PowerToys  Microsoft.PowerToys  0.91.0   winget

2 package(s) installed.
```

### `show`

```text
ID:     Mozilla.Firefox
Source: winget

Effective install command:
  winget install --id Mozilla.Firefox --exact --accept-package-agreements --source winget

Effective upgrade command:
  winget upgrade --id Mozilla.Firefox --exact --accept-package-agreements --source winget
```

If the package has overrides, a "Per-package overrides" block is
appended (update_policy, scope, architecture, elevate, ignore, ignore_version).
Global and per-package action settings can add flags such as `--silent`,
`--scope`, or `--architecture` to the command preview.

### `upgrade --all` / `upgrade --auto`

Headless upgrade commands stream winget output line-by-line under each
package header and print a summary line on completion. A slow installer that
goes quiet (e.g. one stopping a service or replacing in-use files for minutes)
prints a periodic `… still working (Nm elapsed)` heartbeat so it isn't mistaken
for a hang.

## JSON Output

### `check`, `list`

JSON output is an array of package objects with lowercase keys:

```json
[
  {
    "name": "Git",
    "id": "Git.Git",
    "version": "2.44.0",
    "available": "2.45.0",
    "source": "winget"
  }
]
```

### `show`

```json
{
  "id": "Mozilla.Firefox",
  "source": "winget",
  "install_args": ["install", "--id", "Mozilla.Firefox", "--exact", "..."],
  "upgrade_args": ["upgrade", "--id", "Mozilla.Firefox", "--exact", "..."],
  "override": {
    "scope": "user"
  }
}
```

The `override` field is omitted when no per-package rules exist.

### `doctor`

```json
{
  "verdict": "WARN",
  "summary": "WARN: 1 issue",
  "exit_code": 1,
  "counts": { "pass": 4, "warn": 1, "fail": 0, "info": 2 },
  "checks": [
    {
      "check": "WinTUI",
      "status": "PASS",
      "details": "v2.6.0 · installed · config writeable"
    },
    {
      "check": "Sources",
      "status": "WARN",
      "details": "winget · no cached scan yet",
      "recommendation": "Run wintui or winget upgrade to populate the cache."
    }
  ]
}
```

`verdict` is one of `OK` / `WARN` / `FAIL`; `exit_code` always matches.
`recommendation` is omitted on rows that don't have one.

## Notes

- `--json` is valid with `check`, `list`, `show`, `doctor`, `notes`, `history`, and `fix --portable`.
- `wintui upgrade --all` and `wintui upgrade --auto` honor per-package update policy from `settings.json`.
- `wintui upgrade --id` is the user-facing single-package mode. The
  identically-named `--id` on the root command (alongside `--retry-op`,
  `--name`, etc.) is internal to WinTUI's elevated retry flow and is not
  intended for direct user use.
