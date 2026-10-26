package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestHelperRouteAllowed(t *testing.T) {
	tests := []struct {
		scope          InstallScope
		separate       bool
		upfront, retry bool
	}{
		{ScopeDefault, false, true, true},
		{ScopeUser, false, true, true}, // same account: unchanged by this guard
		{ScopeMachine, false, true, true},
		{ScopeDefault, true, false, true},
		{ScopeUser, true, false, false},
		{ScopeMachine, true, true, true},
	}
	for _, tt := range tests {
		eff := Settings{Scope: tt.scope}
		if got := upfrontHelperAllowed(eff, tt.separate); got != tt.upfront {
			t.Errorf("upfrontHelperAllowed(scope=%q, separate=%v) = %v, want %v", tt.scope, tt.separate, got, tt.upfront)
		}
		if got := retryHelperAllowed(eff, tt.separate); got != tt.retry {
			t.Errorf("retryHelperAllowed(scope=%q, separate=%v) = %v, want %v", tt.scope, tt.separate, got, tt.retry)
		}
	}
}

// routeHarness isolates the smart action route: a fresh globalElevator, an
// unelevated process, a pinned elevation-account prediction, a counted helper
// launcher and a stubbed non-elevated winget run.
type routeHarness struct {
	mu           sync.Mutex
	helperStarts int
	normalRuns   int
}

func newRouteHarness(t *testing.T, predictedSeparate bool, normalErr error) *routeHarness {
	t.Helper()
	h := &routeHarness{}

	origElevator, origElevated := globalElevator, isElevated
	origStart, origStream := startElevatedHelperFunc, actionWingetStream
	t.Cleanup(func() {
		globalElevator, isElevated = origElevator, origElevated
		startElevatedHelperFunc, actionWingetStream = origStart, origStream
	})
	globalElevator = &elevationManager{}
	isElevated = func() bool { return false }
	pinPredictedSeparateAccount(t, predictedSeparate)

	startElevatedHelperFunc = func(_ context.Context, _, _ string) (net.Listener, error) {
		h.mu.Lock()
		h.helperStarts++
		h.mu.Unlock()
		return nil, errors.New("no helper in tests")
	}
	actionWingetStream = func(_ context.Context, _ bool, _ ...string) (<-chan string, <-chan error) {
		h.mu.Lock()
		h.normalRuns++
		h.mu.Unlock()
		out := make(chan string)
		errCh := make(chan error, 1)
		close(out)
		errCh <- normalErr
		close(errCh)
		return out, errCh
	}
	return h
}

func (h *routeHarness) counts() (helperStarts, normalRuns int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.helperStarts, h.normalRuns
}

func drainRoute(out <-chan string, errCh <-chan error) ([]string, error) {
	var lines []string
	for line := range out {
		lines = append(lines, line)
	}
	return lines, <-errCh
}

func TestSmartRouteUpfrontHelper(t *testing.T) {
	tests := []struct {
		name       string
		separate   bool
		scope      InstallScope
		wantHelper int
	}{
		{"same account: silent goes to the helper first", false, ScopeDefault, 1},
		{"separate account: unknown scope stays non-elevated", true, ScopeDefault, 0},
		{"separate account: user scope stays non-elevated", true, ScopeUser, 0},
		{"separate account: explicit machine scope may elevate", true, ScopeMachine, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRouteHarness(t, tt.separate, nil)
			eff := Settings{InstallMode: ModeSilent, AutoElevate: true, Scope: tt.scope}
			if _, err := drainRoute(runActionSmartStreamCtxFor(context.Background(), eff, "install", "--id", "Test.App")); err != nil {
				t.Fatalf("route err = %v", err)
			}
			starts, normal := h.counts()
			if starts != tt.wantHelper {
				t.Fatalf("helper starts = %d, want %d", starts, tt.wantHelper)
			}
			// The test launcher always fails, so every case ends on the
			// non-elevated run.
			if normal != 1 {
				t.Fatalf("non-elevated runs = %d, want 1", normal)
			}
		})
	}
}

func TestSmartRouteRequiresAdminRetry(t *testing.T) {
	requiresAdmin := errors.New("This operation requires elevation")
	tests := []struct {
		name       string
		separate   bool
		scope      InstallScope
		wantHelper int
	}{
		{"same account retries elevated", false, ScopeDefault, 1},
		{"separate account: unknown scope retries elevated", true, ScopeDefault, 1},
		{"separate account: user scope does not retry elevated", true, ScopeUser, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRouteHarness(t, tt.separate, requiresAdmin)
			eff := Settings{AutoElevate: true, Scope: tt.scope} // interactive: no upfront route
			_, err := drainRoute(runActionSmartStreamCtxFor(context.Background(), eff, "install", "--id", "Test.App"))
			if err == nil {
				t.Fatal("route err = nil, want the requires-admin failure")
			}
			if starts, _ := h.counts(); starts != tt.wantHelper {
				t.Fatalf("helper starts = %d, want %d", starts, tt.wantHelper)
			}
		})
	}
}

