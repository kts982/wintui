package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Package holds parsed package info from winget output.
type Package struct {
	Name      string
	ID        string
	Version   string
	Available string
	Source    string
}

// FilterValue satisfies the bubbles list.Item interface (used for filtering).
func (p Package) FilterValue() string { return p.Name + " " + p.ID }

func (p Package) Title() string { return p.Name }

func (p Package) Description() string {
	if p.Available != "" {
		return fmt.Sprintf("%s  %s → %s", p.ID, p.Version, p.Available)
	}
	return fmt.Sprintf("%s  %s", p.ID, p.Version)
}

// ── winget command execution ───────────────────────────────────────

// runWingetCtx runs a winget command with a cancellable context.
func runWingetCtx(ctx context.Context, args ...string) (string, error) {
	return runWingetWithModeCtx(ctx, true, args...)
}

// runWingetActionCtx runs mutating winget commands. These should allow
// installer/UI elevation behavior when required by the package.
func runWingetActionCtx(ctx context.Context, args ...string) (string, error) {
	return runWingetWithModeCtx(ctx, false, args...)
}

func runWingetWithModeCtx(ctx context.Context, nonInteractive bool, args ...string) (string, error) {
	allArgs := make([]string, 0, len(args)+2)
	allArgs = append(allArgs, args...)
	if nonInteractive {
		allArgs = append(allArgs, "--disable-interactivity")
	}
	allArgs = append(allArgs, "--accept-source-agreements")
	cmd := exec.CommandContext(ctx, "winget", allArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("cancelled")
		}
		return out, friendlyWingetError(err, strings.TrimSpace(stderr.String()), out)
	}
	return out, nil
}

// ── High-level query operations (read-only, no package agreements) ─

func getUpgradeable() ([]Package, error) {
	return getUpgradeableCtx(context.Background())
}

func getUpgradeableCtx(ctx context.Context) ([]Package, error) {
	// Don't pass --source here: it removes the Available column from output.
	args := []string{"upgrade"}
	if appSettings.IncludeUnknown {
		args = append(args, "--include-unknown")
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out), nil
}

func getInstalled() ([]Package, error) {
	return getInstalledCtx(context.Background())
}

func getInstalledCtx(ctx context.Context) ([]Package, error) {
	args := []string{"list", "--count", "1000"}
	if appSettings.IncludeUnknown {
		args = append(args, "--include-unknown")
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out), nil
}

func searchPackages(query string) ([]Package, error) {
	return searchPackagesCtx(context.Background(), query)
}

func searchPackagesCtx(ctx context.Context, query string) ([]Package, error) {
	args := []string{"search", query, "--count", "100"}
	args = append(args, appSettings.BuildListArgs()...)
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out), nil
}

// ── High-level action operations (mutating, need package agreements) ─

func upgradePackageSourceCtx(ctx context.Context, id, source string) (string, error) {
	args := []string{"upgrade", "--id", id, "--exact", "--accept-package-agreements"}
	args = append(args, appSettings.BuildInstallArgs()...)
	args = appendActionSourceArgs(args, source)
	return runWingetActionCtx(ctx, args...)
}

func installPackageSourceCtx(ctx context.Context, id, source string) (string, error) {
	args := []string{"install", "--id", id, "--exact", "--accept-package-agreements"}
	args = append(args, appSettings.BuildInstallArgs()...)
	args = appendActionSourceArgs(args, source)
	return runWingetActionCtx(ctx, args...)
}

func uninstallPackageSourceCtx(ctx context.Context, id, source string) (string, error) {
	args := []string{"uninstall", "--id", id, "--exact", "--accept-package-agreements"}
	args = append(args, appSettings.BuildUninstallArgs()...)
	args = appendActionSourceArgs(args, source)
	return runWingetActionCtx(ctx, args...)
}

func showPackage(id, source string) (PackageDetail, error) {
	return showPackageCtx(context.Background(), id, source)
}

func showPackageCtx(ctx context.Context, id, source string) (PackageDetail, error) {
	args := []string{"show", "--id", id, "--exact"}
	if source == "winget" || source == "msstore" {
		args = append(args, "--source", source)
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return PackageDetail{}, err
	}
	detail := parseWingetShow(out)
	if detail.ID == "" {
		detail.ID = id
	}
	if detail.Source == "" {
		detail.Source = source
	}
	return detail, nil
}

