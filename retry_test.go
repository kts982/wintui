package main

import (
	"reflect"
	"testing"
)

func TestRetryRequestStartupArgsRoundTripForBatch(t *testing.T) {
	req := retryRequest{
		Op: retryOpUpgrade,
		Items: []retryItem{
			{ID: "Mozilla.Firefox", Source: "winget"},
			{ID: "Neovim.Neovim", Source: "winget", Version: "0.11.5"},
		},
	}

	args, err := req.startupArgs()
	if err != nil {
		t.Fatalf("startupArgs() error = %v", err)
	}

	_, got, err := parseStartupArgs(args)
	if err != nil {
		t.Fatalf("parseStartupArgs() error = %v", err)
	}
	if got == nil {
		t.Fatal("parseStartupArgs() = nil, want retry request")
	}
	if got.Op != req.Op {
		t.Fatalf("Op = %q, want %q", got.Op, req.Op)
	}
	if !reflect.DeepEqual(got.items(), req.items()) {
		t.Fatalf("items() = %#v, want %#v", got.items(), req.items())
	}
}

func TestNewUpgradeScreenWithRetrySupportsBatchItems(t *testing.T) {
	req := retryRequest{
		Op: retryOpUpgrade,
		Items: []retryItem{
			{ID: "Mozilla.Firefox", Source: "winget"},
			{ID: "Neovim.Neovim", Source: "winget", Version: "0.11.5"},
		},
	}

	s := newUpgradeScreenWithRetry(req)
	if s.batchTotal != 2 {
		t.Fatalf("batchTotal = %d, want 2", s.batchTotal)
	}
	if !reflect.DeepEqual(s.batchIDs, []string{"Mozilla.Firefox", "Neovim.Neovim"}) {
		t.Fatalf("batchIDs = %#v", s.batchIDs)
	}
	if got := s.selectedVersions[packageSourceKey("Neovim.Neovim", "winget")]; got != "0.11.5" {
		t.Fatalf("selected version = %q, want 0.11.5", got)
	}
}

func TestNewPackagesScreenWithRetrySupportsBatchItems(t *testing.T) {
	req := retryRequest{
		Op: retryOpUninstall,
		Items: []retryItem{
			{ID: "Mozilla.Firefox", Name: "Mozilla Firefox", Source: "winget"},
			{ID: "Neovim.Neovim", Name: "Neovim", Source: "winget"},
		},
	}

	s := newPackagesScreenWithRetry(req)
	if s.batchTotal != 2 {
		t.Fatalf("batchTotal = %d, want 2", s.batchTotal)
	}
	if len(s.batchPackages) != 2 {
		t.Fatalf("len(batchPackages) = %d, want 2", len(s.batchPackages))
	}
	if s.batchPackages[1].ID != "Neovim.Neovim" {
		t.Fatalf("batchPackages[1].ID = %q, want Neovim.Neovim", s.batchPackages[1].ID)
	}
}
