# Per-Package Rules

Per-package rules let you override global settings or skip specific packages on a case-by-case basis — without changing your defaults for everything else. Rules are stored in `%APPDATA%\wintui\settings.json` under the `packages` map, keyed by `<source>:<id>`.

Open any package's detail view and press:

- `p` — open the Package Rules editor
- `i` — toggle ignore for the focused package

## Rules

| Rule | Values | Effect |
|---|---|---|
| `ignore` | `none`, `all versions` | Hide the package from the Updates list |
| `ignore_version` | any version string | Hide a single version (e.g., skip the current latest) |
| `scope` | `global`, `user`, `machine` | Override the install scope for this package |
| `architecture` | `global`, `x64`, `x86`, `arm64` | Force a specific installer architecture |
| `elevate` | `global`, `always`, `never` | Override the auto-elevate behavior for this package |

`global` means "fall back to the Settings tab value" — the rule is not set.

## Rules Editor (`p`)

Navigate with `↑↓`, cycle values with `←→` / `Enter` / `Space`, then:

- `s` — save
- `d` — clear all rules for this package
- `Esc` — cancel without saving

A hint under the editor describes what each value does. Saved rules take effect immediately — no refresh needed.

## Ignore Toggle (`i`)

Pressing `i` in the detail view is a shortcut for the two ignore states. The next state depends on what the package already has:

| Current state | `i` does |
|---|---|
| Nothing set, package has an available update | Sets `ignore_version` to the current latest — skips just this version |
| `ignore_version` is set | Clears it |
| `ignore = all` | Clears it |
| Nothing set, no available update | Sets `ignore = all` |

To force `ignore = all` on a package that currently has an available update, use the rules editor (`p`) instead.

## Behavior

- **Command preview.** The detail view always shows the exact `winget install` or `winget upgrade` command that would run for this package, with all active rules already applied — the fastest way to verify that `scope=machine` (or any other override) actually changes the command line. In the batch confirm modal, press `?` to expand the same per-item preview for every staged package.
- **Updates list filtering.** Ignored packages are hidden from the Updates Available section. The section header shows a `(N hidden)` count when any are filtered out.
- **Automatic cleanup of `ignore_version`.** When a newer version than the ignored one becomes available, WinTUI drops the stale `ignore_version` so the new upgrade surfaces normally. You only ignore the version you told it to ignore — not everything after it.
- **Source-qualified keys.** Rules are stored as `<source>:<id>`, so the same package ID in `winget` and `msstore` gets independent rules. Legacy plain-ID keys from earlier versions are still read.
- **Overrides apply to install and upgrade.** `scope`, `architecture`, and `elevate` overrides are merged into the winget command line for both install and upgrade operations on that package.
- **Atomic persistence.** Saves write to disk first; runtime state is only updated after the write succeeds, so a failed save never silently changes behavior.

## Example

`settings.json` after setting a few rules:

```json
{
  "scope": "user",
  "architecture": "x64",
  "packages": {
    "winget:Git.Git": {
      "scope": "machine"
    },
    "winget:Mozilla.Firefox": {
      "ignore_version": "125.0.1"
    },
    "msstore:9NBLGGH4NNS1": {
      "ignore": true
    }
  }
}
```

- `Git.Git` installs per-machine even though the global scope is `user`.
- `Firefox 125.0.1` is hidden from Updates until a newer version ships.
- The msstore package is permanently hidden from Updates.
