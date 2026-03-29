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
