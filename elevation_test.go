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

// ── Helper-side: executeCleanupDeleteForHelper ────────────────────────

func TestExecuteCleanupDeleteForHelperRejectsMissingTargetID(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_ = executeCleanupDeleteForHelper(server, helperRequest{Action: "cleanup_delete"})
	}()

	// No bytes should land on the wire — the executor returns an error which
	// the (real) dispatcher would translate into an "error" response. So a
	// short read with a deadline is enough to confirm "no result was sent".
	_ = client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err == nil && n > 0 {
		t.Fatalf("executor should not write anything on missing target_id, got %d bytes: %q", n, buf[:n])
	}
}

func TestExecuteCleanupDeleteForHelperRejectsUnknownTarget(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- executeCleanupDeleteForHelper(server, helperRequest{
			Action:   "cleanup_delete",
			TargetID: "definitely-not-a-real-id",
		})
	}()

	// Drain so the executor goroutine isn't blocked on writes (none expected).
	_ = client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 64)
	client.Read(buf)

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "unknown cleanup target id") {
		t.Errorf("executor err = %v, want 'unknown cleanup target id'", err)
	}
}

func TestExecuteCleanupDeleteForHelperRejectsNonAdminTarget(t *testing.T) {
	// "user_temp" is in the registry but doesn't require admin — the helper
	// must refuse to handle it (defense in depth).
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- executeCleanupDeleteForHelper(server, helperRequest{
			Action:   "cleanup_delete",
			TargetID: "user_temp",
		})
	}()

	_ = client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 64)
	client.Read(buf)

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "does not require admin") {
		t.Errorf("executor err = %v, want 'does not require admin'", err)
	}
}

// ── TUI side: cleanupTargetElevated ───────────────────────────────────

func TestCleanupTargetElevatedSuccess(t *testing.T) {
	origStart := startElevatedHelperFunc
	origTimeout := helperAcceptTimeout
	t.Cleanup(func() {
		startElevatedHelperFunc = origStart
		helperAcceptTimeout = origTimeout
	})

	listener := newFakePipeListener()
	startElevatedHelperFunc = func(_ context.Context, _ string) (net.Listener, error) {
		return listener, nil
	}
	helperAcceptTimeout = time.Second

	serverConn, clientConn := net.Pipe()
	listener.acceptCh <- serverConn

	// Fake helper: read the request, send a result + done.
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
		if req.Action != "cleanup_delete" || req.TargetID != "windows_temp" {
			sendHelperResponse(clientConn, "error", "unexpected request: "+string(line))
			return
		}
		wire := cleanupTargetResultWire{
			ID:           "windows_temp",
			ResolvedPath: `C:\Windows\Temp`,
			SizeBytes:    100,
			FreedBytes:   100,
			Files:        2,
		}
		b, _ := json.Marshal(wire)
		sendHelperResponse(clientConn, "result", string(b))
		sendHelperResponse(clientConn, "done", "")
	}()

	m := &elevationManager{}
	res, err := m.cleanupTargetElevated("windows_temp")
	if err != nil {
		t.Fatalf("cleanupTargetElevated err = %v", err)
	}
	if res.id != "windows_temp" || res.freedBytes != 100 || res.files != 2 {
		t.Errorf("result = %#v", res)
	}
}

func TestCleanupTargetElevatedHelperError(t *testing.T) {
	origStart := startElevatedHelperFunc
	origTimeout := helperAcceptTimeout
	t.Cleanup(func() {
		startElevatedHelperFunc = origStart
		helperAcceptTimeout = origTimeout
	})

	listener := newFakePipeListener()
	startElevatedHelperFunc = func(_ context.Context, _ string) (net.Listener, error) {
		return listener, nil
	}
	helperAcceptTimeout = time.Second

	serverConn, clientConn := net.Pipe()
	listener.acceptCh <- serverConn

	go func() {
		defer clientConn.Close()
		reader := bufio.NewReader(clientConn)
		_, _ = reader.ReadString('\n')
		sendHelperResponse(clientConn, "error", "unknown cleanup target id: \"bogus\"")
	}()

	m := &elevationManager{}
	_, err := m.cleanupTargetElevated("bogus")
	if err == nil || !strings.Contains(err.Error(), "unknown cleanup target id") {
		t.Errorf("err = %v, want 'unknown cleanup target id'", err)
	}
}

func TestCleanupTargetElevatedDoneWithoutResult(t *testing.T) {
	// A misbehaving helper that sends "done" without a prior "result" must
	// surface as an error, not a silent zero-value success.
	origStart := startElevatedHelperFunc
	origTimeout := helperAcceptTimeout
	t.Cleanup(func() {
		startElevatedHelperFunc = origStart
		helperAcceptTimeout = origTimeout
	})

	listener := newFakePipeListener()
	startElevatedHelperFunc = func(_ context.Context, _ string) (net.Listener, error) {
		return listener, nil
	}
	helperAcceptTimeout = time.Second

	serverConn, clientConn := net.Pipe()
	listener.acceptCh <- serverConn

	go func() {
		defer clientConn.Close()
		reader := bufio.NewReader(clientConn)
		_, _ = reader.ReadString('\n')
		sendHelperResponse(clientConn, "done", "")
	}()

	m := &elevationManager{}
	_, err := m.cleanupTargetElevated("windows_temp")
	if err == nil || !strings.Contains(err.Error(), "without sending a result") {
		t.Errorf("err = %v, want 'without sending a result'", err)
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
