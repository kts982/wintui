package main

type upgradePlan struct {
	// Visible contains every non-held upgrade: normal Ask packages plus Auto
	// packages. This preserves the existing "upgrade --all means all visible
	// updates" behavior while allowing Auto to be consumed separately.
	Visible []Package
	Auto    []Package
	Held    []Package
}

func (p upgradePlan) HiddenCount() int {
	return len(p.Held)
}

// planUpgrades partitions the raw winget upgradeable list by per-package
// update policy. It is the single source of truth for "which upgrades count?"
// across the TUI and headless CLI.
//
// Pure function: no globals, no side effects. Settings must be passed in.
func planUpgrades(upgradeable []Package, settings Settings) upgradePlan {
	plan := upgradePlan{
		Visible: make([]Package, 0, len(upgradeable)),
		Auto:    make([]Package, 0),
		Held:    make([]Package, 0),
	}
	for _, pkg := range upgradeable {
		switch settings.updatePolicy(pkg.ID, pkg.Source, pkg.Available) {
		case PolicyHold:
			plan.Held = append(plan.Held, pkg)
			continue
		case PolicyAuto:
			plan.Auto = append(plan.Auto, pkg)
		}
		plan.Visible = append(plan.Visible, pkg)
	}
	return plan
}

// selectUpgrades keeps the v2.4 two-value call shape available while callers
// move to policy-aware buckets.
func selectUpgrades(upgradeable []Package, settings Settings) (visible []Package, hidden int) {
	plan := planUpgrades(upgradeable, settings)
	return plan.Visible, plan.HiddenCount()
}
