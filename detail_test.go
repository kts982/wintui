package main

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestDetailVersionSelectionEmitsSelectedVersion(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.detail = PackageDetail{Name: "Notepad++", ID: "Notepad++.Notepad++"}
	p.pkgID = "Notepad++.Notepad++"
	p.source = "winget"
	p.allowVersionSelect = true
	p.versionsLoading = true

	next, _, handled := p.update(packageVersionsMsg{
		pkgID:    p.pkgID,
		source:   p.source,
		versions: []string{"8.9.3", "8.9.2", "8.9.1"},
	})
	if !handled {
		t.Fatal("expected versions message to be handled")
	}
	if !next.selectingVersion {
		t.Fatal("expected version picker to open")
	}

	next, _, handled = next.update(keyMsg("down"))
	if !handled {
		t.Fatal("expected down key to be handled in version picker")
	}
	if next.versionCursor != 1 {
		t.Fatalf("versionCursor = %d, want 1", next.versionCursor)
	}

	next, cmd, handled := next.update(keyMsg("enter"))
	if !handled {
		t.Fatal("expected enter key to be handled in version picker")
	}
	if cmd == nil {
		t.Fatal("expected selection message command")
	}

	got, ok := cmd().(detailVersionSelectedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want detailVersionSelectedMsg", cmd())
	}
	want := detailVersionSelectedMsg{pkgID: p.pkgID, source: p.source, version: "8.9.2"}
	if got != want {
		t.Fatalf("selection msg = %#v, want %#v", got, want)
	}
}

func TestDetailVersionSelectionCanResetToLatest(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.detail = PackageDetail{Name: "Notepad++", ID: "Notepad++.Notepad++"}
	p.pkgID = "Notepad++.Notepad++"
	p.source = "winget"
	p.allowVersionSelect = true
	p.selectedVersion = "8.9.2"

	next, cmd, handled := p.update(keyMsg("c"))
	if !handled {
		t.Fatal("expected clear key to be handled")
	}
	if cmd == nil {
		t.Fatal("expected selection reset command")
	}
	if next.selectedVersion != "" {
		t.Fatalf("selectedVersion = %q, want empty", next.selectedVersion)
	}

	got, ok := cmd().(detailVersionSelectedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want detailVersionSelectedMsg", cmd())
	}
	want := detailVersionSelectedMsg{pkgID: p.pkgID, source: p.source, version: ""}
	if got != want {
		t.Fatalf("selection msg = %#v, want %#v", got, want)
	}
}

