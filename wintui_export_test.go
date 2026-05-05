package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ── Envelope build + round-trip ───────────────────────────────────────

func TestBuildExportEnvelopeOmitsVersionsByDefault(t *testing.T) {
	installed := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0", Source: "winget"},
		{Name: "VS Code", ID: "Microsoft.VisualStudioCode", Version: "1.90", Source: "winget"},
	}
	env := buildExportEnvelope(installed, false, time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	if env.Version != exportEnvelopeVersion {
		t.Errorf("Version = %d, want %d", env.Version, exportEnvelopeVersion)
	}
	if len(env.Packages) != 2 {
		t.Fatalf("Packages len = %d, want 2", len(env.Packages))
	}
	for _, p := range env.Packages {
		if p.Version != "" {
			t.Errorf("Version field should be omitted by default; %q has %q", p.ID, p.Version)
		}
	}
}

func TestBuildExportEnvelopeIncludesVersionsWhenAsked(t *testing.T) {
	installed := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0", Source: "winget"},
	}
	env := buildExportEnvelope(installed, true, time.Now())
	if env.Packages[0].Version != "128.0" {
		t.Errorf("with-versions should preserve version, got %q", env.Packages[0].Version)
	}
}

func TestExportEnvelopeJSONRoundTripsThroughLoadImportFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")

	env := buildExportEnvelope([]Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0", Source: "winget"},
		{Name: "Git", ID: "Git.Git", Version: "2.45", Source: "winget"},
	}, true, time.Now())
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	pkgs, err := loadImportFile(path, nil)
	if err != nil {
		t.Fatalf("loadImportFile: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d pkgs, want 2", len(pkgs))
	}
	if pkgs[0].ID != "Mozilla.Firefox" || pkgs[1].ID != "Git.Git" {
		t.Errorf("envelope ordering not preserved: %v", pkgs)
	}
}

// ── Backward compat with raw-array form ────────────────────────────────

