package main

import (
	"reflect"
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
