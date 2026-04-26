package main

// selectUpgrades partitions the raw winget upgradeable list into the set the
// user actually wants to act on (visible) and a count of those filtered out
// by ignore rules (hidden). It is the single source of truth for "which
// upgrades count?" — both the TUI's grouped list and the headless CLI must
// route through it so the two surfaces never disagree on whether a hidden
// package is "an update available".
//
// Pure function: no globals, no side effects. Settings must be passed in.
func selectUpgrades(upgradeable []Package, settings Settings) (visible []Package, hidden int) {
	visible = make([]Package, 0, len(upgradeable))
	for _, pkg := range upgradeable {
		if settings.isIgnored(pkg.ID, pkg.Source, pkg.Available) {
			hidden++
			continue
		}
		visible = append(visible, pkg)
	}
	return visible, hidden
}