func TestDetailVersionPickerHelpKeys(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.selectingVersion = true

	got := bindingHelps(p.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyEnter.Help(),
		keyUseLatest.Help(),
		keyEscOrLeft.Help(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helpKeys() = %#v, want %#v", got, want)
	}
}

func TestDetailHelpKeysIncludeReleaseNotesOnlyWhenAvailable(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.detail = PackageDetail{
		Homepage:        "https://example.invalid/home",
		ReleaseNotesURL: "https://example.invalid/release-notes",
	}

	got := bindingHelps(p.helpKeys())
	want := []key.Help{
		keyScroll.Help(),
		keyIgnore.Help(),
		keyOverrides.Help(),
		keyOpen.Help(),
		keyReleaseNotes.Help(),
		keyEscOrLeft.Help(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helpKeys() = %#v, want %#v", got, want)
	}

	p.detail.ReleaseNotesURL = ""
	got = bindingHelps(p.helpKeys())
	want = []key.Help{
		keyScroll.Help(),
		keyIgnore.Help(),
		keyOverrides.Help(),
		keyOpen.Help(),
		keyEscOrLeft.Help(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helpKeys() without release notes = %#v, want %#v", got, want)
	}
}

func TestDetailViewShowsTargetSummaryForInstallSelection(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Notepad++.Notepad++"
	p.source = "winget"
	p.allowVersionSelect = true
	p.latestVersion = "8.9.3"
	p.selectedVersion = "8.9.2"
	p.detail = PackageDetail{
		Name:         "Notepad++",
		ID:           "Notepad++.Notepad++",
		Version:      "8.9.2",
		Source:       "winget",
		Publisher:    "Notepad++ Team",
		Description:  "Target detail body",
		ReleaseNotes: "Improved search.\nFixed startup bugs.",
	}

	got := p.view(120, 28)
	if !strings.Contains(got, "Latest Available") || !strings.Contains(got, "8.9.3") {
		t.Fatalf("view() = %q, want latest available summary", got)
	}
	if !strings.Contains(got, "Target Version") || !strings.Contains(got, "8.9.2") {
		t.Fatalf("view() = %q, want target version summary", got)
	}
	if !strings.Contains(got, "Target Details") || !strings.Contains(got, "Target detail body") {
		t.Fatalf("view() = %q, want target detail section", got)
	}
	if !strings.Contains(got, "What's New in 8.9.2") {
		t.Fatalf("view() = %q, want target-specific release heading", got)
	}
}

func TestDetailViewShowsUpgradeSummaryAndTargetDetails(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Microsoft.Edge"
	p.source = "winget"
	p.allowVersionSelect = true
	p.installedVersion = "146.0.3856.72"
	p.latestVersion = "146.0.3856.78"
	p.selectedVersion = "146.0.3856.76"
	p.detail = PackageDetail{
		Name:         "Microsoft Edge",
		ID:           "Microsoft.Edge",
		Version:      "146.0.3856.76",
		Source:       "winget",
		Publisher:    "Microsoft",
		Description:  "Target detail body",
		ReleaseNotes: "Security updates and performance improvements.",
	}

	got := p.view(120, 32)
	if !strings.Contains(got, "Installed Version") || !strings.Contains(got, "146.0.3856.72") {
		t.Fatalf("view() = %q, want installed version summary", got)
	}
	if !strings.Contains(got, "Latest Available") || !strings.Contains(got, "146.0.3856.78") {
		t.Fatalf("view() = %q, want latest available summary", got)
	}
	if !strings.Contains(got, "Target Version") || !strings.Contains(got, "146.0.3856.76") {
		t.Fatalf("view() = %q, want target version summary", got)
	}
	if !strings.Contains(got, "Target Details") || !strings.Contains(got, "Target detail body") {
		t.Fatalf("view() = %q, want target detail section", got)
	}
	if strings.Contains(got, "Installed Details") {
		t.Fatalf("view() = %q, did not expect installed details section", got)
	}
	if !strings.Contains(got, "What's New in 146.0.3856.76") {
		t.Fatalf("view() = %q, want version-specific release heading", got)
	}
}

func TestDetailViewWrapsLongContentWithoutDroppingBorder(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Mozilla.Firefox"
	p.source = "winget"
	p.allowVersionSelect = true
	p.latestVersion = "149.0"
	p.detail = PackageDetail{
		Name:         "Mozilla Firefox",
		ID:           "Mozilla.Firefox",
		Version:      "149.0",
		Source:       "winget",
		Description:  strings.Repeat("This is a long wrapped description segment. ", 10),
		ReleaseNotes: strings.Repeat("Release note line with a long sentence that must wrap cleanly. ", 10),
	}

	got := p.view(72, 18)
	if !strings.Contains(got, "╰") && !strings.Contains(got, "└") {
		t.Fatalf("view() = %q, want closing border to remain visible", got)
	}
}

func TestDetailScrollStopsAtBottom(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.allowVersionSelect = true
	p.windowWidth = 120
	p.windowHeight = 34
	p.detail = PackageDetail{
		Name:         "Mozilla Firefox",
		ID:           "Mozilla.Firefox",
		Version:      "149.0",
		Source:       "winget",
		Description:  strings.Repeat("Long detail paragraph. ", 80),
		ReleaseNotes: strings.Repeat("Release notes line. ", 80),
	}

	for i := 0; i < 100; i++ {
		var handled bool
		p, _, handled = p.update(keyMsg("down"))
		if !handled {
			t.Fatal("expected down key to be handled")
		}
	}

	maxScroll := p.maxScroll()
	if p.scroll != maxScroll {
		t.Fatalf("scroll = %d, want clamped maxScroll %d", p.scroll, maxScroll)
	}

	next, _, handled := p.update(keyMsg("up"))
	if !handled {
		t.Fatal("expected up key to be handled")
	}
	if next.scroll != max(0, maxScroll-1) {
		t.Fatalf("scroll after up = %d, want %d", next.scroll, max(0, maxScroll-1))
	}
}

func TestDetailPgDownAdvancesScroll(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.allowVersionSelect = true
	p.windowWidth = 120
	p.windowHeight = 34
	p.detail = PackageDetail{
		Name:         "Mozilla Firefox",
		ID:           "Mozilla.Firefox",
		Version:      "149.0",
		Source:       "winget",
		Description:  strings.Repeat("Long detail paragraph. ", 80),
		ReleaseNotes: strings.Repeat("Release notes line. ", 80),
	}

	next, _, handled := p.update(keyMsg("pgdown"))
	if !handled {
		t.Fatal("expected pgdown key to be handled")
	}
	if next.scroll != min(8, next.maxScroll()) {
		t.Fatalf("scroll = %d, want %d", next.scroll, min(8, next.maxScroll()))
	}
}

func TestDetailSmallTerminalCanScrollToReleaseNoteTail(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.allowVersionSelect = true
	p.windowWidth = 72
	p.windowHeight = 18
	p.detail = PackageDetail{
		Name:         "Mozilla Firefox",
		ID:           "Mozilla.Firefox",
		Version:      "149.0",
		Source:       "winget",
		Description:  strings.Repeat("Detail paragraph. ", 30),
		ReleaseNotes: strings.Repeat("Release notes body. ", 40) + "TAIL_MARKER_FINAL_LINE",
	}

	for i := 0; i < 200; i++ {
		var handled bool
		p, _, handled = p.update(keyMsg("down"))
		if !handled {
			t.Fatal("expected down key to be handled")
		}
	}

	got := p.view(72, contentAreaHeightForWindow(72, 18, true)-2)
	if !strings.Contains(got, "TAIL_MARKER_FINAL_LINE") {
		t.Fatalf("view() = %q, want final release note line to remain reachable", got)
	}
}

func TestDetailViewShowsReleaseNotesURLWhenPresent(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Mozilla.Firefox"
	p.source = "winget"
	p.detail = PackageDetail{
		Name:            "Mozilla Firefox",
		ID:              "Mozilla.Firefox",
		Version:         "149.0",
		Source:          "winget",
		ReleaseNotesURL: "https://example.invalid/releases/firefox",
	}

	got := p.view(100, 24)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Release Notes") {
		t.Fatalf("view() = %q, want release notes heading", got)
	}
	if !strings.Contains(plain, "https://example.invalid/releases/firefox") {
		t.Fatalf("view() = %q, want release notes URL", got)
	}
}

func TestDetailOpenReleaseNotesUsesNKey(t *testing.T) {
	origOpen := openExternalURL
	var opened string
	openExternalURL = func(url string) { opened = url }
	t.Cleanup(func() { openExternalURL = origOpen })

	p := newDetailPanel()
	p.state = detailReady
	p.detail = PackageDetail{
		ReleaseNotesURL: "https://example.invalid/releases/firefox",
	}

	_, _, handled := p.update(keyMsg("n"))
	if !handled {
		t.Fatal("expected n key to be handled")
	}
	if opened != "https://example.invalid/releases/firefox" {
		t.Fatalf("opened URL = %q, want release notes URL", opened)
	}
}

func TestDetailOverrideEditorOpensWithPKey(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Mozilla.Firefox"
	p.source = "winget"
	p.detail = PackageDetail{Name: "Mozilla Firefox", ID: "Mozilla.Firefox"}

	next, _, handled := p.update(keyMsg("p"))
	if !handled {
		t.Fatal("expected p key to be handled")
	}
	if !next.editingOverrides {
		t.Fatal("expected editingOverrides to be true")
	}
	if next.overrideCursor != 0 {
		t.Fatalf("overrideCursor = %d, want 0", next.overrideCursor)
	}
}

func TestDetailOverrideEditorCyclesValues(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Mozilla.Firefox"
	p.source = "winget"
	p.detail = PackageDetail{Name: "Mozilla Firefox", ID: "Mozilla.Firefox"}
	p.editingOverrides = true
	p.overrideCursor = 1 // scope (0 is ignore)

	next, _, _ := p.update(keyMsg("right"))
	if next.overrideEdit.Scope != "user" {
		t.Fatalf("scope after cycle = %q, want %q", next.overrideEdit.Scope, "user")
	}

	next, _, _ = next.update(keyMsg("right"))
	if next.overrideEdit.Scope != "machine" {
		t.Fatalf("scope after second cycle = %q, want %q", next.overrideEdit.Scope, "machine")
	}

	next, _, _ = next.update(keyMsg("right"))
	if next.overrideEdit.Scope != "" {
		t.Fatalf("scope after wrap = %q, want empty (global)", next.overrideEdit.Scope)
	}
}

func TestDetailOverrideEditorNavigates(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true

	next, _, _ := p.update(keyMsg("down"))
	if next.overrideCursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", next.overrideCursor)
	}

	next, _, _ = next.update(keyMsg("down"))
	if next.overrideCursor != 2 {
		t.Fatalf("cursor after second down = %d, want 2", next.overrideCursor)
	}

	next, _, _ = next.update(keyMsg("down"))
	if next.overrideCursor != 3 {
		t.Fatalf("cursor after third down = %d, want 3", next.overrideCursor)
	}

	next, _, _ = next.update(keyMsg("down"))
	if next.overrideCursor != 3 {
		t.Fatalf("cursor should not exceed max, got %d", next.overrideCursor)
	}

	next, _, _ = next.update(keyMsg("up"))
	if next.overrideCursor != 2 {
		t.Fatalf("cursor after up = %d, want 2", next.overrideCursor)
	}
}

func TestDetailOverrideEditorSaves(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true
	p.overrideEdit = PackageOverride{Scope: "machine"}

	next, cmd, handled := p.update(keyMsg("s"))
	if !handled {
		t.Fatal("expected s key to be handled")
	}
	if next.editingOverrides {
		t.Fatal("expected editor to close after save")
	}
	if !next.overrideSaved {
		t.Fatal("expected overrideSaved flag")
	}
	if cmd == nil {
		t.Fatal("expected save command")
	}

	o := appSettings.getOverride("Test.Pkg", "")
	if o.Scope != "machine" {
		t.Fatalf("saved scope = %q, want %q", o.Scope, "machine")
	}
}

func TestDetailOverrideEditorClears(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()
	appSettings.setOverride("Test.Pkg", "", PackageOverride{Scope: "user"})

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true
	p.overrideEdit = PackageOverride{Scope: "user"}

	next, _, handled := p.update(keyMsg("d"))
	if !handled {
		t.Fatal("expected d key to be handled")
	}
	if next.editingOverrides {
		t.Fatal("expected editor to close after clear")
	}
	if !next.overrideDeleted {
		t.Fatal("expected overrideDeleted flag")
	}
	if appSettings.hasOverride("Test.Pkg", "") {
		t.Fatal("expected override to be removed")
	}
}

func TestDetailOverrideEditorCancels(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true
	p.overrideEdit = PackageOverride{Scope: "machine"}

	next, _, handled := p.update(keyMsg("esc"))
	if !handled {
		t.Fatal("expected esc key to be handled")
	}
	if next.editingOverrides {
		t.Fatal("expected editor to close on esc")
	}
	if appSettings.hasOverride("Test.Pkg", "") {
		t.Fatal("expected no override to be saved on cancel")
	}
}

func TestDetailOverrideEditorHelpKeys(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.editingOverrides = true

	got := bindingHelps(p.helpKeys())
	want := []key.Help{
		keyCycle.Help(),
		keySaveOverrides.Help(),
		keyClearOverrides.Help(),
		keyEscCancel.Help(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helpKeys() in override editor = %#v, want %#v", got, want)
	}
}

func TestDetailViewShowsActiveOverrides(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	elevTrue := true
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		"Mozilla.Firefox": {
			Scope:   "user",
			Elevate: &elevTrue,
		},
	}

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Mozilla.Firefox"
	p.source = "winget"
	p.detail = PackageDetail{
		Name:    "Mozilla Firefox",
		ID:      "Mozilla.Firefox",
		Version: "149.0",
		Source:  "winget",
	}

	got := p.view(100, 28)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Package Rules") {
		t.Fatalf("view() should show override section header, got %q", plain)
	}
	if !strings.Contains(plain, "user") {
		t.Fatalf("view() should show scope override, got %q", plain)
	}
	if !strings.Contains(plain, "always") {
		t.Fatalf("view() should show elevate override, got %q", plain)
	}
}

