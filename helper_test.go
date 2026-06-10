package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// driveHelperRequest sends one request through a real handleHelperConnection
// (expected token "tok") and returns every response up to the terminal
// done/error message.
func driveHelperRequest(t *testing.T, req helperRequest) []helperResponse {
	t.Helper()

	server, client := net.Pipe()
	go func() {
		_ = handleHelperConnection(server, "tok")
		server.Close()
	}()

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := client.Write(append(b, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var responses []helperResponse
	reader := bufio.NewReader(client)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read response: %v (got %d responses: %#v)", err, len(responses), responses)
		}
		var resp helperResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		responses = append(responses, resp)
		if resp.Type == "done" || resp.Type == "error" {
			client.Close()
			return responses
		}
	}
}

func wantHelperError(t *testing.T, responses []helperResponse, substr string) {
	t.Helper()
	last := responses[len(responses)-1]
	if last.Type != "error" || !strings.Contains(last.Data, substr) {
		t.Fatalf("responses = %#v, want terminal error containing %q", responses, substr)
	}
}

func TestHandleHelperConnectionRejectsMissingToken(t *testing.T) {
	responses := driveHelperRequest(t, helperRequest{
		Action: "winget",
		Args:   []string{"upgrade", "--id", "Test.App", "--exact"},
	})
	wantHelperError(t, responses, "session token")
}

func TestHandleHelperConnectionRejectsWrongToken(t *testing.T) {
	responses := driveHelperRequest(t, helperRequest{
		Action: "winget",
		Args:   []string{"upgrade", "--id", "Test.App", "--exact"},
		Token:  "not-the-token",
	})
	wantHelperError(t, responses, "session token")
}

func TestHandleHelperConnectionRejectsUnknownAction(t *testing.T) {
	responses := driveHelperRequest(t, helperRequest{
		Action: "shell",
		Args:   []string{"cmd", "/c", "whoami"},
		Token:  "tok",
	})
	wantHelperError(t, responses, "unknown helper action")
}

func TestHandleHelperConnectionRejectsEmptyAction(t *testing.T) {
	// Empty action used to fall through to winget for backward compat; the
	// helper now requires the explicit "winget" action.
	responses := driveHelperRequest(t, helperRequest{
		Args:  []string{"upgrade", "--id", "Test.App", "--exact"},
		Token: "tok",
	})
	wantHelperError(t, responses, "unknown helper action")
}

func TestHandleHelperConnectionRejectsDisallowedVerb(t *testing.T) {
	responses := driveHelperRequest(t, helperRequest{
		Action: "winget",
		Args:   []string{"source", "add", "-n", "evil", "-a", `C:\evil`},
		Token:  "tok",
	})
	wantHelperError(t, responses, "not allowed")
}

func TestHandleHelperConnectionRejectsManifestArg(t *testing.T) {
	for _, arg := range []string{"--manifest", "-m", "--MANIFEST"} {
		responses := driveHelperRequest(t, helperRequest{
			Action: "winget",
			Args:   []string{"install", arg, `C:\evil\manifest.yaml`},
			Token:  "tok",
		})
		wantHelperError(t, responses, "not allowed")
	}
}

// The allowlist must also reject install-option escalation vectors that a
// bare verb+manifest denylist would forward to the elevated installer.
func TestHandleHelperConnectionRejectsInstallerEscalationArgs(t *testing.T) {
	cases := [][]string{
		{"install", "--id", "Test.App", "--exact", "--override", "/qn /arbitrary"},
		{"upgrade", "--id", "Test.App", "--exact", "--custom", "--evil"},
		{"install", "--id", "Test.App", "--exact", "--location", `C:\evil`},
		{"install", "--id", "Test.App", "--exact", "Bare.Positional"},
		{"upgrade", "--id"}, // value flag with no value
	}
	for _, args := range cases {
		responses := driveHelperRequest(t, helperRequest{
			Action: "winget",
			Args:   args,
			Token:  "tok",
		})
		last := responses[len(responses)-1]
		if last.Type != "error" {
			t.Errorf("args %v: terminal response = %q, want error", args, last.Type)
		}
	}
}

func TestHandleHelperConnectionRunsAllowedRequest(t *testing.T) {
	origStream := helperWingetStream
	var gotArgs []string
	var gotNonInt bool
	helperWingetStream = func(ctx context.Context, nonInteractive bool, args ...string) (<-chan string, <-chan error) {
		gotArgs = append([]string(nil), args...)
		gotNonInt = nonInteractive
		out := make(chan string, 1)
		out <- "Successfully installed"
		close(out)
		errCh := make(chan error, 1)
		errCh <- nil
		close(errCh)
		return out, errCh
	}
	t.Cleanup(func() { helperWingetStream = origStream })

	wantArgs := []string{"upgrade", "--id", "Test.App", "--exact", "--accept-package-agreements"}
	responses := driveHelperRequest(t, helperRequest{
		Action: "winget",
		Args:   wantArgs,
		Token:  "tok",
	})

	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("helper ran args %#v, want %#v", gotArgs, wantArgs)
	}
	if gotNonInt {
		t.Fatal("helper forced non-interactive mode; NonInt=false must pass through")
	}
	if len(responses) != 2 || responses[0].Type != "line" || responses[1].Type != "done" {
		t.Fatalf("responses = %#v, want line + done", responses)
	}
}

func TestValidateHelperWingetArgsAllowsRealCallSites(t *testing.T) {
	// The allowlist must cover exactly what the TUI builds for elevated runs.
	pkg := Package{ID: "Test.App", Name: "Test App", Source: "winget"}
	for _, args := range [][]string{
		installCommandArgs("Test.App", "winget", ""),
		upgradeCommandArgs("Test.App", "winget", "1.2.3"),
		uninstallCommandArgs(pkg, true, true),
	} {
		if err := validateHelperWingetArgs(args); err != nil {
			t.Errorf("validateHelperWingetArgs(%#v) = %v, want nil", args, err)
		}
	}
}
