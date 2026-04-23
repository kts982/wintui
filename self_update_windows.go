package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	selfUpdateHelperPrefix = "wintui-self-update-"
	selfUpdateScriptPrefix = "handoff-"
	selfUpdateLogName      = "wintui-self-update.log"
	selfUpdateManifestName = "rehearsal-manifest.txt"
)

var (
	// selfPackageID is a var rather than a const so canary rehearsal builds can
	// override it with ldflags without changing production code paths.
	selfPackageID = "kts982.WinTUI"

	currentExecutablePath = os.Executable
	evalSymlinksPath      = filepath.EvalSymlinks
	userCacheDirPath      = os.UserCacheDir
	startSelfUpdateHost   = startSelfUpdateScriptHost

	// hideWingetChildWindow, when true, hides the console window spawned by
	// child processes (winget.exe). This is used by the elevated helper which
	// streams output back to the TUI instead of owning a visible console.
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
	if !isElevated() {
		return fmt.Errorf("self-upgrade handoff requires WinTUI to already be running as administrator")
	}

	scriptPath, err := writeSelfUpdateScript(os.Getpid(), source, version)
	if err != nil {
		return err
	}

	appendSelfUpdateLogf("launching handoff script: %s", scriptPath)
	cmd := exec.Command("powershell.exe", powerShellHostArgs(scriptPath)...)
	if err := startSelfUpdateHost(cmd); err != nil {
		return err
	}
	clearSelfUpgradeManifestOverride()
	return nil
}

func powerShellHostArgs(scriptPath string) []string {
	return []string{
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Minimized",
		"-File", scriptPath,
	}
}

func startPendingSelfUpgradeAdminRelaunch(req retryRequest) error {
	exePath, err := currentExecutablePath()
	if err != nil {
		return err
	}

	args, err := retryRequestArgs(req)
	if err != nil {
		return err
	}

	appendSelfUpdateLogf("relaunching as admin for pending self-upgrade retry")
	return relaunchAsAdminFunc(exePath, args, swShowNormal)
}

func pendingSelfUpgradeManualAdminCommand(req retryRequest) (string, error) {
	args, err := retryRequestArgs(req)
	if err != nil {
		return "", err
	}

	parts := []string{"wintui"}
	for _, arg := range args {
		parts = append(parts, quotePowerShellCommandArg(arg))
	}
	return strings.Join(parts, " "), nil
}

func quotePowerShellCommandArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func retryRequestArgs(req retryRequest) ([]string, error) {
	if !req.valid() {
		return nil, fmt.Errorf("invalid retry request")
	}

	args := []string{"--retry-op", string(req.Op)}
	if req.isBatch() {
		encoded, err := encodeRetryItems(req.Items)
		if err != nil {
			return nil, err
		}
		return append(args, "--retry-batch", encoded), nil
	}

	args = append(args, "--id", req.ID)
	if strings.TrimSpace(req.Name) != "" {
		args = append(args, "--name", req.Name)
	}
	if strings.TrimSpace(req.Source) != "" {
		args = append(args, "--source", req.Source)
	}
	if strings.TrimSpace(req.Version) != "" {
		args = append(args, "--package-version", req.Version)
	}
	return args, nil
}

func writeSelfUpdateScript(parentPID int, source, version string) (string, error) {
	scriptPath := selfUpdateScriptPath(parentPID)
	script := renderSelfUpdateScript(parentPID, selfUpgradeCommandArgs(source, version))
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", err
	}
	return scriptPath, nil
}