func TestDetailViewHidesOverridesWhenNone(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Clean.Pkg"
	p.source = "winget"
	p.detail = PackageDetail{
		Name:    "Clean Pkg",
		ID:      "Clean.Pkg",
		Version: "1.0",
		Source:  "winget",
	}

	got := p.view(100, 28)
	plain := stripANSI(got)
	if strings.Contains(plain, "Package Rules") {
		t.Fatalf("view() should not show override section when none set, got %q", plain)
	}
}

func TestDetailCommandPreviewReflectsOverrides(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Scope = "user"
	appSettings.Packages = map[string]PackageOverride{
		"winget:Git.Git": {Scope: "machine"},
	}

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Git.Git"
	p.source = "winget"
	p.allowVersionSelect = true
	p.installedVersion = "2.44.0"
	p.latestVersion = "2.45.0"
	p.detail = PackageDetail{
		Name:    "Git",
		ID:      "Git.Git",
		Version: "2.45.0",
		Source:  "winget",
	}

	got := p.view(120, 30)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Command Preview") {
		t.Fatalf("view() should show Command Preview heading, got %q", plain)
	}
	if !strings.Contains(plain, "winget upgrade") {
		t.Fatalf("view() should show upgrade command, got %q", plain)
	}
	if !strings.Contains(plain, "--id Git.Git") {
		t.Fatalf("view() should include --id Git.Git, got %q", plain)
	}
	if !strings.Contains(plain, "--scope machine") {
		t.Fatalf("view() should apply per-package scope override, got %q", plain)
	}
}

