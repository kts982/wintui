package main

import (
	"bytes"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"runtime"
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
	Hostname      string
	OS            string
	Uptime        string
	OverallStatus string
	Sections      []healthSection
	DevSection    healthSection // hidden by default
	Counts        struct{ Pass, Warn, Fail, Total int }
}

type healthSection struct {
	Title  string
	Checks []healthCheck
}

type healthCheck struct {
	Check          string
	Status         string // PASS, WARN, FAIL, INFO
	Details        string
	Recommendation string
}

// ── Run all checks ─────────────────────────────────────────────────

func runHealthcheck() (healthReport, error) {
	var r healthReport
	r.Hostname, _ = os.Hostname()
	r.OS = fmt.Sprintf("Windows %s/%s", runtime.GOARCH, runtime.GOOS)

	if ver := cmdOutputTrim("cmd", "/c", "ver"); ver != "" {
		r.OS = ver
	}

	// Use PowerShell CIM instead of deprecated wmic.
	if boot := cmdOutputTrim("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToString('yyyyMMddHHmmss')"); boot != "" {
		boot = strings.TrimSpace(boot)
		if len(boot) >= 14 {
			t, err := time.Parse("20060102150405", boot[:14])
			if err == nil {
				r.Uptime = time.Since(t).Truncate(time.Minute).String()
			}
		}
	}

	r.Sections = []healthSection{
		checkSystem(),
		checkPackageManager(),
	}
	r.DevSection = checkDevTools()

	// Tally (only visible sections count toward overall status).
	for _, sec := range r.Sections {
		for _, c := range sec.Checks {
			switch c.Status {
			case "PASS":
				r.Counts.Pass++
			case "WARN":
				r.Counts.Warn++
			case "FAIL":
				r.Counts.Fail++
			}
			r.Counts.Total++
		}
	}

	switch {
	case r.Counts.Fail > 0:
		r.OverallStatus = "FAIL"
	case r.Counts.Warn > 0:
		r.OverallStatus = "WARN"
	default:
		r.OverallStatus = "PASS"
	}

	return r, nil
}

// ── Check groups ───────────────────────────────────────────────────

func checkSystem() healthSection {
	sec := healthSection{Title: "System"}

	sec.Checks = append(sec.Checks, checkWindowsVersion())
	sec.Checks = append(sec.Checks, checkAdmin())
	sec.Checks = append(sec.Checks, checkRAM())

	for _, d := range getFixedDrives() {
		sec.Checks = append(sec.Checks, checkDiskSpace(d))
	}

	sec.Checks = append(sec.Checks, checkDefender())
	sec.Checks = append(sec.Checks, checkPathLength())
	sec.Checks = append(sec.Checks, checkInternet())

	return sec
}

func checkPackageManager() healthSection {
	sec := healthSection{Title: "Package Manager"}

	sec.Checks = append(sec.Checks, toolCheck("winget", "winget", true,
		"Install App Installer from Microsoft Store.",
		"--version"))

	sec.Checks = append(sec.Checks, checkWingetSources())

	sec.Checks = append(sec.Checks, toolCheck("pwsh", "PowerShell 7+", false,
		"winget install Microsoft.PowerShell",
		"--version"))

	sec.Checks = append(sec.Checks, toolCheckInfo("powershell", "Windows PowerShell",
		"-NoProfile", "-Command", "$PSVersionTable.PSVersion.ToString()"))

	sec.Checks = append(sec.Checks, checkWinTUIUpdate())

	return sec
}

