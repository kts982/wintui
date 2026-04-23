package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRetryRequest_JSON(t *testing.T) {
	items := []retryItem{
		{ID: "id1", Name: "name1", Source: "src1", Version: "v1"},
		{ID: "id2", Name: "name2", Source: "src2", Version: "v2"},
	}
	req := retryRequest{
		Op:    retryOpUpgrade,
		Items: items,
	}

	encoded, err := encodeRetryItems(req.Items)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := decodeRetryItems(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !reflect.DeepEqual(req.Items, decoded) {
		t.Errorf("expected %v, got %v", req.Items, decoded)
	}
}

func TestTabForRetry(t *testing.T) {
	tests := []struct {
		op   retryOp
		want int
	}{
		{retryOpUpgrade, 0},
		{retryOpUninstall, 0},
		{retryOpInstall, 0},
	}

	for _, tt := range tests {
		req := retryRequest{Op: tt.op, ID: "pkg"}
		if got := tabForRetry(req); got != tt.want {
			t.Errorf("tabForRetry(%v) = %v, want %v", tt.op, got, tt.want)
		}
	}
}

func TestNewRetryRequestFromItemsPreservesSingleTargetVersion(t *testing.T) {
	req := newRetryRequestFromItems(retryOpUpgrade, []retryItem{{
		ID:      "Neovim.Neovim",
		Name:    "Neovim",
		Source:  "winget",
		Version: "0.11.5",
	}})
	if req == nil {
		t.Fatal("newRetryRequestFromItems() returned nil")
	}
	if req.Op != retryOpUpgrade || req.ID != "Neovim.Neovim" || req.Version != "0.11.5" {
		t.Fatalf("retry request = %#v, want single item with explicit version", req)
	}
}

func TestExecModalReviewShowsCommandsWhenToggled(t *testing.T) {
	items := []batchItem{
		{
			action:  retryOpUpgrade,
			item:    workspaceItem{pkg: Package{Name: "Git", ID: "Git.Git", Source: "winget"}},
			command: "winget upgrade --id Git.Git --exact --scope machine",
		},
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseReview

	plain := stripANSI(m.view(120, 30))
	if strings.Contains(plain, "--id Git.Git") {
		t.Fatalf("command preview should be hidden by default, got %q", plain)
	}
	if !strings.Contains(plain, "? show commands") {
		t.Fatalf("review help should hint the ? toggle, got %q", plain)
	}

	m.showCommands = true
	plain = stripANSI(m.view(120, 30))
	if !strings.Contains(plain, "winget upgrade --id Git.Git") {
		t.Fatalf("toggled review should show command line, got %q", plain)
	}
	if !strings.Contains(plain, "? hide commands") {
		t.Fatalf("toggle hint should flip to 'hide commands', got %q", plain)
	}
}

func TestExecModalReviewScrollsWhenOverflowing(t *testing.T) {
	items := make([]batchItem, 20)
	for i := range items {
		name := fmt.Sprintf("Pkg%02d", i)
		items[i] = batchItem{
			action:  retryOpUpgrade,
			item:    workspaceItem{pkg: Package{Name: name, ID: name + ".ID", Source: "winget"}},
			command: fmt.Sprintf("winget upgrade --id %s.ID", name),
		}
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseReview
	m.showCommands = true

	// Small terminal so the body must overflow.
	assertOverflowModalFits := func(t *testing.T, termW, termH int) {
		t.Helper()
		plain := stripANSI(m.view(termW, termH))
		if !strings.Contains(plain, "scroll") {
			t.Fatalf("overflow modal should show scroll hint in footer, got %q", plain)
		}
		if !strings.Contains(plain, "enter upgrade") {
			t.Fatalf("overflow modal must always show the actions footer, got %q", plain)
		}
		if !strings.Contains(plain, "╭") {
			t.Fatalf("overflow modal must show top border, got %q", plain)
		}
		if !strings.Contains(plain, "╰") {
			t.Fatalf("overflow modal must show bottom border — the whole point of the fit budget is not clipping it. Got %q", plain)
		}
		rows := strings.Count(plain, "\n") + 1
		if rows > termH {
			t.Fatalf("rendered modal has %d rows, should fit within terminal height %d", rows, termH)
		}
	}

	m.scroll = 0
	assertOverflowModalFits(t, 100, 20)
	// Also verify the tight budget the user actually hit (~30 content rows).
	assertOverflowModalFits(t, 120, 30)

	plain := stripANSI(m.view(100, 20))
	if !strings.Contains(plain, "Pkg00") {
		t.Fatalf("overflow modal should start at top and show Pkg00, got %q", plain)
	}
	if strings.Contains(plain, "Pkg19") {
		t.Fatalf("overflow modal should NOT show Pkg19 when scroll=0, got %q", plain)
	}

	// Scroll to the end — the last item must be visible and the footer still shown.
	maxs := m.maxScroll(100, 20)
	if maxs <= 0 {
		t.Fatalf("maxScroll should be > 0 for overflow modal, got %d", maxs)
	}
	m.scroll = maxs
	plain = stripANSI(m.view(100, 20))
	if !strings.Contains(plain, "Pkg19") {
		t.Fatalf("after scrolling to end, Pkg19 should appear, got %q", plain)
	}
	if !strings.Contains(plain, "enter upgrade") {
		t.Fatalf("actions footer should remain visible when scrolled, got %q", plain)
	}
	if !strings.Contains(plain, "╰") {
		t.Fatalf("bottom border should remain visible when scrolled to end, got %q", plain)
	}
}

func TestExecModalElevationCandidatesFiltersCorrectly(t *testing.T) {
	forceNotElevated(t)
	items := []batchItem{
		{item: workspaceItem{pkg: Package{ID: "Pkg.One", Source: "winget"}}, status: batchFailed, err: assertErr("installer failed with a fatal error (1603)"), output: "exit code: 1603"},
		{item: workspaceItem{pkg: Package{ID: "Pkg.Two", Source: "winget"}}, status: batchDone},
		{item: workspaceItem{pkg: Package{ID: "Pkg.Three", Source: "winget"}}, status: batchFailed, err: assertErr("package requires administrator privileges (0x8a150056)"), output: "0x8a150056"},
	}
	m := newExecModal(retryOpUpgrade, items)
	// Simulate completion.
	m.items = items
	m.phase = execPhaseComplete

	if !m.hasElevationCandidates() {
		t.Fatal("expected elevation candidates")
	}
	candidates := m.elevationCandidateItems()
	if len(candidates) != 2 {
		t.Fatalf("elevation candidates len = %d, want 2", len(candidates))
	}
	if candidates[0].item.pkg.ID != "Pkg.One" {
		t.Fatalf("first candidate = %s, want Pkg.One", candidates[0].item.pkg.ID)
	}
	if candidates[1].item.pkg.ID != "Pkg.Three" {
		t.Fatalf("second candidate = %s, want Pkg.Three", candidates[1].item.pkg.ID)
	}
}

func TestExecModalBlockedByProcessRetryCandidates(t *testing.T) {
	forceNotElevated(t)
	items := []batchItem{
		{
			item:          workspaceItem{pkg: Package{Name: "Comet", ID: "Perplexity.Comet", Source: "winget"}},
			action:        retryOpUninstall,
			status:        batchFailed,
			err:           assertErr("exit status 0x8a150066"),
			output:        "exit status 0x8a150066",
			blockedByProc: true,
			allVersions:   true,
		},
		{item: workspaceItem{pkg: Package{ID: "Pkg.Done", Source: "winget"}}, status: batchDone},
	}
	m := newExecModal(retryOpUninstall, items)
	m.items = items
	m.phase = execPhaseComplete

	if m.hasElevationCandidates() {
		t.Fatal("did not expect elevation candidates for a blocked-by-process-only batch")
	}
	if !m.hasBlockedByProcess() {
		t.Fatal("expected hasBlockedByProcess = true")
	}
	candidates := m.elevationCandidateItems()
	if len(candidates) != 1 {
		t.Fatalf("retry candidates len = %d, want 1", len(candidates))
	}
	if candidates[0].item.pkg.ID != "Perplexity.Comet" {
		t.Fatalf("candidate ID = %q, want Perplexity.Comet", candidates[0].item.pkg.ID)
	}
	if !candidates[0].allVersions {
		t.Fatal("expected allVersions to carry over into retry candidate")
	}

	// helpKeys in complete phase should advertise ctrl+e with the plain
	// "retry" label (no elevation candidates in this batch).
	helps := bindingHelps(m.helpKeys())
	var found bool
	for _, h := range helps {
		if h.Key == "ctrl+e" {
			found = true
			if h.Desc != "retry" {
				t.Fatalf("ctrl+e desc = %q, want %q", h.Desc, "retry")
			}
		}
	}
	if !found {
		t.Fatal("expected ctrl+e binding to be advertised when items are blocked by a running process")
	}
}

func TestExecModalPendingSelfUpgradeRequiresAdminAdvertisesCtrlA(t *testing.T) {
	forceNotElevated(t)

	m := newExecModal(retryOpUpgrade, []batchItem{{
		action: retryOpUpgrade,
		item: workspaceItem{
			pkg: Package{Name: "WinTUI", ID: selfPackageID, Source: "winget"},
		},
		status: batchPendingRestart,
	}})
	m.phase = execPhaseComplete

	helps := bindingHelps(m.helpKeys())
	var (
		foundCtrlA bool
		foundEnter bool
	)
	for _, h := range helps {
		switch h.Key {
		case "ctrl+a":
			foundCtrlA = true
			if h.Desc != "relaunch as admin" {
				t.Fatalf("ctrl+a desc = %q, want %q", h.Desc, "relaunch as admin")
			}
		case "enter":
			foundEnter = true
			if h.Desc != "close" {
				t.Fatalf("enter desc = %q, want %q", h.Desc, "close")
			}
		}
	}
	if !foundCtrlA {
		t.Fatal("expected ctrl+a binding for admin-gated self-upgrade")
	}
	if !foundEnter {
		t.Fatal("expected enter binding to remain available for close")
	}

	_, body, actions := m.viewComplete()
	plainBody := strings.Join(body, "\n")
	if !strings.Contains(plainBody, "restart WinTUI as admin to finish upgrade") {
		t.Fatalf("viewComplete() body missing admin restart guidance: %q", plainBody)
	}
	if !strings.Contains(actions, "ctrl+a") {
		t.Fatalf("viewComplete() actions missing ctrl+a: %q", actions)
	}
}
