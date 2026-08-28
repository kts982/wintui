package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// toastAppID is the AppUserModel ID Windows uses to attribute toasts to
// WinTUI. It must match the ID set on the Start Menu shortcut by
// ensureToastShortcut. Using the winget package ID keeps the value stable
// and discoverable.
const (
	toastAppID       = "kts982.WinTUI"
	toastDisplayName = "WinTUI"
)

// sendToastFn is the seam tests replace with a stub. Production uses
// sendToastWindows. Honoring the seam means tests don't fire real toasts.
var sendToastFn = sendToastWindows

// getEnvFn is the env-lookup seam for shouldSuppressToast. Tests override
// this so CI / kill-switch suppression can be exercised regardless of the
// runner's actual environment.
var getEnvFn = os.Getenv

// sendToast is the public entry point for every trigger site. It honors the
// toast_notifications setting, applies suppression rules (elevated, CI,
// explicit kill switch), and dispatches to the platform send path.
//
// Fire-and-forget: errors are silently dropped so toast plumbing never
// breaks the calling code path. The toast itself is delivered out-of-process
// via PowerShell + WinRT, so transient PowerShell failures cannot block
// the TUI or the CLI.
func sendToast(title, body string) {
	if !currentSettings().ToastNotifications {
		return
	}
	if shouldSuppressToast() {
		return
	}
	sendToastFn(title, body)
}

// shouldSuppressToast filters out environments where toasts would surprise
// users or can't surface at all. Limited to CI and an explicit kill switch:
// UAC-elevated processes share the user's session and notification stream,
// so an `isElevated()` check would be wrong (and would silently break the
// scheduled `wintui check` use case, which often runs elevated).
func shouldSuppressToast() bool {
	if getEnvFn("CI") != "" {
		return true
	}
	if getEnvFn("WINTUI_DISABLE_TOAST") != "" {
		return true
	}
	return false
}

// sendToastWindows is the production toast delivery path: ensures the AUMID
// Start Menu shortcut exists (so attribution renders as "WinTUI" not
// "PowerShell"), then dispatches a hidden-window PowerShell instance that
// invokes Windows.UI.Notifications.ToastNotificationManager.
func sendToastWindows(title, body string) {
	_ = ensureToastShortcut()

	script := renderToastScript(title, body)
	args := toastPowerShellHostArgs(script)
	if err := validatePowerShellCommandLine(powershellExePath(), args); err != nil {
		appendToastErrorLog("toast-send", err, "")
		return
	}

	cmd := exec.Command(powershellExePath(), args...)
	configureToastScriptHost(cmd)
	_ = cmd.Start()
}

// toastPowerShellHostArgs returns the PowerShell args used for both the
// shortcut-ensure and toast-send commands. -Command must remain the final
// switch so the rendered script is passed as one final argv item.
func toastPowerShellHostArgs(script string) []string {
	return inlinePowerShellHostArgs("Hidden", script)
}

// configureToastScriptHost hides the helper console. CREATE_NO_WINDOW alone
// (no DETACHED_PROCESS) is enough for the fire-and-forget case: the child
// outlives the parent because we never call Wait, and DETACHED_PROCESS was
// observed to break PowerShell's WinRT type loading on some configurations.
func configureToastScriptHost(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
}

// toastStateDir returns %LOCALAPPDATA%\wintui\toast, creating it on demand.
// Inline transport no longer drops scripts here; the directory is retained
// for the shortcut/toast error log.
func toastStateDir() (string, error) {
	cacheDir, err := userCacheDirPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cacheDir, "wintui", "toast")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// shortcutPath returns the per-user Start Menu .lnk we drop on first toast.
// The Start Menu location is required: Windows attributes toasts by walking
// shortcuts in this folder looking for one whose AUMID matches the toast's
// ApplicationId. No other location works for AUMID resolution.
func shortcutPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("APPDATA not set")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", toastDisplayName+".lnk"), nil
}

