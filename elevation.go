package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

type elevationManager struct {
	mu     sync.Mutex
	initMu sync.Mutex
	// reqMu serializes whole request/response exchanges on the pipe. The
	// protocol has no framing for interleaved requests, so a second request
	// must wait for the first one's terminal done/error response.
	reqMu sync.Mutex
	ln    net.Listener
	conn  net.Conn
	// reader is the single persistent reader on conn. A per-call
	// bufio.Reader could swallow bytes buffered for the next exchange.
	reader *bufio.Reader
	pipeID string
	// token is the per-session secret generated when the helper was
	// launched; every request must carry it (the helper rejects the rest).
	token string
	// helperSID is the account the last connected helper reported in the
	// identity handshake; empty until a helper has connected.
	helperSID string
	// startErr is the last failed helper start. ensureHelper returns it
	// without prompting again until resetStartFailure, so a batch costs at
	// most one prompt and one wait instead of one per item.
	startErr error
	// starting is set while ensureHelper prompts and waits for the helper.
	starting atomic.Bool
}

var globalElevator = &elevationManager{}
var (
	startElevatedHelperFunc = startElevatedHelper
	helperAcceptTimeout     = 60 * time.Second
	helperIdentityTimeout   = 10 * time.Second
)

// errElevationCancelled is a declined elevation prompt.
var errElevationCancelled = errors.New("elevation was cancelled")

// ensureHelper connects the elevated helper, starting it (one elevation
// prompt) if needed. A failed start is remembered: later calls return it
// without a prompt until resetStartFailure. Cancelling ctx stops the wait for
// the helper to connect; that is not remembered.
func (m *elevationManager) ensureHelper(ctx context.Context) error {
	m.initMu.Lock()
	defer m.initMu.Unlock()

	m.mu.Lock()
	connected, startErr := m.conn != nil, m.startErr
	m.mu.Unlock()
	if connected {
		return nil
	}
	if startErr != nil {
		return startErr
	}

	m.starting.Store(true)
	defer m.starting.Store(false)
	err := m.startHelper(ctx)
	if err != nil && ctx.Err() == nil {
		m.mu.Lock()
		m.startErr = err
		m.mu.Unlock()
	}
	return err
}

// resetStartFailure forgets a failed helper start, so the next elevation
// prompts again. A new batch calls it.
func (m *elevationManager) resetStartFailure() {
	m.mu.Lock()
	m.startErr = nil
	m.mu.Unlock()
}

// waitingForHelper reports whether an elevation prompt or the wait for the
// helper to connect is in progress.
func (m *elevationManager) waitingForHelper() bool {
	return m.starting.Load()
}

func (m *elevationManager) startHelper(ctx context.Context) error {
	pipeID := "wintui-" + uuid.New().String()
	token := uuid.New().String()
	ln, err := startElevatedHelperFunc(ctx, pipeID, token)
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return errElevationCancelled
	}
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.pipeID = pipeID
	m.token = token
	m.ln = ln
	m.mu.Unlock()

	// Wait for helper to connect
	// Set a timeout so we don't hang forever if UAC is cancelled
	type accepted struct {
		conn net.Conn
		err  error
	}
	acceptCh := make(chan accepted, 1)
	go func() {
		conn, err := ln.Accept()
		acceptCh <- accepted{conn, err}
	}()

	// The prompt is over by now (the elevated launch blocks during it), so a
	// timeout means the helper started but never connected.
	var conn net.Conn
	select {
	case a := <-acceptCh:
		if a.err != nil {
			return a.err
		}
		conn = a.conn
	case <-time.After(helperAcceptTimeout):
		m.closeListener(ln)
		return fmt.Errorf("the elevated helper did not connect within %s", helperAcceptTimeout)
	case <-ctx.Done():
		m.closeListener(ln)
		return ctx.Err()
	}

	// Learn which account the helper runs as before any request can use the
	// connection: the routing guard needs it to keep per-user actions out of
	// another account's profile.
	reader := bufio.NewReader(conn)
	id, err := helperIdentityHandshake(conn, reader, token)
	if err != nil {
		conn.Close()
		m.closeListener(ln)
		return fmt.Errorf("elevated helper identity check failed: %w", err)
	}

	m.mu.Lock()
	m.conn = conn
	m.reader = reader
	m.helperSID = id.SID
	m.mu.Unlock()
	return nil
}

