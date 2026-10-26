package main

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// stubHelperStart replaces the helper launcher for the test and counts calls.
func stubHelperStart(t *testing.T, start func() (net.Listener, error)) *atomic.Int32 {
	t.Helper()
	orig := startElevatedHelperFunc
	t.Cleanup(func() { startElevatedHelperFunc = orig })
	var calls atomic.Int32
	startElevatedHelperFunc = func(context.Context, string, string) (net.Listener, error) {
		calls.Add(1)
		return start()
	}
	return &calls
}

// TestEnsureHelperRemembersAFailedStart: the VM pass saw one prompt and one
// 60 s wait per batch item. A failed start now costs one prompt per batch.
func TestEnsureHelperRemembersAFailedStart(t *testing.T) {
	calls := stubHelperStart(t, func() (net.Listener, error) { return nil, errors.New("launch failed") })
	m := &elevationManager{}
	for i := 0; i < 3; i++ {
		if err := m.ensureHelper(context.Background()); err == nil || err.Error() != "launch failed" {
			t.Fatalf("ensureHelper #%d = %v, want the launch failure", i+1, err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("helper launches = %d, want 1 (later calls reuse the failure)", n)
	}
	m.resetStartFailure()
	_ = m.ensureHelper(context.Background())
	if n := calls.Load(); n != 2 {
		t.Fatalf("helper launches after resetStartFailure = %d, want 2", n)
	}
}

func TestEnsureHelperReportsADeclinedPrompt(t *testing.T) {
	stubHelperStart(t, func() (net.Listener, error) { return nil, windows.ERROR_CANCELLED })
	m := &elevationManager{}
	if err := m.ensureHelper(context.Background()); !errors.Is(err, errElevationCancelled) {
		t.Fatalf("ensureHelper = %v, want errElevationCancelled", err)
	}
}

// TestEnsureHelperWaitIsCancellable: Esc during the wait for the helper ends
// it at once, and is not remembered as a failed start.
func TestEnsureHelperWaitIsCancellable(t *testing.T) {
	listener := newFakePipeListener()
	calls := stubHelperStart(t, func() (net.Listener, error) { return listener, nil })
	m := &elevationManager{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.ensureHelper(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for !m.waitingForHelper() {
		if time.Now().After(deadline) {
			t.Fatal("waitingForHelper never became true during the wait")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ensureHelper = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ensureHelper did not return after the context was cancelled")
	}
	if m.waitingForHelper() {
		t.Fatal("waitingForHelper still true after the wait ended")
	}

	// Not remembered: the next call launches again.
	stubHelperStart(t, func() (net.Listener, error) { return nil, errors.New("second launch") })
	if err := m.ensureHelper(context.Background()); err == nil || err.Error() != "second launch" {
		t.Fatalf("ensureHelper after a cancelled wait = %v, want a fresh launch", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("first launcher calls = %d, want 1", n)
	}
}

// TestBatchItemStartDropsStaleStarts: an item that finished starting after
// its batch was cancelled or replaced must not take over the current batch.
func TestBatchItemStartDropsStaleStarts(t *testing.T) {
	ws := newWorkspaceScreen()
	m := newExecModal(retryOpUpgrade, []batchItem{{action: retryOpUpgrade, item: workspaceItem{pkg: Package{ID: "A.Pkg"}}}})
	m.phase = execPhaseRunning
	ws.modal = &m
	ws.state = workspaceExecuting

	out := make(chan string)
	close(out)
	errCh := errChanWith(nil)
	next, cmd := ws.handleBatchItemStarted(batchItemStartedMsg{ctx: context.Background(), idx: 0, out: out, errCh: errCh})
	if got := next.(workspaceScreen); got.streamOut != nil || cmd != nil {
		t.Fatal("a start from another batch context was wired into the current batch")
	}

	next, cmd = ws.handleBatchItemStarted(batchItemStartedMsg{ctx: ws.ctx, idx: 0, out: out, errCh: errCh})
	if got := next.(workspaceScreen); got.streamOut == nil || cmd == nil {
		t.Fatal("the current batch's start was not wired in")
	}

	next, _ = ws.handleBatchItemStarted(batchItemStartedMsg{ctx: ws.ctx, idx: 0, initErr: errElevationCancelled})
	got := next.(workspaceScreen)
	if it := got.modal.items[0]; it.status != batchFailed || it.err == nil || it.err.Error() != "elevation failed: elevation was cancelled" {
		t.Fatalf("item after a failed forced start = %+v", it)
	}
}