// ensureToastShortcut guarantees that the Start Menu shortcut exists and has
// PKEY_AppUserModel_ID = toastAppID set on it. Idempotent and self-healing:
// a previous run that crashed between Save() and SetAppId left a poisoned
// .lnk on disk; the marker-file gate detects that case and re-runs the AUMID
// step rather than trusting bare existence.
//
// The PowerShell + inline C# dance is the well-known approach: WScript.Shell
// to write the .lnk, then SHGetPropertyStoreFromParsingName to set the AUMID
// property. Mirrors what BurntToast's New-BTShortcut does internally.
//
// On failure, stderr is appended to %LOCALAPPDATA%\wintui\toast\error.log
// so future silent failures (Add-Type compile errors, missing types) surface.
func ensureToastShortcut() error {
	path, err := shortcutPath()
	if err != nil {
		return err
	}
	markerPath := aumidMarkerPath(path)

	// Both the .lnk and the success marker must exist to trust the AUMID is set.
	// The marker is written ONLY after SetAppId completes — so its presence
	// means the previous run got past the brittle Add-Type/COM step.
	_, lnkErr := os.Stat(path)
	_, markerErr := os.Stat(markerPath)
	if lnkErr == nil && markerErr == nil {
		return nil
	}

	exePath, err := currentExecutablePath()
	if err != nil || exePath == "" {
		return fmt.Errorf("could not resolve wintui executable path")
	}

	// The script's Save step short-circuits if the .lnk already exists, but
	// the AumidSetter step always runs and is idempotent (SetValue + Commit
	// on an already-correct property is a no-op).
	lnkExists := lnkErr == nil
	script := renderShortcutScript(path, exePath, lnkExists)
	args := toastPowerShellHostArgs(script)
	if err := validatePowerShellCommandLine(powershellExePath(), args); err != nil {
		appendToastErrorLog("shortcut-ensure", err, "")
		return err
	}

	cmd := exec.Command(powershellExePath(), args...)
	configureToastScriptHost(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr != nil || stderr.Len() > 0 {
		appendToastErrorLog("shortcut-ensure", runErr, stderr.String())
		if runErr != nil {
			return fmt.Errorf("shortcut-ensure: %w", runErr)
		}
	}
	// Marker write happens AFTER the script returns clean. If we crashed
	// between Save() and SetAppId, the marker stays absent and the next call
	// re-runs to repair the AUMID.
	if err := os.WriteFile(markerPath, []byte(toastAppID), 0644); err != nil {
		return fmt.Errorf("write aumid marker: %w", err)
	}

	// Settle pause: the Action Center indexer maps AUMID to shortcut by
	// scanning the Start Menu folder asynchronously. Without this delay the
	// very first toast after opt-in races the indexer and silently drops.
	// Only paid on first creation (or repair); subsequent calls hit the
	// early-return above.
	time.Sleep(1500 * time.Millisecond)
	return nil
}

// aumidMarkerPath returns the success-marker file path next to the shortcut.
// Sibling location (same directory, .aumid extension) keeps it discoverable
// and easy to clear manually if a user ever needs to force a re-run.
func aumidMarkerPath(lnkPath string) string {
	return strings.TrimSuffix(lnkPath, ".lnk") + ".aumid"
}

// appendToastErrorLog writes failures from an inline shortcut/toast operation
// to a per-user log. It deliberately records only the fixed operation name,
// never the full command or rendered script (toast text can be package-derived).
func appendToastErrorLog(operation string, runErr error, stderr string) {
	dir, err := toastStateDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "error.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] operation=%s transport=inline err=%v\nstderr:\n%s\n---\n",
		time.Now().Format(time.RFC3339), operation, runErr, stderr)
}

