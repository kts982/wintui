package main

import "testing"

func TestSelectUpgradesEmptyReturnsZeroHidden(t *testing.T) {
	visible, hidden := selectUpgrades(nil, DefaultSettings())
	if len(visible) != 0 {
		t.Fatalf("len(visible) = %d, want 0", len(visible))
	}
	if hidden != 0 {
		t.Fatalf("hidden = %d, want 0", hidden)
	}
}

func TestSelectUpgradesPassesThroughWhenNoIgnoreRules(t *testing.T) {
	pkgs := []Package{
		{Name: "Git", ID: "Git.Git", Source: "winget", Available: "2.1"},
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Available: "120"},
	}
	visible, hidden := selectUpgrades(pkgs, DefaultSettings())
	if len(visible) != 2 {
		t.Fatalf("len(visible) = %d, want 2", len(visible))
	}
	if hidden != 0 {
		t.Fatalf("hidden = %d, want 0", hidden)
	}
}

func TestSelectUpgradesFiltersFullIgnore(t *testing.T) {
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Git.Git", "winget"): {Ignore: true},
	}
	pkgs := []Package{
		{Name: "Git", ID: "Git.Git", Source: "winget", Available: "2.1"},
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Available: "120"},
	}
	visible, hidden := selectUpgrades(pkgs, settings)
	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1", len(visible))
	}
	if visible[0].ID != "Mozilla.Firefox" {
		t.Fatalf("visible[0] = %q, want Mozilla.Firefox", visible[0].ID)
	}
	if hidden != 1 {
		t.Fatalf("hidden = %d, want 1", hidden)
	}
}

func TestSelectUpgradesFiltersVersionPin(t *testing.T) {
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Git.Git", "winget"): {IgnoreVersion: "2.1"},
	}
	pkgs := []Package{
		{Name: "Git", ID: "Git.Git", Source: "winget", Available: "2.1"},
		{Name: "GitNew", ID: "Git.Git", Source: "winget", Available: "2.2"},
	}
	visible, hidden := selectUpgrades(pkgs, settings)
	if len(visible) != 1 {
		t.Fatalf("len(visible) = %d, want 1", len(visible))
	}
	if visible[0].Available != "2.2" {
		t.Fatalf("visible[0].Available = %q, want 2.2 (the pinned 2.1 should be hidden)", visible[0].Available)
	}
	if hidden != 1 {
		t.Fatalf("hidden = %d, want 1", hidden)
	}
}

func TestPlanUpgradesPartitionsPolicies(t *testing.T) {
	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Auto.Pkg", "winget"): {UpdatePolicy: PolicyAuto},
		packageRuleKey("Held.Pkg", "winget"): {UpdatePolicy: PolicyHold},
	}
	pkgs := []Package{
		{Name: "Ask", ID: "Ask.Pkg", Source: "winget", Available: "2.0"},
		{Name: "Auto", ID: "Auto.Pkg", Source: "winget", Available: "2.0"},
		{Name: "Held", ID: "Held.Pkg", Source: "winget", Available: "2.0"},
	}

	plan := planUpgrades(pkgs, settings)
	if len(plan.Visible) != 2 {
		t.Fatalf("len(Visible) = %d, want 2", len(plan.Visible))
	}
	if len(plan.Auto) != 1 || plan.Auto[0].ID != "Auto.Pkg" {
		t.Fatalf("Auto = %#v, want only Auto.Pkg", plan.Auto)
	}
	if len(plan.Held) != 1 || plan.Held[0].ID != "Held.Pkg" {
		t.Fatalf("Held = %#v, want only Held.Pkg", plan.Held)
	}
	if plan.HiddenCount() != 1 {
		t.Fatalf("HiddenCount = %d, want 1", plan.HiddenCount())
	}
}
