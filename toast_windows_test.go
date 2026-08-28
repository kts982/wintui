package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// captureToast wires sendToastFn to a stub and returns a getter for the
// recorded calls. Tests use this instead of dispatching real PowerShell.
func captureToast(t *testing.T) (calls *[]toastCall, restore func()) {
	t.Helper()
	origFn := sendToastFn
	origElevated := isElevated
	origCI := getEnvFn
	captured := []toastCall{}
	sendToastFn = func(title, body string) {
		captured = append(captured, toastCall{title: title, body: body})
	}
	isElevated = func() bool { return false }
	getEnvFn = func(string) string { return "" }
	return &captured, func() {
		sendToastFn = origFn
		isElevated = origElevated
		getEnvFn = origCI
	}
}

type toastCall struct {
	title string
	body  string
}

func TestSendToastSkipsWhenSettingOff(t *testing.T) {
	calls, restore := captureToast(t)
	defer restore()
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings() // ToastNotifications defaults to false

	sendToast("X", "Y")

	if len(*calls) != 0 {
		t.Fatalf("toast fired with setting OFF: %#v", *calls)
	}
}

func TestSendToastFiresWhenSettingOn(t *testing.T) {
	calls, restore := captureToast(t)
	defer restore()
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings()
	appSettings.ToastNotifications = true

	sendToast("Title", "Body")

	if len(*calls) != 1 {
		t.Fatalf("expected 1 toast call, got %d: %#v", len(*calls), *calls)
	}
	if (*calls)[0].title != "Title" || (*calls)[0].body != "Body" {
		t.Fatalf("got %#v, want {Title, Body}", (*calls)[0])
	}
}

func TestSendToastFiresWhenElevated(t *testing.T) {
	// Elevation does NOT suppress: UAC-elevated processes share the user's
	// session and notification stream, and the scheduled-task case for
	// `wintui check` often runs elevated.
	calls, restore := captureToast(t)
	defer restore()
	isElevated = func() bool { return true }
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings()
	appSettings.ToastNotifications = true

	sendToast("X", "Y")

	if len(*calls) != 1 {
		t.Fatalf("expected toast to fire while elevated, got %d calls", len(*calls))
	}
}

func TestSendToastSuppressedInCI(t *testing.T) {
	calls, restore := captureToast(t)
	defer restore()
	getEnvFn = func(name string) string {
		if name == "CI" {
			return "true"
		}
		return ""
	}
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings()
	appSettings.ToastNotifications = true

	sendToast("X", "Y")

	if len(*calls) != 0 {
		t.Fatalf("toast fired in CI: %#v", *calls)
	}
}

func TestSendToastSuppressedByKillSwitch(t *testing.T) {
	calls, restore := captureToast(t)
	defer restore()
	getEnvFn = func(name string) string {
		if name == "WINTUI_DISABLE_TOAST" {
			return "1"
		}
		return ""
	}
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings()
	appSettings.ToastNotifications = true

	sendToast("X", "Y")

	if len(*calls) != 0 {
		t.Fatalf("kill switch did not suppress toast: %#v", *calls)
	}
}

func TestNotifyBatchFinishBodyShape(t *testing.T) {
	calls, restore := captureToast(t)
	defer restore()
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings()
	appSettings.ToastNotifications = true

	tests := []struct {
		name     string
		items    []batchItem
		wantBody string
		wantSent bool
	}{
		{
			"all succeeded",
			[]batchItem{
				{status: batchDone, action: retryOpUpgrade},
				{status: batchDone, action: retryOpUpgrade},
				{status: batchDone, action: retryOpUpgrade},
			},
			"3 of 3 upgraded",
			true,
		},
		{
			"mixed pass/fail",
			[]batchItem{
				{status: batchDone, action: retryOpUpgrade},
				{status: batchFailed, action: retryOpUpgrade},
				{status: batchDone, action: retryOpUpgrade},
			},
			"2 of 3 upgraded · 1 failed",
			true,
		},
		{
			"all failed",
			[]batchItem{
				{status: batchFailed, action: retryOpUpgrade},
				{status: batchFailed, action: retryOpUpgrade},
			},
			// All items failed — verb falls back to "completed" because there's
			// no successful op to attribute to.
			"0 of 2 completed · 2 failed",
			true,
		},
		{
			"done + pending-restart (mixed) surfaces both",
			[]batchItem{
				{status: batchDone, action: retryOpUpgrade},
				{status: batchPendingRestart, action: retryOpUpgrade},
			},
			"1 of 2 upgraded · 1 awaiting restart",
			true,
		},
		{
			"pending-restart only still surfaces — user needs to know",
			[]batchItem{{status: batchPendingRestart, action: retryOpUpgrade}},
			"0 of 1 upgraded · 1 awaiting restart",
			true,
		},
		{
			"failed + pending mix",
			[]batchItem{
				{status: batchFailed, action: retryOpUpgrade},
				{status: batchPendingRestart, action: retryOpUpgrade},
				{status: batchDone, action: retryOpUpgrade},
			},
			"1 of 3 upgraded · 1 failed · 1 awaiting restart",
			true,
		},
		{
			// Regression for the v2.6.x bug where uninstall batches always
			// said "upgraded" in the toast body (project_v2_6_known_bugs.md).
			"uninstall batch reports 'uninstalled'",
			[]batchItem{
				{status: batchDone, action: retryOpUninstall},
				{status: batchDone, action: retryOpUninstall},
			},
			"2 of 2 uninstalled",
			true,
		},
		{
			"install batch reports 'installed'",
			[]batchItem{
				{status: batchDone, action: retryOpInstall},
			},
			"1 of 1 installed",
			true,
		},
		{
			"heterogeneous batch falls back to 'completed' rather than lying",
			[]batchItem{
				{status: batchDone, action: retryOpUpgrade},
				{status: batchDone, action: retryOpUninstall},
			},
			"2 of 2 completed",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*calls = (*calls)[:0]
			notifyBatchFinish(tt.items)
			if !tt.wantSent {
				if len(*calls) != 0 {
					t.Fatalf("unexpected toast: %#v", *calls)
				}
				return
			}
			if len(*calls) != 1 {
				t.Fatalf("expected 1 toast, got %d", len(*calls))
			}
			if (*calls)[0].body != tt.wantBody {
				t.Errorf("body = %q, want %q", (*calls)[0].body, tt.wantBody)
			}
			if (*calls)[0].title != toastDisplayName {
				t.Errorf("title = %q, want %q", (*calls)[0].title, toastDisplayName)
			}
		})
	}
}

