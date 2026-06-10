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

var (
	pipeName    string
	helperToken string
)

type helperRequest struct {
	Action string   `json:"action"`
	Args   []string `json:"args,omitempty"`
	NonInt bool     `json:"non_interactive,omitempty"`
	// Token must equal the per-session token the TUI generated when it
	// launched this helper (passed on the elevated relaunch as --token).
	// It binds every request to the session that spawned the helper.
	Token string `json:"token,omitempty"`
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
		if helperToken == "" {
			return fmt.Errorf("session token required")
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

		return handleHelperConnection(conn, helperToken)
	},
}

func init() {
	helperCmd.Flags().StringVar(&pipeName, "pipe", "", "Named pipe for communication")
	helperCmd.Flags().StringVar(&helperToken, "token", "", "Per-session request token")
	rootCmd.AddCommand(helperCmd)
}

func handleHelperConnection(conn net.Conn, token string) error {
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
		switch {
		case token == "" || req.Token != token:
			execErr = fmt.Errorf("request rejected: missing or invalid session token")
		case req.Action == "cleanup_delete":
			execErr = executeCleanupDeleteForHelper(conn, req)
		case req.Action == "winget":
			execErr = executeWingetForHelper(conn, req)
		default:
			// The helper is a long-lived elevated service: it accepts only
			// the specific operations WinTUI itself issues and rejects
			// everything else rather than forwarding arbitrary argv.
			execErr = fmt.Errorf("unknown helper action %q", req.Action)
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

// The helper is a long-lived elevated process, so it must not be a generic
// "run winget elevated with any options" oracle. Rather than forward the
// TUI's argv after a loose check, it validates the exact flag shapes the arg
// builders emit (installCommandArgs / upgradeCommandArgs /
// uninstallCommandArgs in winget.go) and rejects everything else. A bare
// verb+denylist would still pass through escalation vectors like --override
// and --custom (arbitrary args to an elevated installer) or --location; a
// strict allowlist is the least-privilege contract. If the helper ever needs
// to accept genuinely new shapes, prefer a typed request over loosening this.

// helperWingetVerbs is the set of mutating verbs WinTUI sends through
// runCommandElevated.
var helperWingetVerbs = map[string]bool{
	"install":   true,
	"upgrade":   true,
	"uninstall": true,
}

// helperWingetValueFlags are flags the arg builders emit that consume the
// following argument as their value (the value is opaque — winget parses it
// positionally, so it can never be reinterpreted as a flag).
var helperWingetValueFlags = map[string]bool{
	"--id":           true,
	"--version":      true,
	"--scope":        true,
	"--architecture": true,
	"--source":       true,
	"--name":         true,
	"--product-code": true,
}

// helperWingetBoolFlags are the standalone flags the arg builders emit.
var helperWingetBoolFlags = map[string]bool{
	"--exact":                     true,
	"--accept-package-agreements": true,
	"--accept-source-agreements":  true,
	"--disable-interactivity":     true,
	"--silent":                    true,
	"--interactive":               true,
	"--force":                     true,
	"--allow-reboot":              true,
	"--skip-dependencies":         true,
	"--all-versions":              true,
	"--purge":                     true,
}

// validateHelperWingetArgs enforces the helper's winget contract: a known
// verb, then a sequence of allowlisted flags (value flags consuming exactly
// one opaque value). Any unrecognized token — including --manifest, -m,
// --override, --custom, --location, or a stray positional — is rejected.
func validateHelperWingetArgs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("winget action requires arguments")
	}
	if !helperWingetVerbs[args[0]] {
		return fmt.Errorf("winget verb %q is not allowed by the elevated helper", args[0])
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case helperWingetBoolFlags[arg]:
			// standalone flag — nothing to consume
		case helperWingetValueFlags[arg]:
			if i+1 >= len(args) {
				return fmt.Errorf("winget flag %q is missing its value", arg)
			}
			i++ // skip the opaque value
		default:
			return fmt.Errorf("winget argument %q is not allowed by the elevated helper", arg)
		}
	}
	return nil
}

// helperWingetStream is the seam tests replace to record the args that pass
// validation without spawning a real winget.
var helperWingetStream = runWingetStreamCtx

func executeWingetForHelper(conn net.Conn, req helperRequest) error {
	if err := validateHelperWingetArgs(req.Args); err != nil {
		return err
	}

	// The TUI signals cancellation by closing the pipe. A failed line write
	// is therefore our cue to kill the in-flight winget instead of letting
	// it run to completion against a dead connection.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	outChan, errChan := helperWingetStream(ctx, req.NonInt, req.Args...)

	for line := range outChan {
		if err := sendHelperResponse(conn, "line", line); err != nil {
			cancel()
			for range outChan {
			}
			break
		}
	}

	return <-errChan
}

func sendHelperResponse(w io.Writer, typ, data string) error {
	resp := helperResponse{Type: typ, Data: data}
	b, _ := json.Marshal(resp)
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, err := w.Write([]byte("\n"))
	return err
}

// ── TUI side of the pipe ───────────────────────────────────────────

func startElevatedHelper(ctx context.Context, pipeID, token string) (net.Listener, error) {
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
	args := []string{"helper", "--pipe", pipeID, "--token", token}

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
