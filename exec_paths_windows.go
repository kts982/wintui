package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// This file pins the external binaries WinTUI spawns to absolute paths so we
// run exactly the binaries we intend (especially from the elevated helper)
// rather than whatever an earlier PATH entry resolves to. Note this is about
// pinning, not CWD: since Go 1.19 exec.Command already refuses CWD-relative
// resolution (ErrDot).

// powershellExePath returns the absolute path of Windows PowerShell 5.1,
// which ships with Windows at a fixed System32 location.
func powershellExePath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

var (
	wingetPathOnce sync.Once
	wingetPath     string
	wingetPathErr  error
)

// wingetExePath resolves winget via PATH once per process and validates that
// the result lives under a location Windows actually installs winget to.
func wingetExePath() (string, error) {
	wingetPathOnce.Do(func() {
		wingetPath, wingetPathErr = resolveWingetPath()
	})
	return wingetPath, wingetPathErr
}

func resolveWingetPath() (string, error) {
	path, err := lookPathWinget()
	if err == nil {
		return path, nil
	}
	// The elevated helper can run in a profile with no winget alias yet:
	// under Administrator protection the hidden admin account's WindowsApps
	// alias only appears some time after its first elevations. App Installer's
	// own package directory is admin-protected and passes the allowlist.
	if pkgPath, ok := appInstallerWingetPath(os.Getenv("ProgramFiles"), runtime.GOARCH); ok {
		return pkgPath, nil
	}
	return "", err
}

func lookPathWinget() (string, error) {
	path, err := exec.LookPath("winget")
	if err != nil {
		return "", fmt.Errorf("winget not found on PATH: %w", err)
	}
	if abs, absErr := filepath.Abs(path); absErr == nil {
		path = abs
	}
	if !wingetPathAllowed(path, os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles")) {
		return "", fmt.Errorf("refusing winget at %q: not under a known system app location", path)
	}
	return path, nil
}

// appInstallerPackagePrefix and appInstallerPackageSuffix bound the App
// Installer package directory names under %ProgramFiles%\WindowsApps:
// Microsoft.DesktopAppInstaller_<version>_<arch>__8wekyb3d8bbwe, where the
// suffix is Microsoft's publisher ID.
const (
	appInstallerPackagePrefix = "Microsoft.DesktopAppInstaller_"
	appInstallerPackageSuffix = "__8wekyb3d8bbwe"
)

// appInstallerWingetPath returns winget.exe inside the newest App Installer
// package directory, preferring the package built for goarch. Listing
// WindowsApps needs admin rights, so this only succeeds elevated.
func appInstallerWingetPath(programFiles, goarch string) (string, bool) {
	if programFiles == "" {
		return "", false
	}
	root := filepath.Join(programFiles, "WindowsApps")
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	for _, name := range rankAppInstallerDirs(names, goarch) {
		path := filepath.Join(root, name, "winget.exe")
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path, true
		}
	}
	return "", false
}

// rankAppInstallerDirs filters names to App Installer package directories and
// orders them best first: the package arch matching goarch before any other,
// then newest version. Names with an unparseable version are dropped.
func rankAppInstallerDirs(names []string, goarch string) []string {
	type candidate struct {
		name      string
		version   []int
		archMatch bool
	}
	wantArch := map[string]string{"amd64": "x64", "arm64": "arm64", "386": "x86"}[goarch]
	var cands []candidate
	for _, name := range names {
		if !strings.HasPrefix(name, appInstallerPackagePrefix) || !strings.HasSuffix(name, appInstallerPackageSuffix) {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, appInstallerPackagePrefix), appInstallerPackageSuffix), "_")
		if len(parts) != 2 {
			continue
		}
		version, ok := parseDottedVersion(parts[0])
		if !ok {
			continue
		}
		cands = append(cands, candidate{name: name, version: version, archMatch: parts[1] == wantArch})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].archMatch != cands[j].archMatch {
			return cands[i].archMatch
		}
		return compareDottedVersions(cands[i].version, cands[j].version) > 0
	})
	ranked := make([]string, len(cands))
	for i, c := range cands {
		ranked[i] = c.name
	}
	return ranked
}

func parseDottedVersion(s string) ([]int, bool) {
	fields := strings.Split(s, ".")
	version := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		version[i] = n
	}
	return version, true
}

func compareDottedVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// wingetPathAllowed reports whether path is a descendant of one of the two
// locations winget ships from: the per-user app execution aliases
// (%LOCALAPPDATA%\Microsoft\WindowsApps) or the packaged-app store
// (%ProgramFiles%\WindowsApps).
func wingetPathAllowed(path, localAppData, programFiles string) bool {
	var roots []string
	if localAppData != "" {
		roots = append(roots, filepath.Join(localAppData, "Microsoft", "WindowsApps"))
	}
	if programFiles != "" {
		roots = append(roots, filepath.Join(programFiles, "WindowsApps"))
	}
	for _, root := range roots {
		if pathIsDescendant(path, root) {
			return true
		}
	}
	return false
}

// pathIsDescendant reports whether path sits strictly below root. Comparison
// is case-insensitive (Windows filesystems are) and rejects the root itself,
// siblings that merely share a name prefix, and anything reached via "..".
func pathIsDescendant(path, root string) bool {
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(root)), strings.ToLower(filepath.Clean(path)))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
