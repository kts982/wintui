package main

import (
	"strings"
	"testing"
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
			[]batchItem{{status: batchDone}, {status: batchDone}, {status: batchDone}},
			"3 of 3 succeeded",
			true,
		},
		{
			"mixed",
			[]batchItem{{status: batchDone}, {status: batchFailed}, {status: batchDone}},
			"2 of 3 succeeded · 1 failed",
			true,
		},
		{
			"all failed",
			[]batchItem{{status: batchFailed}, {status: batchFailed}},
			"0 of 2 succeeded · 2 failed",
			true,
		},
		{
			"pending-restart only — no toast",
			[]batchItem{{status: batchPendingRestart}},
			"",
			false,
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
	script := renderToastScript("Build <X>", "OK & done")
	if !strings.Contains(script, "&lt;X&gt;") {
		t.Errorf("script missing escaped title: %s", script)
	}
	if !strings.Contains(script, "OK &amp; done") {
		t.Errorf("script missing escaped body: %s", script)
	}
	if !strings.Contains(script, "CreateToastNotifier('"+toastAppID+"')") {
		t.Errorf("script does not call CreateToastNotifier with toastAppID")
	}
}