// ── Error translation ──────────────────────────────────────────────

// friendlyWingetError translates raw winget exit codes into human-readable messages.
func friendlyWingetError(err error, stderr, stdout string) error {
	msg := err.Error()

	// Map known winget exit/installer codes to friendly descriptions.
	replacements := map[string]string{
		"0x8a150002": "package not found or no applicable installer",
		"0x8a15000e": "upgrade not applicable (already up to date)",
		"0x8a150011": "package not found",
		"0x8a150019": "package version already installed",
		"0x8a15002b": "install technology differs from installed version (package manages its own updates)",
		"0x8a15002c": "some packages failed to upgrade",
		"0x8a150056": "package requires administrator privileges to install",
		"0x80073d28": "installer requires administrator privileges (try running as admin)",
		"0x80073cf3": "package install failed (conflicting package)",
		"0x80073d02": "installation blocked by a running process",
	}

	for code, desc := range replacements {
		if strings.Contains(msg, code) {
			msg = desc
			break
		}
	}

	// Check stdout for admin-related errors when the top-level code is generic.
	if strings.Contains(msg, "some packages failed") &&
		strings.Contains(stdout, "administrator privileges") {
		msg = "some packages require administrator privileges (try running as admin)"
	}

	if stderr != "" {
		return fmt.Errorf("%s: %s", msg, stderr)
	}
	return fmt.Errorf("%s", msg)
}

// requiresElevation reports whether a winget error/output indicates the action
// needs an elevated terminal rather than a different package failure.
func requiresElevation(err error, output string) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error() + "\n" + output)
	patterns := []string{
		"administrator privileges",
		"run as admin",
		"requires elevation",
		"must be run as administrator",
		"0x8a150056",
		"0x80073d28",
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func elevationRetryHint() string {
	return "Press Ctrl+e to relaunch elevated, or run WinTUI in an elevated terminal and retry."
}

func appendActionSourceArgs(args []string, source string) []string {
	if source == "winget" || source == "msstore" {
		return append(args, "--source", source)
	}
	return append(args, appSettings.BuildListArgs()...)
}

// ── Output cleaning ────────────────────────────────────────────────

// cleanWingetOutput strips progress spinner chars, carriage returns,
// and noisy lines from winget command output for display.
func cleanWingetOutput(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	raw := strings.FieldsFunc(output, func(r rune) bool { return r == '\r' })
	output = strings.Join(raw, "")

	// Noise patterns to skip entirely
	noisePatterns := []string{
		"██", "▒▒",
		"This application is licensed",
		"Microsoft is not responsible",
		"nor does it grant any licenses",
		"A newer version was found, but the install technology",
		"Successfully verified installer hash",
		"Starting package install",
		"Starting package uninstall",
		"Downloading",
	}

	lines := strings.Split(output, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "/" || trimmed == "\\" ||
			trimmed == "-" || trimmed == "|" {
			continue
		}
		// Skip lines that are only dashes (separators)
		if strings.Trim(trimmed, "-") == "" {
			continue
		}
		skip := false
		for _, p := range noisePatterns {
			if strings.Contains(trimmed, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		clean = append(clean, trimmed)
	}
	return strings.Join(clean, "\n")
}

// ── Package detail ─────────────────────────────────────────────────

// PackageDetail holds extended info from `winget show`.
type PackageDetail struct {
	Name          string
	ID            string
	Source        string
	Version       string
	Publisher     string
	PublisherURL  string
	Description   string
	Homepage      string
	License       string
	Copyright     string
	ReleaseNotes  string
	ReleaseDate   string
	Tags          string
	InstallerType string
	InstallerURL  string
	Moniker       string
}

func parseWingetShow(output string) PackageDetail {
	var d PackageDetail

	output = strings.ReplaceAll(output, "\r\n", "\n")
	raw := strings.FieldsFunc(output, func(r rune) bool { return r == '\r' })
	output = strings.Join(raw, "")
	lines := strings.Split(output, "\n")

	// Parse "Found Name [ID]" header
	for _, line := range lines {
		if strings.HasPrefix(line, "Found ") {
			if lb := strings.LastIndex(line, "["); lb > 0 {
				rb := strings.LastIndex(line, "]")
				if rb > lb {
					d.ID = line[lb+1 : rb]
					d.Name = strings.TrimSpace(line[6:lb])
				}
			}
			break
		}
	}

	// Parse key: value pairs, handling multi-line indented values
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.Contains(line, ":") {
			continue
		}

		colonIdx := strings.Index(line, ":")
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])

		for i+1 < len(lines) && len(lines[i+1]) > 2 && lines[i+1][:2] == "  " {
			i++
			if val != "" {
				val += "\n"
			}
			val += strings.TrimSpace(lines[i])
		}

		switch key {
		case "Version":
			d.Version = val
		case "Publisher":
			d.Publisher = val
		case "Publisher Url":
			d.PublisherURL = val
		case "Description":
			d.Description = val
		case "Homepage":
			d.Homepage = val
		case "License":
			d.License = val
		case "Copyright":
			d.Copyright = val
		case "Release Notes":
			d.ReleaseNotes = val
		case "Release Date":
			d.ReleaseDate = val
		case "Tags":
			d.Tags = val
		case "Installer Type":
			d.InstallerType = val
		case "Installer Url":
			d.InstallerURL = val
		case "Moniker":
			d.Moniker = val
		}
	}

	return d
}

