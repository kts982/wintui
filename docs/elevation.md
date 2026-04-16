# Elevation and Retry Behavior

WinTUI supports three elevation paths:

1. Run the whole app from an already elevated terminal
2. **Silent + Auto Elevate** — all operations run elevated upfront via the helper
3. **Auto Elevate** — hard admin failures are retried automatically
4. **Ctrl+E** in the result modal — retry only the failed elevation-candidate packages

## Silent + Auto Elevate (Recommended)

When both **Action Mode: Silent** and **Auto Elevate** are enabled in Settings:

- All install, upgrade, and uninstall operations run through the elevated helper from the start
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

### Security

- The pipe is restricted to the current user's SID
- The helper refuses to run unless it is elevated
- The helper process runs hidden (SW_HIDE) — no visible console window

## What Triggers Elevation

### Hard elevation cases

These are treated as administrator-required failures and auto-retried when Auto Elevate is on:

- `0x8a150056` — package requires administrator privileges
- `0x80073d28` — installer requires administrator privileges
- Messages containing `administrator privileges`, `requires elevation`, `run as admin`

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

This does not rerun packages that already succeeded.

### Retry for process-in-use failures

When an install/upgrade/uninstall fails because a related application is still running (winget errors like `0x80073d02`, `0x8a150052`, `0x8a150066`, or messages containing "is in use"), the result modal shows:

```
Close the running application and press ctrl+e to retry.
```

Ctrl+E also retries these items. The label changes to `ctrl+e retry` when only process-blocked items need retrying (no elevation needed); it stays `ctrl+e retry elevated` when there are also permission failures. Close the blocking application before pressing Ctrl+E.

## Recommended Usage

- **Silent + Auto Elevate** for the smoothest experience (all operations elevated upfront)
- **Auto Elevate only** if you want to see installer UI but still handle admin failures automatically
- **Auto Elevate off** if you prefer full control — use `Ctrl+E` in the result modal when needed
- **Run elevated** from the start if you know you are doing many machine-level operations
