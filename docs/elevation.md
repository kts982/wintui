# Elevation and Retry Behavior

WinTUI supports four elevation paths:

1. Run the whole app from an already elevated terminal
2. **Silent + Auto Elevate** — all operations run elevated upfront via the helper
3. **Auto Elevate** — hard admin failures are retried automatically
4. **Ctrl+E** in the result modal — retry only the failed elevation-candidate packages

A package's rules come first: a user-scope package never takes paths 2–4, and
in an already elevated WinTUI (path 1) its actions are skipped — see
[User-scope packages](#user-scope-packages).

## User-scope packages

A package is **user-scope** when its rule says `scope: user`, the global scope
setting is `user`, or winget's portable package folder exists for it
(`%LOCALAPPDATA%\Microsoft\WinGet\Packages\<id>_…`). Running winget elevated
for such a package goes wrong: winget can drop a portable package from your
PATH ([winget-cli #4044](https://github.com/microsoft/winget-cli/issues/4044)),
and it refuses to uninstall a user-scope package as administrator
(`0x8A15007D`).

- WinTUI never **starts** elevation for a user-scope package, or for one with
  an `elevate: never` rule: no upfront helper in silent mode, no automatic
  retry, no `Ctrl+E` retry. The result modal says why ("Not elevated:
  user-scope rule.").
- In a WinTUI that is **already elevated**, `elevate: never` cannot de-elevate
  winget, so an install, upgrade, or uninstall of a user-scope package is
  skipped before winget runs, with "run WinTUI non-elevated" in the message.
  History records it as `skipped` with a note, not as an error: `upgrade
  --all` and `--auto` still exit 0, while `upgrade --id` exits 1, as for a
  held package. Import skips these packages the same way.
- An explicit `elevate: always` rule overrides this for **install and
  upgrade** of that package — it is the most specific setting. `wintui rules
  show` labels the combination "explicit; overrides the user-scope safety".
  It never applies to uninstall.
- Uninstall follows the package's rules like install and upgrade do.

## Silent + Auto Elevate (Recommended)

When both **Action Mode: Silent** and **Auto Elevate** are enabled in Settings:

- All install, upgrade, and uninstall operations run through the elevated helper from the start, except for user-scope packages
- No installer UAC popups — the elevated helper already has admin rights
- A single UAC prompt when the helper first starts, then reused for all subsequent operations

This is the smoothest experience for hands-off package management.

## Auto Elevate (Non-Silent)

When **Auto Elevate** is on but Action Mode is default or interactive:

- Operations run non-elevated first
- If winget reports a hard elevation error, WinTUI automatically retries through the elevated helper
- Soft failures (like 1603) are not auto-retried

## Elevated Helper

The elevated helper is an internal WinTUI process:

```text
wintui helper --pipe <pipe_name>
```

The main TUI stays in the original window. Commands are sent through a Windows named pipe, and output streams back in real-time.

### Lifecycle

- Started on first elevation need (single UAC prompt)
- Reused for all subsequent elevated operations in the session
- Shut down automatically when the TUI exits

### When elevation fails

A declined prompt, a helper that never connects (WinTUI waits 60 s after the
prompt), or a helper that stops responding costs **one** prompt per batch, not
one per package: the failure is remembered until the next TUI batch, Cleanup
run, or CLI command. Each remaining action runs without elevation, and its
first output line says why — for example "Running without elevation:
elevation was cancelled". The same note is saved in the action's history
entry.

While the prompt is up, or while WinTUI waits for the helper to connect, the
running package shows "waiting for elevation…". `Esc` cancels the wait once
the prompt has closed; the prompt itself takes the secure desktop.

### When elevation runs as a different account

With Windows **Administrator protection** enabled, or when a standard user
elevates with an administrator's credentials, the elevated helper runs as a
different account (under Administrator protection, a hidden `ADMIN_<user>`
account with its own profile). That account cannot see packages installed
only for you, and whatever per-user parts it installs land in its own
profile, out of your reach.

WinTUI knows which account elevation will use before it prompts, and the
helper reports its account when it connects. When it is a different account:

- Silent + Auto Elevate sends only `scope: machine` packages to the helper
  upfront; everything else runs as you first.
- A hard admin failure is retried elevated unless the package is user-scope:
  such failures are machine-scope by nature, and machine installs are visible
  to both accounts.
- An `elevate: always` rule elevates only with `scope: machine`; otherwise the
  action runs as you and says so.
- winget's "no installed package found" (`0x8A150014`) from the helper adds
  that the elevated account cannot see packages installed only for you.
- A per-user package that already landed in the other account's profile
  (for example, a portable package installed from an elevated terminal)
  cannot be removed with winget at all: that account only ever runs with a
  full administrator token, and winget refuses to uninstall user-scope
  packages from one (`0x8A15007D`). Remove its folder under that profile's
  `%LOCALAPPDATA%\Microsoft\WinGet\Packages`, its `WinGet\Links` entry, and
  its PATH and Apps & features entries by hand.
- `wintui doctor` adds "elevation runs as a different account" to the
  Privileges row.

### Security

- The pipe admits only the current user and elevated Administrators (an
  elevated helper that runs as another account needs the second); every
  request must also carry the per-session token
- The helper reports which account it runs as before it accepts any request
- The helper refuses to run unless it is elevated, and writes nothing to its
  account's profile before it connects
- The helper process runs hidden (SW_HIDE) — no visible console window

## What Triggers Elevation

### Hard elevation cases

These are treated as administrator-required failures and auto-retried when Auto Elevate is on (except for user-scope packages):

- `0x8a150019` — winget: the command requires administrator privileges (for
  example, a `scope: machine` install from a non-elevated terminal)
- `0x80073d28` — MSIX: a packaged service requires administrator privileges
- Messages containing `administrator privileges`, `requires elevation`, `run as admin`

WinTUI matches winget's own exit code first (Go reports it in decimal, so
`exit status 2316632089` is `0x8a150019`), then any code in winget's output,
such as the installer's exit code. `0x8a150056` is the opposite case — the
installer cannot run from an administrator context — and is never retried
elevated.

### Soft elevation candidates

These may benefit from elevation but are not guaranteed admin-only failures:

- `1603` — installer fatal error
- `0x80070643` — installer error

WinTUI does not auto-retry these. Instead, the result modal offers `Ctrl+E`.

## Ctrl+E Retry (Result Modal)

When Auto Elevate is off and a batch completes with elevation-candidate failures, the result modal shows:

```
ctrl+e retry elevated · enter close
```

Pressing `Ctrl+E`:
- Extracts only the failed packages that could benefit from elevation
- Creates a new batch routed through the elevated helper
- Runs the retry batch (single UAC prompt if helper not already active)
- Shows results in a new modal

This does not rerun packages that already succeeded, and it never retries a
user-scope package or one with an `elevate: never` rule elevated (see
[User-scope packages](#user-scope-packages)); set `elevate: always` on the
package to allow it.

### Retry for process-in-use failures

When an install/upgrade/uninstall fails because a related application is still running (winget errors like `0x8a150101`, `0x8a150103`, `0x80073d02`, `0x8a150052`, `0x8a150066`, or messages containing "is in use"), the result modal shows:

```
Close the running application and press ctrl+e to retry.
```

Ctrl+E also retries these items. The label changes to `ctrl+e retry` when only process-blocked items need retrying (no elevation needed); it stays `ctrl+e retry elevated` when there are also permission failures. Close the blocking application before pressing Ctrl+E.

## Self-Upgrade Safety

WinTUI's own WinGet upgrade path is handled outside the running TUI process:

- **WinTUI Auto Update** is on by default. When WinTUI is running from its winget install, startup checks `kts982.WinTUI` before the TUI starts. If an update is available, WinTUI launches an inline PowerShell handoff and exits.
- Manual self-upgrade from the Updates list uses the same handoff: press `Enter` on the result modal to close WinTUI and let winget upgrade the released binary.
- The handoff passes its PowerShell command inline: it drops neither a `.ps1` file in WinTUI's cache directory nor a temporary helper EXE in `%TEMP%`.
- Visibility tradeoff of the inline transport (also used for toast delivery): while the short-lived PowerShell helper runs, its full command line — including package-derived text such as package names, versions, and toast content — is readable by anything on the machine that can enumerate processes (Task Manager, WMI, command-line auditing, EDR telemetry). The previous transport exposed the same content as a transient world-readable `.ps1` under the user profile, so neither variant hides it from the local machine; nothing beyond package metadata is included, and WinTUI's own error log records only a fixed operation name.
- After the handoff finishes, WinTUI does not reopen itself automatically; start `wintui` again manually

## Recommended Usage

- **Silent + Auto Elevate** for the smoothest experience (all operations elevated upfront)
- **Auto Elevate only** if you want to see installer UI but still handle admin failures automatically
- **Auto Elevate off** if you prefer full control — use `Ctrl+E` in the result modal when needed
- **Run elevated** from the start if you know you are doing many machine-level operations
