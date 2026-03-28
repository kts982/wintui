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
		keyEsc.Help(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helpKeys() = %#v, want %#v", got, want)
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
		Name:        "Notepad++",
		ID:          "Notepad++.Notepad++",
		Version:     "8.9.2",
		Source:      "winget",
		Publisher:   "Notepad++ Team",
		Description: "Target detail body",
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
		Name:        "Microsoft Edge",
		ID:          "Microsoft.Edge",
		Version:     "146.0.3856.76",
		Source:      "winget",
		Publisher:   "Microsoft",
		Description: "Target detail body",
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
