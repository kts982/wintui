package main

import (
	"strings"

	"github.com/spf13/cobra"
)

// Dynamic shell completion for package IDs.
//
// Completion requests run in a separate short-lived `wintui __complete ...`
// process where the in-memory package cache is always cold, so these read the
// on-disk cache (cache.json) directly. They must NEVER call winget: a live
// winget invocation would block the shell while the user is mid-keystroke.
// Truncated IDs are skipped because they can't be passed to
// `winget --id <id> --exact`.

// packageIDCompletions builds completion candidates ("ID\tdescription") from a
// package list, prefix-filtered by what the user has typed so far.
func packageIDCompletions(pkgs []Package, toComplete string) ([]string, cobra.ShellCompDirective) {
	prefix := strings.ToLower(toComplete)
	seen := make(map[string]bool, len(pkgs))
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		if p.ID == "" || p.idTruncated || seen[p.ID] {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(p.ID), prefix) {
			continue
		}
		seen[p.ID] = true
		desc := p.Name
		if p.Available != "" {
			desc = strings.TrimSpace(p.Name + " " + p.Version + " → " + p.Available)
		}
		out = append(out, p.ID+"\t"+desc)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeUpgradeableIDs completes `wintui upgrade --id <TAB>` from the cached
// upgradeable list (only packages with an available update are upgrade targets).
func completeUpgradeableIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	_, upgradeable, _, _ := cache.loadFromDisk()
	return packageIDCompletions(upgradeable, toComplete)
}

// completeInstalledIDs completes `wintui show <TAB>` from the cached installed
// list. Only the first positional arg is completed.
func completeInstalledIDs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	installed, _, _, _ := cache.loadFromDisk()
	return packageIDCompletions(installed, toComplete)
}
