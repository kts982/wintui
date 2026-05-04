# WinTUI v2.6.1 Release Notes

A small follow-up to v2.6.0 with detail-panel theme polish and a packaging
refinement that strips embedded build paths and VCS metadata from the
released binary.

## UI polish

- **Detail overlay theme.** Field labels, version values, and the package URL
  in the full detail overlay now use the workspace's accent / muted palette
  consistently instead of mixing styled and unstyled spans.
- **Settings detail panel.** Hint and description text now word-wrap to the
  inner panel width so narrow terminals don't truncate or overflow.
- **State accent.** A mint-cyan accent (distinct from the structural pink)
  is now used for "intent" elements like chips in the detail panel,
  establishing a consistent two-axis color system across the workspace
  (pink for structure, mint-cyan for intent).

## Packaging

- Built with `-trimpath` and `-buildvcs=false`. Strips embedded local build
  paths (`C:\…\wintui\…`) and VCS metadata (commit, dirty flag) from the
  binary, reducing the static feature surface that Defender's cloud ML
  scored as `Wacatac.C!ml` on v2.6.0 for a small number of users. No
  runtime behavior change; the version string is still injected via
  `ldflags`.

## Notes

- No behavior changes, no setting changes, no breaking changes.
- `settings.json` is fully backward compatible.
- Users on v2.6.0 will pick this up via the existing auto-self-update path
  (or `winget upgrade kts982.WinTUI`).
