package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Package holds parsed package info from winget output.
type Package struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Available string `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`
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

func packageSourceKey(id, source string) string {
	return id + "\x1f" + source
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

func installCommandArgs(id, source, version string) []string {
	args := []string{"install", "--id", id, "--exact", "--accept-package-agreements"}
	args = appendVersionArg(args, version)
	args = append(args, appSettings.BuildInstallArgs()...)
	return appendPreferredSourceArg(args, source)
}

func upgradeCommandArgs(id, source, version string) []string {
	args := []string{"upgrade", "--id", id, "--exact", "--accept-package-agreements"}
	args = appendVersionArg(args, version)
	args = append(args, appSettings.BuildInstallArgs()...)
	return appendPreferredSourceArg(args, source)
}

func uninstallLookupArgs(pkg Package) []string {
	switch {
	case strings.HasPrefix(pkg.ID, "{") && strings.HasSuffix(pkg.ID, "}"):
		return []string{"--product-code", pkg.ID}
	case isNonCanonical(pkg.ID):
		if strings.TrimSpace(pkg.Name) != "" {
			return []string{"--name", pkg.Name}
		}
		return []string{"--id", pkg.ID}
	case pkg.Source == "winget" || pkg.Source == "msstore":
		return []string{"--id", pkg.ID}
	case looksLikeStoreProductID(pkg.ID):
		return []string{"--id", pkg.ID}
	case strings.Contains(pkg.ID, "."):
		return []string{"--id", pkg.ID}
	case strings.TrimSpace(pkg.Name) != "":
		return []string{"--name", pkg.Name}
	default:
		return []string{"--id", pkg.ID}
	}
}

func uninstallCommandArgs(pkg Package, includePurge bool) []string {
	args := []string{"uninstall"}
	args = append(args, uninstallLookupArgs(pkg)...)
	args = append(args, "--exact")
	return append(args, appSettings.BuildUninstallArgs(includePurge)...)
}

func installPackageSourceCtx(ctx context.Context, id, source, version string) (string, error) {
	args := installCommandArgs(id, source, version)
	return runWingetActionCtx(ctx, args...)
}

func shouldRetryUninstallWithoutPurge(err error, output string) bool {
	if err == nil || !appSettings.PurgeOnUninstall {
		return false
	}
	lower := strings.ToLower(err.Error() + "\n" + output)
	patterns := []string{
		"no applicable installer",
		"package not found",
		"0x8a150002",
		"0x8a150011",
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func showPackage(id, source, version string) (PackageDetail, error) {
	return showPackageCtx(context.Background(), id, source, version)
}

func showPackageCtx(ctx context.Context, id, source, version string) (PackageDetail, error) {
	args := []string{"show", "--id", id, "--exact"}
	args = appendVersionArg(args, version)
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

func showPackageVersionsCtx(ctx context.Context, id, source string) ([]string, error) {
	args := []string{"show", "--id", id, "--exact", "--versions"}
	if source == "winget" || source == "msstore" {
		args = append(args, "--source", source)
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetVersions(out), nil
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
		"1603":       "installer failed with a fatal error",
		"1618":       "another installation is already in progress",
		"1638":       "another version of this product is already installed",
		"3010":       "installer completed and a restart is required",
		"1641":       "installer initiated a restart",
	}

	code, desc := matchKnownWingetErrorCode(strings.Join([]string{msg, stderr, stdout}, "\n"), replacements)
	if code != "" {
		msg = fmt.Sprintf("%s (%s)", desc, code)
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

func matchKnownWingetErrorCode(text string, replacements map[string]string) (string, string) {
	lower := strings.ToLower(text)
	for code, desc := range replacements {
		if strings.Contains(lower, strings.ToLower(code)) {
			return code, desc
		}
	}
	return "", ""
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

// likelyBenefitsFromElevation reports whether retrying elevated is worth offering
// even when the failure is not a confirmed admin-only winget error.
func likelyBenefitsFromElevation(err error, output string) bool {
	if err == nil {
		return false
	}
	if requiresElevation(err, output) {
		return true
	}
	lower := strings.ToLower(err.Error() + "\n" + output)
	return strings.Contains(lower, "1603") || strings.Contains(lower, "0x80070643")
}

type elevationRetryKind int

const (
	elevationRetryNone elevationRetryKind = iota
	elevationRetrySoft
	elevationRetryHard
)

func classifyElevationRetry(err error, output string) elevationRetryKind {
	if err == nil {
		return elevationRetryNone
	}
	if requiresElevation(err, output) {
		return elevationRetryHard
	}
	if likelyBenefitsFromElevation(err, output) {
		return elevationRetrySoft
	}
	return elevationRetryNone
}

func appendPreferredSourceArg(args []string, source string) []string {
	if source != "winget" && source != "msstore" {
		source = appSettings.Source
	}
	if source == "winget" || source == "msstore" {
		return append(args, "--source", source)
	}
	return args
}

// ── Output cleaning ────────────────────────────────────────────────

// cleanWingetOutput strips progress spinner chars, carriage returns,
// and noisy lines from winget command output for display.
func cleanWingetOutput(output string) string {
	var clean []string
	for _, line := range splitWingetOutputLines(output) {
		if cleaned, ok := cleanWingetOutputLine(line); ok {
			clean = append(clean, cleaned)
		}
	}
	return strings.Join(clean, "\n")
}

func streamWingetOutputLines(output string) []string {
	var lines []string
	for _, line := range splitWingetOutputLines(output) {
		if cleaned, ok := cleanWingetStreamLine(line); ok {
			if len(lines) == 0 || lines[len(lines)-1] != cleaned {
				lines = append(lines, cleaned)
			}
		}
	}
	return lines
}

func splitWingetOutputLines(output string) []string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	return strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' })
}

func cleanWingetOutputLine(line string) (string, bool) {
	return filterWingetOutputLine(line, []string{
		"██", "▒▒",
		"This application is licensed",
		"Microsoft is not responsible",
		"nor does it grant any licenses",
		"A newer version was found, but the install technology",
		"Successfully verified installer hash",
		"Starting package install",
		"Starting package upgrade",
		"Starting package uninstall",
		"Downloading",
	})
}

func cleanWingetStreamLine(line string) (string, bool) {
	return filterWingetOutputLine(line, []string{
		"██", "▒▒",
		"This application is licensed",
		"Microsoft is not responsible",
		"nor does it grant any licenses",
		"Successfully verified installer hash",
		"Starting package install",
		"Starting package upgrade",
		"Starting package uninstall",
	})
}

func filterWingetOutputLine(line string, noisePatterns []string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || trimmed == "/" || trimmed == "\\" ||
		trimmed == "-" || trimmed == "|" {
		return "", false
	}
	if strings.Trim(trimmed, "-") == "" {
		return "", false
	}
	for _, p := range noisePatterns {
		if strings.Contains(trimmed, p) {
			return "", false
		}
	}
	return trimmed, true
}

func normalizeActionStreamLine(action retryOp, line string) string {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	const prefix = " failed with exit code:"

	var label string
	switch {
	case strings.HasPrefix(lower, "install"+prefix):
		label = "install"
	case strings.HasPrefix(lower, "upgrade"+prefix):
		label = "upgrade"
	case strings.HasPrefix(lower, "uninstall"+prefix):
		label = "uninstall"
	default:
		switch action {
		case retryOpUpgrade:
			switch lower {
			case "successfully uninstalled":
				return "Removed previous version"
			case "successfully installed":
				return "Installed updated version"
			}
		}
		return line
	}

	suffix := trimmed[len(label):]
	switch action {
	case retryOpInstall:
		return "Install" + suffix
	case retryOpUpgrade:
		return "Upgrade" + suffix
	case retryOpUninstall:
		return "Uninstall" + suffix
	default:
		return line
	}
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

func appendVersionArg(args []string, version string) []string {
	version = strings.TrimSpace(version)
	if version == "" {
		return args
	}
	return append(args, "--version", version)
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

func parseWingetVersions(output string) []string {
	lines := splitWingetOutputLines(output)
	var versions []string
	inVersions := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Found ") || trimmed == "Version" {
			continue
		}
		if strings.Trim(trimmed, "-") == "" {
			inVersions = true
			continue
		}
		if !inVersions {
			continue
		}
		versions = append(versions, trimmed)
	}
	return versions
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
	colNames := []string{"Name", "Id", "Moniker", "Version", "Available", "Match", "Source"}
	var cols []colPos
	for _, name := range colNames {
		idx := strings.Index(header, name)
		if idx >= 0 {
			cols = append(cols, colPos{name, idx})
		}
	}
	sort.Slice(cols, func(i, j int) bool {
		return cols[i].start < cols[j].start
	})
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

func awaitStream(args []string, outChan <-chan string, errChan <-chan error) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-outChan
		if !ok {
			return streamDoneMsg{
				err:       <-errChan,
				retryArgs: args,
			}
		}
		return streamMsg(line)
	}
}

func runWingetStreamCtx(ctx context.Context, nonInteractive bool, args ...string) (<-chan string, <-chan error) {
	if ctx == nil {
		panic("runWingetStreamCtx: ctx is nil! args: " + strings.Join(args, " "))
	}
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

		var rawOutput strings.Builder
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			rawLine := scanner.Text()
			rawOutput.WriteString(rawLine)
			rawOutput.WriteByte('\n')

			lines := streamWingetOutputLines(rawLine)
			for _, line := range lines {
				select {
				case outChan <- line:
				case <-ctx.Done():
					errChan <- fmt.Errorf("cancelled")
					return
				}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			if ctx.Err() != nil {
				errChan <- fmt.Errorf("cancelled")
			} else {
				errChan <- scanErr
			}
			return
		}

		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			errChan <- fmt.Errorf("cancelled")
			return
		}
		if waitErr != nil {
			errChan <- friendlyWingetError(waitErr, "", rawOutput.String())
			return
		}
		errChan <- nil
	}()

	return outChan, errChan
}

func runActionSmartStreamCtx(ctx context.Context, args ...string) (<-chan string, <-chan error) {
	// When silent mode + auto-elevate are both on and we're not already
	// elevated, run everything through the elevated helper upfront.
	// This avoids UAC popups from installers that elevate themselves
	// (e.g. MSI packages with ElevationRequirement: elevatesSelf).
	if appSettings.InstallMode == "silent" && appSettings.AutoElevate && !isElevated() {
		out, errCh, initErr := globalElevator.runCommandElevated(args...)
		if initErr == nil {
			return out, errCh
		}
		// Helper failed to start — fall through to normal path.
	}

	// Try running normally (non-elevated)
	outChan, errChan := runWingetStreamCtx(ctx, false, args...)

	// Create a proxy channel so we can switch to elevated if needed
	proxyOut := make(chan string)
	proxyErr := make(chan error, 1)

	go func() {
		defer close(proxyOut)
		defer close(proxyErr)

		var lines []string
		for line := range outChan {
			lines = append(lines, line)
			proxyOut <- line
		}

		err := <-errChan
		if err != nil && requiresElevation(err, strings.Join(lines, "\n")) && !isElevated() && appSettings.AutoElevate {
			proxyOut <- "Elevation required. Requesting..."
			eOut, eErr, initErr := globalElevator.runCommandElevated(args...)
			if initErr != nil {
				proxyOut <- fmt.Sprintf("Automatic elevation failed: %v", initErr)
				proxyErr <- err
				return
			}
			for line := range eOut {
				proxyOut <- line
			}
			proxyErr <- <-eErr
			return
		}
		proxyErr <- err
	}()

	return proxyOut, proxyErr
}

func installPackageStreamCtx(ctx context.Context, id, source, version string) ([]string, <-chan string, <-chan error) {
	args := installCommandArgs(id, source, version)
	out, err := runActionSmartStreamCtx(ctx, args...)
	return args, out, err
}

func installPackageElevatedStreamCtx(id, source, version string) ([]string, <-chan string, <-chan error, error) {
	args := installCommandArgs(id, source, version)
	out, err, initErr := globalElevator.runCommandElevated(args...)
	return args, out, err, initErr
}

func upgradePackageStreamCtx(ctx context.Context, id, source, version string) ([]string, <-chan string, <-chan error) {
	args := upgradeCommandArgs(id, source, version)
	out, err := runActionSmartStreamCtx(ctx, args...)
	return args, out, err
}

func upgradePackageElevatedStreamCtx(id, source, version string) ([]string, <-chan string, <-chan error, error) {
	args := upgradeCommandArgs(id, source, version)
	out, err, initErr := globalElevator.runCommandElevated(args...)
	return args, out, err, initErr
}

func uninstallPackageStreamCtx(ctx context.Context, pkg Package) ([]string, <-chan string, <-chan error) {
	args := uninstallCommandArgs(pkg, appSettings.PurgeOnUninstall)
	outChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		defer close(errChan)

		runAttempt := func(includePurge bool) (string, error, bool) {
			streamOut, streamErr := runActionSmartStreamCtx(ctx, uninstallCommandArgs(pkg, includePurge)...)
			var lines []string
			for line := range streamOut {
				lines = append(lines, line)
				select {
				case outChan <- line:
				case <-ctx.Done():
					errChan <- fmt.Errorf("cancelled")
					return "", fmt.Errorf("cancelled"), true
				}
			}
			err := <-streamErr
			return strings.Join(lines, "\n"), err, false
		}

		output, err, aborted := runAttempt(appSettings.PurgeOnUninstall)
		if aborted {
			return
		}
		if shouldRetryUninstallWithoutPurge(err, output) {
			select {
			case outChan <- "Retrying without purge...":
			case <-ctx.Done():
				errChan <- fmt.Errorf("cancelled")
				return
			}
			_, err, aborted = runAttempt(false)
			if aborted {
				return
			}
		}
		errChan <- err
	}()

	return args, outChan, errChan
}

func uninstallPackageElevatedStreamCtx(pkg Package) ([]string, <-chan string, <-chan error, error) {
	args := uninstallCommandArgs(pkg, appSettings.PurgeOnUninstall)
	outChan := make(chan string)
	errChan := make(chan error, 1)
	go func() {
		defer close(outChan)
		defer close(errChan)

		runAttempt := func(includePurge bool) (string, error, error) {
			streamOut, streamErr, initErr := globalElevator.runCommandElevated(uninstallCommandArgs(pkg, includePurge)...)
			if initErr != nil {
				return "", nil, initErr
			}
			var lines []string
			for line := range streamOut {
				lines = append(lines, line)
				outChan <- line
			}
			err, ok := <-streamErr
			if !ok {
				err = nil
			}
			return strings.Join(lines, "\n"), err, nil
		}

		output, err, initErr := runAttempt(appSettings.PurgeOnUninstall)
		if initErr != nil {
			errChan <- initErr
			return
		}
		if shouldRetryUninstallWithoutPurge(err, output) {
			outChan <- "Retrying without purge..."
			_, err, initErr = runAttempt(false)
			if initErr != nil {
				errChan <- initErr
				return
			}
		}
		errChan <- err
	}()

	return args, outChan, errChan, nil
}
