package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/spf13/cobra"
)

var pipeName string

type helperRequest struct {
	Action  string   `json:"action"`
	Args    []string `json:"args"`
	NonInt  bool     `json:"non_interactive"`
}

type helperResponse struct {
	Type string `json:"type"` // "line", "done", "error"
	Data string `json:"data"`
}

var helperCmd = &cobra.Command{
	Use:    "helper",
	Short:  "Internal elevated helper",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if pipeName == "" {
			return fmt.Errorf("pipe name required")
		}

		if !isElevated() {
			return fmt.Errorf("helper must be run elevated")
		}

		timeout := 10 * time.Second
		conn, err := winio.DialPipe(`\\.\pipe\`+pipeName, &timeout)
		if err != nil {
			return err
		}
		defer conn.Close()

		return handleHelperConnection(conn)
	},
}

func init() {
	helperCmd.Flags().StringVar(&pipeName, "pipe", "", "Named pipe for communication")
	rootCmd.AddCommand(helperCmd)
}

func handleHelperConnection(conn net.Conn) error {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var req helperRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		// Execute the winget command
		err = executeWingetForHelper(conn, req)
		
		// Send final status
		if err != nil {
			sendHelperResponse(conn, "error", err.Error())
		} else {
			sendHelperResponse(conn, "done", "")
		}
	}
}

func executeWingetForHelper(conn net.Conn, req helperRequest) error {
	ctx := context.Background()
	outChan, errChan := runWingetStreamCtx(ctx, req.NonInt, req.Args...)

	for line := range outChan {
		sendHelperResponse(conn, "line", line)
	}

	return <-errChan
}

func sendHelperResponse(w io.Writer, typ, data string) {
	resp := helperResponse{Type: typ, Data: data}
	b, _ := json.Marshal(resp)
	w.Write(b)
	w.Write([]byte("\n"))
}

// ── TUI side of the pipe ───────────────────────────────────────────

func startElevatedHelper(ctx context.Context, pipeID string) (net.Listener, error) {
	pipePath := `\\.\pipe\` + pipeID
	// SDDL: Allow Authenticated Users (AU) Generic All (GA) access.
	// This allows the non-elevated TUI to connect to the elevated listener.
	config := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;AU)",
	}
	ln, err := winio.ListenPipe(pipePath, config)
	if err != nil {
		return nil, err
	}

	// Launch ourselves elevated
	exe, _ := os.Executable()
	args := []string{"helper", "--pipe", pipeID}
	
	err = relaunchElevatedWithArgs(exe, args)
	if err != nil {
		ln.Close()
		return nil, err
	}

	return ln, nil
}

func relaunchElevatedWithArgs(exe string, args []string) error {
	// Re-using ShellExecute logic but for the helper
	return relaunchAsAdmin(exe, args)
}