// closeListener closes ln and forgets it if it is still the current listener.
func (m *elevationManager) closeListener(ln net.Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ln == ln && m.ln != nil {
		m.ln.Close()
		m.ln = nil
	}
}

// helperIdentityHandshake asks a freshly connected helper which account it
// runs as. It is the first exchange on the connection, bounded by
// helperIdentityTimeout so a wedged helper cannot hang the caller.
func helperIdentityHandshake(conn net.Conn, reader *bufio.Reader, token string) (helperIdentity, error) {
	_ = conn.SetDeadline(time.Now().Add(helperIdentityTimeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	b, err := json.Marshal(helperRequest{Action: "identity", Token: token})
	if err != nil {
		return helperIdentity{}, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return helperIdentity{}, err
	}

	var id helperIdentity
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return helperIdentity{}, err
		}
		var resp helperResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		switch resp.Type {
		case "result":
			if err := json.Unmarshal([]byte(resp.Data), &id); err != nil {
				return helperIdentity{}, fmt.Errorf("parse identity: %w", err)
			}
		case "done":
			if id.SID == "" {
				return helperIdentity{}, fmt.Errorf("helper reported no identity")
			}
			return id, nil
		case "error":
			return helperIdentity{}, fmt.Errorf("%s", resp.Data)
		}
	}
}

// separateAccount reports whether elevation runs as an account other than
// the user's. Once a helper has connected, the account it reported is the
// answer; before that, the linked-token prediction is.
func (m *elevationManager) separateAccount() bool {
	m.mu.Lock()
	helperSID := m.helperSID
	m.mu.Unlock()
	if helperSID == "" {
		return predictedSeparateElevationAccount()
	}
	self, err := currentUserSID()
	return err != nil || !strings.EqualFold(helperSID, self)
}

// shutdown closes the helper connection and listener, causing the
// elevated helper process to exit cleanly.
func (m *elevationManager) shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
		m.reader = nil
	}
	if m.ln != nil {
		m.ln.Close()
		m.ln = nil
	}
}

// cleanupTargetElevated runs an admin-required cleanup target through the
// elevated helper. The TUI sends only the registry ID; the helper resolves
// and validates the path on its side. reqMu serializes the whole exchange
// against any outstanding runCommandElevated on the shared connection.
func (m *elevationManager) cleanupTargetElevated(targetID string) (cleanupTargetResult, error) {
	if err := m.ensureHelper(context.Background()); err != nil {
		return cleanupTargetResult{}, err
	}

	m.reqMu.Lock()
	defer m.reqMu.Unlock()

	m.mu.Lock()
	conn, reader, token := m.conn, m.reader, m.token
	m.mu.Unlock()
	if conn == nil {
		return cleanupTargetResult{}, fmt.Errorf("helper connection lost")
	}

	req := helperRequest{Action: "cleanup_delete", TargetID: targetID, Token: token}
	if def, ok := cleanupTargetByID(targetID); ok {
		// The helper must apply the age floor THIS user configured, not
		// whatever settings.json the elevated account happens to have.
		secs := int64(def.effectiveMinAge(currentSettings()).Seconds())
		req.MinAgeSeconds = &secs
	}
	b, err := json.Marshal(req)
	if err != nil {
		return cleanupTargetResult{}, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		err = fmt.Errorf("send cleanup request: %w", err)
		m.markConnLost(err)
		return cleanupTargetResult{}, err
	}

	var wire cleanupTargetResultWire
	gotResult := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			err = fmt.Errorf("helper connection lost: %w", err)
			m.markConnLost(err)
			return cleanupTargetResult{}, err
		}
		var resp helperResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		switch resp.Type {
		case "result":
			if err := json.Unmarshal([]byte(resp.Data), &wire); err != nil {
				return cleanupTargetResult{}, fmt.Errorf("parse helper result: %w", err)
			}
			gotResult = true
		case "done":
			if !gotResult {
				return cleanupTargetResult{}, fmt.Errorf("helper finished without sending a result")
			}
			return cleanupResultFromWire(wire), nil
		case "error":
			return cleanupTargetResult{}, fmt.Errorf("%s", resp.Data)
		}
	}
}

