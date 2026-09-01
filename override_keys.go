package main

import (
	"slices"
	"strings"
)

// ── Package-rule key resolution ────────────────────────────────────
//
// Per-package rules live in Settings.Packages keyed "source:ID" (qualified) or
// "ID" (legacy bare, from before rules were source-aware). winget IDs are
// case-insensitive and every CLI path deliberately accepts any casing, so the
// map lookup must be case-insensitive too — but Go map iteration is random,
// so a naive scan would answer differently across runs whenever case-variant
// duplicates exist in the file. Resolution therefore follows a TOTAL
// precedence:
//
//	exact qualified → exact bare → case-insensitive qualified → case-insensitive bare
//
// with ties inside a case-insensitive tier broken by sorted key order.
//
// Casing policy: the map key is storage identity. A write reuses the casing
// of the key it found (exact first, else the sorted-first case variant) rather
// than re-keying the rule, so the file stays stable; only a brand-new rule is
// keyed with the casing the caller supplied. CLI paths that take user typing
// should pass it through canonicalPackageID first to pick up winget's own
// casing from the read-only disk cache when it is warm.

// overrideKeyAliases returns every map key that refers to pkgID/source in
// either tier, in precedence order. Empty when no rule exists.
func (s Settings) overrideKeyAliases(pkgID, source string) []string {
	if len(s.Packages) == 0 {
		return nil
	}
	qualified := packageRuleKey(pkgID, source)
	bare := pkgID

	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if _, ok := s.Packages[qualified]; ok {
		add(qualified)
	}
	if _, ok := s.Packages[bare]; ok {
		add(bare)
	}

	var ciQualified, ciBare []string
	for k := range s.Packages {
		if seen[k] {
			continue
		}
		switch {
		case qualified != bare && strings.EqualFold(k, qualified):
			ciQualified = append(ciQualified, k)
		case strings.EqualFold(k, bare):
			ciBare = append(ciBare, k)
		}
	}
	slices.Sort(ciQualified)
	slices.Sort(ciBare)
	for _, k := range ciQualified {
		add(k)
	}
	for _, k := range ciBare {
		add(k)
	}
	return out
}

// resolveOverrideKey returns the single map key that answers for pkgID/source
// under the total precedence above.
func (s Settings) resolveOverrideKey(pkgID, source string) (string, bool) {
	aliases := s.overrideKeyAliases(pkgID, source)
	if len(aliases) == 0 {
		return "", false
	}
	return aliases[0], true
}

// canonical returns o with the legacy representation folded into the current
// one: `ignore: true` (pre-policy permanent hold) becomes `update_policy:
// hold`. Applied in the shared write layer so ANY edit — a TUI scope change as
// much as a CLI `rules set` — migrates the rule, and the CLI never has to
// expose a second permanent-hold concept. Version-specific holds keep their
// own field (IgnoreVersion); they are a distinct, honest state.
func (o PackageOverride) canonical() PackageOverride {
	if o.Ignore {
		o.Ignore = false
		o.UpdatePolicy = PolicyHold
	}
	return o
}

// overridesEquivalent reports whether two rules mean the same thing after
// canonicalization (pointer fields compared by value).
func overridesEquivalent(a, b PackageOverride) bool {
	a, b = a.canonical(), b.canonical()
	if (a.Elevate == nil) != (b.Elevate == nil) {
		return false
	}
	if a.Elevate != nil && *a.Elevate != *b.Elevate {
		return false
	}
	return a.Scope == b.Scope &&
		a.Architecture == b.Architecture &&
		normalizeUpdatePolicy(a.UpdatePolicy) == normalizeUpdatePolicy(b.UpdatePolicy) &&
		a.IgnoreVersion == b.IgnoreVersion
}

// overrideAliasGroups groups the keys of s.Packages that are case-variants of
// one another within the same tier (qualified vs bare), returning only groups
// with more than one member, each sorted, in a deterministic order.
func (s Settings) overrideAliasGroups() [][]string {
	if len(s.Packages) < 2 {
		return nil
	}
	byFold := map[string][]string{}
	for k := range s.Packages {
		byFold[strings.ToLower(k)] = append(byFold[strings.ToLower(k)], k)
	}
	folds := make([]string, 0, len(byFold))
	for f, keys := range byFold {
		if len(keys) > 1 {
			folds = append(folds, f)
		}
	}
	slices.Sort(folds)
	groups := make([][]string, 0, len(folds))
	for _, f := range folds {
		keys := byFold[f]
		slices.Sort(keys)
		groups = append(groups, keys)
	}
	return groups
}

// collapseOverrideAliases is the one-shot load-time migration for duplicate
// case-variant keys (precedent: expireVersionIgnores). Groups whose members
// are value-equivalent collapse to their sorted-first key; groups whose
// members disagree are left untouched (the precedence order still answers
// deterministically) and reported so doctor can warn and mutating commands
// can refuse. Returns the number of keys removed.
func (s *Settings) collapseOverrideAliases() (removed int, conflicts [][]string) {
	for _, group := range s.overrideAliasGroups() {
		first := s.Packages[group[0]]
		equivalent := true
		for _, k := range group[1:] {
			if !overridesEquivalent(first, s.Packages[k]) {
				equivalent = false
				break
			}
		}
		if !equivalent {
			conflicts = append(conflicts, group)
			continue
		}
		for _, k := range group[1:] {
			delete(s.Packages, k)
			removed++
		}
	}
	if len(s.Packages) == 0 {
		s.Packages = nil
	}
	return removed, conflicts
}

// overrideConflicts returns the case-variant key groups whose rules disagree.
// Empty when the file is clean.
func (s Settings) overrideConflicts() [][]string {
	var conflicts [][]string
	for _, group := range s.overrideAliasGroups() {
		first := s.Packages[group[0]]
		for _, k := range group[1:] {
			if !overridesEquivalent(first, s.Packages[k]) {
				conflicts = append(conflicts, group)
				break
			}
		}
	}
	return conflicts
}

// overrideConflictFor reports whether pkgID/source is affected by a
// conflicting duplicate group, returning that group. Mutating CLI commands
// refuse with a collision error in that case instead of silently picking one.
func (s Settings) overrideConflictFor(pkgID, source string) ([]string, bool) {
	aliases := s.overrideKeyAliases(pkgID, source)
	if len(aliases) < 2 {
		return nil, false
	}
	for _, group := range s.overrideConflicts() {
		for _, k := range group {
			if slices.Contains(aliases, k) {
				return group, true
			}
		}
	}
	return nil, false
}

// canonicalPackageID returns winget's own casing for a user-typed package ID
// when the read-only disk cache is warm and knows the package (installed or
// upgradeable), else the input unchanged. Reading the cache is allowed — the
// "TUI-write-only" rule governs writes. Cache casing is a courtesy for NEW
// rules only; it is never used to re-key existing ones.
func canonicalPackageID(id string) (string, bool) {
	installed, upgradeable, _, ok := cache.loadFromDisk()
	if !ok {
		return id, false
	}
	for _, list := range [][]Package{installed, upgradeable} {
		for _, p := range list {
			if strings.EqualFold(p.ID, id) {
				return p.ID, true
			}
		}
	}
	return id, false
}
