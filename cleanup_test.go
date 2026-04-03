package main

import (
	"strings"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1536, "1.5 KB"},
		{12 * 1024, "12 KB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}

	for _, tc := range cases {
		if got := formatBytes(tc.bytes); got != tc.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestCleanupReadyViewShowsEstimatedSize(t *testing.T) {
	s := newCleanupScreen()
	s.state = cleanupReady
	s.targets = []cleanupTarget{
		{path: `C:\Temp\a.tmp`, bytes: 2 * 1024 * 1024},
		{path: `C:\Temp\b.tmp`, bytes: 512 * 1024},
	}
	s.totalBytes = s.targets[0].bytes + s.targets[1].bytes

	got := s.view(120, 24)
	if !strings.Contains(got, "2.5 MB") {
		t.Fatalf("view() = %q, want size in panel title", got)
	}
}

func TestCleanupDoneViewShowsFreedBytes(t *testing.T) {
	s := newCleanupScreen()
	s.state = cleanupDone
	s.deleted = 3
	s.freedBytes = 3 * 1024 * 1024

	got := s.view(120, 24)
	if !strings.Contains(got, "Freed 3.0 MB.") {
		t.Fatalf("view() = %q, want freed bytes summary", got)
	}
}
