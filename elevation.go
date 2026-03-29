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
	ln     net.Listener
	conn   net.Conn
	pipeID string
}

var globalElevator = &elevationManager{}

func (m *elevationManager) ensureHelper() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn != nil {
		return nil
	}

	m.pipeID = "wintui-" + uuid.New().String()
	ln, err := startElevatedHelper(context.Background(), m.pipeID)
	if err != nil {
		return err
	}
	m.ln = ln

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
	case <-time.After(60 * time.Second): // Long timeout for UAC
		m.ln.Close()
		m.ln = nil
		return fmt.Errorf("timeout waiting for elevated helper (UAC cancelled?)")
	}
}

func (m *elevationManager) runCommandElevated(args ...string) (<-chan string, <-chan error) {
	outChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		defer close(errChan)

		if err := m.ensureHelper(); err != nil {
			errChan <- err
			return
		}

		m.mu.Lock()
		conn := m.conn
		m.mu.Unlock()

		req := helperRequest{
			Action: "winget",
			Args:   args,
			NonInt: true,
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

	return outChan, errChan
}
