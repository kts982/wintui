package main

import "testing"

func TestFilterPackagesByQuery(t *testing.T) {
	pkgs := []Package{
		{Name: "Mozilla Firefox", ID: "Mozilla.Firefox"},
		{Name: "Git", ID: "Git.Git"},
		{Name: "Visual Studio Code", ID: "Microsoft.VisualStudioCode"},
	}

	cases := []struct {
		query   string // already-lowercased, as runList passes it
		wantIDs []string
	}{
		{"firefox", []string{"Mozilla.Firefox"}},                  // matches name
		{"mozilla.firefox", []string{"Mozilla.Firefox"}},          // matches id
		{"git", []string{"Git.Git"}},                              // name + id
		{"code", []string{"Microsoft.VisualStudioCode"}},          // substring of id
		{"visual studio", []string{"Microsoft.VisualStudioCode"}}, // substring of name
		{"zzz", nil}, // no match
	}

	for _, tc := range cases {
		got := filterPackagesByQuery(pkgs, tc.query)
		if len(got) != len(tc.wantIDs) {
			t.Errorf("query %q: got %d results, want %d (%v)", tc.query, len(got), len(tc.wantIDs), got)
			continue
		}
		for i, want := range tc.wantIDs {
			if got[i].ID != want {
				t.Errorf("query %q: result[%d].ID = %q, want %q", tc.query, i, got[i].ID, want)
			}
		}
	}
}