func TestDetailCommandPreviewUsesInstallForSearchResults(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Some.NewPkg"
	p.source = "winget"
	p.allowVersionSelect = true
	p.latestVersion = "1.0.0"
	p.detail = PackageDetail{
		Name:    "Some NewPkg",
		ID:      "Some.NewPkg",
		Version: "1.0.0",
		Source:  "winget",
	}

	got := p.view(120, 30)
	plain := stripANSI(got)
	if !strings.Contains(plain, "winget install") {
		t.Fatalf("view() should show install command for search result, got %q", plain)
	}
}

func TestDetailCommandPreviewHiddenForInstalledOnly(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Installed.Pkg"
	p.source = "winget"
	p.allowVersionSelect = false
	p.detail = PackageDetail{
		Name:    "Installed Pkg",
		ID:      "Installed.Pkg",
		Version: "1.0",
		Source:  "winget",
	}

	got := p.view(120, 30)
	plain := stripANSI(got)
	if strings.Contains(plain, "Command Preview") {
		t.Fatalf("view() should not show Command Preview for installed-only detail, got %q", plain)
	}
}

func TestDetailOverrideEditorRendersAllFields(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true
	p.overrideEdit = PackageOverride{Scope: "user"}

	got := p.view(100, 28)
	plain := stripANSI(got)
	if !strings.Contains(plain, "Package Rules") {
		t.Fatalf("override editor should show title, got %q", plain)
	}
	if !strings.Contains(plain, "Scope") {
		t.Fatalf("override editor should show Scope field, got %q", plain)
	}
	if !strings.Contains(plain, "Architecture") {
		t.Fatalf("override editor should show Architecture field, got %q", plain)
	}
	if !strings.Contains(plain, "Elevate") {
		t.Fatalf("override editor should show Elevate field, got %q", plain)
	}
	if !strings.Contains(plain, "Ignore") {
		t.Fatalf("override editor should show Ignore field, got %q", plain)
	}
}

