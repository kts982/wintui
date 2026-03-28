package main

import (
	"bytes"
	"fmt"
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

	// Try to get proper OS version
	if ver := cmdOutputTrim("cmd", "/c", "ver"); ver != "" {
		r.OS = ver
	}

	// Uptime via wmic
	if boot := cmdOutputTrim("wmic", "os", "get", "LastBootUpTime"); boot != "" {
		lines := strings.Split(boot, "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if len(l) > 14 && l[0] >= '0' && l[0] <= '9' {
				// Parse WMI datetime: 20260323081500.000000+120
				t, err := time.Parse("20060102150405", l[:14])
				if err == nil {
					r.Uptime = time.Since(t).Truncate(time.Minute).String()
				}
			}
		}
	}

	// ── Sections ───────────────────────────────────────────────
	r.Sections = []healthSection{
		checkEssentials(),
		checkSystem(),
		checkDevTools(),
		checkLanguageRuntimes(),
	}

	// Tally
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

func checkEssentials() healthSection {
	sec := healthSection{Title: "Essentials"}

	sec.Checks = append(sec.Checks, checkAdmin())

	sec.Checks = append(sec.Checks, toolCheck("winget", "winget", false,
		"Install App Installer from Microsoft Store.",
		"--version"))

	sec.Checks = append(sec.Checks, toolCheck("pwsh", "PowerShell 7+", false,
		"winget install Microsoft.PowerShell",
		"--version"))

	sec.Checks = append(sec.Checks, toolCheckInfo("powershell", "Windows PowerShell",
		"-NoProfile", "-Command", "$PSVersionTable.PSVersion.ToString()"))

	return sec
}

func checkSystem() healthSection {
	sec := healthSection{Title: "System"}

	for _, d := range getFixedDrives() {
		sec.Checks = append(sec.Checks, checkDiskSpace(d))
	}

	sec.Checks = append(sec.Checks, checkDefender())
	sec.Checks = append(sec.Checks, checkPathLength())

	return sec
}

func checkDevTools() healthSection {
	sec := healthSection{Title: "Dev Tools"}

	sec.Checks = append(sec.Checks, toolCheck("git", "Git", false,
		"winget install Git.Git", "--version"))

	sec.Checks = append(sec.Checks, toolCheck("code", "VS Code", false,
		"winget install Microsoft.VisualStudioCode", "--version"))

	sec.Checks = append(sec.Checks, toolCheck("docker", "Docker", false,
		"winget install Docker.DockerDesktop", "--version"))

	sec.Checks = append(sec.Checks, toolCheck("ssh", "OpenSSH", false,
		"Enable OpenSSH via Windows Optional Features.", "-V"))

	sec.Checks = append(sec.Checks, toolCheck("curl", "curl", false, "",
		"--version"))

	sec.Checks = append(sec.Checks, toolCheck("npm", "npm", false,
		"Comes with Node.js.", "--version"))

	sec.Checks = append(sec.Checks, toolCheck("pnpm", "pnpm", false,
		"corepack enable && corepack prepare pnpm@latest --activate", "--version"))

	sec.Checks = append(sec.Checks, toolCheck("yarn", "Yarn", false,
		"corepack enable && corepack prepare yarn@stable --activate", "--version"))

	sec.Checks = append(sec.Checks, toolCheck("pip", "pip", false,
		"Comes with Python.", "--version"))

	return sec
}

func checkLanguageRuntimes() healthSection {
	sec := healthSection{Title: "Runtimes"}

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

	return sec
}

// ── Individual check helpers ───────────────────────────────────────

func checkAdmin() healthCheck {
	if isElevated() {
		return healthCheck{
			Check:   "Privileges",
			Status:  "PASS",
			Details: "Administrator",
		}
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
			Check:          label,
			Status:         status,
			Details:        "Not found",
			Recommendation: recommendation,
		}
	}

	ver := ""
	if len(versionArgs) > 0 {
		ver = strings.TrimSpace(cmdOutput(path, versionArgs...))
		// Take just first line
		if i := strings.IndexByte(ver, '\n'); i > 0 {
			ver = strings.TrimSpace(ver[:i])
		}
	}
	if ver == "" {
		ver = "detected"
	}

	// If the "version" output looks like an error, mark as WARN
	lower := strings.ToLower(ver)
	if strings.Contains(lower, "could not") || strings.Contains(lower, "error") ||
		strings.Contains(lower, "not recognized") || strings.Contains(lower, "is not") {
		return healthCheck{
			Check:          label,
			Status:         "WARN",
			Details:        "Found but not working properly",
			Recommendation: recommendation,
		}
	}

	return healthCheck{
		Check:   label,
		Status:  "PASS",
		Details: ver,
	}
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

