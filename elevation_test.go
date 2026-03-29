package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

type fakePipeListener struct {
	acceptCh chan net.Conn
	closeCh  chan struct{}
}

func newFakePipeListener() *fakePipeListener {
	return &fakePipeListener{
		acceptCh: make(chan net.Conn, 1),
		closeCh:  make(chan struct{}),
	}
}

func (l *fakePipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.acceptCh:
		return conn, nil
	case <-l.closeCh:
		return nil, net.ErrClosed
	}
}

func (l *fakePipeListener) Close() error {
	select {
	case <-l.closeCh:
	default:
		close(l.closeCh)
	}
	return nil
}

func (l *fakePipeListener) Addr() net.Addr { return fakePipeAddr("pipe") }

type fakePipeAddr string

func (a fakePipeAddr) Network() string { return "pipe" }
func (a fakePipeAddr) String() string  { return string(a) }

func TestEnsureHelperTimeout(t *testing.T) {
	origStart := startElevatedHelperFunc
	origTimeout := helperAcceptTimeout
	t.Cleanup(func() {
		startElevatedHelperFunc = origStart
		helperAcceptTimeout = origTimeout
	})

	listener := newFakePipeListener()
	startElevatedHelperFunc = func(_ context.Context, pipeID string) (net.Listener, error) {
		return listener, nil
	}
	helperAcceptTimeout = 10 * time.Millisecond

	m := &elevationManager{}
	err := m.ensureHelper()
	if err == nil || !strings.Contains(err.Error(), "timeout waiting for elevated helper") {
		t.Fatalf("ensureHelper() error = %v, want timeout", err)
	}
}

func TestRunCommandElevated(t *testing.T) {
	origStart := startElevatedHelperFunc
	origTimeout := helperAcceptTimeout
	t.Cleanup(func() {
		startElevatedHelperFunc = origStart
		helperAcceptTimeout = origTimeout
	})

	listener := newFakePipeListener()
	startElevatedHelperFunc = func(_ context.Context, pipeID string) (net.Listener, error) {
		return listener, nil
	}
	helperAcceptTimeout = time.Second

	serverConn, clientConn := net.Pipe()
	listener.acceptCh <- serverConn

	go func() {
		defer clientConn.Close()
		reader := bufio.NewReader(clientConn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		var req helperRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			return
		}
		if req.NonInt {
			sendHelperResponse(clientConn, "error", "expected mutating helper commands to preserve interactive mode")
			return
		}
		sendHelperResponse(clientConn, "line", "Downloading package")
		sendHelperResponse(clientConn, "line", "Successfully installed")
		sendHelperResponse(clientConn, "done", "")
	}()

	m := &elevationManager{}
	outChan, errChan, err := m.runCommandElevated("install", "--id", "Test.App")
	if err != nil {
		t.Fatalf("runCommandElevated() init error = %v", err)
	}

	var lines []string
	for line := range outChan {
		lines = append(lines, line)
	}
	if err := <-errChan; err != nil {
		t.Fatalf("runCommandElevated() err = %v", err)
	}

	want := []string{"Downloading package", "Successfully installed"}
	if len(lines) != len(want) {
		t.Fatalf("streamed lines = %#v, want %#v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("streamed lines = %#v, want %#v", lines, want)
		}
	}
}
