package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
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
}

var globalElevator = &elevationManager{}
var (
	startElevatedHelperFunc = startElevatedHelper
	helperAcceptTimeout     = 60 * time.Second
)

func (m *elevationManager) ensureHelper() error {
	m.initMu.Lock()
	defer m.initMu.Unlock()

	m.mu.Lock()
	if m.conn != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	pipeID := "wintui-" + uuid.New().String()
	token := uuid.New().String()
	ln, err := startElevatedHelperFunc(context.Background(), pipeID, token)
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
	errChan := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errChan <- err
			return
		}
		m.mu.Lock()
		m.conn = conn
		m.reader = bufio.NewReader(conn)
		m.mu.Unlock()
		errChan <- nil
	}()

	select {
	case err := <-errChan:
		return err
	case <-time.After(helperAcceptTimeout):
		m.mu.Lock()
		if m.ln != nil {
			m.ln.Close()
			m.ln = nil
		}
		m.mu.Unlock()
		return fmt.Errorf("timeout waiting for elevated helper (UAC cancelled?)")
	}
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
	if err := m.ensureHelper(); err != nil {
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
	b, err := json.Marshal(req)
	if err != nil {
		return cleanupTargetResult{}, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		m.markConnLost()
		return cleanupTargetResult{}, fmt.Errorf("send cleanup request: %w", err)
	}

	var wire cleanupTargetResultWire
	gotResult := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			m.markConnLost()
			return cleanupTargetResult{}, fmt.Errorf("helper connection lost: %w", err)
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

func (m *elevationManager) markConnLost() {
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.conn = nil
	m.reader = nil
	m.mu.Unlock()
}

// runCommandElevated streams one winget command through the elevated helper.
// The caller's ctx propagates: cancelling it closes the pipe, which makes the
// helper cancel its in-flight winget and exit (the next elevated action
// restarts it with a fresh UAC prompt — the price of a real cancel).
func (m *elevationManager) runCommandElevated(ctx context.Context, args ...string) (<-chan string, <-chan error, error) {
	if err := m.ensureHelper(); err != nil {
		return nil, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
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
			m.markConnLost() // reset so the next action can restart the helper
			if ctx.Err() != nil {
				errChan <- fmt.Errorf("cancelled")
				return
			}
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
				errChan <- fmt.Errorf("%s", resp.Data)
				return
			}
		}
	}()

	return outChan, errChan, nil
}
