package main

import (
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// inlineScriptFixture is one of the three rendered inline PowerShell scripts
// exactly as production launches it (window style + rendered script), so
// every transport test below exercises the same bytes from one table.
type inlineScriptFixture struct {
	name        string
	windowStyle string
	script      string
}

func (f inlineScriptFixture) args() []string {
	return inlinePowerShellHostArgs(f.windowStyle, f.script)
}

// inlineScriptFixtures renders the three production scripts with awkward
// input (quotes, $, &, <>, apostrophes, non-ASCII) under a temp cache dir.
func inlineScriptFixtures(t *testing.T) []inlineScriptFixture {
	t.Helper()
	origSettings := appSettings
	origCacheDir := userCacheDirPath
	dir := t.TempDir()
	appSettings = DefaultSettings()
	userCacheDirPath = func() (string, error) { return dir, nil }
	t.Cleanup(func() {
		appSettings = origSettings
		userCacheDirPath = origCacheDir
	})
	return []inlineScriptFixture{
		{
			name:        "toast-send",
			windowStyle: toastWindowStyle,
			script: renderToastScript(
				`WinTUI "quoted" $dollar Δ`,
				`Package <name> completed & it's ready`,
			),
		},
		{
			name:        "shortcut-ensure",
			windowStyle: toastWindowStyle,
			script: renderShortcutScript(
				`C:\Users\O'Brien\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\WinTUI.lnk`,
				`C:\Users\O'Brien\AppData\Local\Microsoft\WinGet\Links\wintui.exe`,
				false,
			),
		},
		{
			name:        "self-update-handoff",
			windowStyle: selfUpdateWindowStyle,
			script: renderSelfUpdateScript(
				42,
				`C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.0.0.0_x64__8wekyb3d8bbwe\winget.exe`,
				selfUpgradeCommandArgs("winget", "2.11.2"),
			),
		},
	}
}

func TestRenderedInlinePowerShellCommandsStayBelowWindowsLimit(t *testing.T) {
	for _, f := range inlineScriptFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			args := f.args()
			units := windowsCommandLineUTF16Units(powershellExePath(), args)
			if units > windowsCommandLineLimitUTF16 {
				t.Fatalf("command line = %d UTF-16 units, limit = %d", units, windowsCommandLineLimitUTF16)
			}
			if err := validatePowerShellCommandLine(powershellExePath(), args); err != nil {
				t.Fatalf("validatePowerShellCommandLine(): %v", err)
			}
			if strings.Contains(args[len(args)-1], "$PSCommandPath") {
				t.Fatal("inline renderer still references $PSCommandPath")
			}
		})
	}
}

// TestInlinePowerShellArgvSurvivesWindowsCommandLineParser is the real-argv
// transport test: the command line os/exec hands to CreateProcess (the
// executable plus syscall.EscapeArg per argument, space-joined) is parsed
// back by CommandLineToArgvW — the same parser the PowerShell host uses — and
// the rendered script must come out as exactly ONE final argument after
// -Command. The whole inline transport rests on that property.
func TestInlinePowerShellArgvSurvivesWindowsCommandLineParser(t *testing.T) {
	for _, f := range inlineScriptFixtures(t) {
		t.Run(f.name, func(t *testing.T) {
			exe := powershellExePath()
			args := f.args()
			parts := []string{syscall.EscapeArg(exe)}
			for _, a := range args {
				parts = append(parts, syscall.EscapeArg(a))
			}
			cmdLine, err := syscall.UTF16PtrFromString(strings.Join(parts, " "))
			if err != nil {
				t.Fatal(err)
			}
			var argc int32
			argv, err := syscall.CommandLineToArgv(cmdLine, &argc)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = syscall.LocalFree(syscall.Handle(unsafe.Pointer(argv))) }()

			got := make([]string, 0, argc)
			for i := 0; i < int(argc); i++ {
				got = append(got, syscall.UTF16ToString((*argv[i])[:]))
			}
			want := append([]string{exe}, args...)
			if !slices.Equal(got, want) {
				t.Fatalf("argv did not survive the Windows parser:\n got %q\nwant %q", got, want)
			}
			if got[len(got)-2] != "-Command" || got[len(got)-1] != f.script {
				t.Fatalf("script must be the single final argument after -Command; got tail %q", got[len(got)-2:])
			}
		})
	}
}

// TestNewInlinePowerShellCmdBuildsValidatedAbsoluteHost: the one constructor
// every launch site uses resolves the absolute Windows PowerShell 5.1 host,
// passes the host args verbatim, and refuses an oversized script.
func TestNewInlinePowerShellCmdBuildsValidatedAbsoluteHost(t *testing.T) {
	cmd, err := newInlinePowerShellCmd(toastWindowStyle, "Write-Host hi")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != powershellExePath() {
		t.Errorf("Path = %q, want the absolute host %q", cmd.Path, powershellExePath())
	}
	want := append([]string{powershellExePath()}, inlinePowerShellHostArgs(toastWindowStyle, "Write-Host hi")...)
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("Args = %q, want %q", cmd.Args, want)
	}
	if cmd, err := newInlinePowerShellCmd(toastWindowStyle, strings.Repeat("a", windowsCommandLineLimitUTF16)); err == nil || cmd != nil {
		t.Fatal("oversized script must be rejected before a command is built")
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
