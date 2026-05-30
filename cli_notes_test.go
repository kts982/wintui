package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

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

	var gotPkg Package
	notesFetchFn = func(_ context.Context, pkg Package, _ string) (PackageDetail, error) {
		gotPkg = pkg
		return PackageDetail{ID: pkg.ID, Source: pkg.Source, Name: "Stub", Version: "9.9", ReleaseNotes: "- Synthetic note"}, nil
	}

	var buf bytes.Buffer
	if err := runNotes("Test.Pkg", "", false, &buf); err != nil {
		t.Fatalf("runNotes: %v", err)
	}
	// Empty --source defaults to winget before the fetch.
	if gotPkg.ID != "Test.Pkg" || gotPkg.Source != "winget" {
		t.Errorf("fetch got %+v, want ID=Test.Pkg Source=winget", gotPkg)
	}
	if !strings.Contains(buf.String(), "Stub 9.9") || !strings.Contains(buf.String(), "Synthetic note") {
		t.Errorf("expected stubbed detail in output, got:\n%s", buf.String())
	}
}
