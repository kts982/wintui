package main

import (
	"reflect"
	"strings"
	"testing"
)

// Issue #44 reproduction: an Italian winget upgrade output. Before the
// schema-aware parser, parseWingetTable returned nil silently because the
// English-only colNames lookup found ≤2 columns. The fix uses the vendored
// locale dictionary and recognises Nome/Id/Versione/Disponibile/Origine.
func TestParseWingetTableLocalizedItalianUpgrade(t *testing.T) {
	got, err := parseWingetTable(loadWingetFixture(t, "upgrade_it_IT.txt"), tableUpgrade)
	if err != nil {
		t.Fatalf("parseWingetTable returned error: %v", err)
	}
	want := []Package{
		{Name: "Synology Drive Client (remove only)", ID: "Synology.DriveClient", Version: "8.0.1.17885", Available: "8.0.2.17889", Source: "winget"},
		{Name: "Oh My Posh", ID: "JanDeDobbeleer.OhMyPosh", Version: "29.12.0", Available: "29.13.0", Source: "winget"},
		{Name: "Microsoft Teams Meeting Add-in for Microsoft Office", ID: "XP8BT8DW290MPQ", Version: "1.25.18202", Available: "1.25.24601", Source: "msstore"},
		{Name: "Adobe Acrobat (64-bit)", ID: "Adobe.Acrobat.Reader.64-bit", Version: "26.001.21483", Available: "26.001.21529", Source: "winget"},
		{Name: "Google Chrome", ID: "Google.Chrome.EXE", Version: "147.0.7727.138", Available: "148.0.7778.97", Source: "winget"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWingetTable(it-IT upgrade) = %#v, want %#v", got, want)
	}
}

// CJK headers have full-width characters (each takes 2 display cells but 3
// UTF-8 bytes). Byte-indexed slicing would desync from ASCII data rows; the
// new parser walks by display cells via runewidth.
func TestParseWingetTableLocalizedJapaneseList(t *testing.T) {
	got, err := parseWingetTable(loadWingetFixture(t, "list_ja_JP.txt"), tableList)
	if err != nil {
		t.Fatalf("parseWingetTable returned error: %v", err)
	}
	want := []Package{
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode", Version: "1.119.0", Source: "winget"},
		{Name: "Windows Terminal", ID: "Microsoft.WindowsTerminal", Version: "1.24.10921.0", Source: "winget"},
		{Name: "Greenshot", ID: "Greenshot.Greenshot", Version: "1.3.315", Source: "winget"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWingetTable(ja-JP list) = %#v, want %#v", got, want)
	}
}

// 5-column upgrade and 5-column search tables have identical shape but
// different semantics for the fourth column (Available vs Match). The schema
// fallback must use tableKind to pick the right mapping when the dictionary
// can't resolve the headers (e.g. an unknown future locale).
func TestParseWingetTableSchemaFallbackUpgradeVsSearch(t *testing.T) {
	// Fake "unknown locale" headers that look like upgrade/search shapes but
	// share no entries with the vendored dictionary. The parser must lean on
	// tableKind alone here.
	upgradeOutput := strings.Join([]string{
		"Nimi      Tunnus    Versio    Saatavilla   Lähde",
		"---------------------------------------------------",
		"Sample    Foo.Bar   1.0       2.0          winget",
		"",
	}, "\n")
	got, err := parseWingetTable(upgradeOutput, tableUpgrade)
	if err != nil {
		t.Fatalf("upgrade fallback returned error: %v", err)
	}
	want := []Package{{Name: "Sample", ID: "Foo.Bar", Version: "1.0", Available: "2.0", Source: "winget"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade-shaped fallback = %#v, want %#v", got, want)
	}

	searchOutput := strings.Join([]string{
		"Nimi      Tunnus    Versio    Vastaavuus   Lähde",
		"---------------------------------------------------",
		"Sample    Foo.Bar   1.0       Tag: foo     winget",
		"",
	}, "\n")
	gotSearch, err := parseWingetTable(searchOutput, tableSearch)
	if err != nil {
		t.Fatalf("search fallback returned error: %v", err)
	}
	// In search mode the 4th column is Match — informational, not stored on
	// Package. So Available stays empty.
	wantSearch := []Package{{Name: "Sample", ID: "Foo.Bar", Version: "1.0", Source: "winget"}}
	if !reflect.DeepEqual(gotSearch, wantSearch) {
		t.Fatalf("search-shaped fallback = %#v, want %#v", gotSearch, wantSearch)
	}
}

// Korean headers `장치 ID` and `사용 가능` contain a single internal space.
// splitHeaderCells must keep these as single column cells (only ≥2 spaces
// close a column) and the dictionary must match them verbatim.
func TestSplitHeaderCellsKoreanInnerSpace(t *testing.T) {
	header := "이름        장치 ID    버전        원본"
	cells := splitHeaderCells(header)
	if len(cells) != 4 {
		t.Fatalf("expected 4 cells, got %d: %#v", len(cells), cells)
	}
	wantTexts := []string{"이름", "장치 ID", "버전", "원본"}
	for i, want := range wantTexts {
		if cells[i].text != want {
			t.Errorf("cell %d = %q, want %q", i, cells[i].text, want)
		}
	}
	// Verify the dictionary still resolves them to the right kinds.
	wantKinds := []columnKind{colName, colID, colVersion, colSource}
	for i, want := range wantKinds {
		got, ok := wingetHeaderLookup[cells[i].text]
		if !ok {
			t.Errorf("cell %d %q: not in dictionary", i, cells[i].text)
			continue
		}
		if got != want {
			t.Errorf("cell %d %q: dictionary kind = %d, want %d", i, cells[i].text, got, want)
		}
	}
}

// When a table is present (separator line, header line) but the parser cannot
// resolve any column from the dictionary AND the column count does not match
// any known schema for tableKind, return an error rather than silently
// returning nil. This is the bug behind issue #44 — silent nil masked the
// real problem as "0 packages installed".
func TestParseWingetTableErrorOnUnknownShape(t *testing.T) {
	output := strings.Join([]string{
		"Foo  Bar  Baz  Qux  Quux  Corge  Grault", // 7 columns, no schema
		"------------------------------------------",
		"a    b    c    d    e     f      g",
		"",
	}, "\n")
	_, err := parseWingetTable(output, tableList)
	if err == nil {
		t.Fatal("expected error for unknown table shape, got nil")
	}
	if !strings.Contains(err.Error(), "not a known shape") {
		t.Errorf("error message %q should mention unknown shape", err.Error())
	}
}

// Empty winget output (no separator, no rows) is a legitimate "no packages"
// signal, not a parse error. Callers should be able to render "0 installed"
// or "All packages are up to date" without seeing an error.
func TestParseWingetTableEmptyIsNotAnError(t *testing.T) {
	for _, input := range []string{
		"",
		"   ",
		"No installed package found matching input criteria.",
	} {
		pkgs, err := parseWingetTable(input, tableList)
		if err != nil {
			t.Errorf("input %q: unexpected error %v", input, err)
		}
		if len(pkgs) != 0 {
			t.Errorf("input %q: got %d packages, want 0", input, len(pkgs))
		}
	}
}

// Italian table footer "5 aggiornamenti disponibili." must not be parsed as
// a package row. Validates that the trailer-keyword filter handles localised
// strings, not just English ones.
func TestIsTableTrailerLocalized(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"5 aggiornamenti disponibili.", true},
		{"3 package(s) installed.", true},
		{"No installed package found matching input criteria.", true},
		{"Mozilla.Firefox  Firefox  138.0  winget", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isTableTrailer(c.line); got != c.want {
			t.Errorf("isTableTrailer(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// The vendored locale dictionary should always cover at least the canonical
// 11 winget locales and have a non-empty source SHA so wintui doctor can
// report which winget-cli revision the headers came from.
func TestVendoredLocaleDictionaryShape(t *testing.T) {
	if wingetLocaleSourceSHA == "" {
		t.Error("wingetLocaleSourceSHA is empty — generator did not write SHA pin")
	}
	if len(wingetLocaleCoverage) < 11 {
		t.Errorf("wingetLocaleCoverage has %d entries, want at least 11", len(wingetLocaleCoverage))
	}
	for _, kind := range []columnKind{colName, colID, colVersion, colAvailable, colSource, colMatch} {
		found := 0
		for _, k := range wingetHeaderLookup {
			if k == kind {
				found++
			}
		}
		if found == 0 {
			t.Errorf("no dictionary entries for column kind %d", kind)
		}
	}
}
