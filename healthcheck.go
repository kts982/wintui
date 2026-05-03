package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

// ── Health check data ──────────────────────────────────────────────

type healthReport struct {
	Checks []healthCheck `json:"checks"`
}

type healthCheck struct {
	Check          string `json:"check"`
	Status         string `json:"status"` // PASS, WARN, FAIL, INFO
	Details        string `json:"details"`
	Recommendation string `json:"recommendation,omitempty"`
}

// ── Run all checks ─────────────────────────────────────────────────

// runHealthcheck is the slim TUI Health-tab default — readiness only,
// all rows read from cached data or cheap syscalls.
func runHealthcheck() (healthReport, error) {
	return runDoctorReport(false, false), nil
}

// runDoctorReport is the shared engine behind both the TUI Health tab and
// `wintui doctor`. The slim 7-row default always runs; full=true re-adds
// the verbose system-diagnostics rows (RAM, Defender, internet ping, extra
// drives, OS/uptime, PATH, Windows PowerShell). devTools=true appends the
// developer-tools detection group.
func runDoctorReport(full, devTools bool) healthReport {
	checks := []healthCheck{
		checkWinTUI(),
		checkWingetTool(),
		checkSources(),
		checkUpdates(),
		checkPrivileges(),
		checkSystemDrive(),
		checkSettingsSummary(),
	}
	if full {
		checks = append(checks, checkHostInfo(), checkUptime(), checkRAM())
		checks = append(checks, checkExtraDrives()...)
		checks = append(checks, checkDefender(), checkInternet(), checkPathLength(), checkWindowsPowerShell())
	}
	if devTools {
		checks = append(checks, checkDevTools()...)
	}
	return healthReport{Checks: checks}
}

// ── Check implementations ─────────────────────────────────────────

func checkWinTUI() healthCheck {
	exePath, _ := currentExecutablePath()
	mode := "dev build"
	if exePath != "" && pathLooksLikeInstalledWinTUI(exePath) {
		mode = "installed"
	}
	configState := "config writeable"
	if !isConfigDirWriteable() {
		configState = "config NOT writeable"
	}
	versionLabel := "v" + version
	if version == "dev" {
		versionLabel = "dev"
	}
	return healthCheck{
		Check:   "WinTUI",
		Status:  "PASS",
		Details: fmt.Sprintf("%s · %s · %s", versionLabel, mode, configState),
	}
}

func checkWingetTool() healthCheck {
	path, err := exec.LookPath("winget")
	if err != nil {
		return healthCheck{
			Check:          "winget",
			Status:         "FAIL",
			Details:        "Not found",
			Recommendation: "Install App Installer from Microsoft Store.",
		}
	}
	ver := strings.TrimSpace(cmdOutput(path, "--version"))
	if i := strings.IndexByte(ver, '\n'); i > 0 {
		ver = strings.TrimSpace(ver[:i])
	}
	if ver == "" {
		ver = "detected"
	}
	return healthCheck{Check: "winget", Status: "PASS", Details: ver}
}

func checkSources() healthCheck {
	source := appSettings.Source
	if source == "" {
		source = "all"
	}
	upgAt := cache.getUpgradeableSavedAt()
	if upgAt.IsZero() {
		return healthCheck{
			Check:   "Sources",
			Status:  "INFO",
			Details: fmt.Sprintf("%s · no cached scan yet", source),
		}
	}
	return healthCheck{
		Check:   "Sources",
		Status:  "PASS",
		Details: fmt.Sprintf("%s · last winget scan %s ago", source, humanDuration(time.Since(upgAt))),
	}
}

func checkUpdates() healthCheck {
	upgradeable := cache.getUpgradeableRaw()
	upgAt := cache.getUpgradeableSavedAt()
	if upgradeable == nil || upgAt.IsZero() {
		return healthCheck{
			Check:   "Updates",
			Status:  "INFO",
			Details: "no cached upgrade scan yet",
		}
	}
	plan := planUpgrades(upgradeable, appSettings)
	return healthCheck{
		Check:  "Updates",
		Status: "PASS",
		Details: fmt.Sprintf("%d visible · %d auto · %d held · cached %s ago",
			len(plan.Visible), len(plan.Auto), len(plan.Held), humanDuration(time.Since(upgAt))),
	}
}

