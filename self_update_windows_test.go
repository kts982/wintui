package main

import (
	"os/exec"
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

func TestConfigureDetachedHelperSetsDetachedFlags(t *testing.T) {
	cmd := exec.Command("cmd")

	devNull, err := configureDetachedHelper(cmd)
	if err != nil {
		t.Fatalf("configureDetachedHelper: %v", err)
	}
	defer devNull.Close()

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil")
	}
	if cmd.SysProcAttr.CreationFlags&windows.DETACHED_PROCESS == 0 {
		t.Fatal("expected DETACHED_PROCESS flag")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("expected CREATE_NEW_PROCESS_GROUP flag")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("expected HideWindow = true")
	}
	if cmd.Stdin == nil || cmd.Stdout == nil || cmd.Stderr == nil {
		t.Fatal("expected stdio to be redirected away from the parent console")
	}
}

func TestConfigureRelaunchCommandUsesNewConsole(t *testing.T) {
	cmd := exec.Command("cmd")
	configureRelaunchCommand(cmd)
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
