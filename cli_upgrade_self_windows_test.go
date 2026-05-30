package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

const installedWinTUIPath = `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`

func TestUpgradeSelfDevBuildPrintsHint(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	currentExecutablePath = func() (string, error) { return `D:\Projects\wintui\wintui.exe`, nil }
	evalSymlinksPath = func(p string) (string, error) { return p, nil }
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
	})

	var buf bytes.Buffer
	if err := upgradeSelf(&buf); err != nil {
		t.Fatalf("upgradeSelf: %v", err)
	}
	if !strings.Contains(buf.String(), "dev or portable build") {
		t.Errorf("expected dev-build hint, got: %q", buf.String())
	}
}

func TestUpgradeSelfUpToDate(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origRunCheck := runSelfUpdateCheckCtx
	currentExecutablePath = func() (string, error) { return installedWinTUIPath, nil }
	evalSymlinksPath = func(p string) (string, error) { return p, nil }
	// No kts982.WinTUI row with an Available version → nothing to upgrade.
	runSelfUpdateCheckCtx = func(_ context.Context, _ ...string) (string, error) {
		return "Name    Id             Version  Available  Source\n" +
			"---------------------------------------------------\n", nil
	}
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		runSelfUpdateCheckCtx = origRunCheck
	})

	var buf bytes.Buffer
	if err := upgradeSelf(&buf); err != nil {
		t.Fatalf("upgradeSelf: %v", err)
	}
	if !strings.Contains(buf.String(), "already up to date") {
		t.Errorf("expected up-to-date message, got: %q", buf.String())
	}
}

func TestUpgradeSelfAvailableStartsHandoff(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	origCacheDir := userCacheDirPath
	origRunCheck := runSelfUpdateCheckCtx
	origStartHost := startSelfUpdateHost
	currentExecutablePath = func() (string, error) { return installedWinTUIPath, nil }
	evalSymlinksPath = func(p string) (string, error) { return p, nil }
	userCacheDirPath = func() (string, error) { return t.TempDir(), nil }
	runSelfUpdateCheckCtx = func(_ context.Context, _ ...string) (string, error) {
		return "Name    Id             Version  Available  Source\n" +
			"---------------------------------------------------\n" +
			"WinTUI  kts982.WinTUI  2.8.0    2.9.0      winget\n", nil
	}
	started := false
	startSelfUpdateHost = func(_ *exec.Cmd) error { started = true; return nil }
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
		userCacheDirPath = origCacheDir
		runSelfUpdateCheckCtx = origRunCheck
		startSelfUpdateHost = origStartHost
	})

	var buf bytes.Buffer
	if err := upgradeSelf(&buf); err != nil {
		t.Fatalf("upgradeSelf: %v", err)
	}
	if !started {
		t.Fatal("expected self-upgrade handoff to be started")
	}
	out := buf.String()
	if !strings.Contains(out, "2.8.0 → 2.9.0") || !strings.Contains(out, "Closing now") {
		t.Errorf("expected availability + closing message, got: %q", out)
	}
}
