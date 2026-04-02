package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSummaryPanelRendersPackageInfo(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(40, 20)

	pkg := Package{Name: "Node.js", ID: "OpenJS.NodeJS.22", Version: "22.21.0", Available: "22.22.2", Source: "winget"}
	p.focus(&pkg, "22.21.0", "")

	got := p.view()
	if !strings.Contains(got, "Node.js") {
		t.Errorf("view should contain package name, got: %s", got)
	}
	if !strings.Contains(got, "OpenJS.NodeJS.22") {
		t.Errorf("view should contain package ID, got: %s", got)
	}
	if !strings.Contains(got, "22.21.0") {
		t.Errorf("view should contain installed version, got: %s", got)
	}
	if !strings.Contains(got, "22.22.2") {
		t.Errorf("view should contain available version, got: %s", got)
	}
}

func TestSummaryPanelRendersEmptyState(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(40, 20)

	got := p.view()
	if !strings.Contains(got, "No package selected") {
		t.Errorf("empty view should show placeholder, got: %s", got)
	}
}

func TestSummaryPanelTinySize(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(2, 2)
	got := p.view()
	if got != "" {
		t.Errorf("expected empty string for tiny panel, got: %q", got)
	}
}

func TestSummaryPanelFocusNilClearsState(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(40, 20)

	pkg := Package{Name: "Foo", ID: "Foo.Bar", Source: "winget"}
	p.focus(&pkg, "", "")

	p.focus(nil, "", "")
	if p.pkg != nil {
		t.Fatal("focus(nil) should clear pkg")
	}
	if p.loading {
		t.Fatal("focus(nil) should clear loading")
	}
}

func TestSummaryPanelSamePackageSkipsFetch(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(40, 20)

	pkg := Package{Name: "Foo", ID: "Foo.Bar", Source: "winget", Version: "1.0"}
	cmd1 := p.focus(&pkg, "1.0", "")
	if cmd1 == nil {
		t.Fatal("first focus should return a tick cmd")
	}

	cmd2 := p.focus(&pkg, "1.0", "2.0")
	if cmd2 != nil {
		t.Fatal("same package focus should not return a cmd")
	}
	if p.target != "2.0" {
		t.Fatal("target should be updated")
	}
}

func TestSummaryPanelAsyncFetchFlow(t *testing.T) {
	fetched := false
	fakeFetch := func(ctx context.Context, id, source, version string) (PackageDetail, error) {
		fetched = true
		return PackageDetail{Publisher: "TestPub", License: "MIT"}, nil
	}
	p := newSummaryPanelWith(fakeFetch)
	p.setSize(40, 20)

	pkg := Package{Name: "Foo", ID: "Foo.Bar", Source: "winget"}
	p.focus(&pkg, "", "")

	// Simulate the debounce tick.
	cmd := p.update(summaryFetchTickMsg{fetchID: "Foo.Bar|winget"})
	if cmd == nil {
		t.Fatal("expected fetch cmd after tick")
	}

	// Execute the cmd synchronously.
	msg := cmd()
	p.update(msg)

	if !fetched {
		t.Fatal("fetch function was not called")
	}
	if p.detail == nil {
		t.Fatal("detail should be set after fetch")
	}
	if p.detail.Publisher != "TestPub" {
		t.Fatalf("publisher = %q, want TestPub", p.detail.Publisher)
	}
	if p.loading {
		t.Fatal("loading should be false after fetch")
	}
}

func TestSummaryPanelStaleResultDiscarded(t *testing.T) {
	p := newSummaryPanelWith(func(ctx context.Context, id, source, version string) (PackageDetail, error) {
		return PackageDetail{Publisher: "Stale"}, nil
	})
	p.setSize(40, 20)

	pkg1 := Package{Name: "Foo", ID: "Foo.Bar", Source: "winget"}
	p.focus(&pkg1, "", "")

	// Switch to a different package before the fetch completes.
	pkg2 := Package{Name: "Baz", ID: "Baz.Qux", Source: "winget"}
	p.focus(&pkg2, "", "")

	// Deliver the stale result for pkg1.
	p.update(summaryDetailMsg{fetchID: "Foo.Bar|winget", detail: PackageDetail{Publisher: "Stale"}})

	if p.detail != nil {
		t.Fatal("stale result should be discarded")
	}
}

func TestSummaryPanelFetchError(t *testing.T) {
	p := newSummaryPanelWith(func(ctx context.Context, id, source, version string) (PackageDetail, error) {
		return PackageDetail{}, fmt.Errorf("network error")
	})
	p.setSize(40, 20)

	pkg := Package{Name: "Foo", ID: "Foo.Bar", Source: "winget"}
	p.focus(&pkg, "", "")

	cmd := p.update(summaryFetchTickMsg{fetchID: "Foo.Bar|winget"})
	msg := cmd()
	p.update(msg)

	if p.err == nil {
		t.Fatal("expected error to be set")
	}
	if p.loading {
		t.Fatal("loading should be false after error")
	}
	got := p.view()
	if !strings.Contains(got, "Details unavailable") {
		t.Errorf("error view should show unavailable message, got: %s", got)
	}
}

func TestSummaryPanelArpPackageSkipsFetch(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(40, 20)

	pkg := Package{Name: "Some App", ID: "Some.App", Source: ""}
	cmd := p.focus(&pkg, "1.0", "")

	if cmd != nil {
		t.Fatal("ARP package should not trigger a fetch command")
	}
	if p.loading {
		t.Fatal("loading should be false for ARP package")
	}
	if !p.noFetch {
		t.Fatal("noFetch should be true for ARP package")
	}
	got := p.view()
	if !strings.Contains(got, "managed outside winget") {
		t.Errorf("view should show non-winget message, got: %s", got)
	}
}

func TestSummaryPanelContentClippedToHeight(t *testing.T) {
	p := newSummaryPanel()
	p.setSize(40, 10) // small height

	pkg := Package{Name: "Test", ID: "Test.Pkg", Version: "1.0", Source: "winget"}
	p.focus(&pkg, "1.0", "")
	// Simulate fetched detail with long description.
	p.loading = false
	p.detail = &PackageDetail{
		Publisher:   "Test Publisher",
		License:     "MIT",
		Homepage:    "https://example.com",
		Description: "This is a very long description that should be truncated when the panel height is too small to show everything including all the metadata fields and the full description text.",
	}

	got := p.view()
	lines := strings.Split(got, "\n")
	// Total rendered lines should not exceed panel height.
	if len(lines) > p.height {
		t.Errorf("rendered %d lines, but panel height is %d", len(lines), p.height)
	}
}

func TestWordWrap(t *testing.T) {
	input := "This is a test of the word wrapping function that should break lines"
	got := wordWrap(input, 20)
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 20 {
			t.Errorf("line too long (%d): %q", len(line), line)
		}
	}
}

func TestWordWrapZeroWidth(t *testing.T) {
	got := wordWrap("hello world", 0)
	if got != "hello world" {
		t.Errorf("wordWrap with 0 width should return input, got: %q", got)
	}
}
