package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIContract(t *testing.T) {
	// Build the wintui binary for testing
	exePath := filepath.Join(t.TempDir(), "wintui-test.exe")
	buildCmd := exec.Command("go", "build", "-o", exePath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build test binary: %v\nOutput: %s", err, out)
	}

	// Create a dummy winget executable to mock winget behavior
	dummyWingetDir := filepath.Join(t.TempDir(), "bin")
	os.MkdirAll(dummyWingetDir, 0755)
	dummyWingetSrc := filepath.Join(t.TempDir(), "mock_winget.go")
	dummyWinget := filepath.Join(dummyWingetDir, "winget.exe")

	// Helper to run wintui with the dummy winget in PATH
	runWintui := func(wingetOutput string, args ...string) (string, int) {
		mockSrc := `package main
	import (
	"fmt"
	)
	func main() {
	fmt.Print(` + "`" + wingetOutput + "`" + `)
	}
	`
		err := os.WriteFile(dummyWingetSrc, []byte(mockSrc), 0644)
		if err != nil {
			t.Fatal(err)
		}
		buildMock := exec.Command("go", "build", "-o", dummyWinget, dummyWingetSrc)
		if out, err := buildMock.CombinedOutput(); err != nil {
			t.Fatalf("Failed to build mock winget: %v\nOutput: %s", err, out)
		}

		cmd := exec.Command(exePath, args...)
		cmd.Env = append(os.Environ(), "PATH="+dummyWingetDir+";"+os.Getenv("PATH"))
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		err = cmd.Run()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("Unexpected error running wintui: %v", err)
			}
		}
		return out.String(), exitCode
	}

	t.Run("list", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0               winget\n"
		out, code := runWintui(output, "--list")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, "Name") || !strings.Contains(out, "Version") {
			t.Errorf("Expected header row, got: %q", out)
		}
		if !strings.Contains(out, "Test App1") {
			t.Errorf("Expected output to contain 'Test App1', got: %q", out)
		}
		if !strings.Contains(out, "1 package(s) installed.") {
			t.Errorf("Expected install summary, got: %q", out)
		}
	})

	t.Run("check_updates_exist", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0     2.0       winget\n"
		out, code := runWintui(output, "--check")
		if code != 1 {
			t.Errorf("Expected exit code 1 when updates exist, got %d", code)
		}
		if !strings.Contains(out, "Available") {
			t.Errorf("Expected human-readable table header, got: %q", out)
		}
		if !strings.Contains(out, "App1") {
			t.Errorf("Expected output to contain 'App1', got: %q", out)
		}
		if !strings.Contains(out, "1 package(s) have updates available.") {
			t.Errorf("Expected update summary, got: %q", out)
		}
	})

	t.Run("check_no_updates", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n"
		out, code := runWintui(output, "--check")
		if code != 0 {
			t.Errorf("Expected exit code 0 when no updates exist, got %d", code)
		}
		if !strings.Contains(out, "All packages are up to date.") {
			t.Errorf("Expected output 'All packages are up to date.', got: %q", out)
		}
	})

	t.Run("json_output", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0     2.0       winget\n"
		out, code := runWintui(output, "--check", "--json")
		if code != 1 {
			t.Errorf("Expected exit code 1, got %d", code)
		}
		if !strings.Contains(out, `"id": "App1.ID"`) {
			t.Errorf("Expected JSON output, got: %q", out)
		}
	})
}
