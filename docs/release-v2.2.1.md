# WinTUI v2.2.1 Release Notes

WinTUI v2.2.1 is a focused hotfix release for the self-upgrade path introduced in v2.1.0.

## Highlights

- Fixed the `restart & finish` flow so pressing `Enter` on the self-upgrade completion modal now exits WinTUI and actually starts the handoff helper.
- Detached the temporary self-upgrade helper from the current console so it no longer corrupts the caller's terminal session while it finishes the portable-package upgrade in the background.
- Relaunch now opens WinTUI in a new console window after the helper completes.
- Cleared stale package cache after a successful self-upgrade so WinTUI does not relaunch showing itself as still upgradeable.
- Added helper-side logging and regression coverage around the self-upgrade handoff path.

## Notes

- This release does not add new user-facing package-management features. It hardens the existing WinTUI self-update behavior.
- Portable-package self-upgrade now forces the WinGet replacement step in the helper path to recover from the modified-package state created during bootstrap testing.
