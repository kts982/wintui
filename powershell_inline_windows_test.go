package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRenderedInlinePowerShellCommandsStayBelowWindowsLimit(t *testing.T) {
	origSettings := appSettings
	origCacheDir := userCacheDirPath
	dir := t.TempDir()
	appSettings = DefaultSettings()
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() {
		appSettings = origSettings
		userCacheDirPath = origCacheDir
	})

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "toast-send",
			args: toastPowerShellHostArgs(renderToastScript(
				`WinTUI "quoted" $dollar Δ`,
				`Package <name> completed & it's ready`,
			)),
		},
		{
			name: "shortcut-ensure",
			args: toastPowerShellHostArgs(renderShortcutScript(
				`C:\Users\O'Brien\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\WinTUI.lnk`,
				`C:\Users\O'Brien\AppData\Local\Microsoft\WinGet\Links\wintui.exe`,
				false,
			)),
		},
		{
			name: "self-update-handoff",
			args: powerShellHostArgs(renderSelfUpdateScript(
				42,
				`C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.0.0.0_x64__8wekyb3d8bbwe\winget.exe`,
				selfUpgradeCommandArgs("winget", "2.11.2"),
			)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units := windowsCommandLineUTF16Units(powershellExePath(), tt.args)
			if units > windowsCommandLineLimitUTF16 {
				t.Fatalf("command line = %d UTF-16 units, limit = %d", units, windowsCommandLineLimitUTF16)
			}
			if err := validatePowerShellCommandLine(powershellExePath(), tt.args); err != nil {
				t.Fatalf("validatePowerShellCommandLine(): %v", err)
			}
			if strings.Contains(tt.args[len(tt.args)-1], "$PSCommandPath") {
				t.Fatal("inline renderer still references $PSCommandPath")
			}
		})
	}
}

func TestValidatePowerShellCommandLineRejectsOversizeScript(t *testing.T) {
	args := inlinePowerShellHostArgs("Hidden", strings.Repeat("界", windowsCommandLineLimitUTF16))
	if err := validatePowerShellCommandLine(powershellExePath(), args); err == nil {
		t.Fatal("validatePowerShellCommandLine() accepted an oversized script")
	}
}

// CreateProcess accepts a command line of exactly 32,767 UTF-16 units
// including the terminating NUL; only one unit more must be rejected.
func TestValidatePowerShellCommandLineBoundary(t *testing.T) {
	exe := powershellExePath()
	// A script of only ASCII letters is passed through syscall.EscapeArg
	// unquoted, so every additional letter costs exactly one UTF-16 unit.
	base := windowsCommandLineUTF16Units(exe, inlinePowerShellHostArgs("Hidden", "a"))
	script := strings.Repeat("a", 1+windowsCommandLineLimitUTF16-base)

	args := inlinePowerShellHostArgs("Hidden", script)
	if units := windowsCommandLineUTF16Units(exe, args); units != windowsCommandLineLimitUTF16 {
		t.Fatalf("constructed command line = %d UTF-16 units, want exactly %d", units, windowsCommandLineLimitUTF16)
	}
	if err := validatePowerShellCommandLine(exe, args); err != nil {
		t.Fatalf("validatePowerShellCommandLine() rejected the exact CreateProcess limit: %v", err)
	}

	over := inlinePowerShellHostArgs("Hidden", script+"a")
	if err := validatePowerShellCommandLine(exe, over); err == nil {
		t.Fatal("validatePowerShellCommandLine() accepted one unit over the limit")
	}
}

func TestInlinePowerShellCommandExecutesQuotedUnicodeScript(t *testing.T) {
	powershellPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not available")
	}

	want := "quotes \" ' $ ` and Unicode Δ"
	script := "$value = " + quotePowerShellLiteral(want) + "\n" +
		"[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)\n" +
		"[Console]::Out.Write($value)"
	args := inlinePowerShellHostArgs("Hidden", script)
	cmd := exec.Command(powershellPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inline PowerShell command failed: %v\n%s", err, output)
	}
	if got := string(output); got != want {
		t.Fatalf("inline PowerShell output = %q, want %q", got, want)
	}
}
