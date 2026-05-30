package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/glamour/v2"
)

func TestMappedGlamourStylesAreValid(t *testing.T) {
	// Every style our theme mapping can emit must be a real glamour style;
	// otherwise renderReleaseNotesMarkdown silently falls back to raw text.
	seen := map[string]bool{}
	for _, id := range append([]string{"default", "dracula", "tokyonight", "mono"}, themeOrder...) {
		seen[glamourStyleForTheme(id)] = true
	}
	seen["notty"] = true // piped fallback
	for style := range seen {
		if _, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style)); err != nil {
			t.Errorf("glamour style %q is not valid: %v", style, err)
		}
	}
}

func TestRenderNotesWithMarkdownText(t *testing.T) {
	var buf bytes.Buffer
	d := PackageDetail{
		ID:           "Test.Pkg",
		Name:         "Test Pkg",
		Version:      "2.0",
		ReleaseNotes: "## What's New\n\n- Faster startup\n- Fixed crash",
	}
	if err := renderNotes(d, false, &buf); err != nil {
		t.Fatalf("renderNotes: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Test Pkg 2.0") {
		t.Errorf("expected header with name+version, got:\n%s", out)
	}
	// glamour (notty style in tests) preserves the note text.
	if !strings.Contains(out, "Faster startup") || !strings.Contains(out, "Fixed crash") {
		t.Errorf("expected rendered note body, got:\n%s", out)
	}
}

func TestRenderNotesURLFallback(t *testing.T) {
	var buf bytes.Buffer
	d := PackageDetail{ID: "Test.Pkg", Name: "Test Pkg", ReleaseNotesURL: "https://example.invalid/notes"}
	if err := renderNotes(d, false, &buf); err != nil {
		t.Fatalf("renderNotes: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No release notes in the winget manifest") {
		t.Errorf("expected URL-fallback message, got:\n%s", out)
	}
	if !strings.Contains(out, "https://example.invalid/notes") {
		t.Errorf("expected the URL, got:\n%s", out)
	}
}

func TestRenderNotesNoneAvailable(t *testing.T) {
	var buf bytes.Buffer
	if err := renderNotes(PackageDetail{ID: "Test.Pkg", Name: "Test Pkg"}, false, &buf); err != nil {
		t.Fatalf("renderNotes: %v", err)
	}
	if !strings.Contains(buf.String(), "No release notes available") {
		t.Errorf("expected none-available message, got:\n%s", buf.String())
	}
}

func TestRenderNotesJSON(t *testing.T) {
	var buf bytes.Buffer
	d := PackageDetail{ID: "Test.Pkg", Source: "winget", Version: "2.0", ReleaseNotes: "hello", ReleaseNotesURL: "u"}
	if err := renderNotes(d, true, &buf); err != nil {
		t.Fatalf("renderNotes json: %v", err)
	}
	var got notesOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got.ID != "Test.Pkg" || got.ReleaseNotes != "hello" || got.ReleaseNotesURL != "u" {
		t.Errorf("json payload mismatch: %+v", got)
	}
}

func TestRunNotesInvalidSourceErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := runNotes("Foo.Bar", "private", false, &buf); err == nil {
		t.Fatal("expected error for unknown source, got nil")
	}
}

func TestRunNotesUsesFetchSeam(t *testing.T) {
	saved := notesFetchFn
	defer func() { notesFetchFn = saved }()

	var gotID, gotSource string
	notesFetchFn = func(_ context.Context, id, source string) (PackageDetail, error) {
		gotID, gotSource = id, source
		return PackageDetail{ID: id, Source: source, Name: "Stub", Version: "9.9", ReleaseNotes: "- Synthetic note"}, nil
	}

	var buf bytes.Buffer
	if err := runNotes("Test.Pkg", "", false, &buf); err != nil {
		t.Fatalf("runNotes: %v", err)
	}
	// Empty --source defaults to winget before the fetch.
	if gotID != "Test.Pkg" || gotSource != "winget" {
		t.Errorf("fetch got id=%q source=%q, want Test.Pkg / winget", gotID, gotSource)
	}
	if !strings.Contains(buf.String(), "Stub 9.9") || !strings.Contains(buf.String(), "Synthetic note") {
		t.Errorf("expected stubbed detail in output, got:\n%s", buf.String())
	}
}

func TestPrintCheckWithNotesRendersEachPackage(t *testing.T) {
	saved := notesFetchFn
	defer func() { notesFetchFn = saved }()
	notesFetchFn = func(_ context.Context, id, _ string) (PackageDetail, error) {
		return PackageDetail{ID: id, Version: "9", ReleaseNotes: "- note for " + id}, nil
	}

	pkgs := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "1.0", Available: "2.0", Source: "winget"},
		{Name: "Notepad", ID: "Np.Np", Version: "8", Available: "9", Source: "winget"},
	}
	var buf bytes.Buffer
	printCheckWithNotes(context.Background(), pkgs, &buf)
	out := buf.String()

	for _, want := range []string{
		"Mozilla.Firefox", "1.0 → 2.0", "note for Mozilla.Firefox",
		"Np.Np", "8 → 9", "note for Np.Np",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check --notes output missing %q\n%s", want, out)
		}
	}
}

