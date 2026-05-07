package main

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// columnKind identifies a logical winget table column. The set is intentionally
// small — winget only renders these six headers across list/upgrade/search.
type columnKind int

const (
	colUnknown columnKind = iota
	colName
	colID
	colVersion
	colAvailable
	colSource
	colMatch
)

// Aliases so generator output and parser stay readable. The generator emits
// colName / colId / colVersion / colAvailable / colSource / colMatch as map
// values; we expose them here.
const (
	colId = colID //nolint:revive // matches generated dictionary
)

// tableKind selects which schema the parser should expect when the header
// dictionary lookup fails (e.g. unknown future locale). Without it,
// 5-column upgrade tables (Name/Id/Version/Available/Source) and 5-column
// search tables (Name/Id/Version/Match/Source) are positionally ambiguous —
// the fourth cell carries different semantics. Callers always know the kind
// because they chose the winget command, so we make it explicit.
type tableKind int

const (
	tableList tableKind = iota
	tableUpgrade
	tableSearch
)

// schemaFor returns the column order the parser should assume for `kind` when
// the dictionary cannot resolve enough header cells. Indexed by header-cell
// count so the same kind can accept the variants winget actually emits
// (e.g. `winget list` with or without a Source column).
func schemaFor(kind tableKind, count int) []columnKind {
	switch kind {
	case tableList:
		switch count {
		case 3:
			return []columnKind{colName, colID, colVersion}
		case 4:
			return []columnKind{colName, colID, colVersion, colSource}
		}
	case tableUpgrade:
		if count == 5 {
			return []columnKind{colName, colID, colVersion, colAvailable, colSource}
		}
		if count == 4 {
			// Defensive: some older winget builds omit the Source column on
			// upgrade output. Treat the fourth cell as Available.
			return []columnKind{colName, colID, colVersion, colAvailable}
		}
	case tableSearch:
		switch count {
		case 4:
			return []columnKind{colName, colID, colVersion, colSource}
		case 5:
			return []columnKind{colName, colID, colVersion, colMatch, colSource}
		}
	}
	return nil
}

// headerCell records a single column header detected on the header line. The
// display-cell start position is what we use to slice data rows; byte indexes
// alone would mis-slice when headers contain CJK or other multi-cell runes.
type headerCell struct {
	text     string
	startCol int // inclusive; column index measured in display cells
	endCol   int // exclusive; one past the last cell of this column
	kind     columnKind
}

// parseWingetTable parses winget's fixed-width table output. The schema-aware
// signature (a tableKind) lets the parser disambiguate same-shape tables with
// different semantics (5-col upgrade vs 5-col search) and lets it surface a
// clear error when a table exists but no column can be mapped — which today's
// silent-nil behaviour mistook for "0 packages installed."
//
// Returns ([]Package, nil) on success — including the empty-table case.
// Returns (nil, error) only when a table appears to be present but the parser
// cannot recover columns from it (unknown locale + unknown column count).
// Callers should treat that as a bug to surface, not as "no packages."
func parseWingetTable(output string, kind tableKind) ([]Package, error) {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	raw := strings.FieldsFunc(output, func(r rune) bool { return r == '\r' || r == '\n' })
	var lines []string
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}

	sepIdx := findTableSeparator(lines)
	if sepIdx < 1 || sepIdx >= len(lines)-1 {
		// No separator → no table → no packages. Not an error; winget legitimately
		// prints "No installed package found matching input criteria." here.
		return nil, nil
	}

	cells, err := detectHeaderCells(lines[sepIdx-1], kind)
	if err != nil {
		return nil, err
	}

	pkgs := make([]Package, 0, len(lines)-sepIdx-1)
	for _, line := range lines[sepIdx+1:] {
		if isTableTrailer(line) {
			continue
		}
		pkg := extractPackageFromCells(line, cells)
		if pkg.ID != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs, nil
}

// findTableSeparator locates the dashed run that visually separates the header
// row from data rows. Accepts ASCII '-' and box-drawing '─' (U+2500); some
// CJK locales use the box-drawing variant.
func findTableSeparator(lines []string) int {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 10 && strings.Trim(trimmed, "-─") == "" {
			return i
		}
	}
	return -1
}

// detectHeaderCells walks the header line by display cells, splitting on runs
// of 2+ spaces (winget always pads columns by ≥2 spaces, even in CJK locales).
// Each detected cell is then resolved through the vendored locale dictionary;
// when fewer than 3 cells resolve, we fall back to the positional schema for
// `kind`. Returns an error only when both the dictionary and the schema fail.
func detectHeaderCells(header string, kind tableKind) ([]headerCell, error) {
	cells := splitHeaderCells(header)
	if len(cells) == 0 {
		return nil, fmt.Errorf("winget output: header row has no detectable columns")
	}

	resolved := 0
	for i := range cells {
		if k, ok := wingetHeaderLookup[cells[i].text]; ok {
			cells[i].kind = k
			resolved++
		}
	}

	if resolved < 3 {
		schema := schemaFor(kind, len(cells))
		if schema == nil {
			return nil, fmt.Errorf("winget output: %d-column %s table is not a known shape; first header was %q (locale possibly not yet vendored)", len(cells), tableKindName(kind), cells[0].text)
		}
		// Apply schema only to cells that the dictionary couldn't identify.
		// This way a partially recognised header (e.g. mixed locale) still
		// keeps its dictionary-resolved kinds for the cells it knows.
		for i := range cells {
			if cells[i].kind == colUnknown {
				cells[i].kind = schema[i]
			}
		}
	}

	// Fill any gaps left by partial dictionary coverage using the schema as a
	// best-effort backup.
	if resolved >= 3 {
		schema := schemaFor(kind, len(cells))
		for i := range cells {
			if cells[i].kind == colUnknown && i < len(schema) {
				cells[i].kind = schema[i]
			}
		}
	}

	return cells, nil
}

