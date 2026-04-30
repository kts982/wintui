# WinTUI v2.5.0 Release Notes

WinTUI v2.5.0 introduces a per-package update policy (Ask/Auto/Hold), an
on-launch auto-update flow, two new `upgrade` subcommand modes, and a
significant refresh-latency fix for users with long winget package IDs.

## Per-package update policy

Each package can now declare how WinTUI handles its updates:

- **Ask** (default) — the existing behavior. Updates appear in the list
  and you decide when to apply them.
- **Auto** — eligible to be upgraded by `wintui upgrade --auto` and by
  the on-launch countdown (see below).
- **Hold** — kept out of the upgrade list entirely.

The policy is a first-class field on each per-package override
(`update_policy: ask | auto | hold`). The legacy `ignore` and
`ignore_version` keys keep working — the planner treats them as Hold,
so existing `settings.json` files don't need to be rewritten or
migrated.

Two ways to set the policy:

- From the main package list, press **`t`** on the focused row to cycle
  Ask → Auto → Hold (winget / msstore packages only — non-canonical
  rows like MSIX or ARP entries don't get a toggle).
- From the package detail view, the Package Rules editor (`p`) gains
  an `update_policy` field alongside the existing rules.

See [docs/package-rules.md](../docs/package-rules.md) for the full
field reference.

## `wintui upgrade --auto` and `--id`

Two new modes for the headless `upgrade` subcommand, both reusing the
same streaming pipeline as `--all`:

- **`wintui upgrade --auto`** — upgrade only packages with
  `update_policy: auto`. Lower blast radius than `--all` for cron
  jobs and scheduled tasks: held packages stay held, Ask packages stay
  Ask. Mutually exclusive with `--all`.
- **`wintui upgrade --id <pkg> [--id <pkg> ...]`** — upgrade named
  packages from a pipeline. Held IDs error out with exit 1, unknown
  IDs are no-ops, and the running WinTUI binary is skipped with the
  same hint `--all` and `--auto` already use.

See [docs/cli.md](../docs/cli.md) for the full reference and the new
Recipes section.

## On-launch auto-update

When you start `wintui` and any Auto-policy package has an update
available, a 5-second cancelable countdown modal appears before the
upgrade batch starts. Press `Enter` to start immediately or `Esc` to
cancel the auto-batch and use the TUI normally. Once cancelled, the
countdown won't re-appear in the same session — only an explicit `r`
refresh will re-evaluate.

The WinTUI self-package is deliberately filtered out of the on-launch
batch even when set to Auto. Self-upgrade can't run unattended (the
handoff needs an explicit Enter when admin, Ctrl+A when not), so
WinTUI defers it to the regular Updates flow and surfaces a banner
explaining that your Auto setting was acknowledged but isn't running
this round.

## Headless CLI cleanup

The deprecated root flags `wintui --check` and `wintui --list` (last
issued a deprecation warning in v2.4.0) are removed. Use `wintui
check` and `wintui list` instead.

## Bug fixes

- **Refresh no longer stalls on packages with long winget IDs.**
  Truncated-ID recovery now runs lazily at action time instead of
  during every listing, so launch and `r` no longer pay a per-row
  cost for IDs that winget abbreviated with `…` in its table output.

## Notes

- No breaking changes to settings, keybindings, or the v2.3.x
  self-upgrade handoff.
- `settings.json` is fully backward compatible — Ignore and
  IgnoreVersion still work, just routed through the new planner as
  Hold.
- `wintui upgrade --auto` does **not** upgrade the running WinTUI
  binary; like `--all`, it skips with a hint pointing at the TUI.
