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
		{retryOpUninstall, 1},
		{retryOpInstall, 2},
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

func TestUpgradeRetryInfoUsesOnlyFailedElevationCandidates(t *testing.T) {
	forceNotElevated(t)
	s := newUpgradeScreen()
	s.batchIDs = []string{"Pkg.One", "Pkg.Two", "Pkg.Three"}
	s.batchSources = []string{"winget", "winget", "winget"}
	s.batchVersions = []string{"1.0.0", "2.0.0", ""}
	s.batchErrs = []error{
		assertErr("installer failed with a fatal error (1603)"),
		nil,
		assertErr("package requires administrator privileges to install (0x8a150056)"),
	}
	s.batchOutputs = []string{
		"Upgrade failed with exit code: 1603",
		"",
		"package requires administrator privileges to install (0x8a150056)",
	}
	s.packages = []Package{
		{Name: "One", ID: "Pkg.One", Source: "winget"},
		{Name: "Two", ID: "Pkg.Two", Source: "winget"},
		{Name: "Three", ID: "Pkg.Three", Source: "winget"},
	}

	info := s.retryInfo()
	if info.req == nil {
		t.Fatal("retryInfo() returned nil request")
	}
	if !info.hard {
		t.Fatal("retryInfo() hard = false, want true when one failure requires elevation")
	}
	items := info.req.items()
	if len(items) != 2 {
		t.Fatalf("retry items len = %d, want 2", len(items))
	}
	if items[0].ID != "Pkg.One" || items[0].Version != "1.0.0" {
		t.Fatalf("first retry item = %#v, want Pkg.One with explicit target version", items[0])
	}
	if items[1].ID != "Pkg.Three" {
		t.Fatalf("second retry item = %#v, want Pkg.Three", items[1])
	}
}

func TestPackagesRetryInfoUsesOnlyFailedElevationCandidates(t *testing.T) {
	forceNotElevated(t)
	s := newPackagesScreen()
	s.batchPackages = []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"},
		{Name: "Neovim", ID: "Neovim.Neovim", Source: "winget"},
	}
	s.batchErrs = []error{
		nil,
		assertErr("installer failed with a fatal error (1603)"),
	}
	s.batchOutputs = []string{
		"",
		"Uninstall failed with exit code: 1603",
	}

	info := s.retryInfo()
	if info.req == nil {
		t.Fatal("retryInfo() returned nil request")
	}
	if info.hard {
		t.Fatal("retryInfo() hard = true, want false for soft 1603-only retry")
	}
	items := info.req.items()
	if len(items) != 1 || items[0].ID != "Neovim.Neovim" {
		t.Fatalf("retry items = %#v, want only failed Neovim uninstall", items)
	}
}
