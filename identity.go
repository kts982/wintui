package main

import "strings"

// isNonCanonical returns true if the ID is a raw system identity
// (MSIX path, GUID, package family name) rather than a canonical winget ID.
func isNonCanonical(id string) bool {
	// MSIX paths
	if strings.HasPrefix(id, "MSIX\\") || strings.HasPrefix(id, "MSIX/") {
		return true
	}
	// GUIDs: {xxxxxxxx-xxxx-...}
	if strings.HasPrefix(id, "{") && strings.HasSuffix(id, "}") {
		return true
	}
	// Package family names: Name_hash (13+ char suffix after last underscore)
	if idx := strings.LastIndex(id, "_"); idx > 0 {
		suffix := id[idx+1:]
		if len(suffix) >= 13 {
			return true
		}
	}
	return false
}

// identityKind returns a human-readable identity category for a package.
func identityKind(p Package) string {
	if p.Source == "winget" {
		return "winget"
	}
	if p.Source == "msstore" {
		return "msstore"
	}
	if isNonCanonical(p.ID) {
		return "system"
	}
	return "other"
}

// deduplicatePackages removes raw system identity entries when a canonical
// entry with the same name already exists. Order is preserved.
func deduplicatePackages(pkgs []Package) []Package {
	// First pass: find names that have at least one canonical entry.
	hasCanonical := make(map[string]bool)
	for _, p := range pkgs {
		if !isNonCanonical(p.ID) || p.Source == "winget" || p.Source == "msstore" {
			hasCanonical[strings.ToLower(strings.TrimSpace(p.Name))] = true
		}
	}

	// Second pass: drop non-canonical duplicates.
	result := make([]Package, 0, len(pkgs))
	for _, p := range pkgs {
		key := strings.ToLower(strings.TrimSpace(p.Name))
		if isNonCanonical(p.ID) && p.Source == "" && hasCanonical[key] {
			continue
		}
		result = append(result, p)
	}
	return result
}