func checkPrivileges() healthCheck {
	role := "Standard User"
	if isElevated() {
		role = "Administrator"
	}
	autoElev := "off"
	if appSettings.AutoElevate {
		autoElev = "on"
	}
	return healthCheck{
		Check:   "Privileges",
		Status:  "PASS",
		Details: fmt.Sprintf("%s · Auto Elevate %s", role, autoElev),
	}
}

func checkSystemDrive() healthCheck {
	drive := strings.TrimSuffix(os.Getenv("SystemDrive"), `\`)
	if drive == "" {
		drive = "C:"
	}
	return checkDiskSpace(drive)
}

func checkSettingsSummary() healthCheck {
	source := appSettings.Source
	if source == "" {
		source = "all"
	}
	mode := string(appSettings.InstallMode)
	if mode == "" {
		mode = "default"
	}
	return healthCheck{
		Check:  "Settings",
		Status: "INFO",
		Details: fmt.Sprintf("Auto Elevate: %s · Self Update: %s · Action Mode: %s · Source: %s",
			onOff(appSettings.AutoElevate), onOff(appSettings.AutoSelfUpdate), mode, source),
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// isConfigDirWriteable probes the WinTUI config directory by writing and
// removing a temp file. Used by the WinTUI readiness row.
func isConfigDirWriteable() bool {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return false
	}
	dir := filepath.Join(configDir, "wintui")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	probe, err := os.CreateTemp(dir, ".wintui-write-probe-*")
	if err != nil {
		return false
	}
	probe.Close()
	os.Remove(probe.Name())
	return true
}

// ── System drive disk-space check ─────────────────────────────────

func checkDiskSpace(drive string) healthCheck {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	root, _ := syscall.UTF16PtrFromString(drive + `\`)
	var freeBytesAvail, totalBytes, totalFreeBytes uint64
	r1, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&freeBytesAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 || totalBytes == 0 {
		return healthCheck{Check: "Storage", Status: "WARN", Details: drive + " · could not read disk info"}
	}
	freeGB := float64(freeBytesAvail) / (1024 * 1024 * 1024)
	totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
	pctFree := (float64(freeBytesAvail) / float64(totalBytes)) * 100

	status := "PASS"
	rec := ""
	if pctFree < 5 {
		status = "FAIL"
		rec = fmt.Sprintf("Critical: only %.1f%% free on %s.", pctFree, drive)
	} else if pctFree < 15 {
		status = "WARN"
		rec = fmt.Sprintf("Low disk space on %s.", drive)
	}
	return healthCheck{
		Check:          "Storage",
		Status:         status,
		Details:        fmt.Sprintf("%s · %.0f GB free / %.0f GB (%.0f%%)", drive, freeGB, totalGB, pctFree),
		Recommendation: rec,
	}
}

// ── Utility ────────────────────────────────────────────────────────

func cmdOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	return out.String()
}

func cmdOutputTrim(name string, args ...string) string {
	return strings.TrimSpace(cmdOutput(name, args...))
}

// ── Doctor --full extras (not on the slim TUI Health tab) ─────────

func checkHostInfo() healthCheck {
	hostname, _ := os.Hostname()
	osLabel := cmdOutputTrim("cmd", "/c", "ver")
	if osLabel == "" {
		osLabel = "Windows"
	}
	return healthCheck{
		Check:   "Host",
		Status:  "INFO",
		Details: fmt.Sprintf("%s · %s", hostname, osLabel),
	}
}

func checkUptime() healthCheck {
	boot := cmdOutputTrim("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToString('yyyyMMddHHmmss')")
	if len(boot) < 14 {
		return healthCheck{Check: "Uptime", Status: "INFO", Details: "unknown"}
	}
	t, err := time.Parse("20060102150405", boot[:14])
	if err != nil {
		return healthCheck{Check: "Uptime", Status: "INFO", Details: "unknown"}
	}
	return healthCheck{
		Check:   "Uptime",
		Status:  "INFO",
		Details: time.Since(t).Truncate(time.Minute).String(),
	}
}

func checkRAM() healthCheck {
	type memoryStatusEx struct {
		Length               uint32
		MemoryLoad           uint32
		TotalPhys            uint64
		AvailPhys            uint64
		TotalPageFile        uint64
		AvailPageFile        uint64
		TotalVirtual         uint64
		AvailVirtual         uint64
		AvailExtendedVirtual uint64
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")

	var mem memoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))
	r1, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if r1 == 0 || mem.TotalPhys == 0 {
		return healthCheck{Check: "RAM", Status: "WARN", Details: "Could not determine"}
	}
	totalGB := float64(mem.TotalPhys) / (1024 * 1024 * 1024)
	freeGB := float64(mem.AvailPhys) / (1024 * 1024 * 1024)
	pctFree := (freeGB / totalGB) * 100
	status := "PASS"
	rec := ""
	if pctFree < 10 {
		status = "WARN"
		rec = "Low available memory. Close unused applications."
	}
	return healthCheck{
		Check:          "RAM",
		Status:         status,
		Details:        fmt.Sprintf("%.1f GB free / %.1f GB (%.0f%%)", freeGB, totalGB, pctFree),
		Recommendation: rec,
	}
}

// checkExtraDrives returns one disk row per fixed drive, excluding the system
// drive (which is always part of the slim default).
func checkExtraDrives() []healthCheck {
	systemDrive := strings.TrimSuffix(os.Getenv("SystemDrive"), `\`)
	if systemDrive == "" {
		systemDrive = "C:"
	}
	systemDrive = strings.ToUpper(systemDrive)
	var out []healthCheck
	for _, d := range getFixedDrives() {
		if strings.EqualFold(d, systemDrive) {
			continue
		}
		row := checkDiskSpace(d)
		row.Check = "Drive " + d
		out = append(out, row)
	}
	return out
}

func checkDefender() healthCheck {
	out := cmdOutputTrim("powershell", "-NoProfile", "-Command",
		"(Get-MpComputerStatus).RealTimeProtectionEnabled")
	switch strings.ToLower(strings.TrimSpace(out)) {
	case "true":
		return healthCheck{Check: "Defender", Status: "PASS", Details: "Real-time protection enabled"}
	case "false":
		return healthCheck{
			Check: "Defender", Status: "WARN", Details: "Real-time protection disabled",
			Recommendation: "Consider enabling Windows Defender.",
		}
	default:
		return healthCheck{Check: "Defender", Status: "WARN", Details: "Could not determine status"}
	}
}

func checkInternet() healthCheck {
	cmd := exec.Command("ping", "-n", "1", "-w", "3000", "8.8.8.8")
	if err := cmd.Run(); err != nil {
		return healthCheck{
			Check:          "Internet",
			Status:         "WARN",
			Details:        "No connectivity",
			Recommendation: "Check your network connection. winget requires internet access.",
		}
	}
	return healthCheck{Check: "Internet", Status: "PASS", Details: "Connected"}
}

func checkPathLength() healthCheck {
	pathVal := os.Getenv("PATH")
	length := len(pathVal)
	entries := len(strings.Split(pathVal, ";"))
	status := "PASS"
	rec := ""
	if length > 7000 {
		status = "WARN"
		rec = "PATH is very long. Consider cleaning up."
	}
	return healthCheck{
		Check:          "PATH",
		Status:         status,
		Details:        fmt.Sprintf("%d chars, %d entries", length, entries),
		Recommendation: rec,
	}
}

func checkWindowsPowerShell() healthCheck {
	return toolCheckInfo("powershell", "Windows PowerShell",
		"-NoProfile", "-Command", "$PSVersionTable.PSVersion.ToString()")
}

func checkDevTools() []healthCheck {
	return []healthCheck{
		toolCheck("git", "Git", false, "winget install Git.Git", "--version"),
		toolCheck("code", "VS Code", false, "winget install Microsoft.VisualStudioCode", "--version"),
		toolCheck("docker", "Docker", false, "winget install Docker.DockerDesktop", "--version"),
		toolCheck("ssh", "OpenSSH", false, "Enable OpenSSH via Windows Optional Features.", "-V"),
		toolCheck("curl", "curl", false, "", "--version"),
		toolCheck("node", "Node.js", false, "winget install OpenJS.NodeJS.LTS", "--version"),
		toolCheck("python", "Python", false, "winget install Python.Python.3.13", "--version"),
		toolCheck("go", "Go", false, "winget install GoLang.Go", "version"),
		toolCheck("rustc", "Rust", false, "Install rustup: https://rustup.rs", "--version"),
		toolCheck("java", "Java", false, "winget install Oracle.JDK.24", "--version"),
		toolCheck("dotnet", "dotnet", false, "winget install Microsoft.DotNet.SDK.9", "--version"),
		toolCheck("npm", "npm", false, "Comes with Node.js.", "--version"),
		toolCheck("pwsh", "PowerShell 7+", false, "winget install Microsoft.PowerShell", "--version"),
	}
}

func toolCheck(cmd, label string, required bool, recommendation string, versionArgs ...string) healthCheck {
	path, err := exec.LookPath(cmd)
	if err != nil {
		status := "WARN"
		if required {
			status = "FAIL"
		}
		return healthCheck{
			Check: label, Status: status, Details: "Not found",
			Recommendation: recommendation,
		}
	}
	ver := strings.TrimSpace(cmdOutput(path, versionArgs...))
	if i := strings.IndexByte(ver, '\n'); i > 0 {
		ver = strings.TrimSpace(ver[:i])
	}
	if ver == "" {
		ver = "detected"
	}
	lower := strings.ToLower(ver)
	if strings.Contains(lower, "could not") || strings.Contains(lower, "error") ||
		strings.Contains(lower, "not recognized") || strings.Contains(lower, "is not") {
		return healthCheck{
			Check: label, Status: "WARN", Details: "Found but not working properly",
			Recommendation: recommendation,
		}
	}
	return healthCheck{Check: label, Status: "PASS", Details: ver}
}

func toolCheckInfo(cmd, label string, versionArgs ...string) healthCheck {
	path, err := exec.LookPath(cmd)
	if err != nil {
		return healthCheck{Check: label, Status: "WARN", Details: "Not found"}
	}
	ver := strings.TrimSpace(cmdOutput(path, versionArgs...))
	if ver == "" {
		ver = "detected"
	}
	return healthCheck{Check: label, Status: "PASS", Details: ver}
}

func getFixedDrives() []string {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrives := kernel32.NewProc("GetLogicalDrives")
	getDriveType := kernel32.NewProc("GetDriveTypeW")

	mask, _, _ := getLogicalDrives.Call()
	var drives []string
	for i := range 26 {
		if mask&(1<<uint(i)) != 0 {
			letter := string(rune('A' + i))
			root, _ := syscall.UTF16PtrFromString(letter + `:\`)
			dt, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(root)))
			if dt == 3 {
				drives = append(drives, letter+":")
			}
		}
	}
	return drives
}

// ── Healthcheck screen ─────────────────────────────────────────────

type healthcheckState int

const (
	hcLoading healthcheckState = iota
	hcReady
	hcError
)

type healthcheckScreen struct {
	state   healthcheckState
	report  healthReport
	spinner spinner.Model
	scroll  int
	err     error
	width   int
	height  int
}

type healthcheckDoneMsg struct {
	report healthReport
	err    error
}

func newHealthcheckScreen() healthcheckScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)
	return healthcheckScreen{state: hcLoading, spinner: sp, width: 80, height: 24}
}

func (s healthcheckScreen) init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, func() tea.Msg {
		report, err := runHealthcheck()
		return healthcheckDoneMsg{report: report, err: err}
	})
}

func (s healthcheckScreen) reload() (healthcheckScreen, tea.Cmd) {
	s.state = hcLoading
	s.err = nil
	s.scroll = 0
	return s, tea.Batch(s.spinner.Tick, func() tea.Msg {
		report, err := runHealthcheck()
		return healthcheckDoneMsg{report: report, err: err}
	})
}

func (s healthcheckScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		s = s.clampScroll()
		return s, nil

	case tea.KeyPressMsg:
		switch s.state {
		case hcReady:
			switch msg.String() {
			case "up", "k":
				if s.scroll > 0 {
					s.scroll--
				}
			case "down", "j":
				s.scroll++
				maxScr := max(s.contentLineCount()-max(s.height-6, 8), 0)
				s.scroll = min(s.scroll, maxScr)
			case "pgup":
				s.scroll -= 8
				if s.scroll < 0 {
					s.scroll = 0
				}
			case "pgdown":
				s.scroll += 8
				maxScr := max(s.contentLineCount()-max(s.height-6, 8), 0)
				s.scroll = min(s.scroll, maxScr)
			case "r":
				return s.reload()
			case "esc":
				if s.scroll > 0 {
					s.scroll = 0
				}
			}
		case hcError:
			if msg.String() == "r" {
				return s.reload()
			}
		}

	case healthcheckDoneMsg:
		if msg.err != nil {
			s.err = msg.err
			s.state = hcError
		} else {
			s.report = msg.report
			s.state = hcReady
			s = s.clampScroll()
		}
		return s, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	}
	return s, nil
}

// ── View ──────────────────────────────────────────────────────────

func (s healthcheckScreen) view(width, height int) string {
	switch s.state {
	case hcLoading:
		return fmt.Sprintf("  %s Running readiness checks...\n", s.spinner.View())
	case hcError:
		return "  " + errorStyle.Render("Error: "+s.err.Error()) + "\n\n  " +
			helpStyle.Render("Press r to retry.") + "\n"
	case hcReady:
		return s.viewReady(width, height)
	}
	return ""
}

func (s healthcheckScreen) viewReady(width, height int) string {
	panelWidth := width - 4
	innerW := max(panelWidth-2, 10)

	var rowLines []string
	for _, c := range s.report.Checks {
		rowLines = append(rowLines, renderCheckLine(c, innerW))
	}
	panel := renderTitledPanel("WinTUI Readiness", strings.Join(rowLines, "\n"), panelWidth, len(rowLines), accent)
	allLines := strings.Split(panel, "\n")

	var recs []string
	for _, c := range s.report.Checks {
		if c.Status != "PASS" && c.Status != "INFO" && c.Recommendation != "" {
			recs = appendUnique(recs, c.Recommendation)
		}
	}
	if len(recs) > 0 {
		allLines = append(allLines, "")
		allLines = append(allLines, "  "+lipgloss.NewStyle().Bold(true).Foreground(warning).Render("Recommendations"))
		for _, rec := range recs {
			allLines = append(allLines, "  "+helpStyle.Render("• "+rec))
		}
	}

	maxVisible := max(height-2, 8)
	totalLines := len(allLines)
	maxScr := max(totalLines-maxVisible, 0)
	if s.scroll > maxScr {
		s.scroll = maxScr
	}
	start := s.scroll
	end := min(start+maxVisible, totalLines)

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(allLines[i] + "\n")
	}
	return b.String()
}

func (s healthcheckScreen) contentLineCount() int {
	if s.state != hcReady {
		return 0
	}
	return len(s.report.Checks) + 8
}

func (s healthcheckScreen) clampScroll() healthcheckScreen {
	if s.scroll < 0 {
		s.scroll = 0
	}
	return s
}

func (s healthcheckScreen) helpKeys() []key.Binding {
	switch s.state {
	case hcLoading:
		return nil
	case hcError:
		return []key.Binding{keyRefresh}
	case hcReady:
		return []key.Binding{keyScroll, keyRefresh}
	}
	return nil
}

func renderCheckLine(c healthCheck, width int) string {
	status := statusStyle(c.Status).Render(fmt.Sprintf("%-4s", c.Status))
	name := lipgloss.NewStyle().Bold(true).Width(12).Render(c.Check)
	maxDetail := max(width-26, 20)
	detail := helpStyle.Render(truncate(c.Details, maxDetail))
	return fmt.Sprintf("  %s  %s  %s", status, name, detail)
}

func statusStyle(status string) lipgloss.Style {
	switch strings.ToUpper(status) {
	case "PASS":
		return lipgloss.NewStyle().Foreground(success).Bold(true)
	case "WARN":
		return lipgloss.NewStyle().Foreground(warning).Bold(true)
	case "FAIL":
		return lipgloss.NewStyle().Foreground(danger).Bold(true)
	case "INFO":
		return lipgloss.NewStyle().Foreground(dim).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(dim)
	}
}

func truncate(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func appendUnique(slice []string, val string) []string {
	if slices.Contains(slice, val) {
		return slice
	}
	return append(slice, val)
}