// TestPackageElevateRuleUnderSeparateAccount covers an explicit elevate=true
// rule: it always tries the helper when elevation runs as the user, but under
// another account only with machine scope, and otherwise runs as the user and
// says why.
func TestPackageElevateRuleUnderSeparateAccount(t *testing.T) {
	tests := []struct {
		name        string
		separate    bool
		globalScope InstallScope
		ruleScope   InstallScope
		wantHelper  int
		wantNote    bool
	}{
		{"same account elevates", false, ScopeDefault, ScopeDefault, 1, false},
		{"same account elevates even a user-scope rule", false, ScopeDefault, ScopeUser, 1, false},
		{"separate account: unknown scope runs as the user", true, ScopeDefault, ScopeDefault, 0, true},
		{"separate account: global user scope runs as the user", true, ScopeUser, ScopeDefault, 0, true},
		{"separate account: rule scope=machine elevates", true, ScopeDefault, ScopeMachine, 1, false},
		{"separate account: global scope=machine elevates", true, ScopeMachine, ScopeDefault, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRouteHarness(t, tt.separate, nil)
			original := appSettings
			t.Cleanup(func() { appSettings = original })
			elevate := true
			appSettings = DefaultSettings()
			appSettings.Scope = tt.globalScope
			appSettings.setOverride("Test.App", "winget", PackageOverride{Scope: tt.ruleScope, Elevate: &elevate})

			lines, err := drainRoute(runActionStreamForPackage(context.Background(), retryOpInstall, "Test.App", "winget", "install", "--id", "Test.App"))
			if err != nil {
				t.Fatalf("route err = %v", err)
			}
			if starts, _ := h.counts(); starts != tt.wantHelper {
				t.Fatalf("helper starts = %d, want %d", starts, tt.wantHelper)
			}
			gotNote := len(lines) > 0 && lines[0] == elevateRuleSkippedNote
			if gotNote != tt.wantNote {
				t.Fatalf("skipped-rule note = %v, want %v (lines %q)", gotNote, tt.wantNote, lines)
			}
		})
	}
}

// TestSmartRouteRefusesHelperThatReportsAnotherAccount covers a wrong
// prediction: the helper connects, reports a different account, and the
// unknown-scope action must still run non-elevated instead of in the helper.
func TestSmartRouteRefusesHelperThatReportsAnotherAccount(t *testing.T) {
	h := newRouteHarness(t, false, nil)
	origTimeout := helperAcceptTimeout
	t.Cleanup(func() { helperAcceptTimeout = origTimeout })
	helperAcceptTimeout = time.Second

	listener := newFakePipeListener()
	startElevatedHelperFunc = func(_ context.Context, _, _ string) (net.Listener, error) {
		h.mu.Lock()
		h.helperStarts++
		h.mu.Unlock()
		return listener, nil
	}
	serverConn, clientConn := net.Pipe()
	listener.acceptCh <- serverConn
	defer clientConn.Close()

	sawWinget := make(chan bool, 1)
	go func() {
		reader := bufio.NewReader(clientConn)
		if !answerIdentity(reader, clientConn, "S-1-5-21-9-9-9-1002") {
			sawWinget <- false
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			sawWinget <- false
			return
		}
		var req helperRequest
		_ = json.Unmarshal([]byte(line), &req)
		sawWinget <- req.Action == "winget"
	}()

	eff := Settings{InstallMode: ModeSilent, AutoElevate: true}
	if _, err := drainRoute(runActionSmartStreamCtxFor(context.Background(), eff, "install", "--id", "Test.App")); err != nil {
		t.Fatalf("route err = %v", err)
	}
	if starts, normal := h.counts(); starts != 1 || normal != 1 {
		t.Fatalf("helper starts = %d, non-elevated runs = %d; want 1 and 1", starts, normal)
	}
	if !globalElevator.separateAccount() {
		t.Fatal("separateAccount() = false after the helper reported another account")
	}

	// Close our end so the fake helper's pending read returns: it must not
	// have received a winget request.
	globalElevator.shutdown()
	if <-sawWinget {
		t.Fatal("the helper received the winget request despite running as another account")
	}
}