// ── winget table parser ────────────────────────────────────────────

type colPos struct {
	name  string
	start int
}

// parseWingetTable parses the fixed-width table output from winget.
func parseWingetTable(output string) []Package {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	raw := strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' })
	var lines []string
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}

	// Find the separator line (all dashes).
	sepIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 10 && strings.Trim(trimmed, "-\u2500") == "" {
			sepIdx = i
			break
		}
	}
	if sepIdx < 1 || sepIdx >= len(lines)-1 {
		return nil
	}

	// Detect column start positions from the header.
	header := lines[sepIdx-1]
	colNames := []string{"Name", "Id", "Version", "Available", "Source"}
	var cols []colPos
	for _, name := range colNames {
		idx := strings.Index(header, name)
		if idx >= 0 {
			cols = append(cols, colPos{name, idx})
		}
	}
	if len(cols) < 3 {
		return nil
	}

	// Parse data rows.
	var pkgs []Package
	for _, line := range lines[sepIdx+1:] {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "upgrades available") ||
			strings.Contains(lower, "package(s)") ||
			strings.Contains(lower, "no installed") {
			continue
		}

		pkg := extractPackage(line, cols)
		if pkg.ID != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

func extractPackage(line string, cols []colPos) Package {
	// Normalize multi-byte ellipsis (…, 3 bytes) to single-byte so
	// byte-indexed column positions from the header stay aligned.
	line = strings.ReplaceAll(line, "\u2026", ".")

	field := func(c int) string {
		start := cols[c].start
		if start >= len(line) {
			return ""
		}
		end := len(line)
		if c+1 < len(cols) {
			end = cols[c+1].start
		}
		if end > len(line) {
			end = len(line)
		}
		return strings.TrimSpace(line[start:end])
	}

	pkg := Package{}
	for i, c := range cols {
		val := field(i)
		switch c.name {
		case "Name":
			pkg.Name = val
		case "Id":
			pkg.ID = val
		case "Version":
			pkg.Version = val
		case "Available":
			pkg.Available = val
		case "Source":
			// Only accept known source values; garbage from misaligned rows is ignored.
			if val == "winget" || val == "msstore" {
				pkg.Source = val
			}
		}
		_ = i
	}
	return pkg
}

// -- Streaming command execution ------------------------------------

func runWingetStreamCtx(ctx context.Context, nonInteractive bool, args ...string) (<-chan string, <-chan error) {
	outChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		defer close(errChan)

		allArgs := make([]string, 0, len(args)+2)
		allArgs = append(allArgs, args...)
		if nonInteractive {
			allArgs = append(allArgs, "--disable-interactivity")
		}
		allArgs = append(allArgs, "--accept-source-agreements")
		cmd := exec.CommandContext(ctx, "winget", allArgs...)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errChan <- err
			return
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			errChan <- err
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			outChan <- scanner.Text()
		}

		errChan <- cmd.Wait()
	}()

	return outChan, errChan
}

func installPackageStream(id, source string) (<-chan string, <-chan error) {
	args := []string{"install", "--id", id, "--exact", "--accept-package-agreements"}
	args = append(args, appSettings.BuildInstallArgs()...)
	args = appendActionSourceArgs(args, source)
	return runWingetStreamCtx(context.Background(), false, args...)
}
