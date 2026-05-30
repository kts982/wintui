package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// TestDiskCacheRoundTripPreservesTruncationForCompletion guards the bug where a
// truncated ID (clean prefix, ellipsis stripped) round-tripped through cache.json
// with idTruncated lost — because the flag was unexported — and then slipped past
// the completion filter looking like a valid full ID.
func TestDiskCacheRoundTripPreservesTruncationForCompletion(t *testing.T) {
	in := []Package{
		{Name: "Visual Studio Build Tools 2022", ID: "Microsoft.VisualStudio.2022.BuildTo", Source: "winget", idTruncated: true},
		{Name: "Mozilla Firefox", ID: "Mozilla.Firefox", Source: "winget"},
	}

	// Marshal/unmarshal exactly as the disk cache does.
	b, err := json.Marshal(diskCacheData{
		Installed:   toCachedPackages(in),
		Upgradeable: toCachedPackages(in),
		SavedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var data diskCacheData
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := fromCachedPackages(data.Installed)

	if !out[0].idTruncated {
		t.Errorf("truncated flag lost through cache round-trip: %+v", out[0])
	}
	if out[1].idTruncated {
		t.Errorf("non-truncated package gained truncated flag: %+v", out[1])
	}

	// The truncated prefix must NOT be offered; the full ID still should be.
	cands, _ := packageIDCompletions(out, "")
	for _, c := range cands {
		if strings.HasPrefix(c, "Microsoft.VisualStudio.2022.BuildTo\t") {
			t.Errorf("truncated ID offered as completion candidate: %q", c)
		}
	}
	foundFull := false
	for _, c := range cands {
		if strings.HasPrefix(c, "Mozilla.Firefox\t") {
			foundFull = true
		}
	}
	if !foundFull {
		t.Error("non-truncated ID missing from completion candidates")
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
