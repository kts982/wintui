package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadWingetFixture(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", "winget", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseWingetTableFixtures(t *testing.T) {
	t.Run("search with match and source columns", func(t *testing.T) {
		got := parseWingetTable(strings.ReplaceAll(loadWingetFixture(t, "search_match_source.txt"), "\n", "\r\n"))
		want := []Package{
			{Name: "Firefox Developer Edition", ID: "Mozilla.Firefox.DeveloperEdition", Version: "138.0b3", Source: "winget"},
			{Name: "Firefox Beta", ID: "Mozilla.Firefox.Beta", Version: "137.0b9", Source: "winget"},
			{Name: "Mozilla Firefox", ID: "9NZVDKPMR9RD", Version: "Unknown", Source: "msstore"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parseWingetTable(search) = %#v, want %#v", got, want)
		}
	})

	t.Run("installed list with mixed sources and raw identities", func(t *testing.T) {
		got := parseWingetTable(loadWingetFixture(t, "installed_mixed_sources.txt"))
		want := []Package{
			{Name: "Notepad++", ID: "Notepad++.Notepad++", Version: "8.6.4", Source: "winget"},
			{Name: "Notepad++", ID: "MSIX\\NotepadPlusPlus_1.0.0.0_neutral__gabc1234.", Version: "1.0.0.0"},
			{Name: "Microsoft To Do", ID: "9NBLGGH5R558", Version: "Unknown", Source: "msstore"},
			{Name: "Contoso Legacy Tool", ID: "{11111111-2222-3333-4444-555555555555}", Version: "2.4.1"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parseWingetTable(installed mixed) = %#v, want %#v", got, want)
		}
	})

	t.Run("installed list without source column", func(t *testing.T) {
		got := parseWingetTable(loadWingetFixture(t, "installed_no_source.txt"))
		want := []Package{
			{Name: "Legacy Control Panel", ID: "{AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE}", Version: "10.0"},
			{Name: "Notepad++", ID: "MSIX\\NotepadPlusPlus_1.0.0.0_neutral__gabc1234.", Version: "1.0.0.0"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parseWingetTable(installed no source) = %#v, want %#v", got, want)
		}
	})

	t.Run("upgrade list with available versions", func(t *testing.T) {
		got := parseWingetTable(loadWingetFixture(t, "upgrade_available_source.txt"))
		want := []Package{
			{Name: "Microsoft PowerToys", ID: "Microsoft.PowerToys", Version: "0.77.1", Available: "0.78.0", Source: "winget"},
			{Name: "Claude", ID: "Anthropic.Claude", Version: "0.8.2", Available: "0.8.3", Source: "winget"},
			{Name: "Store App", ID: "9WZDNCRFJ3PT", Version: "Unknown", Available: "5.0.0", Source: "msstore"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parseWingetTable(upgrade) = %#v, want %#v", got, want)
		}
	})
}

func TestParseWingetShowFixture(t *testing.T) {
	got := parseWingetShow(strings.ReplaceAll(loadWingetFixture(t, "show_firefox.txt"), "\n", "\r\n"))
	want := PackageDetail{
		Name:          "Mozilla Firefox",
		ID:            "Mozilla.Firefox",
		Version:       "138.0.1",
		Publisher:     "Mozilla",
		PublisherURL:  "https://www.mozilla.org",
		Description:   "Fast, private browsing for everyone.",
		Homepage:      "https://www.mozilla.org/firefox/",
		License:       "MPL-2.0",
		ReleaseNotes:  "Fixed a startup crash on some systems.\nImproved browser performance and memory usage.",
		ReleaseDate:   "2026-03-20",
		Tags:          "browser\nfirefox",
		InstallerType: "wix",
		InstallerURL:  "https://download.mozilla.org/?product=firefox-latest",
		Moniker:       "firefox",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWingetShow() = %#v, want %#v", got, want)
	}
}

func TestCleanWingetOutputFixture(t *testing.T) {
	got := cleanWingetOutput(strings.ReplaceAll(loadWingetFixture(t, "clean_upgrade_output.txt"), "\n", "\r\n"))
	want := "Installer failed with exit code: 1603\nSee log: C:\\Temp\\winget.log"
	if got != want {
		t.Fatalf("cleanWingetOutput() = %q, want %q", got, want)
	}
}

func TestStreamWingetOutputLinesSkipsNoiseButKeepsUsefulStatus(t *testing.T) {
	raw := strings.Join([]string{
		"\r   - \r",
		"This application is licensed to you by its owner.",
		"Microsoft is not responsible for, nor does it grant any licenses to, third-party packages.",
		"Downloading https://example.invalid/installer.exe",
		"Successfully verified installer hash",
		"Starting package install...",
		"Installer failed with exit code: 1603",
	}, "\n")

	got := streamWingetOutputLines(raw)
	want := []string{
		"Downloading https://example.invalid/installer.exe",
		"Installer failed with exit code: 1603",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("streamWingetOutputLines() = %#v, want %#v", got, want)
	}
}

func TestFriendlyWingetErrorMapsKnownUpgradeCode(t *testing.T) {
	err := friendlyWingetError(assertErr("exit status 0x8a15002b"), "", "")
	got := err.Error()
	want := "install technology differs from installed version (package manages its own updates)"
	if got != want {
		t.Fatalf("friendlyWingetError() = %q, want %q", got, want)
	}
}

type fixedErr string

func (e fixedErr) Error() string { return string(e) }

func assertErr(msg string) error { return fixedErr(msg) }

func TestActionCommandArgs(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	t.Run("install uses explicit package source", func(t *testing.T) {
		appSettings = DefaultSettings()
		got := installCommandArgs("Git.Git", "winget")
		want := []string{"install", "--id", "Git.Git", "--exact", "--accept-package-agreements", "--source", "winget"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("installCommandArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("install falls back to configured default source only", func(t *testing.T) {
		appSettings = DefaultSettings()
		appSettings.Source = "msstore"
		appSettings.IncludeUnknown = true
		got := installCommandArgs("Contoso.App", "")
		want := []string{"install", "--id", "Contoso.App", "--exact", "--accept-package-agreements", "--source", "msstore"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("installCommandArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("upgrade uses explicit package source", func(t *testing.T) {
		appSettings = DefaultSettings()
		got := upgradeCommandArgs("GitHub.cli", "winget")
		want := []string{"upgrade", "--id", "GitHub.cli", "--exact", "--accept-package-agreements", "--source", "winget"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("upgradeCommandArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("uninstall never forces source", func(t *testing.T) {
		appSettings = DefaultSettings()
		appSettings.Source = "winget"
		got := uninstallCommandArgs(Package{ID: "Notepad++.Notepad++", Source: "winget"}, false)
		want := []string{"uninstall", "--id", "Notepad++.Notepad++", "--exact"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("uninstallCommandArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("uninstall includes purge only when requested", func(t *testing.T) {
		appSettings = DefaultSettings()
		appSettings.PurgeOnUninstall = true
		got := uninstallCommandArgs(Package{ID: "Contoso.Portable", Source: "winget"}, true)
		want := []string{"uninstall", "--id", "Contoso.Portable", "--exact", "--purge"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("uninstallCommandArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("uninstall retry without purge drops purge flag", func(t *testing.T) {
		appSettings = DefaultSettings()
		appSettings.PurgeOnUninstall = true
		got := uninstallCommandArgs(Package{ID: "Mozilla.Firefox", Source: "winget"}, false)
		want := []string{"uninstall", "--id", "Mozilla.Firefox", "--exact"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("uninstallCommandArgs() = %#v, want %#v", got, want)
		}
	})

	t.Run("uninstall respects silent mode", func(t *testing.T) {
		appSettings = DefaultSettings()
		appSettings.InstallMode = "silent"
		got := uninstallCommandArgs(Package{ID: "Mozilla.Firefox", Source: "winget"}, false)
		want := []string{"uninstall", "--id", "Mozilla.Firefox", "--exact", "--silent"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("uninstallCommandArgs() = %#v, want %#v", got, want)
		}
	})
}
