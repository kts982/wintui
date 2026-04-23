package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsSelfUpgradeBatchItemRequiresInstalledExecutable(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	currentExecutablePath = func() (string, error) {
		return `D:\Projects\wintui\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
	})

	item := batchItem{
		action: retryOpUpgrade,
		item: workspaceItem{
			pkg: Package{ID: selfPackageID, Source: "winget"},
		},
	}

	if isSelfUpgradeBatchItem(item) {
		t.Fatal("expected repo binary path to not qualify as installed self-upgrade target")
	}

	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	if !isSelfUpgradeBatchItem(item) {
		t.Fatal("expected WinGet Links path to qualify as installed self-upgrade target")
	}

	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Packages\kts982.WinTUI_Microsoft.Winget.Source_8wekyb3d8bbwe\wintui.exe`, nil
	}
	if !isSelfUpgradeBatchItem(item) {
		t.Fatal("expected WinGet Packages path to qualify as installed self-upgrade target")
	}
}

func TestIsRunningInstalledWinTIUAcceptsResolvedPackagePath(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Packages\kts982.WinTUI_Microsoft.Winget.Source_8wekyb3d8bbwe\wintui.exe`, nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
	})

	if !isRunningInstalledWinTUI() {
		t.Fatal("expected resolved WinGet package target to qualify as installed WinTUI")
	}
}

func TestConfigureSelfUpdateScriptHostUsesNewConsole(t *testing.T) {
	cmd := exec.Command("cmd")
	configureSelfUpdateScriptHost(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_CONSOLE == 0 {
		t.Fatal("expected CREATE_NEW_CONSOLE flag")
	}
}

func TestSelfUpgradeCommandArgsIncludeForce(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	args := selfUpgradeCommandArgs("winget", "2.2.0")
	if !containsArg(args, "--force") {
		t.Fatalf("selfUpgradeCommandArgs(%#v) missing --force: %#v", []string{"winget", "2.2.0"}, args)
	}
}

func TestSelfUpgradeCommandArgsUseOverriddenSelfPackageID(t *testing.T) {
	origSettings := appSettings
	origPackageID := selfPackageID
	defer func() {
		appSettings = origSettings
		selfPackageID = origPackageID
	}()

	appSettings = DefaultSettings()
	selfPackageID = "kts982.WinTUI.Canary"

	args := selfUpgradeCommandArgs("winget", "0.0.2")
	if len(args) < 4 || args[2] != "kts982.WinTUI.Canary" {
		t.Fatalf("selfUpgradeCommandArgs() = %#v, want overridden package id", args)
	}
}

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

func TestPathLooksLikeInstalledWinTUIUsesOverriddenSelfPackageID(t *testing.T) {
	origPackageID := selfPackageID
	selfPackageID = "kts982.WinTUI.Canary"
	t.Cleanup(func() { selfPackageID = origPackageID })

	path := `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Packages\kts982.WinTUI.Canary_Microsoft.Winget.Source_8wekyb3d8bbwe\wintui.exe`
	if !pathLooksLikeInstalledWinTUI(path) {
		t.Fatalf("expected overridden self package id to match installed package path: %s", path)
	}
}

func TestStartSelfUpgradeHandoffClearsManifestOverrideAfterLaunch(t *testing.T) {
	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "manifest")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatalf("MkdirAll(manifestDir): %v", err)
	}

	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origElevated := isElevated
	origCacheDir := userCacheDirPath
	origStartHost := startSelfUpdateHost
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	isElevated = func() bool { return true }
	userCacheDirPath = func() (string, error) { return dir, nil }
	startSelfUpdateHost = func(cmd *exec.Cmd) error { return nil }
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		isElevated = origElevated
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
	})

	overridePath := selfUpdateManifestOverridePath()
	if err := os.WriteFile(overridePath, []byte(manifestDir), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", overridePath, err)
	}

	if err := startSelfUpgradeHandoff("winget", "0.0.2"); err != nil {
		t.Fatalf("startSelfUpgradeHandoff() err = %v", err)
	}
	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Fatalf("expected manifest override to be cleared after successful launch, got err=%v", err)
	}
}

func TestStartSelfUpgradeHandoffRequiresAdmin(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origElevated := isElevated
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	isElevated = func() bool { return false }
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		isElevated = origElevated
	})

	err := startSelfUpgradeHandoff("winget", "")
	if err == nil || !strings.Contains(err.Error(), "already be running as administrator") {
		t.Fatalf("startSelfUpgradeHandoff() err = %v, want admin requirement", err)
	}
}

func TestPendingSelfUpgradeManualAdminCommand(t *testing.T) {
	cmd, err := pendingSelfUpgradeManualAdminCommand(retryRequest{
		Op:      retryOpUpgrade,
		ID:      "kts982.WinTUI.Canary",
		Name:    "WinTUI Canary",
		Source:  "winget",
		Version: "0.0.3",
	})
	if err != nil {
		t.Fatalf("pendingSelfUpgradeManualAdminCommand() err = %v", err)
	}

	for _, want := range []string{
		"wintui",
		"'--retry-op' 'upgrade'",
		"'--id' 'kts982.WinTUI.Canary'",
		"'--name' 'WinTUI Canary'",
		"'--source' 'winget'",
		"'--package-version' '0.0.3'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("pendingSelfUpgradeManualAdminCommand() = %q, missing %q", cmd, want)
		}
	}
}

func TestRenderSelfUpdateScriptIncludesExpectedCommands(t *testing.T) {
	origSettings := appSettings
	origCacheDir := userCacheDirPath
	defer func() {
		appSettings = origSettings
		userCacheDirPath = origCacheDir
	}()

	appSettings = DefaultSettings()
	userCacheDirPath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local`, nil
	}

	script := renderSelfUpdateScript(42, selfUpgradeCommandArgs("winget", "2.2.0"))
	for _, want := range []string{
		"Wait-Process -Id $ParentPid -Timeout 120",
		"& winget.exe @WingetArgs",
		"'kts982.WinTUI'",
		"'--force'",
		"manual relaunch required: start wintui again",
		"$PSCommandPath",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("renderSelfUpdateScript() missing %q\nscript:\n%s", want, script)
		}
	}
	for _, unwanted := range []string{"Start-Process -FilePath $RelaunchExe", "$RelaunchExe = "} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("renderSelfUpdateScript() unexpectedly contains %q\nscript:\n%s", unwanted, script)
		}
	}
}