func TestPrintCheckWithNotesEmpty(t *testing.T) {
	var buf bytes.Buffer
	printCheckWithNotes(context.Background(), nil, &buf)
	if !strings.Contains(buf.String(), "up to date") {
		t.Errorf("expected up-to-date message, got: %q", buf.String())
	}
}

func TestPrintCheckWithNotesContinuesPastFetchError(t *testing.T) {
	saved := notesFetchFn
	defer func() { notesFetchFn = saved }()
	calls := 0
	notesFetchFn = func(_ context.Context, id, _ string) (PackageDetail, error) {
		calls++
		if id == "Bad.Pkg" {
			return PackageDetail{}, errNotesPackageNotFound
		}
		return PackageDetail{ID: id, Version: "2", ReleaseNotes: "good notes"}, nil
	}

	pkgs := []Package{
		{ID: "Bad.Pkg", Version: "1", Available: "2", Source: "winget"},
		{ID: "Good.Pkg", Version: "1", Available: "2", Source: "winget"},
	}
	var buf bytes.Buffer
	printCheckWithNotes(context.Background(), pkgs, &buf)
	out := buf.String()

	if !strings.Contains(out, "couldn't load notes") {
		t.Errorf("expected a per-package error line, got:\n%s", out)
	}
	if !strings.Contains(out, "good notes") {
		t.Errorf("a fetch error must not stop later packages, got:\n%s", out)
	}
	if calls != 2 {
		t.Errorf("expected both packages attempted, got %d calls", calls)
	}
}

func TestGlamourStyleForTheme(t *testing.T) {
	cases := map[string]string{
		"default":    "pink",
		"dracula":    "dracula",
		"tokyonight": "tokyo-night",
		"mono":       "ascii",
		"nord":       "dark",
		"wintui":     "dark",
		"catppuccin": "dark",
		"ember":      "dark",
	}
	for theme, want := range cases {
		if got := glamourStyleForTheme(theme); got != want {
			t.Errorf("glamourStyleForTheme(%q) = %q, want %q", theme, got, want)
		}
	}
}

func TestStyleNotesHeaderPlainWhenNotTTY(t *testing.T) {
	// Tests don't run on a terminal, so the header must come back unstyled —
	// guaranteeing piped output carries no ANSI escapes.
	const h = "══ X ══"
	if got := styleNotesHeader(h); got != h {
		t.Errorf("expected plain header off-TTY, got %q", got)
	}
}

func TestRunNotesNotFoundSurfacesClearError(t *testing.T) {
	saved := notesFetchFn
	defer func() { notesFetchFn = saved }()
	notesFetchFn = func(_ context.Context, _, _ string) (PackageDetail, error) {
		return PackageDetail{}, errNotesPackageNotFound
	}

	var buf bytes.Buffer
	err := runNotes("No.Such.Pkg", "", false, &buf)
	if err == nil {
		t.Fatal("expected an error for an unknown package, got nil")
	}
	if !strings.Contains(err.Error(), "no package found") {
		t.Errorf("expected a not-found error, got: %v", err)
	}
	// Must NOT claim the package simply has no notes.
	if strings.Contains(buf.String(), "No release notes") {
		t.Errorf("not-found must not be reported as 'no release notes', got:\n%s", buf.String())
	}
}
