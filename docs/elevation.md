# Elevation And Retry Behavior

WinTUI supports three elevation paths:

1. Run the whole app from an already elevated terminal
2. Let `Auto Elevate` handle hard admin-required failures automatically
3. Press `Ctrl+e` after a failed action to retry only the failed elevation-candidate items

## Auto Elevate

`Auto Elevate` is configured from the Settings tab.

When it is enabled:
- WinTUI detects hard elevation errors from `winget`
- starts a local named-pipe listener
- launches an elevated helper process
- reuses that helper for later elevated commands in the same session

The goal is to keep a batch moving with at most one UAC prompt instead of relaunching the full app for every package.

## Elevated Helper

The elevated helper is an internal WinTUI mode:

```text
wintui helper --pipe <pipe_name>
```

The main TUI stays in the original window and streams command output through a Windows named pipe.

### Security

- The pipe is restricted to the current user SID
- The helper refuses to run unless it is elevated

## What Triggers Elevation

### Hard elevation cases

These are treated as administrator-required failures.

Examples:
- `0x8a150056`
- `0x80073d28`
- messages containing `administrator privileges`

If `Auto Elevate` is on, WinTUI will try the helper automatically.
If `Auto Elevate` is off, WinTUI will show a retry hint after the run fails.

### Soft elevation candidates

These are not guaranteed admin-only failures, but retrying elevated may help.

Examples:
- `1603`
- `0x80070643`

For these cases, WinTUI does not auto-elevate.
Instead, it shows a softer post-run hint and lets you decide whether to retry with `Ctrl+e`.

## Ctrl+E Retry

`Ctrl+e` is offered only when WinTUI has a concrete retry target.

Behavior:
- retries only the failed/current items
- does not rerun packages that already succeeded
- preserves explicit target versions for install and upgrade retries

### Per action

#### Install

- single-package retry
- warning text notes that elevated retry may install machine-wide instead of per-user

#### Upgrade

- single-package and batch retry
- only failed upgrade items are retried
- warning text notes that elevated retry may change installer behavior

#### Uninstall

- single-package and batch retry
- only failed uninstall items are retried
- warning text notes that elevated retry may help with permissions or service-related removal problems

## Fallback Behavior

If the helper cannot be started:
- WinTUI keeps the original package failure visible
- manual `Ctrl+e` still falls back to the older full-process elevated relaunch with retry arguments

That fallback is less elegant because it opens a separate elevated WinTUI session, but it keeps the action recoverable.

## Current Limitation

Once a run is actively executing through the elevated helper, WinTUI does not provide true remote cancellation yet.
Because of that:
- helper-backed runs no longer pretend `Esc` can cancel them
- the UI keeps the execution state honest until the helper command completes

## Recommended Usage

- Leave `Auto Elevate` on for the smoothest experience with admin-required packages
- Run WinTUI elevated from the start if you know you are about to do many machine-level installs or upgrades
- Use `Ctrl+e` when a soft installer failure looks like it may be privilege-related