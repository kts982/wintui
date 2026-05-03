package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

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
	if !appSettings.ToastNotifications {
		return
	}
	if shouldSuppressToast() {
		return
	}
	sendToastFn(title, body)
}

// shouldSuppressToast filters out environments where toasts can't surface or
// would surprise the user. Elevated sessions notify into the elevated user's
// desktop (often invisible to the interactive logon); CI and explicit kill
// switches also skip.
func shouldSuppressToast() bool {
	if isElevated() {
		return true
	}
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
// "PowerShell"), then dispatches a detached, hidden-window PowerShell
// instance that invokes Windows.UI.Notifications.ToastNotificationManager.
func sendToastWindows(title, body string) {
	_ = ensureToastShortcut()

	script := renderToastScript(title, body)
	scriptPath, err := writeToastScript(script)
	if err != nil {
		return
	}

	cmd := exec.Command("powershell.exe", toastPowerShellHostArgs(scriptPath)...)
	configureToastScriptHost(cmd)
	_ = cmd.Start()
}

// toastPowerShellHostArgs returns the PowerShell args used for both the
// shortcut-ensure and the toast-send scripts. -WindowStyle Hidden keeps the
// console invisible; ExecutionPolicy Bypass survives stricter user policies.
func toastPowerShellHostArgs(scriptPath string) []string {
	return []string{
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", scriptPath,
	}
}

// configureToastScriptHost detaches the helper so the parent (TUI / CLI) can
// exit before the toast renders, and hides the console window entirely.
// CREATE_NO_WINDOW is the difference vs the self-update host, which uses
// CREATE_NEW_CONSOLE because that script has visible progress output.
func configureToastScriptHost(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS,
	}
}

// toastStateDir returns %LOCALAPPDATA%\wintui\toast, creating it on demand.
// Used for transient PS scripts.
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

// writeToastScript drops the PS script under the toast state dir with a
// PID-based suffix so concurrent toasts don't collide. The script self-deletes
// at the end so we don't leak files even if cleanup never runs.
func writeToastScript(script string) (string, error) {
	dir, err := toastStateDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("toast-%d.ps1", os.Getpid()))
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		return "", err
	}
	return path, nil
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

// ensureToastShortcut creates the Start Menu shortcut on first toast and sets
// PKEY_AppUserModel_ID = toastAppID via IPropertyStore (the only way Windows
// will route toasts back to WinTUI by AUMID). Subsequent calls no-op.
//
// The PowerShell + inline C# dance is the well-known approach: WScript.Shell
// to write the .lnk, then SHGetPropertyStoreFromParsingName to set the AUMID
// property. Mirrors what BurntToast's New-BTShortcut does internally.
func ensureToastShortcut() error {
	path, err := shortcutPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	exePath, err := currentExecutablePath()
	if err != nil || exePath == "" {
		return fmt.Errorf("could not resolve wintui executable path")
	}

	script := renderShortcutScript(path, exePath)
	scriptPath, err := writeToastScript(script)
	if err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe", toastPowerShellHostArgs(scriptPath)...)
	configureToastScriptHost(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("shortcut-ensure: %w", err)
	}
	_ = os.Remove(scriptPath)
	return nil
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

Remove-Item -LiteralPath $PSCommandPath -ErrorAction SilentlyContinue
`, escapeToastXML(title), escapeToastXML(body), toastAppID)
}

// notifyBatchFinish summarizes a completed exec-modal batch and dispatches
// the toast. Pending-restart-only batches (self-upgrade handoff staged but
// no real result yet) are skipped — there's nothing meaningful to report.
func notifyBatchFinish(items []batchItem) {
	var done, failed int
	for _, bi := range items {
		switch bi.status {
		case batchDone:
			done++
		case batchFailed:
			failed++
		}
	}
	if done == 0 && failed == 0 {
		return
	}
	total := done + failed
	body := fmt.Sprintf("%d of %d succeeded", done, total)
	if failed > 0 {
		body += fmt.Sprintf(" · %d failed", failed)
	}
	sendToast(toastDisplayName, body)
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

// renderShortcutScript writes the .lnk via WScript.Shell, then sets the AUMID
// property via SHGetPropertyStoreFromParsingName. Both steps run inline; the
// inline C# is compiled once per invocation, which is fine because we only
// hit this on the very first toast after the user opts in.
func renderShortcutScript(lnkPath, exePath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'

$lnkPath = %q
$exePath = %q
$appId   = %q

$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($lnkPath)
$shortcut.TargetPath = $exePath
$shortcut.WorkingDirectory = (Split-Path -Parent $exePath)
$shortcut.IconLocation = "$exePath,0"
$shortcut.Description = 'WinTUI'
$shortcut.Save()
[System.Runtime.InteropServices.Marshal]::ReleaseComObject($shortcut) | Out-Null
[System.Runtime.InteropServices.Marshal]::ReleaseComObject($shell) | Out-Null

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
'@ -ReferencedAssemblies System.Runtime.InteropServices

[AumidSetter]::SetAppId($lnkPath, $appId)
`, lnkPath, exePath, toastAppID)
}
