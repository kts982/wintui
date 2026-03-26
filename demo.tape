# WinTUI Demo — run with: vhs demo.tape
#
# Prerequisites:
#   - Windows 10/11 with winget
#   - ffmpeg installed and available on PATH
#   - VHS installed (go install github.com/charmbracelet/vhs@latest)
#   - wintui.exe built in the repo root
#
# Timing notes:
#   Adjust Sleep values if winget responses are slower on your machine.
#   The first winget call after a reboot can take 5-10s to initialize sources.

Output demo.gif
Require winget
Require ffmpeg

Set Shell "powershell"
Set FontSize 14
Set Width 1200
Set Height 650
Set TypingSpeed 60ms
Set Padding 15
Set Framerate 24
Set PlaybackSpeed 1.25

# ── Build and launch ────────────────────────────────────────────────

Hide
Type "clear"
Enter
Show

Type ".\\wintui.exe"
Enter
Sleep 5s

# ── Upgrade tab (loaded automatically) ──────────────────────────────

Down
Sleep 400ms
Up
Sleep 600ms

# ── Installed tab ───────────────────────────────────────────────────

Type "2"
Sleep 3s

Down@150ms 4
Sleep 500ms

# Fuzzy filter for Firefox
Type "/"
Sleep 400ms
Type "firefox"
Sleep 1s
Enter
Sleep 800ms

# Package details
Type "i"
Sleep 3s
Escape
Sleep 500ms

# Clear filter
Escape
Sleep 800ms

# ── Install tab ─────────────────────────────────────────────────────

Type "3"
Sleep 500ms

Type "neovim"
Enter
Sleep 4s

Down@300ms 2
Up
Sleep 500ms

# Package details
Type "i"
Sleep 3s
Escape
Sleep 500ms

# Back to search
Escape
Sleep 500ms

# ── Health Check tab ────────────────────────────────────────────────

Type "5"
Sleep 4s

Down@300ms 3
Sleep 1s

# ── Settings tab ────────────────────────────────────────────────────

Type "6"
Sleep 2s
Down@300ms 2
Sleep 500ms

# ── Quit ────────────────────────────────────────────────────────────

Ctrl+C