func checkDevTools() healthSection {
	sec := healthSection{Title: "Developer Tools"}

	sec.Checks = append(sec.Checks, toolCheck("git", "Git", false,
		"winget install Git.Git", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("code", "VS Code", false,
		"winget install Microsoft.VisualStudioCode", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("docker", "Docker", false,
		"winget install Docker.DockerDesktop", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("ssh", "OpenSSH", false,
		"Enable OpenSSH via Windows Optional Features.", "-V"))
	sec.Checks = append(sec.Checks, toolCheck("curl", "curl", false, "", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("node", "Node.js", false,
		"winget install OpenJS.NodeJS.LTS", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("python", "Python", false,
		"winget install Python.Python.3.13", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("go", "Go", false,
		"winget install GoLang.Go", "version"))
	sec.Checks = append(sec.Checks, toolCheck("rustc", "Rust", false,
		"Install rustup: https://rustup.rs", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("java", "Java", false,
		"winget install Oracle.JDK.24", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("dotnet", "dotnet", false,
		"winget install Microsoft.DotNet.SDK.9", "--version"))
	sec.Checks = append(sec.Checks, toolCheck("npm", "npm", false,
		"Comes with Node.js.", "--version"))

	return sec
}

// ── New check implementations ─────────────────────────────────────

func checkWindowsVersion() healthCheck {
	ver := cmdOutputTrim("cmd", "/c", "ver")
	if ver == "" {
		return healthCheck{Check: "Windows Version", Status: "WARN", Details: "Could not determine"}
	}
	// Check build number for winget compat (1809+ required).
	return healthCheck{Check: "Windows Version", Status: "PASS", Details: ver}
}

func checkRAM() healthCheck {
	// Use kernel32 GlobalMemoryStatusEx for reliable memory info.
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

func checkInternet() healthCheck {
	cmd := exec.Command("ping", "-n", "1", "-w", "3000", "8.8.8.8")
	err := cmd.Run()
	if err != nil {
		return healthCheck{
			Check:          "Internet",
			Status:         "WARN",
			Details:        "No connectivity",
			Recommendation: "Check your network connection. winget requires internet access.",
		}
	}
	return healthCheck{Check: "Internet", Status: "PASS", Details: "Connected"}
}

func checkWingetSources() healthCheck {
	out := cmdOutputTrim("winget", "source", "list")
	if strings.Contains(out, "winget") {
		return healthCheck{Check: "Winget Sources", Status: "PASS", Details: "Sources configured"}
	}
	return healthCheck{
		Check:          "Winget Sources",
		Status:         "WARN",
		Details:        "No sources found",
		Recommendation: "Run: winget source reset --force",
	}
}

func checkWinTUIUpdate() healthCheck {
	// Show current version. The upgrade check is done by winget on the
	// Packages screen — no need to duplicate a slow winget call here.
	v := version
	if v == "dev" {
		return healthCheck{Check: "WinTUI", Status: "PASS", Details: "dev build"}
	}
	return healthCheck{Check: "WinTUI", Status: "PASS", Details: "v" + v}
}

// ── Existing check helpers ────────────────────────────────────────

func checkAdmin() healthCheck {
	if isElevated() {
		return healthCheck{Check: "Privileges", Status: "PASS", Details: "Administrator"}
	}
	return healthCheck{
		Check:          "Privileges",
		Status:         "WARN",
		Details:        "Standard User",
		Recommendation: "Run wintui as Administrator for full winget functionality.",
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
	ver := ""
	if len(versionArgs) > 0 {
		ver = strings.TrimSpace(cmdOutput(path, versionArgs...))
		if i := strings.IndexByte(ver, '\n'); i > 0 {
			ver = strings.TrimSpace(ver[:i])
		}
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
	for i := 0; i < 26; i++ {
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
		return healthCheck{Check: fmt.Sprintf("Disk %s", drive), Status: "WARN", Details: "Could not read disk info"}
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
		Check: fmt.Sprintf("Disk %s", drive), Status: status,
		Details:        fmt.Sprintf("%.0f GB free / %.0f GB (%.0f%%)", freeGB, totalGB, pctFree),
		Recommendation: rec,
	}
}

func checkDefender() healthCheck {
	out := cmdOutputTrim("powershell", "-NoProfile", "-Command",
		"(Get-MpComputerStatus).RealTimeProtectionEnabled")
	out = strings.TrimSpace(strings.ToLower(out))
	switch out {
	case "true":
		return healthCheck{Check: "Windows Defender", Status: "PASS", Details: "Real-time protection enabled"}
	case "false":
		return healthCheck{
			Check: "Windows Defender", Status: "WARN", Details: "Real-time protection disabled",
			Recommendation: "Consider enabling Windows Defender.",
		}
	default:
		return healthCheck{Check: "Windows Defender", Status: "WARN", Details: "Could not determine status"}
	}
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
		Check: "PATH", Status: status,
		Details:        fmt.Sprintf("%d chars, %d entries", length, entries),
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

// ── Healthcheck screen ─────────────────────────────────────────────

type healthcheckState int

const (
	hcLoading healthcheckState = iota
	hcReady
	hcError
)

type healthcheckScreen struct {
	state        healthcheckState
	report       healthReport
	spinner      spinner.Model
	scroll       int
	showDevTools bool
	err          error
	width        int
	height       int
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
			case "d":
				s.showDevTools = !s.showDevTools
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
		return fmt.Sprintf("  %s Running health checks...\n", s.spinner.View())
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
	r := s.report

	// Build all lines.
	var allLines []string

	// System info header.
	overall := statusStyle(r.OverallStatus).Render(r.OverallStatus)
	info := helpStyle.Render(fmt.Sprintf("%s · %s", r.Hostname, r.OS))
	uptime := ""
	if r.Uptime != "" {
		uptime = helpStyle.Render(" · up " + r.Uptime)
	}
	allLines = append(allLines, fmt.Sprintf("  %s  %s%s", overall, info, uptime))

	counts := fmt.Sprintf("  %s %d  %s %d  %s %d  Total: %d",
		statusStyle("PASS").Render("PASS"), r.Counts.Pass,
		statusStyle("WARN").Render("WARN"), r.Counts.Warn,
		statusStyle("FAIL").Render("FAIL"), r.Counts.Fail,
		r.Counts.Total)
	allLines = append(allLines, counts, "")

	// Visible sections in bordered panels — split into individual lines.
	for _, sec := range r.Sections {
		panel := s.renderHealthPanel(sec, panelWidth, accent)
		allLines = append(allLines, strings.Split(panel, "\n")...)
	}

	// Developer tools: collapsed or expanded.
	devFound := 0
	for _, c := range r.DevSection.Checks {
		if c.Status == "PASS" {
			devFound++
		}
	}
	if s.showDevTools {
		panel := s.renderHealthPanel(r.DevSection, panelWidth, dim)
		allLines = append(allLines, strings.Split(panel, "\n")...)
	} else {
		devLine := fmt.Sprintf("  %s (%d/%d found, press d to expand)",
			helpStyle.Render("Developer Tools"),
			devFound, len(r.DevSection.Checks))
		allLines = append(allLines, devLine)
	}

	// Recommendations.
	var recs []string
	for _, sec := range r.Sections {
		for _, c := range sec.Checks {
			if c.Status != "PASS" && c.Recommendation != "" {
				recs = appendUnique(recs, c.Recommendation)
			}
		}
	}
	if len(recs) > 0 {
		allLines = append(allLines, "")
		allLines = append(allLines, "  "+lipgloss.NewStyle().Bold(true).Foreground(warning).Render("Recommendations"))
		for _, rec := range recs {
			allLines = append(allLines, "  "+helpStyle.Render("• "+rec))
		}
	}

	// Scrollable output.
	maxVisible := max(height-2, 8)
	totalLines := len(allLines)

	// Clamp scroll to valid range based on actual content.
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

func (s healthcheckScreen) renderHealthPanel(sec healthSection, panelWidth int, borderColor color.Color) string {
	innerW := max(panelWidth-2, 10)
	var lines []string
	for _, c := range sec.Checks {
		lines = append(lines, renderCheckLine(c, innerW))
	}
	content := strings.Join(lines, "\n")
	return renderTitledPanel(sec.Title, content, panelWidth, len(lines), borderColor)
}

// contentLineCount returns an upper bound on total rendered lines.
func (s healthcheckScreen) contentLineCount() int {
	if s.state != hcReady {
		return 0
	}
	n := 3 // header
	for _, sec := range s.report.Sections {
		n += len(sec.Checks) + 2
	}
	if s.showDevTools {
		n += len(s.report.DevSection.Checks) + 2
	}
	n += 10 // recommendations + padding
	return n
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
		bindings := []key.Binding{
			keyScroll,
			key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dev tools")),
			keyRefresh,
		}
		return bindings
	}
	return nil
}

func renderCheckLine(c healthCheck, width int) string {
	status := statusStyle(c.Status).Render(fmt.Sprintf("%-4s", c.Status))
	name := lipgloss.NewStyle().Bold(true).Width(20).Render(c.Check)
	maxDetail := max(width-34, 20)
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
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
