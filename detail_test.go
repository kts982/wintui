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
		keyUp.Help(),
		keyDown.Help(),
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

func TestDetailViewShowsInstalledAndTargetDetailsForUpgrade(t *testing.T) {
	p := newDetailPanel()
	p.state = detailReady
	p.pkgID = "Microsoft.Edge"
	p.source = "winget"
	p.allowVersionSelect = true
	p.installedVersion = "146.0.3856.72"
	p.latestVersion = "146.0.3856.78"
	p.detail = PackageDetail{
		Name:        "Microsoft Edge",
		ID:          "Microsoft.Edge",
		Version:     "146.0.3856.78",
		Source:      "winget",
		Publisher:   "Microsoft",
		Description: "Target detail body",
	}
	p.compareDetail = PackageDetail{
		Name:        "Microsoft Edge",
		ID:          "Microsoft.Edge",
		Version:     "146.0.3856.72",
		Source:      "winget",
		Publisher:   "Microsoft",
		Description: "Installed detail body",
	}

	got := p.view(120, 32)
	if !strings.Contains(got, "Installed Version") || !strings.Contains(got, "146.0.3856.72") {
		t.Fatalf("view() = %q, want installed version summary", got)
	}
	if !strings.Contains(got, "Latest Available") || !strings.Contains(got, "146.0.3856.78") {
		t.Fatalf("view() = %q, want latest available summary", got)
	}
	if !strings.Contains(got, "Target Details") || !strings.Contains(got, "Installed Details") {
		t.Fatalf("view() = %q, want target and installed detail sections", got)
	}
	if !strings.Contains(got, "Target detail body") || !strings.Contains(got, "Installed detail body") {
		t.Fatalf("view() = %q, want both detail bodies", got)
	}
}