// getFixedDrives returns drive letters (e.g. "C:", "D:") for fixed disks.
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
			if dt == 3 { // DRIVE_FIXED
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
		rec = fmt.Sprintf("Critical: only %.1f%% free on %s. Free up disk space.", pctFree, drive)
	} else if pctFree < 15 {
		status = "WARN"
		rec = fmt.Sprintf("Low disk space on %s. Consider cleaning up.", drive)
	}

	return healthCheck{
		Check:          fmt.Sprintf("Disk %s", drive),
		Status:         status,
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
			Recommendation: "Consider enabling Windows Defender real-time protection.",
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
		rec = "PATH is very long. Consider cleaning up unused entries to avoid issues."
	}

	return healthCheck{
		Check:          "PATH",
		Status:         status,
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

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
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
	return healthcheckScreen{
		state:   hcLoading,
		spinner: sp,
		width:   80,
		height:  24,
	}
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
				if s.scroll < s.maxScroll() {
					s.scroll++
				}
			case "pgup":
				s.scroll -= 8
				if s.scroll < 0 {
					s.scroll = 0
				}
			case "pgdown":
				s.scroll += 8
				if s.scroll > s.maxScroll() {
					s.scroll = s.maxScroll()
				}
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

func (s healthcheckScreen) reportLines() []string {
	var lines []string
	var recs []string
	for _, sec := range s.report.Sections {
		lines = append(lines, "section:"+sec.Title)
		for _, c := range sec.Checks {
			lines = append(lines, renderCheckLine(c, s.width))
			if c.Status != "PASS" && c.Recommendation != "" {
				recs = appendUnique(recs, c.Recommendation)
			}
		}
		lines = append(lines, "")
	}
	if len(recs) > 0 {
		lines = append(lines, "section:Recommendations")
		for _, rec := range recs {
			lines = append(lines, "  "+itemDescStyle.Render("• "+rec))
		}
		lines = append(lines, "")
	}
	return lines
}

func (s healthcheckScreen) maxVisibleLines() int {
	maxVisible := contentAreaHeightForWindow(s.width, s.height, true) - 10
	if maxVisible < 5 {
		return 5
	}
	return maxVisible
}

func (s healthcheckScreen) maxScroll() int {
	if s.state != hcReady {
		return 0
	}
	maxScroll := len(s.reportLines()) - s.maxVisibleLines()
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (s healthcheckScreen) clampScroll() healthcheckScreen {
	if s.scroll < 0 {
		s.scroll = 0
	}
	maxScroll := s.maxScroll()
	if s.scroll > maxScroll {
		s.scroll = maxScroll
	}
	return s
}

func (s healthcheckScreen) view(width, height int) string {
	var b strings.Builder
	b.WriteString("  " + sectionTitleStyle.Render("System Health Check") + "\n\n")

	switch s.state {
	case hcLoading:
		fmt.Fprintf(&b, "  %s Running checks...\n", s.spinner.View())

	case hcError:
		b.WriteString("  " + errorStyle.Render("Error: "+s.err.Error()) + "\n")

	case hcReady:
		r := s.report

		// System info line
		overall := statusStyle(r.OverallStatus).Render(r.OverallStatus)
		info := helpStyle.Render(fmt.Sprintf("%s · %s", r.Hostname, r.OS))
		uptime := ""
		if r.Uptime != "" {
			uptime = helpStyle.Render(" · up " + r.Uptime)
		}
		fmt.Fprintf(&b, "  %s  %s%s\n\n", overall, info, uptime)

		// Counts
		b.WriteString(fmt.Sprintf("  %s %d    %s %d    %s %d    Total: %d\n\n",
			statusStyle("PASS").Render("PASS"), r.Counts.Pass,
			statusStyle("WARN").Render("WARN"), r.Counts.Warn,
			statusStyle("FAIL").Render("FAIL"), r.Counts.Fail,
			r.Counts.Total))

		// Build flat list of renderable lines (section headers + checks + recommendations)
		lines := s.reportLines()
		var recs []string
		for _, sec := range r.Sections {
			for _, c := range sec.Checks {
				if c.Status != "PASS" && c.Recommendation != "" {
					recs = appendUnique(recs, c.Recommendation)
				}
			}
		}

		if len(recs) > 0 {
			b.WriteString("  " + helpStyle.Render(fmt.Sprintf("%d recommendation(s) listed at the end of the report.", len(recs))) + "\n")
		}

		// Scrollable display
		maxVisible := height - 10
		if maxVisible < 5 {
			maxVisible = 5
		}
		totalLines := len(lines)
		start := s.scroll
		if totalLines > maxVisible && start > totalLines-maxVisible {
			start = totalLines - maxVisible
		}
		if start < 0 {
			start = 0
		}
		end := start + maxVisible
		if end > totalLines {
			end = totalLines
		}

		for i := start; i < end; i++ {
			line := lines[i]
			if strings.HasPrefix(line, "section:") {
				title := strings.TrimPrefix(line, "section:")
				b.WriteString("  " + lipgloss.NewStyle().Bold(true).Foreground(secondary).Render(title) + "\n")
			} else {
				b.WriteString(line + "\n")
			}
		}

		if totalLines > maxVisible {
			b.WriteString(fmt.Sprintf("\n  %s\n", helpStyle.Render(
				fmt.Sprintf("Showing %d-%d of %d (↑↓/PgUp/PgDn to scroll)", start+1, end, totalLines))))
		} else {
			b.WriteString("\n  " + helpStyle.Render("Press r to rerun checks or tab to switch screens") + "\n")
		}

	}

	return b.String()
}

func (s healthcheckScreen) helpKeys() []key.Binding {
	switch s.state {
	case hcLoading:
		return []key.Binding{}
	case hcError:
		return []key.Binding{keyRefresh, keyTabs}
	case hcReady:
		bindings := []key.Binding{keyScroll, keyRefresh}
		if s.scroll > 0 {
			bindings = append(bindings, keyEscClear)
		}
		bindings = append(bindings, keyTabs)
		return bindings
	}
	return []key.Binding{keyTabs}
}

func renderCheckLine(c healthCheck, width int) string {
	status := statusStyle(c.Status).Render(fmt.Sprintf("%-4s", c.Status))
	name := lipgloss.NewStyle().Bold(true).Width(20).Render(c.Check)
	maxDetail := width - 34
	if maxDetail < 20 {
		maxDetail = 20
	}
	detail := helpStyle.Render(truncate(c.Details, maxDetail))
	return fmt.Sprintf("  %s  %s  %s", status, name, detail)
}

// ── View helpers ───────────────────────────────────────────────────

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
