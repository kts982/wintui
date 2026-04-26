package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// Self-upgrade skip path: when WinTUI itself appears in the upgradeable list
// and is running from an installed location, upgradeAll must NOT call the
// per-package upgrade dispatcher for it. The user-facing skip message must
// also surface so people know why the package was left untouched.
func TestUpgradeAllSkipsRunningWinTUI(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	currentExecutablePath = func() (string, error) {
		return `C:\Users\test\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	var streamCalls []string
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		streamCalls = append(streamCalls, pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	raw := []Package{
		{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.3.3", Available: "2.4.0"},
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "120", Available: "121"},
	}

	var buf bytes.Buffer
	if err := upgradeAll(context.Background(), raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeAll: %v", err)
	}

	if len(streamCalls) != 1 || streamCalls[0] != "Mozilla.Firefox" {
		t.Fatalf("streamUpgradeFn calls = %v, want only [Mozilla.Firefox]; the running WinTUI must be skipped", streamCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "skipped: WinTUI cannot upgrade itself headlessly") {
		t.Fatalf("missing skip message in output: %q", out)
	}
	if !strings.Contains(out, "WinTUI self-upgrade skipped") {
		t.Fatalf("missing trailing skip note in summary: %q", out)
	}
	if !strings.Contains(out, "1/2 succeeded.") {
		t.Fatalf("expected 1/2 succeeded summary, got: %q", out)
	}
	if cliExitCode != 0 {
		t.Fatalf("cliExitCode = %d, want 0 (skipping the self-package is not a failure)", cliExitCode)
	}
}

// When WinTUI is NOT running from an installed location (e.g. `go run`,
// developer checkout), upgrade --all should not skip a package whose ID
// happens to match selfPackageID -- the safety check is about the running
// binary, not the package metadata.
func TestUpgradeAllUpgradesSelfPackageWhenNotRunningInstalled(t *testing.T) {
	origExe := currentExecutablePath
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	currentExecutablePath = func() (string, error) {
		return `C:\Users\test\Desktop\wintui-dev.exe`, nil
	}
	var streamCalls []string
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		streamCalls = append(streamCalls, pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	raw := []Package{
		{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.3.3", Available: "2.4.0"},
	}

	var buf bytes.Buffer
	if err := upgradeAll(context.Background(), raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeAll: %v", err)
	}

	if len(streamCalls) != 1 || streamCalls[0] != selfPackageID {
		t.Fatalf("streamUpgradeFn calls = %v, want [%s]; non-installed binary should not trigger the skip", streamCalls, selfPackageID)
	}
	if strings.Contains(buf.String(), "WinTUI self-upgrade skipped") {
		t.Fatalf("unexpected skip note when not running installed binary: %q", buf.String())
	}
}
