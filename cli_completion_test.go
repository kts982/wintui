package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPackageIDCompletionsFiltersAndFormats(t *testing.T) {
	pkgs := []Package{
		{ID: "Mozilla.Firefox", Name: "Mozilla Firefox", Version: "1.0", Available: "1.1"},
		{ID: "Microsoft.Edge", Name: "Microsoft Edge"},
		{ID: "Truncated.Pkg…", Name: "Truncated", idTruncated: true},
		{ID: "Mozilla.Firefox", Name: "dup"}, // duplicate ID
		{ID: "", Name: "blank id"},
	}

	out, directive := packageIDCompletions(pkgs, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", directive)
	}
	// Truncated, blank, and the duplicate are dropped → 2 candidates.
	if len(out) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(out), out)
	}
	for _, c := range out {
		if strings.HasPrefix(c, "Truncated") || strings.HasPrefix(c, "\t") {
			t.Errorf("unexpected candidate kept: %q", c)
		}
	}
	// Upgradeable package shows the version transition in its description.
	if !strings.Contains(out[0], "Mozilla.Firefox\t") || !strings.Contains(out[0], "1.0 → 1.1") {
		t.Errorf("firefox candidate description wrong: %q", out[0])
	}
}

func TestPackageIDCompletionsPrefixIsCaseInsensitive(t *testing.T) {
	pkgs := []Package{
		{ID: "Mozilla.Firefox", Name: "Firefox"},
		{ID: "Microsoft.Edge", Name: "Edge"},
		{ID: "Notepad++.Notepad++", Name: "Notepad++"},
	}
	out, _ := packageIDCompletions(pkgs, "mic")
	if len(out) != 1 || !strings.HasPrefix(out[0], "Microsoft.Edge\t") {
		t.Fatalf("prefix 'mic' should match only Microsoft.Edge, got: %v", out)
	}
}
