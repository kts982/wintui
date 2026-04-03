package main

import (
	"reflect"
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

func TestExecModalElevationCandidatesFiltersCorrectly(t *testing.T) {
	forceNotElevated(t)
	items := []batchItem{
		{item: workspaceItem{pkg: Package{ID: "Pkg.One", Source: "winget"}}, status: batchFailed, err: assertErr("installer failed with a fatal error (1603)"), output: "exit code: 1603"},
		{item: workspaceItem{pkg: Package{ID: "Pkg.Two", Source: "winget"}}, status: batchDone},
		{item: workspaceItem{pkg: Package{ID: "Pkg.Three", Source: "winget"}}, status: batchFailed, err: assertErr("package requires administrator privileges (0x8a150056)"), output: "0x8a150056"},
	}
	m := newExecModal("upgrade", items)
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
