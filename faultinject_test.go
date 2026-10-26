//go:build faultinject

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run with: go test -tags faultinject -run TestFault .

func withHelperFault(t *testing.T, name string) {
	t.Helper()
	orig := helperFaultName
	t.Cleanup(func() { helperFaultName = orig })
	helperFaultName = name
}

func TestFaultIdentityFail(t *testing.T) {
	withHelperFault(t, "identity-fail")
	resps := driveHelperRequest(t, helperRequest{Action: "identity", Token: "tok"})
	if last := resps[len(resps)-1]; last.Type != "error" || !strings.Contains(last.Data, "identity-fail") {
		t.Fatalf("responses = %+v, want the injected identity error", resps)
	}
}

func TestFaultDropAfterConnectEndsTheConnection(t *testing.T) {
	withHelperFault(t, "drop-after-connect")
	server, client := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- handleHelperConnection(server, "tok") }()
	if _, err := client.Write([]byte(`{"action":"winget","args":["upgrade","--id","A.B"],"token":"tok"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the helper kept the connection open")
	}
	_ = client.Close()
}

func TestFaultArgsAndLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "helper.log")
	t.Setenv("WINTUI_FAULT", "no-connect")
	t.Setenv("WINTUI_FAULT_LOG", logPath)
	if got := strings.Join(helperFaultArgs(), " "); got != "--fault no-connect --fault-log "+logPath {
		t.Fatalf("helperFaultArgs = %q", got)
	}

	origLog := helperFaultLogPath
	t.Cleanup(func() { helperFaultLogPath = origLog })
	helperFaultLogPath = logPath
	withHelperFault(t, "no-connect")
	if !helperFault("no-connect") {
		t.Fatal("helperFault(no-connect) = false")
	}
	b, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(b), "injecting fault no-connect") {
		t.Fatalf("fault log = %q, %v", b, err)
	}
}
