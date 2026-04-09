package main

import "testing"

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