// splitHeaderCells walks `header` by display cells and returns one entry per
// column boundary detected by a run of ≥2 spaces. Display-cell aware so CJK
// headers don't desync from ASCII data rows further down.
func splitHeaderCells(header string) []headerCell {
	var cells []headerCell

	type run struct {
		startCol int
		endCol   int
		text     []rune
	}
	var current run
	inRun := false
	col := 0
	gap := 0

	flush := func() {
		if !inRun {
			return
		}
		cells = append(cells, headerCell{
			text:     strings.TrimRight(string(current.text), " "),
			startCol: current.startCol,
			endCol:   current.endCol,
		})
		inRun = false
		current = run{}
	}

	for _, r := range header {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			// Zero-width (e.g. combining marks) — fold into current run.
			if inRun {
				current.text = append(current.text, r)
			}
			continue
		}
		if r == ' ' {
			gap++
			if inRun && gap < 2 {
				// Single space inside a header cell ("장치 ID", "사용 가능") —
				// keep it; only ≥2 spaces close the cell.
				current.text = append(current.text, r)
				current.endCol = col + w
			}
			col += w
			continue
		}
		if !inRun || gap >= 2 {
			flush()
			current.startCol = col
			inRun = true
		}
		current.text = append(current.text, r)
		current.endCol = col + w
		col += w
		gap = 0
	}
	flush()

	// Extend each cell's endCol to the start of the next cell so data-row
	// slicing covers the full padded column. The final cell extends to "end".
	for i := range cells {
		if i+1 < len(cells) {
			cells[i].endCol = cells[i+1].startCol
		} else {
			cells[i].endCol = -1 // sentinel: read to end of line
		}
	}
	return cells
}

// extractPackageFromCells slices a data row by display-cell ranges (not byte
// offsets) so CJK-aligned columns don't desync from ASCII data values. The
// trailing ellipsis (U+2026) is stripped and recorded as a truncation flag.
func extractPackageFromCells(line string, cells []headerCell) Package {
	var pkg Package
	for _, c := range cells {
		val, truncated := sliceDisplayRange(line, c.startCol, c.endCol)
		val = strings.TrimSpace(val)
		switch c.kind {
		case colName:
			pkg.Name = val
			pkg.nameTruncated = truncated
		case colID:
			pkg.ID = val
			pkg.idTruncated = truncated
		case colVersion:
			pkg.Version = val
		case colAvailable:
			pkg.Available = val
		case colSource:
			if val == "winget" || val == "msstore" {
				pkg.Source = val
			}
		case colMatch:
			// Match column is purely informational for `winget search`; we
			// don't store it on Package.
		}
	}
	return pkg
}

// sliceDisplayRange returns the substring of `line` covering display cells
// [startCol, endCol) — endCol == -1 means "to end of line." It also strips a
// trailing horizontal ellipsis (U+2026) and reports it as truncation: winget
// uses '…' to flag a value clipped to the console width, and downstream
// `--id <truncated> --exact` calls fail if we don't notice.
func sliceDisplayRange(line string, startCol, endCol int) (string, bool) {
	col := 0
	var out []rune
	for _, r := range line {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			if col > startCol && (endCol < 0 || col <= endCol) {
				out = append(out, r)
			}
			continue
		}
		if col >= startCol && (endCol < 0 || col < endCol) {
			out = append(out, r)
		}
		col += w
		if endCol >= 0 && col >= endCol {
			break
		}
	}
	s := string(out)
	if trimmed, ok := strings.CutSuffix(strings.TrimRight(s, " "), "…"); ok {
		return trimmed, true
	}
	return s, false
}

// isTableTrailer reports whether `line` is a localised table-footer message
// (e.g. "5 aggiornamenti disponibili.", "5 upgrades available.",
// "5 package(s) installed.") that should not be parsed as a package row.
//
// The data-row guard (`pkg.ID != ""`) usually catches these because trailers
// don't have an Id column at the right display position — but matching by
// sentinel keywords up front keeps the code's intent obvious and avoids
// relying on alignment quirks.
func isTableTrailer(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, marker := range []string{
		"upgrades available", "package(s)", "no installed",
		"aggiornamenti disponibili", // it-IT
		"actualizaciones disponibles",
		"actualizações disponíveis",
		"mises à jour disponibles",
		"verfügbare upgrades",
		"доступно обновлен",
		"个升级可用", "個升級可用",
		"アップグレード", "업그레이드",
		"個のパッケージ",   // ja-JP "X package(s)..."
		"個のアップグレード", // ja-JP upgrade footer variant
		"개의 패키지",    // ko-KR "X package(s)"
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func tableKindName(kind tableKind) string {
	switch kind {
	case tableList:
		return "list"
	case tableUpgrade:
		return "upgrade"
	case tableSearch:
		return "search"
	default:
		return "unknown"
	}
}
