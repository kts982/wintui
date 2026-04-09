package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
)

const (
	selfPackageID          = "kts982.WinTUI"
	selfUpdateHelperPrefix = "wintui-self-update-"
)

var (
	selfUpdateParentPID int
	selfUpdateSource    string
	selfUpdateVersion   string
	selfUpdateRelaunch  string

	currentExecutablePath = os.Executable
	evalSymlinksPath      = filepath.EvalSymlinks
)

var selfUpgradeCmd = &cobra.Command{
	Use:    "self-upgrade",
	Short:  "Internal self-update helper",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		appSettings = LoadSettings()

		if selfUpdateParentPID > 0 {
			_ = waitForProcessExit(selfUpdateParentPID, 2*time.Minute)
		}

		_ = runSelfUpgrade(selfUpdateSource, selfUpdateVersion)

		if selfUpdateRelaunch != "" {
			_ = waitForPath(selfUpdateRelaunch, 30*time.Second)
			_ = relaunchUpdatedBinary(selfUpdateRelaunch)
		}
		return nil
	},
}

func init() {
	selfUpgradeCmd.Flags().IntVar(&selfUpdateParentPID, "parent-pid", 0, "Parent WinTUI process to wait for")
	selfUpgradeCmd.Flags().StringVar(&selfUpdateSource, "source", "", "Package source for self-upgrade")
	selfUpgradeCmd.Flags().StringVar(&selfUpdateVersion, "package-version", "", "Package version for self-upgrade")
	selfUpgradeCmd.Flags().StringVar(&selfUpdateRelaunch, "relaunch-exe", "", "Executable path to relaunch after self-upgrade")
	rootCmd.AddCommand(selfUpgradeCmd)
}

func isSelfPackageID(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), selfPackageID)
}

func isSelfUpgradeBatchItem(item batchItem) bool {
	return item.action == retryOpUpgrade && isSelfPackageID(item.item.pkg.ID) && isRunningInstalledWinTUI()
}

func startSelfUpgradeHandoff(source, version string) error {
	if !isRunningInstalledWinTUI() {
		return fmt.Errorf("self-upgrade requires the installed WinTUI binary")
	}

	exePath, err := currentExecutablePath()
	if err != nil {
		return err
	}

	helperPath, err := copySelfUpdateHelper(exePath)
	if err != nil {
		return err
	}

	args := []string{
		"self-upgrade",
		"--parent-pid", strconv.Itoa(os.Getpid()),
		"--relaunch-exe", exePath,
	}
	if strings.TrimSpace(source) != "" {
		args = append(args, "--source", source)
	}
	if strings.TrimSpace(version) != "" {
		args = append(args, "--package-version", version)
	}

	cmd := exec.Command(helperPath, args...)
	return cmd.Start()
}

func copySelfUpdateHelper(exePath string) (string, error) {
	src, err := os.Open(exePath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.CreateTemp(os.TempDir(), selfUpdateHelperPrefix+"*.exe")
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	return dst.Name(), nil
}

func cleanupStaleSelfUpdateHelpers() {
	pattern := filepath.Join(os.TempDir(), selfUpdateHelperPrefix+"*.exe")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	currentExe, _ := currentExecutablePath()
	for _, path := range matches {
		if strings.EqualFold(path, currentExe) {
			continue
		}
		_ = os.Remove(path)
	}
}

func runSelfUpgrade(source, version string) error {
	_, err := runActionSmartSyncCtx(context.Background(), upgradeCommandArgs(selfPackageID, source, version)...)
	return err
}

func runActionSmartSyncCtx(ctx context.Context, args ...string) (string, error) {
	outChan, errChan := runActionSmartStreamCtx(ctx, args...)
	var lines []string
	for line := range outChan {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), <-errChan
}

func relaunchUpdatedBinary(exePath string) error {
	cmd := exec.Command(exePath)
	return cmd.Start()
}

func waitForPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(handle)

	waitMS := uint32(timeout / time.Millisecond)
	if waitMS == 0 {
		waitMS = 1
	}

	status, err := windows.WaitForSingleObject(handle, waitMS)
	if err != nil {
		return err
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("timed out waiting for process %d to exit", pid)
	}
	return nil
}

func isRunningInstalledWinTUI() bool {
	exePath, err := currentExecutablePath()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return false
	}

	candidates := []string{exePath}
	if resolved, err := evalSymlinksPath(exePath); err == nil && !strings.EqualFold(resolved, exePath) {
		candidates = append(candidates, resolved)
	}

	for _, path := range candidates {
		if pathLooksLikeInstalledWinTUI(path) {
			return true
		}
	}
	return false
}

func pathLooksLikeInstalledWinTUI(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	if filepath.Base(clean) != "wintui.exe" {
		return false
	}

	linksPatterns := []string{
		strings.ToLower(filepath.Join("microsoft", "winget", "links", "wintui.exe")),
		strings.ToLower(filepath.Join("winget", "links", "wintui.exe")),
	}
	for _, pattern := range linksPatterns {
		if strings.HasSuffix(clean, pattern) {
			return true
		}
	}

	packagePatterns := []string{
		strings.ToLower(filepath.Join("microsoft", "winget", "packages", selfPackageID+"_")),
		strings.ToLower(filepath.Join("winget", "packages", selfPackageID+"_")),
	}
	for _, pattern := range packagePatterns {
		if strings.Contains(clean, pattern) {
			return true
		}
	}

	return false
}
