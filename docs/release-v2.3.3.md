# WinTUI v2.3.3 Release Notes

WinTUI v2.3.3 is a single-bug patch release.

## Bug fix

- **"Include Unknown Versions" setting no longer empties the package lists.** When the setting was on, WinTUI was passing `--include-unknown` to `winget list` and `winget search`. That flag is only valid for `winget upgrade` — winget rejected the other invocations with a usage error, the parser saw no rows, and the installed list and search results came back empty. The flag is now only attached to upgrade scans, which matches the setting's UI copy ("Upgrade scans will include packages with unknown installed versions").

## Notes

- No breaking changes to settings, CLI flags, or keybindings from v2.3.2.
