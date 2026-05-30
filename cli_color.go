package main

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

// CLI color helpers. These paint short status strings (per-package headers,
// success/failure markers, summary lines) in the active theme palette — but
// ONLY when stdout is an interactive, color-capable terminal and NO_COLOR is
// unset. Piped/redirected output (scripts, CI, files) and --json stay plain, so
// the human-readable text contract is preserved. Table bodies are intentionally
// left uncolored: they go through text/tabwriter, which would miscount ANSI
// escapes as visible width and misalign the columns.

// cliColorEnabled reports whether CLI output should carry ANSI color.
func cliColorEnabled() bool {
	return term.IsTerminal(os.Stdout.Fd()) && os.Getenv("NO_COLOR") == ""
}

func cliPaint(c color.Color, s string) string {
	if !cliColorEnabled() {
		return s
	}
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

func cliAccent(s string) string  { return cliPaint(accent, s) }
func cliSuccess(s string) string { return cliPaint(success, s) }
func cliDanger(s string) string  { return cliPaint(danger, s) }
