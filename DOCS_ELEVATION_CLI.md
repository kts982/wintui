# WinTUI CLI & Elevation Strategy

This document describes the high-level approach for implementing a headless CLI mode and a robust, single-prompt elevation strategy for WinTUI.

## 1. Architectural Approach

### Headless CLI (Issue #17)
WinTUI will transition from a TUI-only application to a multi-mode CLI using the **Cobra** library.
- **Root Command:** Defaults to launching the interactive TUI if no subcommands are provided.
- **Subcommands:** Implements `list`, `check`, `install`, `upgrade`, and `uninstall` for headless operation.
- **Output:** Supports a `--json` flag for machine-readable output, suitable for scripts and CI/CD.

### Elevated Helper (Issue #21)
To avoid multiple UAC prompts and UI state loss, we implement an "Elevated Companion Process" model.
- **Trigger:** When the TUI detects a need for admin rights (e.g., a 0x8a150056 error from winget), it launches a second instance of itself with the `runas` verb.
- **Helper Mode:** The second instance runs in a special hidden mode: `wintui helper --pipe <pipe_name>`.
- **IPC:** Communication between the non-elevated TUI and the elevated helper is handled via **Windows Named Pipes** using `go-winio`.
- **Persistence:** The helper process remains active to handle subsequent elevated commands in a batch, requiring only one UAC prompt per session.

## 2. Libraries

### [spf13/cobra](https://github.com/spf13/cobra)
- **Role:** CLI Framework.
- **Reason:** Industry standard for Go CLIs; provides robust flag parsing, subcommand management, and automatic help generation.

### [github.com/Microsoft/go-winio](https://github.com/Microsoft/go-winio)
- **Role:** Windows Named Pipes & IPC.
- **Reason:** Provides a `net.Conn` compatible interface for Windows-specific IPC, supporting security descriptors (SDDL) necessary for cross-privilege communication.

## 3. Implementation Plan

1. **Phase 1: CLI Foundation**
   - Refactor `main.go` to use Cobra.
   - Implement `list` and `check` commands using existing `winget.go` logic.
   - Add unit tests for command routing.

2. **Phase 3: Elevation Bridge**
   - Implement the `helper` subcommand.
   - Setup bidirectional streaming over Named Pipes.
   - Update `execution.go` and `winget.go` to route commands through the helper when necessary.
