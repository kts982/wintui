package main

import (
	"context"
	"fmt"
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

// TestSelfUpgradeCommandArgsIgnoreManifestOverrideInReleaseBuilds asserts the
// release-build behavior: a planted rehearsal-manifest.txt must be ignored and
// the normal `upgrade <selfPackageID> …` args returned. The honoring side runs
// only under -tags rehearsal (see self_update_rehearsal_test.go).
func TestSelfUpgradeCommandArgsIgnoreManifestOverrideInReleaseBuilds(t *testing.T) {
	if rehearsalMode {
		t.Skip("override honoring is exercised by the rehearsal-tagged test")
	}

	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "manifest")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	origSettings := appSettings
	origCacheDir := userCacheDirPath
	appSettings = DefaultSettings()
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() {
		appSettings = origSettings
		userCacheDirPath = origCacheDir
	})

	overridePath := selfUpdateManifestOverridePath()
	if err := os.WriteFile(overridePath, []byte(manifestDir), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", overridePath, err)
	}

	args := selfUpgradeCommandArgs("winget", "0.0.2")
	if containsArg(args, "--manifest") {
		t.Fatalf("selfUpgradeCommandArgs() = %#v, release build must ignore the manifest override", args)
	}
	if len(args) < 3 || args[0] != "upgrade" || args[1] != "--id" || args[2] != selfPackageID {
		t.Fatalf("selfUpgradeCommandArgs() = %#v, want normal upgrade %s args", args, selfPackageID)
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
	origResolver := selfUpdateWingetResolver
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	isElevated = func() bool { return true }
	userCacheDirPath = func() (string, error) { return dir, nil }
	var startedCmd *exec.Cmd
	startSelfUpdateHost = func(cmd *exec.Cmd) error {
		startedCmd = cmd
		return nil
	}
	selfUpdateWingetResolver = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WindowsApps\winget.exe`, nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		isElevated = origElevated
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
		selfUpdateWingetResolver = origResolver
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
	if startedCmd == nil {
		t.Fatal("startSelfUpdateHost did not receive the inline command")
	}
	if got := startedCmd.Args[len(startedCmd.Args)-1]; !strings.Contains(got, "& $WingetExe @WingetArgs") {
		t.Fatalf("last command arg does not contain the rendered handoff script: %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "wintui", "self-update", selfUpdateScriptPrefix+"*.ps1"))
	if err != nil {
		t.Fatalf("Glob handoff scripts: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("inline handoff created script files: %#v", matches)
	}
	logData, err := os.ReadFile(filepath.Join(dir, "wintui", "self-update", selfUpdateLogName))
	if err != nil {
		t.Fatalf("ReadFile(self-update log): %v", err)
	}
	if !strings.Contains(string(logData), "launching inline handoff: winget upgrade") {
		t.Fatalf("self-update log missing the pre-launch winget command record:\n%s", logData)
	}
}

func TestStartSelfUpgradeHandoffDoesNotRequireAdmin(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origElevated := isElevated
	origCacheDir := userCacheDirPath
	origStartHost := startSelfUpdateHost
	origResolver := selfUpdateWingetResolver
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	isElevated = func() bool { return false }
	userCacheDirPath = func() (string, error) { return t.TempDir(), nil }
	startSelfUpdateHost = func(cmd *exec.Cmd) error { return nil }
	selfUpdateWingetResolver = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WindowsApps\winget.exe`, nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		isElevated = origElevated
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
		selfUpdateWingetResolver = origResolver
	})

	if err := startSelfUpgradeHandoff("winget", ""); err != nil {
		t.Fatalf("startSelfUpgradeHandoff() err = %v, want nil for non-admin handoff", err)
	}
}

// When winget can't be resolved to a validated absolute path, the handoff
// must fail closed (actionable error, no inline command launched)
// rather than fall back to a bare name PowerShell would re-resolve off PATH.
func TestStartSelfUpgradeHandoffFailsClosedWhenWingetUnresolved(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origElevated := isElevated
	origCacheDir := userCacheDirPath
	origStartHost := startSelfUpdateHost
	origResolver := selfUpdateWingetResolver
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	isElevated = func() bool { return false }
	userCacheDirPath = func() (string, error) { return t.TempDir(), nil }
	hostStarted := false
	startSelfUpdateHost = func(cmd *exec.Cmd) error { hostStarted = true; return nil }
	selfUpdateWingetResolver = func() (string, error) {
		return "", fmt.Errorf("refusing winget at %q: not under a known system app location", `C:\evil\winget.exe`)
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		isElevated = origElevated
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
		selfUpdateWingetResolver = origResolver
	})

	err := startSelfUpgradeHandoff("winget", "")
	if err == nil || !strings.Contains(err.Error(), "cannot start self-upgrade") {
		t.Fatalf("startSelfUpgradeHandoff() err = %v, want a fail-closed self-upgrade error", err)
	}
	if hostStarted {
		t.Fatal("PowerShell host was started despite winget resolution failing — must fail closed")
	}
}

