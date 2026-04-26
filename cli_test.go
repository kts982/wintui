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

	t.Run("check_and_list_are_mutually_exclusive", func(t *testing.T) {
		// Cobra's MarkFlagsMutuallyExclusive should reject this combination
		// before any winget call happens.
		out, code := runWintui("", "--check", "--list")
		if code == 0 {
			t.Errorf("Expected non-zero exit when --check and --list are combined, got %d", code)
		}
		if !strings.Contains(out, "[check list]") {
			t.Errorf("Expected mutual-exclusion error mentioning [check list], got: %q", out)
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

	t.Run("check_subcommand_exits_one_when_updates_exist", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0     2.0       winget\n"
		out, code := runWintui(output, "check")
		if code != 1 {
			t.Errorf("Expected exit code 1, got %d", code)
		}
		if !strings.Contains(out, "App1") {
			t.Errorf("Expected output to contain 'App1', got: %q", out)
		}
	})

	t.Run("check_subcommand_exits_zero_when_up_to_date", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n"
		out, code := runWintui(output, "check")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, "All packages are up to date.") {
			t.Errorf("Expected up-to-date message, got: %q", out)
		}
	})

	t.Run("list_subcommand", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0               winget\n"
		out, code := runWintui(output, "list")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, "Test App1") || !strings.Contains(out, "1 package(s) installed.") {
			t.Errorf("Expected list output, got: %q", out)
		}
	})

	t.Run("check_subcommand_json", func(t *testing.T) {
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0     2.0       winget\n"
		out, code := runWintui(output, "check", "--json")
		if code != 1 {
			t.Errorf("Expected exit code 1, got %d", code)
		}
		if !strings.Contains(out, `"id": "App1.ID"`) {
			t.Errorf("Expected JSON output, got: %q", out)
		}
	})

	t.Run("show_subcommand_human_output", func(t *testing.T) {
		// show is read-only: doesn't actually call winget. Mock output is
		// irrelevant.
		out, code := runWintui("", "show", "Mozilla.Firefox")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, "ID:     Mozilla.Firefox") {
			t.Errorf("Expected ID line, got: %q", out)
		}
		if !strings.Contains(out, "Effective install command:") {
			t.Errorf("Expected install command section, got: %q", out)
		}
		if !strings.Contains(out, "Effective upgrade command:") {
			t.Errorf("Expected upgrade command section, got: %q", out)
		}
	})

	t.Run("show_subcommand_json", func(t *testing.T) {
		out, code := runWintui("", "show", "Mozilla.Firefox", "--json")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, `"id": "Mozilla.Firefox"`) {
			t.Errorf("Expected JSON id, got: %q", out)
		}
		if !strings.Contains(out, `"install_args"`) {
			t.Errorf("Expected install_args field, got: %q", out)
		}
		if !strings.Contains(out, `"upgrade_args"`) {
			t.Errorf("Expected upgrade_args field, got: %q", out)
		}
	})

	t.Run("show_subcommand_requires_id", func(t *testing.T) {
		out, code := runWintui("", "show")
		if code == 0 {
			t.Errorf("Expected non-zero exit when id is missing, got %d", code)
		}
		if !strings.Contains(out, "accepts 1 arg") && !strings.Contains(out, "Error") {
			t.Errorf("Expected arg-count error, got: %q", out)
		}
	})

	t.Run("show_subcommand_msstore_source", func(t *testing.T) {
		out, code := runWintui("", "show", "9NCBCSZSJRSB", "--source", "msstore", "--json")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, `"source": "msstore"`) {
			t.Errorf("Expected msstore source in JSON, got: %q", out)
		}
		if !strings.Contains(out, `"--source",`) || !strings.Contains(out, `"msstore"`) {
			t.Errorf("Expected --source msstore in args, got: %q", out)
		}
	})

	t.Run("upgrade_requires_all_flag", func(t *testing.T) {
		// Without --all there is no action; cobra should surface the hint
		// without ever calling winget.
		out, code := runWintui("", "upgrade")
		if code == 0 {
			t.Errorf("Expected non-zero exit when --all is missing, got %d", code)
		}
		if !strings.Contains(out, "--all") {
			t.Errorf("Expected --all hint in error, got: %q", out)
		}
	})

	t.Run("upgrade_all_with_no_updates", func(t *testing.T) {
		// Empty winget upgrade list → nothing to do, exit 0.
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n"
		out, code := runWintui(output, "upgrade", "--all")
		if code != 0 {
			t.Errorf("Expected exit code 0, got %d", code)
		}
		if !strings.Contains(out, "up to date") {
			t.Errorf("Expected up-to-date message, got: %q", out)
		}
	})

	t.Run("deprecated_check_flag_still_works", func(t *testing.T) {
		// Backwards compat: --check at the root must keep working for one
		// minor release, with a deprecation warning on stderr.
		output := "Name       Id         Version Available Source\n" +
			"----------------------------------------------\n" +
			"Test App1  App1.ID    1.0     2.0       winget\n"
		out, code := runWintui(output, "--check")
		if code != 1 {
			t.Errorf("Expected exit code 1, got %d", code)
		}
		if !strings.Contains(out, "deprecated") {
			t.Errorf("Expected deprecation warning, got: %q", out)
		}
	})
}