// renderToastScript builds the PS script that fires a single text-only toast
// under the AUMID. Title/body are XML-escaped because the LoadXml call is
// strict; the surrounding here-string is single-quoted so PowerShell won't
// expand $vars in user content.
func renderToastScript(title, body string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
[Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime] | Out-Null

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml(@'
<toast>
  <visual>
    <binding template="ToastGeneric">
      <text>%s</text>
      <text>%s</text>
    </binding>
  </visual>
</toast>
'@)

$toast = New-Object Windows.UI.Notifications.ToastNotification $xml
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('%s').Show($toast)
`, escapeToastXML(title), escapeToastXML(body), toastAppID)
}

// notifyBatchFinish summarizes a completed exec-modal batch and dispatches
// the toast. Pending-restart items (e.g. WinTUI self-upgrade handoff staged
// but waiting for the user to press Enter) are surfaced explicitly so a
// mixed batch doesn't claim "1 of 1 succeeded" while one item still needs
// action. Skipped only if the entire batch had no meaningful outcomes.
func notifyBatchFinish(items []batchItem) {
	var done, failed, pending int
	for _, bi := range items {
		switch bi.status {
		case batchDone:
			done++
		case batchFailed:
			failed++
		case batchPendingRestart:
			pending++
		}
	}
	if done == 0 && failed == 0 && pending == 0 {
		return
	}
	total := done + failed + pending
	body := fmt.Sprintf("%d of %d %s", done, total, batchActionVerb(items))
	if failed > 0 {
		body += fmt.Sprintf(" · %d failed", failed)
	}
	if pending > 0 {
		body += fmt.Sprintf(" · %d awaiting restart", pending)
	}
	sendToast(toastDisplayName, body)
}

// batchActionVerb picks the past-tense verb for the toast body based on
// what the batch actually did. A homogeneous install/uninstall/upgrade run
// reports its specific verb; mixed-action batches fall back to "completed"
// so we don't lie to the user (the v2.6.x bug was always saying "upgraded"
// regardless of what ran).
func batchActionVerb(items []batchItem) string {
	var seen retryOp
	mixed := false
	for _, bi := range items {
		if bi.status != batchDone && bi.status != batchPendingRestart {
			continue
		}
		if seen == "" {
			seen = bi.action
			continue
		}
		if seen != bi.action {
			mixed = true
			break
		}
	}
	if mixed || seen == "" {
		return "completed"
	}
	switch seen {
	case retryOpInstall, retryOpApply:
		return "installed"
	case retryOpUninstall:
		return "uninstalled"
	case retryOpUpgrade:
		return "upgraded"
	default:
		return "completed"
	}
}

// notifyCLIUpgradeFinish is the same summary for the headless
// `wintui upgrade --auto/--all/--id` exit point. Skipped when nothing ran.
func notifyCLIUpgradeFinish(succeeded, failed int) {
	if succeeded == 0 && failed == 0 {
		return
	}
	total := succeeded + failed
	body := fmt.Sprintf("%d of %d succeeded", succeeded, total)
	if failed > 0 {
		body += fmt.Sprintf(" · %d failed", failed)
	}
	sendToast(toastDisplayName, body)
}

// notifyCheckFoundUpdates is the `wintui check` trigger. Fires only when
// the scan actually finds something — silent when up-to-date, so a
// scheduled `wintui check` doesn't spam the user with "all good" toasts.
func notifyCheckFoundUpdates(count int) {
	if count <= 0 {
		return
	}
	noun := "update"
	if count != 1 {
		noun = "updates"
	}
	sendToast(toastDisplayName, fmt.Sprintf("%d %s available", count, noun))
}

// escapeToastXML escapes the five XML special characters. Apostrophe is also
// escaped because the PS here-string is single-quoted; an unescaped ' would
// terminate it. We use the entity form which is valid XML and PowerShell-safe.
func escapeToastXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

// renderShortcutScript writes the .lnk via WScript.Shell (skipped if it
// already exists — see lnkExists), then always sets the AUMID property via
// SHGetPropertyStoreFromParsingName. The AumidSetter step is idempotent
// (SetValue + Commit on an already-correct property is a no-op), so always
// running it is the cheap way to repair a previously-poisoned .lnk.
//
// Path values are interpolated as PowerShell single-quoted literals (psQuote)
// so backslashes stay single. %q would produce Go-style "C:\\..." which PS
// reads literally as doubled backslashes — WScript.Shell tolerates that, but
// SHGetPropertyStoreFromParsingName rejects it with E_INVALIDARG.
func renderShortcutScript(lnkPath, exePath string, lnkExists bool) string {
	saveStep := `$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($lnkPath)
$shortcut.TargetPath = $exePath
$shortcut.WorkingDirectory = (Split-Path -Parent $exePath)
$shortcut.IconLocation = "$exePath,0"
$shortcut.Description = 'WinTUI'
$shortcut.Save()
[System.Runtime.InteropServices.Marshal]::ReleaseComObject($shortcut) | Out-Null
[System.Runtime.InteropServices.Marshal]::ReleaseComObject($shell) | Out-Null`
	if lnkExists {
		saveStep = "# .lnk already on disk — skip Save() and only repair the AUMID property"
	}

	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'

$lnkPath = %s
$exePath = %s
$appId   = %s

%s

Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class AumidSetter {
    [StructLayout(LayoutKind.Sequential)]
    public struct PROPERTYKEY { public Guid fmtid; public uint pid; }

    [StructLayout(LayoutKind.Explicit, Size = 24)]
    public struct PropVariant {
        [FieldOffset(0)]  public ushort vt;
        [FieldOffset(8)]  public IntPtr pwszVal;
    }

    [ComImport]
    [Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99")]
    [InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    public interface IPropertyStore {
        uint GetCount(out uint cProps);
        uint GetAt(uint iProp, out PROPERTYKEY pkey);
        uint GetValue(ref PROPERTYKEY key, out PropVariant pv);
        uint SetValue(ref PROPERTYKEY key, ref PropVariant pv);
        uint Commit();
    }

    [DllImport("shell32.dll", CharSet = CharSet.Unicode)]
    public static extern int SHGetPropertyStoreFromParsingName(
        string pszPath, IntPtr zeroWorks, int flags,
        ref Guid riid, out IPropertyStore ppv);

    public static void SetAppId(string lnk, string appId) {
        Guid iid = typeof(IPropertyStore).GUID;
        IPropertyStore store;
        int hr = SHGetPropertyStoreFromParsingName(lnk, IntPtr.Zero, 2, ref iid, out store);
        if (hr < 0) throw new System.ComponentModel.Win32Exception(hr);

        PROPERTYKEY key;
        key.fmtid = new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3");
        key.pid   = 5;

        IntPtr ptr = Marshal.StringToCoTaskMemUni(appId);
        PropVariant pv = new PropVariant { vt = 31, pwszVal = ptr };
        try {
            store.SetValue(ref key, ref pv);
            store.Commit();
        } finally {
            Marshal.FreeCoTaskMem(ptr);
            Marshal.ReleaseComObject(store);
        }
    }
}
'@

[AumidSetter]::SetAppId($lnkPath, $appId)
`, psQuote(lnkPath), psQuote(exePath), psQuote(toastAppID), saveStep)
}

// psQuote wraps s as a PowerShell single-quoted string literal. In single
// quotes, the only special character is the apostrophe itself (escaped by
// doubling it); backslashes, dollar signs, and backticks are all literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