func TestMaybeStartStartupSelfUpdateSchedulesHandoff(t *testing.T) {
	origSettings := appSettings
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origCacheDir := userCacheDirPath
	origStartHost := startSelfUpdateHost
	origRunCheck := runSelfUpdateCheckCtx
	origResolver := selfUpdateWingetResolver
	appSettings = DefaultSettings()
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	cacheDir := t.TempDir()
	userCacheDirPath = func() (string, error) { return cacheDir, nil }
	selfUpdateWingetResolver = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WindowsApps\winget.exe`, nil
	}
	var gotArgs []string
	runSelfUpdateCheckCtx = func(ctx context.Context, args ...string) (string, error) {
		gotArgs = append([]string(nil), args...)
		return "Name    Id             Version  Available  Source\n" +
			"---------------------------------------------------\n" +
			"WinTUI  kts982.WinTUI  2.4.0    2.5.0      winget\n", nil
	}
	started := false
	startSelfUpdateHost = func(cmd *exec.Cmd) error {
		started = true
		return nil
	}
	t.Cleanup(func() {
		appSettings = origSettings
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
		runSelfUpdateCheckCtx = origRunCheck
		selfUpdateWingetResolver = origResolver
	})

	scheduled, err := maybeStartStartupSelfUpdate()
	if err != nil {
		t.Fatalf("maybeStartStartupSelfUpdate() err = %v", err)
	}
	if !scheduled {
		t.Fatal("scheduled = false, want true")
	}
	if !started {
		t.Fatal("startSelfUpdateHost was not called")
	}
	wantArgs := []string{"list", "--upgrade-available", "--id", selfPackageID, "--exact", "--source", "winget"}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("startup check args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestMaybeStartStartupSelfUpdateSkipsWhenNoUpdate(t *testing.T) {
	origSettings := appSettings
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origCacheDir := userCacheDirPath
	origStartHost := startSelfUpdateHost
	origRunCheck := runSelfUpdateCheckCtx
	appSettings = DefaultSettings()
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	cacheDir := t.TempDir()
	userCacheDirPath = func() (string, error) { return cacheDir, nil }
	runSelfUpdateCheckCtx = func(ctx context.Context, args ...string) (string, error) {
		return "Name    Id             Version  Source\n" +
			"-------------------------------------------\n" +
			"WinTUI  kts982.WinTUI  2.5.0    winget\n", nil
	}
	started := false
	startSelfUpdateHost = func(cmd *exec.Cmd) error {
		started = true
		return nil
	}
	t.Cleanup(func() {
		appSettings = origSettings
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		userCacheDirPath = origCacheDir
		startSelfUpdateHost = origStartHost
		runSelfUpdateCheckCtx = origRunCheck
	})

	scheduled, err := maybeStartStartupSelfUpdate()
	if err != nil {
		t.Fatalf("maybeStartStartupSelfUpdate() err = %v", err)
	}
	if scheduled {
		t.Fatal("scheduled = true, want false when no Available column")
	}
	if started {
		t.Fatal("startSelfUpdateHost was called despite no available update")
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

	script := renderSelfUpdateScript(42, `C:\Users\ktsio\AppData\Local\Microsoft\WindowsApps\winget.exe`, selfUpgradeCommandArgs("winget", "2.2.0"))
	for _, want := range []string{
		"Wait-Process -Id $ParentPid -Timeout 120",
		"$WingetExe = '",
		"& $WingetExe @WingetArgs",
		"'kts982.WinTUI'",
		"'--accept-source-agreements'",
		"'--force'",
		"manual relaunch required: start wintui again",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("renderSelfUpdateScript() missing %q\nscript:\n%s", want, script)
		}
	}
	for _, unwanted := range []string{"Start-Process -FilePath $RelaunchExe", "$RelaunchExe = ", "$PSCommandPath", "Start-Sleep -Seconds 2"} {
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
		`C:\Users\ktsio\AppData\Local\Microsoft\WindowsApps\winget.exe`,
		selfUpgradeManifestArgs(`C:\TEST\canary-build\manifests\k\kts982\WinTUI.Canary\0.0.2`),
	)
	cmd := exec.Command(
		powershellPath,
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"[void][scriptblock]::Create("+quotePowerShellLiteral(script)+")",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PowerShell failed to parse generated handoff script: %v\n%s", err, output)
	}
}

func TestPowerShellHostArgsUseInlineCommand(t *testing.T) {
	script := "Write-Output 'handoff $value ` Δ'\nWrite-Output \"quoted\""
	args := powerShellHostArgs(script)

	for _, want := range []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Minimized", "-Command"} {
		if !containsArg(args, want) {
			t.Fatalf("powerShellHostArgs() = %#v, missing %q", args, want)
		}
	}
	for _, unwanted := range []string{"-ExecutionPolicy", "Bypass", "-File"} {
		if containsArg(args, unwanted) {
			t.Fatalf("powerShellHostArgs() = %#v, unexpectedly contains %q", args, unwanted)
		}
	}
	if args[len(args)-2] != "-Command" || args[len(args)-1] != script {
		t.Fatalf("powerShellHostArgs() = %#v, expected trailing -Command <script>", args)
	}
	cmd := exec.Command(powershellExePath(), args...)
	if got := cmd.Args[len(cmd.Args)-1]; got != script {
		t.Fatalf("exec.Cmd last arg = %q, want script preserved verbatim %q", got, script)
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