func TestNotifyCheckFoundUpdatesPluralizes(t *testing.T) {
	calls, restore := captureToast(t)
	defer restore()
	origSettings := appSettings
	defer func() { appSettings = origSettings }()
	appSettings = DefaultSettings()
	appSettings.ToastNotifications = true

	tests := []struct {
		count    int
		wantSent bool
		wantBody string
	}{
		{0, false, ""},
		{1, true, "1 update available"},
		{5, true, "5 updates available"},
	}

	for _, tt := range tests {
		*calls = (*calls)[:0]
		notifyCheckFoundUpdates(tt.count)
		if !tt.wantSent {
			if len(*calls) != 0 {
				t.Fatalf("count=%d should not toast, got %#v", tt.count, *calls)
			}
			continue
		}
		if len(*calls) != 1 || (*calls)[0].body != tt.wantBody {
			t.Errorf("count=%d body = %#v, want %q", tt.count, *calls, tt.wantBody)
		}
	}
}

func TestEscapeToastXMLHandlesAllSpecialChars(t *testing.T) {
	got := escapeToastXML(`a<b>c&d"e'f`)
	want := "a&lt;b&gt;c&amp;d&quot;e&apos;f"
	if got != want {
		t.Fatalf("escapeToastXML = %q, want %q", got, want)
	}
}

func TestRenderToastScriptInjectsEscapedFields(t *testing.T) {
	script := renderToastScript("Build <X> 'Δ'", "OK & done $5 ` now")
	if !strings.Contains(script, "&lt;X&gt;") {
		t.Errorf("script missing escaped title: %s", script)
	}
	if !strings.Contains(script, "OK &amp; done") {
		t.Errorf("script missing escaped body: %s", script)
	}
	if !strings.Contains(script, "CreateToastNotifier('"+toastAppID+"')") {
		t.Errorf("script does not call CreateToastNotifier with toastAppID")
	}
	if strings.Contains(script, "$PSCommandPath") {
		t.Errorf("inline toast script still references $PSCommandPath: %s", script)
	}
}

func TestToastPowerShellHostArgsUseInlineCommand(t *testing.T) {
	script := "Write-Output 'toast $value ` Δ'\nWrite-Output \"quoted\""
	args := toastPowerShellHostArgs(script)

	for _, want := range []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command"} {
		if !containsArg(args, want) {
			t.Fatalf("toastPowerShellHostArgs() = %#v, missing %q", args, want)
		}
	}
	for _, unwanted := range []string{"-ExecutionPolicy", "Bypass", "-File"} {
		if containsArg(args, unwanted) {
			t.Fatalf("toastPowerShellHostArgs() = %#v, unexpectedly contains %q", args, unwanted)
		}
	}
	if args[len(args)-2] != "-Command" || args[len(args)-1] != script {
		t.Fatalf("toastPowerShellHostArgs() = %#v, expected trailing -Command <script>", args)
	}
	cmd := exec.Command(powershellExePath(), args...)
	if got := cmd.Args[len(cmd.Args)-1]; got != script {
		t.Fatalf("exec.Cmd last arg = %q, want script preserved verbatim %q", got, script)
	}
}

func TestConfigureToastScriptHostPreservesWindowFlags(t *testing.T) {
	cmd := exec.Command("cmd")
	configureToastScriptHost(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr = nil")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow = false")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("CREATE_NO_WINDOW flag missing")
	}
	if cmd.SysProcAttr.CreationFlags&windows.DETACHED_PROCESS != 0 {
		t.Fatal("DETACHED_PROCESS must remain disabled for WinRT toast loading")
	}
}

func TestAppendToastErrorLogNamesInlineOperationWithoutCommand(t *testing.T) {
	dir := t.TempDir()
	origCacheDir := userCacheDirPath
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userCacheDirPath = origCacheDir })

	appendToastErrorLog("shortcut-ensure", errors.New("boom"), "synthetic stderr")
	data, err := os.ReadFile(filepath.Join(dir, "wintui", "toast", "error.log"))
	if err != nil {
		t.Fatalf("ReadFile(error.log): %v", err)
	}
	logText := string(data)
	for _, want := range []string{"operation=shortcut-ensure", "transport=inline", "synthetic stderr"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("error log missing %q: %s", want, logText)
		}
	}
	if strings.Contains(logText, "script=") || strings.Contains(logText, "-Command") {
		t.Fatalf("error log leaked command/script metadata: %s", logText)
	}
}
