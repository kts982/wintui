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
	if !strings.Contains(out, "wintui upgrade --self") {
		t.Fatalf("missing skip message in output: %q", out)
	}
	if !strings.Contains(out, "WinTUI self-upgrade skipped") {
		t.Fatalf("missing trailing skip note in summary: %q", out)
	}
	if !strings.Contains(out, "1/1 succeeded.") {
		t.Fatalf("expected 1/1 succeeded summary (skipped self excluded from the denominator), got: %q", out)
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

func TestUpgradeAutoOnlyRunsAutoPolicyPackages(t *testing.T) {
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	var streamCalls []string
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		streamCalls = append(streamCalls, pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Auto.Pkg", "winget"): {UpdatePolicy: PolicyAuto},
		packageRuleKey("Held.Pkg", "winget"): {UpdatePolicy: PolicyHold},
	}
	raw := []Package{
		{Name: "Auto", ID: "Auto.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
		{Name: "Ask", ID: "Ask.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
		{Name: "Held", ID: "Held.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	var buf bytes.Buffer
	if err := upgradeAuto(context.Background(), raw, settings, &buf); err != nil {
		t.Fatalf("upgradeAuto: %v", err)
	}

	if len(streamCalls) != 1 || streamCalls[0] != "Auto.Pkg" {
		t.Fatalf("streamUpgradeFn calls = %v, want only [Auto.Pkg]", streamCalls)
	}
	out := buf.String()
	if !strings.Contains(out, "Auto-upgrading 1 package(s)") {
		t.Fatalf("output missing auto-upgrade header: %q", out)
	}
	if !strings.Contains(out, "1 held by policy") {
		t.Fatalf("output missing held count: %q", out)
	}
}

func TestUpgradeAutoNoAutoPackages(t *testing.T) {
	settings := DefaultSettings()
	raw := []Package{
		{Name: "Ask", ID: "Ask.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	var buf bytes.Buffer
	if err := upgradeAuto(context.Background(), raw, settings, &buf); err != nil {
		t.Fatalf("upgradeAuto: %v", err)
	}
	if !strings.Contains(buf.String(), "No auto-update packages have updates available.") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}

func TestUpgradeIDsHonorsRequestedSet(t *testing.T) {
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	var streamCalls []string
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		streamCalls = append(streamCalls, pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	raw := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "120", Available: "121"},
		{Name: "Code", ID: "Microsoft.VisualStudioCode", Source: "winget", Version: "1.0", Available: "1.1"},
		{Name: "Other", ID: "Other.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	var buf bytes.Buffer
	if err := upgradeIDs(context.Background(), []string{"Mozilla.Firefox", "Microsoft.VisualStudioCode"}, raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}
	if len(streamCalls) != 2 || streamCalls[0] != "Mozilla.Firefox" || streamCalls[1] != "Microsoft.VisualStudioCode" {
		t.Fatalf("streamUpgradeFn calls = %v, want [Mozilla.Firefox Microsoft.VisualStudioCode]", streamCalls)
	}
	if cliExitCode != 0 {
		t.Fatalf("cliExitCode = %d, want 0", cliExitCode)
	}
	if !strings.Contains(buf.String(), "2/2 succeeded.") {
		t.Fatalf("unexpected summary: %q", buf.String())
	}
}

func TestUpgradeIDsNoUpdateAvailableExitsZero(t *testing.T) {
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		t.Fatalf("streamUpgradeFn should not be called for an unknown ID, got %q", pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	var buf bytes.Buffer
	if err := upgradeIDs(context.Background(), []string{"NotInList.Pkg"}, nil, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}
	if cliExitCode != 0 {
		t.Fatalf("cliExitCode = %d, want 0 (no update available is not a failure)", cliExitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "no update available") {
		t.Fatalf("expected 'no update available' line, got %q", out)
	}
	if !strings.Contains(out, "No update: NotInList.Pkg") {
		t.Fatalf("expected summary 'No update: NotInList.Pkg', got %q", out)
	}
}

func TestUpgradeIDsDeduplicatesAndIgnoresBlankIDs(t *testing.T) {
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	var streamCalls []string
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		streamCalls = append(streamCalls, pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	raw := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "120", Available: "121"},
	}

	var buf bytes.Buffer
	if err := upgradeIDs(context.Background(), []string{"", "Mozilla.Firefox", " mozilla.firefox "}, raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}
	if len(streamCalls) != 1 || streamCalls[0] != "Mozilla.Firefox" {
		t.Fatalf("streamUpgradeFn calls = %v, want [Mozilla.Firefox]", streamCalls)
	}
	if !strings.Contains(buf.String(), "1/1 succeeded.") {
		t.Fatalf("unexpected summary: %q", buf.String())
	}
}

func TestUpgradeIDsHeldPackageErrors(t *testing.T) {
	origStream := streamUpgradeFn
	origExitCode := cliExitCode
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		t.Fatalf("streamUpgradeFn should not be called for a held ID, got %q", pkg.ID)
		return nil
	}
	t.Cleanup(func() {
		streamUpgradeFn = origStream
		cliExitCode = origExitCode
	})

	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Held.Pkg", "winget"): {UpdatePolicy: PolicyHold},
	}
	raw := []Package{
		{Name: "Held", ID: "Held.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	var buf bytes.Buffer
	if err := upgradeIDs(context.Background(), []string{"Held.Pkg"}, raw, settings, &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}
	if cliExitCode != 1 {
		t.Fatalf("cliExitCode = %d, want 1 for held package", cliExitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "held by policy") {
		t.Fatalf("expected 'held by policy' message, got %q", out)
	}
	if !strings.Contains(out, "Held: Held.Pkg") {
		t.Fatalf("expected summary 'Held: Held.Pkg', got %q", out)
	}
}

func TestUpgradeIDsSkipsRunningWinTUI(t *testing.T) {
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
		{Name: "WinTUI", ID: selfPackageID, Source: "winget", Version: "2.4.0", Available: "2.5.0"},
	}

	var buf bytes.Buffer
	if err := upgradeIDs(context.Background(), []string{selfPackageID}, raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}
	if len(streamCalls) != 0 {
		t.Fatalf("streamUpgradeFn called for self-package: %v", streamCalls)
	}
	if cliExitCode != 0 {
		t.Fatalf("cliExitCode = %d, want 0 (self-skip is not a failure)", cliExitCode)
	}
	if !strings.Contains(buf.String(), "wintui upgrade --self") {
		t.Fatalf("expected self-skip hint, got %q", buf.String())
	}
}
