package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

func TestBuildSelectableTableUsesResponsiveColumns(t *testing.T) {
	cache.invalidate()
	defer cache.invalidate()
	cache.setUpgradeable([]Package{{ID: "Git.Git"}})

	pkgs := []Package{
		{Name: "Git", ID: "Git.Git", Version: "2.45.1", Source: "winget"},
	}

	narrow := buildSelectableTable(pkgs, map[string]bool{}, packagesTableWidth(80), 10)
	wide := buildSelectableTable(pkgs, map[string]bool{}, packagesTableWidth(120), 10)

	narrowTitles := extractColumnTitles(narrow.Columns())
	wideTitles := extractColumnTitles(wide.Columns())

	if reflect.DeepEqual(narrowTitles, wideTitles) {
		t.Fatalf("column titles should differ between narrow and wide layouts: narrow=%v wide=%v", narrowTitles, wideTitles)
	}
	if containsString(narrowTitles, "Source") {
		t.Fatalf("narrow layout unexpectedly included Source column: %v", narrowTitles)
	}
	if !containsString(wideTitles, "Source") {
		t.Fatalf("wide layout did not include Source column: %v", wideTitles)
	}
	if narrow.Width() != packagesTableWidth(80) {
		t.Fatalf("narrow table width = %d, want %d", narrow.Width(), packagesTableWidth(80))
	}
	if wide.Width() != packagesTableWidth(120) {
		t.Fatalf("wide table width = %d, want %d", wide.Width(), packagesTableWidth(120))
	}

	narrowIDWidth := findColumnWidth(narrow.Columns(), "ID")
	wideIDWidth := findColumnWidth(wide.Columns(), "ID")
	if wideIDWidth <= narrowIDWidth {
		t.Fatalf("wide ID column width = %d, want > narrow width %d", wideIDWidth, narrowIDWidth)
	}
}

func TestPackagesResizeRebuildsTableAndPreservesCursor(t *testing.T) {
	cache.invalidate()
	defer cache.invalidate()
	cache.setUpgradeable([]Package{{ID: "Git.Git"}})

	s := newPackagesScreen()
	s.state = packagesReady
	s.packages = []Package{
		{Name: "Git", ID: "Git.Git", Version: "2.45.1", Source: "winget"},
		{Name: "Neovim", ID: "Neovim.Neovim", Version: "0.11.5", Source: "winget"},
	}
	s.tableWidth = packagesTableWidth(80)
	s.rebuildTable()
	s.table.SetCursor(1)

	next, _ := s.update(tea.WindowSizeMsg{Width: 140, Height: 40})
	got := next.(packagesScreen)

	if got.tableWidth != packagesTableWidth(140) {
		t.Fatalf("tableWidth = %d, want %d", got.tableWidth, packagesTableWidth(140))
	}
	if got.table.Width() != packagesTableWidth(140) {
		t.Fatalf("table width = %d, want %d", got.table.Width(), packagesTableWidth(140))
	}
	if got.table.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", got.table.Cursor())
	}
	if !containsString(extractColumnTitles(got.table.Columns()), "Source") {
		t.Fatalf("resized table did not add Source column: %v", extractColumnTitles(got.table.Columns()))
	}
}

func TestPackagesSmallTerminalKeepsSelectedRowVisible(t *testing.T) {
	s := newPackagesScreen()
	s.state = packagesReady
	for i := 0; i < 40; i++ {
		s.packages = append(s.packages, Package{
			Name:    fmt.Sprintf("Package %02d", i),
			ID:      fmt.Sprintf("pkg.%02d", i),
			Version: "1.0",
			Source:  "winget",
		})
	}
	next, _ := s.update(tea.WindowSizeMsg{Width: 100, Height: 24})
	s = next.(packagesScreen)
	s.rebuildTable()
	ensureTableCursorVisible(&s.table, 25)

	next, _ = s.update(tea.WindowSizeMsg{Width: 100, Height: 24})
	got := next.(packagesScreen)
	view := got.table.View()
	if !strings.Contains(view, "Package 25") {
		t.Fatalf("table view = %q, want selected row to remain visible on small terminal", view)
	}
}

func extractColumnTitles(cols []table.Column) []string {
	titles := make([]string, 0, len(cols))
	for _, col := range cols {
		titles = append(titles, col.Title)
	}
	return titles
}

func findColumnWidth(cols []table.Column, title string) int {
	for _, col := range cols {
		if col.Title == title {
			return col.Width
		}
	}
	return 0
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