func TestRenderSelfUpdateScriptParsesInPowerShell(t *testing.T) {
	powershellPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not available")
	}

	origSettings := appSettings
	origCacheDir := userCacheDirPath
	defer func() {
		appSettings = origSettings
		userCacheDirPath = origCacheDir
	}()

	appSettings = DefaultSettings()
	userCacheDirPath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local`, nil
	}

	script := renderSelfUpdateScript(
		42,
		selfUpgradeManifestArgs(`C:\TEST\canary-build\manifests\k\kts982\WinTUI.Canary\0.0.2`),
	)
	scriptPath := filepath.Join(t.TempDir(), "handoff.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", scriptPath, err)
	}

	cmd := exec.Command(
		powershellPath,
		"-NoProfile",
		"-Command",
		"[void][scriptblock]::Create((Get-Content -Raw -LiteralPath '"+scriptPath+"'))",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PowerShell failed to parse generated handoff script: %v\n%s", err, output)
	}
}

func TestPowerShellHostArgsIncludesExecutionPolicyBypass(t *testing.T) {
	scriptPath := `C:\Users\ktsio\AppData\Local\wintui\self-update\handoff-42.ps1`
	args := powerShellHostArgs(scriptPath)

	for _, want := range []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Minimized", "-File"} {
		if !containsArg(args, want) {
			t.Fatalf("powerShellHostArgs() = %#v, missing %q", args, want)
		}
	}
	if args[len(args)-2] != "-File" || args[len(args)-1] != scriptPath {
		t.Fatalf("powerShellHostArgs() = %#v, expected trailing -File <path>", args)
	}
}

func TestCleanupStaleSelfUpdateScriptsRemovesLeftoverHandoffs(t *testing.T) {
	dir := t.TempDir()
	origCacheDir := userCacheDirPath
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDirPath = origCacheDir })

	stateDir := filepath.Join(dir, "wintui", "self-update")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	leftover := filepath.Join(stateDir, selfUpdateScriptPrefix+"9999.ps1")
	if err := os.WriteFile(leftover, []byte("Write-Host stale"), 0644); err != nil {
		t.Fatalf("write leftover: %v", err)
	}
	unrelated := filepath.Join(stateDir, "wintui-self-update.log")
	if err := os.WriteFile(unrelated, []byte("log"), 0644); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}

	cleanupStaleSelfUpdateScripts()

	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatalf("expected leftover handoff script to be removed; err=%v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("expected unrelated file in state dir to remain, got err=%v", err)
	}
}

func TestCleanupStaleSelfUpdateScriptsSkipsMissingStateDir(t *testing.T) {
	dir := t.TempDir()
	origCacheDir := userCacheDirPath
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDirPath = origCacheDir })

	cleanupStaleSelfUpdateScripts()

	stateDir := filepath.Join(dir, "wintui", "self-update")
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("cleanupStaleSelfUpdateScripts created state dir on empty cache; err=%v", err)
	}
}

func TestCleanupStaleSelfUpdateManifestOverrideRemovesMissingTarget(t *testing.T) {
	dir := t.TempDir()
	origCacheDir := userCacheDirPath
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDirPath = origCacheDir })

	overridePath := selfUpdateManifestOverridePath()
	if err := os.WriteFile(overridePath, []byte(filepath.Join(dir, "missing-manifest")), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", overridePath, err)
	}

	cleanupStaleSelfUpdateManifestOverride()

	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Fatalf("expected missing-target manifest override to be removed; err=%v", err)
	}
}

func TestRetryRequestArgsEncodesBatch(t *testing.T) {
	args, err := retryRequestArgs(retryRequest{
		Op: retryOpUpgrade,
		Items: []retryItem{
			{ID: "kts982.WinTUI", Name: "WinTUI", Source: "winget", Version: "2.4.0"},
			{ID: "Git.Git", Name: "Git", Source: "winget"},
		},
	})
	if err != nil {
		t.Fatalf("retryRequestArgs() err = %v", err)
	}
	if len(args) != 4 || args[0] != "--retry-op" || args[1] != "upgrade" || args[2] != "--retry-batch" || args[3] == "" {
		t.Fatalf("retryRequestArgs() = %#v, want retry batch args", args)
	}
}
