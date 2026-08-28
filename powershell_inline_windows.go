package main

import (
	"fmt"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Windows CreateProcess limits the command line to 32,767 UTF-16 code units,
// including the terminating NUL. Inline PowerShell scripts are intentionally
// kept well below this ceiling; validating it here makes future template growth
// fail clearly instead of turning into a silent toast or handoff failure.
const windowsCommandLineLimitUTF16 = 32767

func inlinePowerShellHostArgs(windowStyle, script string) []string {
	return []string{
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", windowStyle,
		"-Command", script,
	}
}

func validatePowerShellCommandLine(executable string, args []string) error {
	units := windowsCommandLineUTF16Units(executable, args)
	if units > windowsCommandLineLimitUTF16 {
		return fmt.Errorf("inline PowerShell command is too long: %d UTF-16 units including the terminating NUL (limit %d)", units, windowsCommandLineLimitUTF16)
	}
	return nil
}

// windowsCommandLineUTF16Units mirrors the quoting os/exec uses for ordinary
// Windows executables closely enough to enforce CreateProcess's hard limit.
// The executable is argv[0], each argument is separated by one space, and the
// returned count includes the terminating NUL.
func windowsCommandLineUTF16Units(executable string, args []string) int {
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, executable)
	argv = append(argv, args...)

	var commandLine strings.Builder
	for i, arg := range argv {
		if i > 0 {
			commandLine.WriteByte(' ')
		}
		commandLine.WriteString(syscall.EscapeArg(arg))
	}
	return len(utf16.Encode([]rune(commandLine.String()))) + 1
}
