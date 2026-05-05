package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/spf13/cobra"
)

var pipeName string

type helperRequest struct {
	Action string   `json:"action"`
	Args   []string `json:"args,omitempty"`
	NonInt bool     `json:"non_interactive,omitempty"`
	// TargetID identifies a registry entry for action="cleanup_delete".
	// The helper resolves the path from the registry on its side; the TUI
	// never sends raw filesystem paths to the privileged process.
	TargetID string `json:"target_id,omitempty"`
}

// helperResponse encodes one message on the pipe. Types:
//
//	line   - streaming output from a long-running action (winget)
//	done   - terminal success; data is empty for streaming actions
//	error  - terminal failure; data is the error message
//	result - structured payload that an action wants to return verbatim
//	         (currently used by cleanup_delete to ship a result struct).
//	         Always followed by a "done" or "error".
type helperResponse struct {
	Type string `json:"type"`
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

		// The helper is a hidden service — its winget children must not pop
		// their own console windows. This is always safe here because all
		// winget output is streamed back to the TUI over the named pipe.
		hideWingetChildWindow = true

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

		var execErr error
		switch req.Action {
		case "cleanup_delete":
			execErr = executeCleanupDeleteForHelper(conn, req)
		default:
			// Empty action falls through to winget for backward compat
			// with older requests on the wire.
			execErr = executeWingetForHelper(conn, req)
		}

		if execErr != nil {
			sendHelperResponse(conn, "error", execErr.Error())
		} else {
			sendHelperResponse(conn, "done", "")
		}
	}
}

// executeCleanupDeleteForHelper performs an admin-only cleanup target delete
// inside the elevated process. The helper resolves the target path itself
// from the registry — the TUI only sends an ID — and refuses targets the
// registry doesn't actually mark as requiring admin (defense-in-depth: a
// bug elsewhere can't route per-user paths through this elevated channel).
func executeCleanupDeleteForHelper(conn net.Conn, req helperRequest) error {
	if req.TargetID == "" {
		return fmt.Errorf("cleanup_delete requires target_id")
	}
	def, ok := cleanupTargetByID(req.TargetID)
	if !ok {
		return fmt.Errorf("unknown cleanup target id: %q", req.TargetID)
	}
	if !def.requiresAdmin {
		return fmt.Errorf("cleanup target %q does not require admin", req.TargetID)
	}

	res := cleanupDelete(context.Background(), def)
	wire := cleanupResultToWire(res)
	b, err := json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("encode cleanup result: %w", err)
	}
	sendHelperResponse(conn, "result", string(b))
	return nil
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

	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	// SDDL: Allow the current user's SID Generic All (GA) access.
	// This ensures only the user who spawned the TUI can connect to the pipe.
	sddl := fmt.Sprintf("D:P(A;;GA;;;%s)", u.Uid)

	config := &winio.PipeConfig{
		SecurityDescriptor: sddl,
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
	return relaunchAsAdminFunc(exe, args, swHide)
}