// markConnLost drops the helper connection. A non-nil cause — the helper
// failed, not the user cancelling — also counts as a failed start for the
// rest of the batch: re-prompting for every remaining item is what the
// failure memo exists to prevent.
func (m *elevationManager) markConnLost(cause error) {
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.conn = nil
	m.reader = nil
	if cause != nil {
		m.startErr = fmt.Errorf("the elevated helper stopped responding (%w)", cause)
	}
	m.mu.Unlock()
}

// runCommandElevated streams one winget command through the elevated helper.
// The caller's ctx propagates: cancelling it closes the pipe, which makes the
// helper cancel its in-flight winget and exit (the next elevated action
// restarts it with a fresh UAC prompt — the price of a real cancel).
func (m *elevationManager) runCommandElevated(ctx context.Context, args ...string) (<-chan string, <-chan error, error) {
	return m.runCommandElevatedIf(ctx, nil, args...)
}

// errHelperOtherAccount refuses a helper route because elevation runs as
// another account than the user's, and the action must stay with the user.
var errHelperOtherAccount = errors.New("elevation runs as a different account than yours")

// runCommandElevatedIf is runCommandElevated for actions that may only use
// the helper when allowed(separate) holds, where separate reports whether
// elevation runs as another account. It is checked before the prompt, so a
// refused action never prompts, and again once the helper has reported its
// account. A nil allowed admits every action.
func (m *elevationManager) runCommandElevatedIf(ctx context.Context, allowed func(separate bool) bool, args ...string) (<-chan string, <-chan error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if allowed != nil && !allowed(m.separateAccount()) {
		return nil, nil, errHelperOtherAccount
	}
	if err := m.ensureHelper(ctx); err != nil {
		return nil, nil, err
	}
	if allowed != nil && !allowed(m.separateAccount()) {
		return nil, nil, errHelperOtherAccount
	}

	outChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		defer close(errChan)

		// One exchange at a time: the pipe protocol has no request framing,
		// so hold reqMu from the request write through the terminal response.
		m.reqMu.Lock()
		defer m.reqMu.Unlock()

		m.mu.Lock()
		conn, reader, token := m.conn, m.reader, m.token
		m.mu.Unlock()
		if conn == nil {
			errChan <- fmt.Errorf("helper connection lost")
			return
		}

		// Closing the conn is the only way to unblock the pipe read below.
		stopCancelWatch := context.AfterFunc(ctx, func() { conn.Close() })
		defer stopCancelWatch()

		fail := func(err error) {
			if ctx.Err() != nil {
				m.markConnLost(nil) // the next action may restart the helper
				errChan <- fmt.Errorf("cancelled")
				return
			}
			m.markConnLost(err)
			errChan <- err
		}

		req := helperRequest{
			Action: "winget",
			Args:   args,
			NonInt: false,
			Token:  token,
		}
		b, _ := json.Marshal(req)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			fail(fmt.Errorf("send winget request: %w", err))
			return
		}

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				fail(fmt.Errorf("helper connection lost: %w", err))
				return
			}

			var resp helperResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}

			switch resp.Type {
			case "line":
				outChan <- resp.Data
			case "done":
				errChan <- nil
				return
			case "error":
				errChan <- m.helperWingetError(resp.Data)
				return
			}
		}
	}()

	return outChan, errChan, nil
}

// helperWingetError is the error for a failed winget run in the helper. A
// helper running as another account cannot see the user's per-user installs,
// so winget's "no installed package found" there gets that explanation (the
// 2026-10-26 VM pass: an elevated uninstall of a user-scope package exits
// 0x8A150014 under Administrator protection, not 0x8A15007D).
func (m *elevationManager) helperWingetError(msg string) error {
	if m.separateAccount() && containsCodeToken(strings.ToLower(msg), "0x8a150014") {
		return fmt.Errorf("%s (elevation on this PC runs as a different account, which cannot see packages installed only for you; run it without elevation)", msg)
	}
	return fmt.Errorf("%s", msg)
}
