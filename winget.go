package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Package holds parsed package info from winget output.
type Package struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Available string `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`

	// idTruncated/nameTruncated record that the corresponding column in
	// winget's table output ended in a Unicode horizontal ellipsis (U+2026)
	// because the value didn't fit the console width. Consumers must not
	// pass a truncated ID to `winget --id ... --exact` — it will fail with
	// 0x8a150014. resolveTruncatedPackage substitutes the full ID lazily,
	// at the action gate, so refresh-time listings don't pay the per-row
	// re-query cost.
	idTruncated   bool
	nameTruncated bool
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
	wingetExe, err := wingetExePath()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, wingetExe, allArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
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
	if currentSettings().IncludeUnknown {
		args = append(args, "--include-unknown")
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out, tableUpgrade)
}

func getInstalledCtx(ctx context.Context) ([]Package, error) {
	// --include-unknown is only valid for `winget upgrade`; passing it to
	// `winget list` makes winget exit non-zero with a usage error, which
	// surfaces as an empty installed list in the TUI.
	//
	// No --count: winget returns the full inventory by default, and the flag
	// caps at 1000 — passing it silently truncated >1000-package machines.
	args := []string{"list"}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out, tableList)
}

func searchPackagesCtx(ctx context.Context, query string) ([]Package, error) {
	args := []string{"search", query, "--count", "100"}
	args = append(args, currentSettings().BuildListArgs()...)
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out, tableSearch)
}

// lookupSinglePackageCtx fetches the installed state of a single package by ID.
// Used for incremental cache updates after install/upgrade/uninstall.
func lookupSinglePackageCtx(ctx context.Context, pkg Package) ([]Package, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, err
	}
	args := []string{"list", "--id", resolved.ID, "--exact"}
	if resolved.Source != "" {
		args = append(args, "--source", resolved.Source)
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetTable(out, tableList)
}

// ── High-level action operations (mutating, need package agreements) ─

