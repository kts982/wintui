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
	ln     net.Listener
	conn   net.Conn
	pipeID string
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
	ln, err := startElevatedHelperFunc(context.Background(), pipeID)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.pipeID = pipeID
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
	}
	if m.ln != nil {
		m.ln.Close()
		m.ln = nil
	}
}

// cleanupTargetElevated runs an admin-required cleanup target through the
// elevated helper. The TUI sends only the registry ID; the helper resolves
// and validates the path on its side. Caller must serialize against any
// outstanding runCommandElevated — both share m.conn without locking the
// connection across reads.
func (m *elevationManager) cleanupTargetElevated(targetID string) (cleanupTargetResult, error) {
	if err := m.ensureHelper(); err != nil {
		return cleanupTargetResult{}, err
	}

	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()

	req := helperRequest{Action: "cleanup_delete", TargetID: targetID}
	b, err := json.Marshal(req)
	if err != nil {
		return cleanupTargetResult{}, err
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		m.markConnLost()
		return cleanupTargetResult{}, fmt.Errorf("send cleanup request: %w", err)
	}

	reader := bufio.NewReader(conn)
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
	m.conn = nil
	m.mu.Unlock()
}

func (m *elevationManager) runCommandElevated(args ...string) (<-chan string, <-chan error, error) {
	if err := m.ensureHelper(); err != nil {
		return nil, nil, err
	}

	outChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		defer close(errChan)

		m.mu.Lock()
		conn := m.conn
		m.mu.Unlock()

		req := helperRequest{
			Action: "winget",
			Args:   args,
			NonInt: false,
		}
		b, _ := json.Marshal(req)
		conn.Write(b)
		conn.Write([]byte("\n"))

		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errChan <- fmt.Errorf("helper connection lost: %w", err)
				m.mu.Lock()
				m.conn = nil // Reset so we can restart it
				m.mu.Unlock()
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
