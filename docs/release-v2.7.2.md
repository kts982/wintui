# WinTUI v2.7.2 Release Notes

A small follow-up to v2.7.1 fixing two bugs that together prevented the
disk cache from being written and made the Health tab show "no cached
scan yet" right after a successful workspace scan.

## Narrow-column upgrade tables now parse

When only one or two upgrades are pending and their package data is
short, winget renders the upgrade table with **single-space** gaps
between Version, Available, and Source instead of the usual ≥2-space
column padding:

```
Name      Id                  Version Available Source
------------------------------------------------------
WireGuard WireGuard.WireGuard 1.0.1   1.1       winget
```

The v2.7.1 column-aware parser groups headers by runs of two-or-more
spaces, so it folded those three headers into a single cell
(`Version Available Source`), reported

```
winget output: 3-column upgrade table is not a known shape; first header was "Name"
```

and bailed out. As a downstream effect:

- `cache.setUpgradeable` was never called, so the in-memory upgrade
  cache stayed `nil`.
- `saveToDiskLocked` requires both lists populated, so `cache.json` was
  never written.
- The Health tab showed `INFO  Sources  no cached scan yet` and
  `INFO  Updates  no cached upgrade scan yet` even immediately after
  the workspace tab had finished scanning successfully.

The header parser now subdivides any cell whose whitespace-separated
tokens all resolve through the locale dictionary, so tightly-packed
English upgrade tables parse correctly. Localized headers with
intentional internal spaces (Korean `장치 ID`, `사용 가능`, etc.) are
unaffected because their tokens don't individually resolve through the
dictionary.

## winget list and winget upgrade no longer race

Cold-start scans previously ran `winget list` and `winget upgrade` in
parallel goroutines. winget serialises those subcommands on an internal
lock and one of them returns `0x8a150001` with empty output ~50% of the
time when called concurrently. The existing retry covered the empty
case, but on systems that hit the parser bug above, the retry's parsed
output was the same broken-shape failure and `setUpgradeable` was still
skipped.

`fetchPackages` and `startBackgroundRefresh` now run the two queries
sequentially. Cold start is ~1s slower; subsequent restarts are
unaffected because the disk cache covers them for 24h.

## Notes

- No setting changes, no breaking changes, no new Win32 surface.
- `settings.json` and `cache.json` formats are fully backward
  compatible.
- Regression coverage:
  `testdata/winget/upgrade_narrow_columns.txt` exercises the
  WireGuard-shaped header that triggered the bug end-to-end.