func renderSelfUpdateScript(parentPID int, wingetArgs []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "$ParentPid = %d\n", parentPID)
	fmt.Fprintf(&b, "$LogPath = %s\n", quotePowerShellLiteral(selfUpdateLogPath()))
	fmt.Fprintf(&b, "$CachePath = %s\n", quotePowerShellLiteral(diskCachePath()))
	b.WriteString("$WingetArgs = @(\n")
	for i, arg := range wingetArgs {
		suffix := ","
		if i == len(wingetArgs)-1 {
			suffix = ""
		}
		fmt.Fprintf(&b, "  %s%s\n", quotePowerShellLiteral(arg), suffix)
	}
	b.WriteString(")\n")
	b.WriteString(`
$ErrorActionPreference = 'Stop'

function Write-Log([string]$Message) {
  $timestamp = Get-Date -Format o
  Add-Content -Path $LogPath -Value "$timestamp $Message"
}

Write-Log ("handoff start parent={0}" -f $ParentPid)

if ($ParentPid -gt 0) {
  try {
    Wait-Process -Id $ParentPid -Timeout 120 -ErrorAction Stop
  } catch {
    Write-Log ("wait for parent failed: {0}" -f $_.Exception.Message)
  }
}

try {
  Write-Log ("winget start: {0}" -f ($WingetArgs -join ' '))
  & winget.exe @WingetArgs
  $wingetExit = $LASTEXITCODE
  if ($wingetExit -ne 0) {
    throw "winget exited with code $wingetExit"
  }
  Write-Log "winget self-upgrade completed"
  if (Test-Path -LiteralPath $CachePath) {
    Remove-Item -LiteralPath $CachePath -Force -ErrorAction SilentlyContinue
    Write-Log "cache cleared after upgrade"
  }
  Write-Log "manual relaunch required: start wintui again"
} catch {
  Write-Log ("winget self-upgrade failed: {0}" -f $_.Exception.Message)
}

Start-Sleep -Seconds 2
Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
`)
	return b.String()
}

func quotePowerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func startSelfUpdateScriptHost(cmd *exec.Cmd) error {
	configureSelfUpdateScriptHost(cmd)
	return cmd.Start()
}

func configureSelfUpdateScriptHost(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
	}
}

func cleanupStaleSelfUpdateHelpers() {
	cleanupStaleSelfUpdateExeHelpers()
	cleanupStaleSelfUpdateScripts()
	cleanupStaleSelfUpdateManifestOverride()
}

func cleanupStaleSelfUpdateExeHelpers() {
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

func cleanupStaleSelfUpdateScripts() {
	baseDir, err := userCacheDirPath()
	if err != nil || strings.TrimSpace(baseDir) == "" {
		return
	}
	dir := filepath.Join(baseDir, "wintui", "self-update")
	if _, err := os.Stat(dir); err != nil {
		return
	}
	pattern := filepath.Join(dir, selfUpdateScriptPrefix+"*.ps1")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

func cleanupStaleSelfUpdateManifestOverride() {
	path := selfUpdateManifestOverridePath()
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	if time.Since(info.ModTime()) > 24*time.Hour || loadSelfUpgradeManifestOverride() == "" {
		_ = os.Remove(path)
	}
}

func selfUpdateStateDir() string {
	baseDir, err := userCacheDirPath()
	if err != nil || strings.TrimSpace(baseDir) == "" {
		baseDir = os.TempDir()
	}
	dir := filepath.Join(baseDir, "wintui", "self-update")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func selfUpdateScriptPath(parentPID int) string {
	name := "handoff.ps1"
	if parentPID > 0 {
		name = fmt.Sprintf("%s%d.ps1", selfUpdateScriptPrefix, parentPID)
	}
	return filepath.Join(selfUpdateStateDir(), name)
}

func selfUpdateLogPath() string {
	return filepath.Join(selfUpdateStateDir(), selfUpdateLogName)
}

func selfUpdateManifestOverridePath() string {
	return filepath.Join(selfUpdateStateDir(), selfUpdateManifestName)
}

func appendSelfUpdateLogf(format string, args ...any) {
	f, err := os.OpenFile(selfUpdateLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func selfUpgradeCommandArgs(source, version string) []string {
	if manifestPath := loadSelfUpgradeManifestOverride(); manifestPath != "" {
		return selfUpgradeManifestArgs(manifestPath)
	}

	args := upgradeCommandArgs(selfPackageID, source, version)
	if !containsArg(args, "--force") {
		args = append(args, "--force")
	}
	return args
}

func selfUpgradeManifestArgs(manifestPath string) []string {
	args := []string{
		"upgrade",
		"--manifest", manifestPath,
		"--accept-package-agreements",
		"--disable-interactivity",
	}
	if !containsArg(args, "--force") {
		args = append(args, "--force")
	}
	return args
}

func loadSelfUpgradeManifestOverride() string {
	data, err := os.ReadFile(selfUpdateManifestOverridePath())
	if err != nil {
		return ""
	}

	manifestPath := strings.TrimSpace(string(data))
	if manifestPath == "" {
		return ""
	}
	if !filepath.IsAbs(manifestPath) {
		if absPath, err := filepath.Abs(manifestPath); err == nil {
			manifestPath = absPath
		}
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return ""
	}
	return manifestPath
}

func clearSelfUpgradeManifestOverride() {
	_ = os.Remove(selfUpdateManifestOverridePath())
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
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
