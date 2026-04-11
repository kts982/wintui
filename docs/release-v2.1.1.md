# WinTUI v2.1.1 Release Notes

WinTUI v2.1.1 is a focused polish release for the Packages screen.

## Highlights

- Fixed Packages screen height allocation so the Installed panel keeps usable space even when many updates are available.
- Fixed a content-height accounting bug that could leave an empty band above the help bar on taller terminals.
- Aligned the Installed list panel and the right-side summary panel so they terminate on the same row.
- Added a friendlier error message for `0x80072efd` / `InternetOpenUrl() failed`, which now points users toward network, VPN, proxy, or firewall issues instead of showing an opaque raw code.

## Notes

- No workflow or keybinding changes in this release.
- This is intended as a GitHub-only follow-up to `v2.1.0`; WinGet submission can wait for a later bundled release if preferred.
