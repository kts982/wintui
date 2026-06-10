//go:build rehearsal

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The manifest override is a rehearsal-only capability (rehearsalMode); this
// test asserts the honoring side and therefore only runs under -tags rehearsal.
// The release-build counterpart (override ignored) lives in
// self_update_windows_test.go under //go:build !rehearsal semantics via
// TestSelfUpgradeCommandArgsIgnoreManifestOverrideInReleaseBuilds.
func TestSelfUpgradeCommandArgsUseManifestOverride(t *testing.T) {
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "manifest")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	origCacheDir := userCacheDirPath
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDirPath = origCacheDir })

	overridePath := selfUpdateManifestOverridePath()
	if err := os.WriteFile(overridePath, []byte(manifestDir), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", overridePath, err)
	}

	args := selfUpgradeCommandArgs("winget", "0.0.2")
	if len(args) < 3 || args[0] != "upgrade" || args[1] != "--manifest" || args[2] != manifestDir {
		t.Fatalf("selfUpgradeCommandArgs() = %#v, want manifest override", args)
	}
	for _, want := range []string{"--accept-package-agreements", "--disable-interactivity", "--force"} {
		if !containsArg(args, want) {
			t.Fatalf("selfUpgradeCommandArgs() = %#v, missing %q", args, want)
		}
	}
}
