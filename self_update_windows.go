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
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
)

const (
	selfPackageID          = "kts982.WinTUI"
	selfUpdateHelperPrefix = "wintui-self-update-"
	selfUpdateLogName      = "wintui-self-update.log"
)

var (
	selfUpdateParentPID int
	selfUpdateSource    string
	selfUpdateVersion   string
	selfUpdateRelaunch  string

	currentExecutablePath = os.Executable
	evalSymlinksPath      = filepath.EvalSymlinks

	// hideWingetChildWindow, when true, hides the console window spawned by
	// child processes (winget.exe) — set by the self-upgrade helper which
	// runs detached and would otherwise cause winget to pop its own window.
	hideWingetChildWindow bool
)

// applyHiddenChildWindow configures a command to run without a visible
// console window. Called by runWingetStreamCtx when hideWingetChildWindow
// is set.
func applyHiddenChildWindow(cmd *exec.Cmd) {
	if !hideWingetChildWindow {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.HideWindow = true
}

var selfUpgradeCmd = &cobra.Command{
	Use:    "self-upgrade",
	Short:  "Internal self-update helper",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		appSettings = LoadSettings()
		hideWingetChildWindow = true
		appendSelfUpdateLogf("helper start parent=%d source=%q version=%q relaunch=%q", selfUpdateParentPID, selfUpdateSource, selfUpdateVersion, selfUpdateRelaunch)

		if selfUpdateParentPID > 0 {
			if err := waitForProcessExit(selfUpdateParentPID, 2*time.Minute); err != nil {
				appendSelfUpdateLogf("wait for parent failed: %v", err)
			}
		}

		if err := runSelfUpgrade(selfUpdateSource, selfUpdateVersion); err != nil {
			appendSelfUpdateLogf("winget self-upgrade failed: %v", err)
		} else {
			appendSelfUpdateLogf("winget self-upgrade completed")
			cache.deleteDiskCache()
			appendSelfUpdateLogf("cache cleared before relaunch")
		}

		if selfUpdateRelaunch != "" {
			if err := waitForPath(selfUpdateRelaunch, 30*time.Second); err != nil {
				appendSelfUpdateLogf("wait for relaunch path failed: %v", err)
			} else if err := relaunchUpdatedBinary(selfUpdateRelaunch); err != nil {
				appendSelfUpdateLogf("relaunch failed: %v", err)
			} else {
				appendSelfUpdateLogf("relaunch started: %s", selfUpdateRelaunch)
			}
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
	return startDetachedHelper(cmd)
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

func selfUpdateLogPath() string {
	return filepath.Join(os.TempDir(), selfUpdateLogName)
}

func appendSelfUpdateLogf(format string, args ...any) {
	f, err := os.OpenFile(selfUpdateLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func configureDetachedHelper(cmd *exec.Cmd) (*os.File, error) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	return devNull, nil
}

func startDetachedHelper(cmd *exec.Cmd) error {
	devNull, err := configureDetachedHelper(cmd)
	if err != nil {
		return err
	}
	defer devNull.Close()
	return cmd.Start()
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func selfUpgradeCommandArgs(source, version string) []string {
	args := upgradeCommandArgs(selfPackageID, source, version)
	if !containsArg(args, "--force") {
		args = append(args, "--force")
	}
	return args
}

func runSelfUpgrade(source, version string) error {
	args := selfUpgradeCommandArgs(source, version)
	_, err := runActionSmartSyncCtx(context.Background(), args...)
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
	configureRelaunchCommand(cmd)
	return cmd.Start()
}

func configureRelaunchCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
	}
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