// formatWingetCommand renders an argv slice as a displayable shell command.
// Arguments containing whitespace are quoted so the preview is copy-pasteable.
func formatWingetCommand(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "winget")
	for _, a := range args {
		if a == "" {
			parts = append(parts, `""`)
			continue
		}
		if strings.ContainsAny(a, " \t\"") {
			parts = append(parts, `"`+strings.ReplaceAll(a, `"`, `\"`)+`"`)
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

func installCommandArgs(id, source, version string) []string {
	settings := currentSettings()
	args := []string{"install", "--id", id, "--exact", "--accept-package-agreements"}
	args = appendVersionArg(args, version)
	args = append(args, settings.effectiveSettings(id, source).BuildInstallArgs()...)
	return appendPreferredSourceArg(args, source, settings.Source)
}

func upgradeCommandArgs(id, source, version string) []string {
	settings := currentSettings()
	args := []string{"upgrade", "--id", id, "--exact", "--accept-package-agreements"}
	args = appendVersionArg(args, version)
	args = append(args, settings.effectiveSettings(id, source).BuildInstallArgs()...)
	return appendPreferredSourceArg(args, source, settings.Source)
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

func uninstallCommandArgs(pkg Package, includePurge, allVersions bool) []string {
	args := []string{"uninstall"}
	args = append(args, uninstallLookupArgs(pkg)...)
	args = append(args, "--exact")
	if allVersions {
		args = append(args, "--all-versions")
	}
	return append(args, currentSettings().BuildUninstallArgs(includePurge)...)
}

func installPackageSourceCtx(ctx context.Context, pkg Package, version string) (string, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return "", err
	}
	args := installCommandArgs(resolved.ID, resolved.Source, version)
	return runWingetActionCtx(ctx, args...)
}

func shouldRetryUninstallWithoutPurge(err error, output string) bool {
	if err == nil || !currentSettings().PurgeOnUninstall {
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

func showPackage(pkg Package, version string) (PackageDetail, error) {
	return showPackageCtx(context.Background(), pkg, version)
}

func showPackageCtx(ctx context.Context, pkg Package, version string) (PackageDetail, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return PackageDetail{}, err
	}
	args := []string{"show", "--id", resolved.ID, "--exact"}
	args = appendVersionArg(args, version)
	if resolved.Source == "winget" || resolved.Source == "msstore" {
		args = append(args, "--source", resolved.Source)
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return PackageDetail{}, err
	}
	detail := parseWingetShow(out)
	if detail.ID == "" {
		detail.ID = resolved.ID
	}
	if detail.Source == "" {
		detail.Source = resolved.Source
	}
	return detail, nil
}

func showPackageVersionsCtx(ctx context.Context, pkg Package) ([]string, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, err
	}
	args := []string{"show", "--id", resolved.ID, "--exact", "--versions"}
	if resolved.Source == "winget" || resolved.Source == "msstore" {
		args = append(args, "--source", resolved.Source)
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseWingetVersions(out), nil
}

// ── Error translation ──────────────────────────────────────────────

// wingetErrorCodes maps known winget exit/installer codes to friendly
// descriptions. An ordered slice, not a map: when output mentions several
// known codes the FIRST entry here wins, so the mapping is deterministic.
// Specific winget hex codes come before the generic MSI decimal codes.
var wingetErrorCodes = []struct{ code, desc string }{
	{"0x8a150002", "package not found or no applicable installer"},
	{"0x8a150003", "installer command failed"},
	{"0x8a15000e", "upgrade not applicable (already up to date)"},
	{"0x8a150011", "package not found"},
	{"0x8a150014", "no installed package found matching input criteria"},
	{"0x8a150015", "no applicable update found"},
	{"0x8a150019", "package version already installed"},
	{"0x8a15002b", "install technology differs from installed version (package manages its own updates)"},
	{"0x8a15002c", "some packages failed to upgrade"},
	{"0x8a150006", "installer failed (the installer process was terminated)"},
	{"0x8a150052", "portable package could not replace files (close the running app before upgrading)"},
	{"0x8a150056", "package requires administrator privileges to install"},
	{"0x8a150066", "one or more versions failed to uninstall (close the app and retry)"},
	{"0x80073d28", "installer requires administrator privileges (try running as admin)"},
	{"0x80073cf3", "package install failed (conflicting package)"},
	{"0x80073d02", "installation blocked by a running process"},
	{"0x80072efd", "network connection failed while reaching the package source (check VPN/proxy/firewall)"},
	{"3221226525", "installer was terminated (close the app before upgrading)"},
	{"1603", "installer failed with a fatal error"},
	{"1618", "another installation is already in progress"},
	{"1638", "another version of this product is already installed"},
	{"3010", "installer completed and a restart is required"},
	{"1641", "installer initiated a restart"},
}

// friendlyWingetError translates raw winget exit codes into human-readable messages.
func friendlyWingetError(err error, stderr, stdout string) error {
	msg := err.Error()

	code, desc := matchKnownWingetErrorCode(strings.Join([]string{msg, stderr, stdout}, "\n"))
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

func matchKnownWingetErrorCode(text string) (string, string) {
	lower := strings.ToLower(text)
	for _, entry := range wingetErrorCodes {
		if containsCodeToken(lower, entry.code) {
			return entry.code, entry.desc
		}
	}
	return "", ""
}

// containsCodeToken reports whether text contains code bounded by
// non-alphanumeric characters, so "1603" doesn't match inside "31603" or
// "0x8a150002" inside a longer hex literal. text must already be lowercased.
func containsCodeToken(text, code string) bool {
	for start := 0; ; {
		idx := strings.Index(text[start:], code)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(code)
		if (idx == 0 || !isCodeChar(text[idx-1])) && (end == len(text) || !isCodeChar(text[end])) {
			return true
		}
		start = idx + 1
	}
}

func isCodeChar(c byte) bool {
	return c == '_' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// isProcessInUseError reports whether a winget error/output indicates the
// package action was blocked because a related process is currently running.
// The standard fix is for the user to close the app and retry.
func isProcessInUseError(err error, output string) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error() + "\n" + output)
	patterns := []string{
		"0x80073d02",             // installation blocked by a running process
		"0x8a150052",             // portable package could not replace files
		"0x8a150066",             // one or more versions failed to uninstall
		"process was terminated", // 0x8a150006
		"is in use",
		"being used by another process",
		"close the running app",
		"close the app",
		"3221226525", // installer terminated
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
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

func appendPreferredSourceArg(args []string, source, defaultSource string) []string {
	if source != "winget" && source != "msstore" {
		source = defaultSource
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

// progressSentinelPrefix marks a stream message carrying a percent update
// rather than a normal text line. Awaiters detect and route these to a
// progressUpdateMsg. The prefix uses a NUL byte so it cannot collide with
// real winget output.
const progressSentinelPrefix = "\x00WT_PROGRESS\x00"

func progressLineSentinel(percent int) string {
	return fmt.Sprintf("%s%d", progressSentinelPrefix, percent)
}

// parseProgressSentinel reports whether a stream line is a progress update
// and extracts the percent.
func parseProgressSentinel(line string) (int, bool) {
	if !strings.HasPrefix(line, progressSentinelPrefix) {
		return 0, false
	}
	raw := strings.TrimPrefix(line, progressSentinelPrefix)
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 100 {
		return 0, false
	}
	return n, true
}

// extractProgressPercent attempts to pull a percentage from a winget
// progress bar line like `██████████▒▒▒▒▒  65%` or `  50% (12.5 MB / 25 MB)`.
// Returns (percent, true) if the line looks like a progress update.
func extractProgressPercent(line string) (int, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0, false
	}
	// Only treat as progress if the line actually contains a bar or looks
	// like a winget progress line (avoids matching "100% legal").
	hasBar := strings.Contains(trimmed, "██") || strings.Contains(trimmed, "▒▒")
	idx := strings.Index(trimmed, "%")
	if idx < 0 {
		return 0, false
	}
	end := idx
	start := end
	for start > 0 {
		c := trimmed[start-1]
		if c < '0' || c > '9' {
			break
		}
		start--
	}
	if start == end {
		return 0, false
	}
	n, err := strconv.Atoi(trimmed[start:end])
	if err != nil || n < 0 || n > 100 {
		return 0, false
	}
	if !hasBar {
		return 0, false
	}
	return n, true
}

// splitCRorLF is a bufio.Scanner split function that terminates tokens on
// either '\r' or '\n'. Winget uses '\r' to update progress bars in place,
// which the default scanner would buffer until a '\n' arrives.
func splitCRorLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\r' || b == '\n' {
			// Skip combined CRLF by advancing one extra byte.
			advance := i + 1
			if b == '\r' && i+1 < len(data) && data[i+1] == '\n' {
				advance++
			}
			return advance, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
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
	Name            string
	ID              string
	Source          string
	Version         string
	Publisher       string
	PublisherURL    string
	Description     string
	Homepage        string
	License         string
	Copyright       string
	ReleaseNotes    string
	ReleaseNotesURL string
	ReleaseDate     string
	Tags            string
	InstallerType   string
	InstallerURL    string
	Moniker         string
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
		case "Release Notes Url":
			d.ReleaseNotesURL = val
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

// ── winget table parser ───────────────────────────────────────────────────────────────
//
// parseWingetTable lives in winget_table.go. It is schema-aware
// (list/upgrade/search), display-cell aware (handles CJK headers), and
// matches localised column headers via a vendored dictionary in
// winget_locales_gen.go.

// resolveTruncatedPackage substitutes the full ID for a package whose Id
// column was truncated by winget at the console width. winget renders the
// trailing characters as "…" (U+2026); without recovery, downstream calls of
// the form `winget --id <truncated> --exact` fail with 0x8a150014.
//
// Resolution is lazy: callers invoke this just before issuing a winget action
// that uses --id ... --exact, so refresh-time listings don't pay the per-row
// re-query cost. Returns the original package unchanged when idTruncated is
// false or the ID is non-canonical (MSIX/GUID/store identities can't be
// resolved this way and need a different lookup strategy).
//
// Recovery strategy: re-query winget with the truncated value as a non-exact
// --id prefix. The result set is small enough that columns auto-size to fit,
// returning the full ID. When the prefix matches more than one row, the Name
// (if intact) disambiguates.
func resolveTruncatedPackage(ctx context.Context, pkg Package) (Package, error) {
	if !pkg.idTruncated || pkg.ID == "" {
		return pkg, nil
	}
	if !shouldResolveTruncatedPackageID(pkg) {
		return pkg, nil
	}
	resolved, ok := resolvePackageID(ctx, pkg, "list")
	if !ok {
		return pkg, fmt.Errorf("could not recover full package ID for %q (winget truncated the listing)", pkg.ID)
	}
	return resolved, nil
}

func shouldResolveTruncatedPackageID(pkg Package) bool {
	return !isNonCanonical(pkg.ID)
}

const truncatedIDResolveTimeout = 3 * time.Second

func resolvePackageID(ctx context.Context, pkg Package, resolverCmd string) (Package, bool) {
	args := []string{resolverCmd, "--id", pkg.ID}
	if pkg.Source != "" {
		args = append(args, "--source", pkg.Source)
	}
	resolveCtx, cancel := context.WithTimeout(ctx, truncatedIDResolveTimeout)
	defer cancel()
	out, err := runWingetCtx(resolveCtx, args...)
	if err != nil && len(out) == 0 {
		return pkg, false
	}
	kind := tableList
	if resolverCmd == "search" {
		kind = tableSearch
	}
	candidates, parseErr := parseWingetTable(out, kind)
	if parseErr != nil {
		return pkg, false
	}
	return pickResolvedID(pkg, candidates)
}

// pickResolvedID selects the candidate whose full ID should replace the
// truncated value on pkg. Returns (pkg, false) when there's no usable match
// rather than guessing — a wrong --id would still fail under --exact, just
// with a misleading symptom.
func pickResolvedID(pkg Package, candidates []Package) (Package, bool) {
	var matches []Package
	for _, c := range candidates {
		if c.idTruncated || !strings.HasPrefix(c.ID, pkg.ID) {
			continue
		}
		matches = append(matches, c)
	}
	switch len(matches) {
	case 0:
		return pkg, false
	case 1:
		pkg.ID = matches[0].ID
		pkg.idTruncated = false
		return pkg, true
	default:
		if pkg.Name == "" || pkg.nameTruncated {
			return pkg, false
		}
		for _, m := range matches {
			if m.Name == pkg.Name {
				pkg.ID = m.ID
				pkg.idTruncated = false
				return pkg, true
			}
		}
		return pkg, false
	}
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
		if pct, isProgress := parseProgressSentinel(line); isProgress {
			return streamProgressMsg(pct)
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
		wingetExe, pathErr := wingetExePath()
		if pathErr != nil {
			errChan <- pathErr
			return
		}
		cmd := exec.CommandContext(ctx, wingetExe, allArgs...)
		applyHiddenChildWindow(cmd)

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
		scanner.Split(splitCRorLF)
		for scanner.Scan() {
			rawLine := scanner.Text()
			rawOutput.WriteString(rawLine)
			rawOutput.WriteByte('\n')

			// Emit progress update first (before filtering the line out).
			if pct, ok := extractProgressPercent(rawLine); ok {
				select {
				case outChan <- progressLineSentinel(pct):
				case <-ctx.Done():
					errChan <- fmt.Errorf("cancelled")
					return
				}
			}

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
	settings := currentSettings()

	// When silent mode + auto-elevate are both on and we're not already
	// elevated, run everything through the elevated helper upfront.
	// This avoids UAC popups from installers that elevate themselves
	// (e.g. MSI packages with ElevationRequirement: elevatesSelf).
	if settings.InstallMode == ModeSilent && settings.AutoElevate && !isElevated() {
		out, errCh, initErr := globalElevator.runCommandElevated(ctx, args...)
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
		if err != nil && requiresElevation(err, strings.Join(lines, "\n")) && !isElevated() && settings.AutoElevate {
			proxyOut <- "Elevation required. Requesting..."
			eOut, eErr, initErr := globalElevator.runCommandElevated(ctx, args...)
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

func runActionStreamForPackage(ctx context.Context, pkgID, source string, args ...string) (<-chan string, <-chan error) {
	elev := currentSettings().packageElevateOverride(pkgID, source)
	if elev != nil {
		if *elev && !isElevated() {
			out, errCh, initErr := globalElevator.runCommandElevated(ctx, args...)
			if initErr == nil {
				return out, errCh
			}
		}
		if !*elev {
			return runWingetStreamCtx(ctx, false, args...)
		}
	}
	return runActionSmartStreamCtx(ctx, args...)
}

func installPackageStreamCtx(ctx context.Context, pkg Package, version string) ([]string, <-chan string, <-chan error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, closedStringChan(), errChanWith(err)
	}
	args := installCommandArgs(resolved.ID, resolved.Source, version)
	out, errCh := runActionStreamForPackage(ctx, resolved.ID, resolved.Source, args...)
	return args, out, errCh
}

func installPackageElevatedStreamCtx(ctx context.Context, pkg Package, version string) ([]string, <-chan string, <-chan error, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, closedStringChan(), errChanWith(err), nil
	}
	args := installCommandArgs(resolved.ID, resolved.Source, version)
	out, errCh, initErr := globalElevator.runCommandElevated(ctx, args...)
	return args, out, errCh, initErr
}

func upgradePackageStreamCtx(ctx context.Context, pkg Package, version string) ([]string, <-chan string, <-chan error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, closedStringChan(), errChanWith(err)
	}
	args := upgradeCommandArgs(resolved.ID, resolved.Source, version)
	out, errCh := runActionStreamForPackage(ctx, resolved.ID, resolved.Source, args...)
	return args, out, errCh
}

func upgradePackageElevatedStreamCtx(ctx context.Context, pkg Package, version string) ([]string, <-chan string, <-chan error, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, closedStringChan(), errChanWith(err), nil
	}
	args := upgradeCommandArgs(resolved.ID, resolved.Source, version)
	out, errCh, initErr := globalElevator.runCommandElevated(ctx, args...)
	return args, out, errCh, initErr
}

// closedStringChan returns an immediately-closed string channel, used by the
// stream wrappers when truncated-ID resolution fails so consumers see a clean
// EOF on stdout while reading the surfaced error from errChan.
func closedStringChan() <-chan string {
	c := make(chan string)
	close(c)
	return c
}

// errChanWith returns a buffered error channel pre-loaded with err and closed,
// so callers receive the error on a single select/range without blocking.
func errChanWith(err error) <-chan error {
	c := make(chan error, 1)
	c <- err
	close(c)
	return c
}

func uninstallPackageStreamCtx(ctx context.Context, pkg Package, allVersions bool) ([]string, <-chan string, <-chan error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, closedStringChan(), errChanWith(err)
	}
	pkg = resolved
	purgeOnUninstall := currentSettings().PurgeOnUninstall
	args := uninstallCommandArgs(pkg, purgeOnUninstall, allVersions)
	outChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		defer close(errChan)

		runAttempt := func(includePurge bool) (string, error, bool) {
			streamOut, streamErr := runActionSmartStreamCtx(ctx, uninstallCommandArgs(pkg, includePurge, allVersions)...)
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

		output, err, aborted := runAttempt(purgeOnUninstall)
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

func uninstallPackageElevatedStreamCtx(ctx context.Context, pkg Package, allVersions bool) ([]string, <-chan string, <-chan error, error) {
	resolved, err := resolveTruncatedPackage(ctx, pkg)
	if err != nil {
		return nil, closedStringChan(), errChanWith(err), nil
	}
	pkg = resolved
	purgeOnUninstall := currentSettings().PurgeOnUninstall
	args := uninstallCommandArgs(pkg, purgeOnUninstall, allVersions)
	outChan := make(chan string)
	errChan := make(chan error, 1)
	go func() {
		defer close(outChan)
		defer close(errChan)

		runAttempt := func(includePurge bool) (string, error, error) {
			streamOut, streamErr, initErr := globalElevator.runCommandElevated(ctx, uninstallCommandArgs(pkg, includePurge, allVersions)...)
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

		output, err, initErr := runAttempt(purgeOnUninstall)
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