func TestDetailIgnoreToggleSetsVersionIgnore(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.source = "winget"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.installedVersion = "1.0.0"
	p.latestVersion = "2.0.0"
	p.allowVersionSelect = true

	next, cmd, handled := p.update(keyMsg("i"))
	if !handled {
		t.Fatal("expected i key to be handled")
	}
	if cmd == nil {
		t.Fatal("expected save command")
	}
	_ = next

	o := appSettings.getOverride("Test.Pkg", "winget")
	if o.IgnoreVersion != "2.0.0" {
		t.Fatalf("IgnoreVersion = %q, want %q", o.IgnoreVersion, "2.0.0")
	}
	if o.Ignore {
		t.Fatal("Ignore should be false for version-specific")
	}
}

func TestDetailIgnoreToggleSetsIgnoreAllWhenNotUpgradeable(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Installed.Pkg"
	p.source = "winget"
	p.detail = PackageDetail{Name: "Installed", ID: "Installed.Pkg"}

	p.update(keyMsg("i"))

	o := appSettings.getOverride("Installed.Pkg", "winget")
	if !o.Ignore {
		t.Fatal("expected Ignore = true for non-upgradeable package")
	}
}

func TestDetailIgnoreToggleClearsIgnore(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()
	appSettings.setOverride("Test.Pkg", "", PackageOverride{Ignore: true})

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}

	p.update(keyMsg("i"))

	o := appSettings.getOverride("Test.Pkg", "")
	if o.Ignore {
		t.Fatal("expected Ignore to be cleared after toggle")
	}
}

