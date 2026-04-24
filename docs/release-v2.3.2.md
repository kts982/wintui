# WinTUI v2.3.2 Release Notes

WinTUI v2.3.2 is a small patch release centered on a more AV-friendly self-upgrade path and a handful of polish fixes.

## Highlights

- **Self-upgrade no longer drops an EXE into `%TEMP%`.** v2.3.1 and earlier copied `wintui.exe` into `%TEMP%\` and ran it detached/hidden to finish the upgrade, which triggered AV heuristics on some machines. The handoff now runs from a short-lived PowerShell script under `%LOCALAPPDATA%\wintui\self-update\` that waits for the parent to exit, runs winget, and self-deletes. No dropped executable, fewer false positives.
- **Self-upgrade requires an already elevated WinTUI session.** Non-admin sessions stop at the result modal and show `Ctrl+A` to relaunch as admin and retry. If Windows blocks or cancels the relaunch, the modal shows a manual administrator PowerShell command the user can run instead. After the handoff finishes, WinTUI no longer reopens itself automatically — start `wintui` again manually.
- **Atomic settings persistence.** `%APPDATA%\wintui\settings.json` is now written via temp file + rename, matching the disk cache. A crash or power loss mid-save can no longer leave a partial JSON file.

## Bug fixes

- Settings → Default Source detail text claimed the setting affected "searches, installs, and upgrade queries". It doesn't affect upgrade queries — WinTUI deliberately omits `--source` on `winget upgrade` to preserve the `Available` column. Text corrected to "affects searches and installs".
- `docs/elevation.md` opened with "three elevation paths" while listing four. Count corrected.

## Internal

- Dropped a couple of unused function parameters (`formatBatchResults`'s `outputs`, `viewConfirm`'s `bg`) and a vestigial `_ = i` in the winget table parser.

## Notes

- No breaking changes to settings, CLI flags, or keybindings from v2.3.1.
- If you self-upgraded with v2.3.1 or earlier and still have a stale `wintui_selfupdate_*.exe` under `%TEMP%`, you can delete it manually — the new handoff path no longer creates that file.
