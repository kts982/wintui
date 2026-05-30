package main

import (
	"strings"
	"testing"
)

// Tests run without a terminal, so every CLI color helper must return the input
// unchanged — guaranteeing piped/redirected output carries no ANSI escapes and
// the human-readable text contract (and scripts parsing it) stays intact.
func TestCLIColorHelpersPlainWhenNotTTY(t *testing.T) {
	const s = "status line"
	for name, got := range map[string]string{
		"cliAccent":  cliAccent(s),
		"cliSuccess": cliSuccess(s),
		"cliDanger":  cliDanger(s),
	} {
		if got != s {
			t.Errorf("%s off-TTY = %q, want plain %q", name, got, s)
		}
		if strings.Contains(got, "\x1b") {
			t.Errorf("%s leaked an ANSI escape off-TTY: %q", name, got)
		}
	}
	if cliColorEnabled() {
		t.Error("cliColorEnabled() should be false in the test harness (not a TTY)")
	}
}
