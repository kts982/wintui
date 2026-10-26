//go:build faultinject

package main

import (
	"fmt"
	"os"
	"time"
)

// Fault injection for the elevated helper, in test builds only
// (`go build -tags faultinject`), so release builds and their Win32 surface
// stay unchanged. It reproduces helper failures that cannot be caused from
// outside — killing the elevated helper needs an elevated, racy taskkill.
//
// The unelevated TUI reads two environment variables and passes them to the
// helper it launches as flags, because an elevated process does not reliably
// inherit the caller's environment (under Administrator protection it runs as
// another account):
//
//	WINTUI_FAULT      no-connect          the helper exits without connecting
//	                  identity-fail       it fails the identity handshake
//	                  drop-after-connect  it drops the connection on its first
//	                                      winget request
//	WINTUI_FAULT_LOG  a file both accounts can write (e.g. under C:\testdev):
//	                  the helper appends timestamped startup steps to it, so a
//	                  test can tell what happened before the connect.

var (
	helperFaultName    string
	helperFaultLogPath string
)

func init() {
	helperCmd.Flags().StringVar(&helperFaultName, "fault", "", "Fault to inject (faultinject builds only)")
	helperCmd.Flags().StringVar(&helperFaultLogPath, "fault-log", "", "File to log helper startup steps to (faultinject builds only)")
}

// helperFaultArgs are the extra helper flags the TUI passes on launch.
func helperFaultArgs() []string {
	var args []string
	if v := os.Getenv("WINTUI_FAULT"); v != "" {
		args = append(args, "--fault", v)
	}
	if v := os.Getenv("WINTUI_FAULT_LOG"); v != "" {
		args = append(args, "--fault-log", v)
	}
	return args
}

// helperFault reports whether the helper was told to inject fault name.
func helperFault(name string) bool {
	if helperFaultName != name {
		return false
	}
	helperFaultLog("injecting fault %s", name)
	return true
}

// helperFaultLog appends one timestamped line to the fault log, if any.
func helperFaultLog(format string, args ...any) {
	if helperFaultLogPath == "" {
		return
	}
	f, err := os.OpenFile(helperFaultLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s pid=%d %s\n", time.Now().Format(time.RFC3339Nano), os.Getpid(), fmt.Sprintf(format, args...))
}