func TestDetailIgnoreToggleClearsVersionIgnore(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()
	appSettings = DefaultSettings()
	appSettings.setOverride("Test.Pkg", "", PackageOverride{IgnoreVersion: "2.0.0"})

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.installedVersion = "1.0.0"
	p.latestVersion = "2.0.0"

	p.update(keyMsg("i"))

	if appSettings.hasOverride("Test.Pkg", "") {
		t.Fatal("expected override to be removed after clearing version ignore")
	}
}

func TestDetailViewShowsIgnoreStatus(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		"Ignored.Pkg": {Ignore: true},
	}

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Ignored.Pkg"
	p.source = "winget"
	p.detail = PackageDetail{Name: "Ignored", ID: "Ignored.Pkg", Version: "1.0", Source: "winget"}

	got := p.view(100, 28)
	plain := stripANSI(got)
	if !strings.Contains(plain, "all versions") {
		t.Fatalf("view() should show ignore-all status, got %q", plain)
	}
}

func TestDetailViewShowsVersionIgnoreStatus(t *testing.T) {
	original := appSettings
	defer func() { appSettings = original }()

	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		"VersionIgnored.Pkg": {IgnoreVersion: "3.0.0"},
	}

	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "VersionIgnored.Pkg"
	p.source = "winget"
	p.detail = PackageDetail{Name: "VIgnored", ID: "VersionIgnored.Pkg", Version: "2.0", Source: "winget"}

	got := p.view(100, 28)
	plain := stripANSI(got)
	if !strings.Contains(plain, "v3.0.0") {
		t.Fatalf("view() should show version-specific ignore, got %q", plain)
	}
}

func TestDetailOverrideEditorCyclesIgnore(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true
	p.overrideCursor = 0 // ignore field

	next, _, _ := p.update(keyMsg("right"))
	if next.overrideEdit.getValue("ignore") != "all" {
		t.Fatalf("ignore after cycle = %q, want %q", next.overrideEdit.getValue("ignore"), "all")
	}

	next, _, _ = next.update(keyMsg("right"))
	if next.overrideEdit.getValue("ignore") != "" {
		t.Fatalf("ignore after second cycle = %q, want empty", next.overrideEdit.getValue("ignore"))
	}
}

func TestDetailOverrideEditorShowsVersionIgnoreInline(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Test.Pkg"
	p.detail = PackageDetail{Name: "Test", ID: "Test.Pkg"}
	p.editingOverrides = true
	p.overrideEdit = PackageOverride{IgnoreVersion: "3.0.0"}

	got := p.view(100, 28)
	plain := stripANSI(got)
	if !strings.Contains(plain, "v3.0.0") {
		t.Fatalf("override editor should show version ignore inline, got %q", plain)
	}
}

func TestDetailHelpKeysIncludeIgnoreForUpgradeable(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.allowVersionSelect = true
	p.detail = PackageDetail{}

	got := bindingHelps(p.helpKeys())
	found := false
	for _, h := range got {
		if h == keyIgnore.Help() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("helpKeys() should include ignore binding for upgradeable, got %#v", got)
	}
}