func TestLoadImportFileAcceptsLegacyRawArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	raw := []byte(`[{"name":"Git","id":"Git.Git","version":"2.45","source":"winget"}]`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, err := loadImportFile(path, nil)
	if err != nil {
		t.Fatalf("loadImportFile: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].ID != "Git.Git" {
		t.Errorf("got %v, want single Git.Git", pkgs)
	}
}

func TestLoadImportFileRejectsUnknownEnvelopeVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future.json")
	raw := []byte(`{"version":99,"packages":[]}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadImportFile(path, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported export version") {
		t.Errorf("expected unsupported-version error, got %v", err)
	}
}

func TestLoadImportFileRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImportFile(path, nil); err == nil {
		t.Error("expected error on non-JSON input")
	}
}

func TestLoadImportFileRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadImportFile(path, nil); err == nil {
		t.Error("expected error on empty file")
	}
}

// ── Name normalization + collision detection ──────────────────────────

func TestNormalizePackageName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Git", "git"},
		{"  Git  ", "git"},
		{"Microsoft  Git", "microsoft git"},
		{"\tGit\nfor\tWindows ", "git for windows"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizePackageName(tc.in); got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFindNameCollisionsExactMatchDifferentID(t *testing.T) {
	pkg := importPkg{Name: "Git", ID: "Git.Git"}
	installed := []Package{
		{Name: "Git", ID: "Microsoft.Git"},
		{Name: "Microsoft Git", ID: "Microsoft.GitOther"},
	}
	hits := findNameCollisions(pkg, installed)
	want := []string{"Microsoft.Git"}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("collisions = %v, want %v", hits, want)
	}
}

func TestFindNameCollisionsIgnoresSameID(t *testing.T) {
	pkg := importPkg{Name: "Git", ID: "Git.Git"}
	installed := []Package{
		{Name: "Git", ID: "git.git"}, // case-insensitive same ID
	}
	if hits := findNameCollisions(pkg, installed); len(hits) != 0 {
		t.Errorf("expected no collisions for same-ID hit, got %v", hits)
	}
}

func TestFindNameCollisionsIgnoresEmptyName(t *testing.T) {
	pkg := importPkg{Name: "", ID: "Some.ID"}
	installed := []Package{
		{Name: "", ID: "Other.ID"},
	}
	if hits := findNameCollisions(pkg, installed); len(hits) != 0 {
		t.Errorf("empty names should not collide, got %v", hits)
	}
}

func TestLoadImportFilePopulatesCollisions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	raw := []byte(`[{"name":"Git","id":"Git.Git","version":"2.45","source":"winget"}]`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	installed := []Package{
		{Name: "Git", ID: "Microsoft.Git", Version: "2.46", Source: "winget"},
	}
	pkgs, err := loadImportFile(path, installed)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs[0].Collisions) != 1 || pkgs[0].Collisions[0] != "Microsoft.Git" {
		t.Errorf("expected Microsoft.Git collision, got %v", pkgs[0].Collisions)
	}
}

// ── planImport partitioning ───────────────────────────────────────────

func TestPlanImportFourCategories(t *testing.T) {
	pkgs := []importPkg{
		{Name: "Firefox", ID: "Mozilla.Firefox"},                             // installable
		{Name: "VS Code", ID: "Microsoft.VisualStudioCode", Installed: true}, // installed
		{Name: "Raw", ID: "MSIX\\Foo_hash", NonCanonical: true},              // non-canonical
		{Name: "Git", ID: "Git.Git", Collisions: []string{"Microsoft.Git"}},  // collision
	}
	plan := planImport(pkgs, false)
	if len(plan.WillInstall) != 1 || plan.WillInstall[0].ID != "Mozilla.Firefox" {
		t.Errorf("WillInstall = %v", plan.WillInstall)
	}
	if len(plan.AlreadyInstalled) != 1 || plan.AlreadyInstalled[0].ID != "Microsoft.VisualStudioCode" {
		t.Errorf("AlreadyInstalled = %v", plan.AlreadyInstalled)
	}
	if len(plan.NonCanonical) != 1 {
		t.Errorf("NonCanonical = %v", plan.NonCanonical)
	}
	if len(plan.ReviewNeeded) != 1 || plan.ReviewNeeded[0].ID != "Git.Git" {
		t.Errorf("ReviewNeeded = %v", plan.ReviewNeeded)
	}
}

func TestPlanImportAllPromotesCollisionsToWillInstall(t *testing.T) {
	pkgs := []importPkg{
		{Name: "Git", ID: "Git.Git", Collisions: []string{"Microsoft.Git"}},
	}
	plan := planImport(pkgs, true)
	if len(plan.WillInstall) != 1 {
		t.Errorf("with --all, collision should land in WillInstall: %v", plan)
	}
	if len(plan.ReviewNeeded) != 0 {
		t.Errorf("ReviewNeeded should be empty with --all: %v", plan.ReviewNeeded)
	}
}

// ── Dry-run formatting smoke tests ─────────────────────────────────────

func TestPrintImportPlanIncludesAllSectionsThatHaveContent(t *testing.T) {
	plan := importPlan{
		WillInstall:      []importPkg{{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"}},
		AlreadyInstalled: []importPkg{{Name: "VS Code", ID: "Microsoft.VisualStudioCode"}},
		ReviewNeeded:     []importPkg{{Name: "Git", ID: "Git.Git", Collisions: []string{"Microsoft.Git"}}},
		NonCanonical:     []importPkg{{Name: "Raw", ID: "MSIX\\Foo"}},
	}
	var buf bytes.Buffer
	printImportPlan(&buf, plan, importOptions{DryRun: true})
	out := buf.String()

	for _, want := range []string{
		"dry-run",
		"Will install (1)",
		"Mozilla.Firefox",
		"Already installed (1, skipped)",
		"Review needed",
		"Microsoft.Git",
		"--all",
		"Non-canonical (1, can't restore)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestPrintImportPlanOmitsEmptySections(t *testing.T) {
	plan := importPlan{
		WillInstall: []importPkg{{Name: "Firefox", ID: "Mozilla.Firefox"}},
	}
	var buf bytes.Buffer
	printImportPlan(&buf, plan, importOptions{})
	out := buf.String()

	for _, unwanted := range []string{
		"Already installed",
		"Review needed",
		"Non-canonical",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("plan output should not include %q when section is empty\n%s", unwanted, out)
		}
	}
}

// ── Export CLI shape ──────────────────────────────────────────────────

func TestWriteExportToFile(t *testing.T) {
	pkgs := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Version: "128.0", Source: "winget"},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	var stdout bytes.Buffer
	if err := writeExport(pkgs, exportOptions{Output: path}, &stdout); err != nil {
		t.Fatalf("writeExport: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Contains(data, []byte("Mozilla.Firefox")) {
		t.Errorf("exported file missing package, got:\n%s", data)
	}
	if !bytes.Contains(data, []byte(`"version": 1`)) {
		t.Errorf("exported file missing envelope version field:\n%s", data)
	}
	if !strings.Contains(stdout.String(), "Exported 1 package") {
		t.Errorf("stdout summary missing, got %q", stdout.String())
	}
}

// Regression: the previous YYYY-MM-DD-only filename silently overwrote
// same-day exports. The new timestamp suffix differentiates back-to-back
// runs in the same minute.
func TestExportDestinationPathHasTimestampSuffix(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	t1 := time.Date(2026, 5, 5, 14, 30, 22, 0, time.UTC)
	t2 := t1.Add(time.Second) // one second later

	p1, err := exportDestinationPath(t1)
	if err != nil {
		t.Fatalf("path 1: %v", err)
	}
	p2, err := exportDestinationPath(t2)
	if err != nil {
		t.Fatalf("path 2: %v", err)
	}
	if p1 == p2 {
		t.Errorf("paths must differ across calls one second apart, got %q twice", p1)
	}
	if !strings.Contains(p1, "143022") {
		t.Errorf("path %q should include HHMMSS suffix", p1)
	}
}

// Regression: non-canonical IDs (MSIX paths, GUID-form, package-family
// `Foo_hash` patterns) often report idTruncated=true on narrow pipe
// widths but can't be resolved against the winget catalog by design.
// They must be kept in the export — the import side filters them as
// "non-canonical (can't restore)" regardless. Earlier code dropped
// 100+ of them silently when the live export shipped on a real box.
func TestResolveTruncatedForExportKeepsNonCanonicalTruncated(t *testing.T) {
	pkgs := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox"}, // canonical, not truncated
		{Name: "MSIX Truncated",
			ID: "MSIX\\Microsoft.Photos_8wekyb3d8bbwe", idTruncated: true},
		{Name: "FamilyName Truncated",
			ID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe", idTruncated: true},
		{Name: "GUID Truncated",
			ID: "{12345678-1234-1234-1234-123456789012}", idTruncated: true},
	}
	kept, dropped := resolveTruncatedForExport(context.Background(), pkgs)
	if len(kept) != 4 {
		t.Errorf("expected all 4 entries kept (3 non-canonical + 1 canonical), got %d (kept=%v)", len(kept), kept)
	}
	if len(dropped) != 0 {
		t.Errorf("non-canonical truncated entries should not be dropped, got dropped=%v", dropped)
	}
}

func TestWriteExportToStdout(t *testing.T) {
	pkgs := []Package{
		{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"},
	}
	var stdout bytes.Buffer
	if err := writeExport(pkgs, exportOptions{}, &stdout); err != nil {
		t.Fatalf("writeExport: %v", err)
	}
	out := stdout.String()
	var env exportEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("stdout is not valid envelope JSON: %v\n%s", err, out)
	}
	if len(env.Packages) != 1 || env.Packages[0].ID != "Mozilla.Firefox" {
		t.Errorf("stdout envelope = %v", env.Packages)
	}
}
