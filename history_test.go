package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func tempHistoryPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "history.json")
}

func TestHistoryRoundTrip(t *testing.T) {
	path := tempHistoryPath(t)
	rec := historyRecord{
		ID:        "20260621T141500Z-ab12",
		Timestamp: time.Date(2026, 6, 21, 14, 15, 0, 0, time.UTC),
		Trigger:   historyTriggerCLIAll,
		Action:    historyActionUpgrade,
		Items: []historyItem{
			{ID: "Mozilla.Firefox", Name: "Mozilla Firefox", Source: "winget", Action: "upgrade", FromVersion: "126.0", ToVersion: "127.0", Status: historyStatusOK},
			{ID: "Bad.Pkg", Action: "upgrade", FromVersion: "1.0", ToVersion: "1.1", Status: historyStatusError, Error: "hash mismatch"},
		},
	}

	id, err := recordHistoryTo(path, rec)
	if err != nil {
		t.Fatalf("recordHistoryTo: %v", err)
	}
	if id != rec.ID {
		t.Fatalf("returned id = %q, want %q", id, rec.ID)
	}

	env, err := loadHistoryFrom(path)
	if err != nil {
		t.Fatalf("loadHistoryFrom: %v", err)
	}
	if env.Version != historyEnvelopeVersion {
		t.Errorf("version = %d, want %d", env.Version, historyEnvelopeVersion)
	}
	if len(env.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(env.Records))
	}
	got := env.Records[0]
	if got.Trigger != historyTriggerCLIAll || got.Action != historyActionUpgrade {
		t.Errorf("trigger/action = %q/%q", got.Trigger, got.Action)
	}
	// Summary must be derived from items by the writer.
	wantSummary := historySummary{Total: 2, OK: 1, Failed: 1}
	if got.Summary != wantSummary {
		t.Errorf("summary = %+v, want %+v", got.Summary, wantSummary)
	}
	if !got.Timestamp.Equal(rec.Timestamp) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, rec.Timestamp)
	}
}

func TestRecordHistoryFillsIDAndTimestamp(t *testing.T) {
	path := tempHistoryPath(t)
	id, err := recordHistoryTo(path, historyRecord{Trigger: historyTriggerTUI, Action: historyActionInstall, Items: []historyItem{{ID: "X", Action: "install", Status: historyStatusOK}}})
	if err != nil {
		t.Fatalf("recordHistoryTo: %v", err)
	}
	if id == "" {
		t.Fatal("expected a generated batch id, got empty")
	}
	env, _ := loadHistoryFrom(path)
	if env.Records[0].Timestamp.IsZero() {
		t.Error("expected a filled timestamp")
	}
}

func TestHistoryAppendsAcrossWrites(t *testing.T) {
	path := tempHistoryPath(t)
	for i := range 3 {
		if _, err := recordHistoryTo(path, historyRecord{Trigger: historyTriggerTUI, Action: historyActionUpgrade, Items: []historyItem{{ID: "A", Action: "upgrade", Status: historyStatusOK}}}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	env, err := loadHistoryFrom(path)
	if err != nil {
		t.Fatalf("loadHistoryFrom: %v", err)
	}
	if len(env.Records) != 3 {
		t.Fatalf("records = %d, want 3", len(env.Records))
	}
}

func TestHistorySizeBound(t *testing.T) {
	path := tempHistoryPath(t)
	// Seed a file already over the cap to also exercise trimming on read-back.
	total := historyMaxRecords + 50
	for i := range total {
		mark := historyStatusOK
		rec := historyRecord{
			ID:      batchIDFor(time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC)),
			Trigger: historyTriggerTUI,
			Action:  historyActionUpgrade,
			Items:   []historyItem{{ID: "P", Action: "upgrade", Status: mark, Notes: itoa(i)}},
		}
		if _, err := recordHistoryTo(path, rec); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	env, err := loadHistoryFrom(path)
	if err != nil {
		t.Fatalf("loadHistoryFrom: %v", err)
	}
	if len(env.Records) != historyMaxRecords {
		t.Fatalf("records = %d, want %d (capped)", len(env.Records), historyMaxRecords)
	}
	// Oldest trimmed: the surviving first record must be the (total-cap)-th written.
	wantFirstNote := itoa(total - historyMaxRecords)
	if env.Records[0].Items[0].Notes != wantFirstNote {
		t.Errorf("oldest surviving note = %q, want %q (front should be trimmed)", env.Records[0].Items[0].Notes, wantFirstNote)
	}
	wantLastNote := itoa(total - 1)
	if env.Records[len(env.Records)-1].Items[0].Notes != wantLastNote {
		t.Errorf("newest note = %q, want %q", env.Records[len(env.Records)-1].Items[0].Notes, wantLastNote)
	}
}

func TestHistoryMissingFileIsNotAnError(t *testing.T) {
	env, err := loadHistoryFrom(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(env.Records) != 0 {
		t.Errorf("records = %d, want 0", len(env.Records))
	}
}

func TestHistoryCorruptSelfHealsOnWrite(t *testing.T) {
	path := tempHistoryPath(t)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0644); err != nil {
		t.Fatal(err)
	}
	// Read path surfaces corruption as an error...
	if _, err := loadHistoryFrom(path); err == nil {
		t.Error("expected corrupt-file read to error")
	}
	// ...but the write path self-heals by starting fresh.
	if _, err := recordHistoryTo(path, historyRecord{Trigger: historyTriggerTUI, Action: historyActionUpgrade, Items: []historyItem{{ID: "Z", Action: "upgrade", Status: historyStatusOK}}}); err != nil {
		t.Fatalf("recordHistoryTo should self-heal corrupt file, got %v", err)
	}
	env, err := loadHistoryFrom(path)
	if err != nil {
		t.Fatalf("loadHistoryFrom after heal: %v", err)
	}
	if len(env.Records) != 1 {
		t.Fatalf("records after heal = %d, want 1", len(env.Records))
	}
}

func TestHistoryFutureVersionRejectedAndNotClobbered(t *testing.T) {
	path := tempHistoryPath(t)
	future := []byte(`{"version":99,"generator":"wintui 9.9.9","records":[{"id":"keep-me","trigger":"tui","action":"upgrade","items":[],"summary":{}}]}`)
	if err := os.WriteFile(path, future, 0644); err != nil {
		t.Fatal(err)
	}

	// Reader rejects with a clear unsupported-version error.
	if _, err := loadHistoryFrom(path); err == nil || !strings.Contains(err.Error(), "unsupported history format v99") {
		t.Fatalf("expected unsupported-version error, got %v", err)
	}

	// Writer refuses and must NOT modify the file.
	if _, err := recordHistoryTo(path, historyRecord{Trigger: historyTriggerTUI, Items: []historyItem{{ID: "new", Status: historyStatusOK}}}); err == nil {
		t.Fatal("expected recordHistoryTo to refuse a future-version file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(future) {
		t.Errorf("future-version file was modified:\n got: %s\nwant: %s", after, future)
	}
}

// Regression: a future schema may change a field's TYPE (here records is an
// object, not an array) so it won't unmarshal into the v1 struct. It must still
// be recognized as future and left untouched, not mislabeled corrupt + clobbered.
func TestHistoryFutureVersionIncompatibleFieldNoClobber(t *testing.T) {
	path := tempHistoryPath(t)
	future := []byte(`{"version":99,"records":{"shape":"changed-in-v99"}}`)
	if err := os.WriteFile(path, future, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadHistoryFrom(path); err == nil || !strings.Contains(err.Error(), "unsupported history format v99") {
		t.Fatalf("expected unsupported-version error, got %v", err)
	}
	if _, err := recordHistoryTo(path, historyRecord{Trigger: historyTriggerTUI, Items: []historyItem{{ID: "x", Status: historyStatusOK}}}); err == nil {
		t.Fatal("expected recordHistoryTo to refuse the future-version file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(future) {
		t.Errorf("future-version file was clobbered:\n got: %s\nwant: %s", after, future)
	}
}

func TestSummarizeCounts(t *testing.T) {
	got := summarize([]historyItem{
		{Status: historyStatusOK},
		{Status: historyStatusOK},
		{Status: historyStatusError},
		{Status: historyStatusPending},
		{Status: historyStatusSkipped},
	})
	want := historySummary{Total: 5, OK: 2, Failed: 1, Pending: 1, Skipped: 1}
	if got != want {
		t.Errorf("summarize = %+v, want %+v", got, want)
	}
}

func TestBatchIDFormatIsSortable(t *testing.T) {
	re := regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)
	earlier := batchIDFor(time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC))
	later := batchIDFor(time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC))
	if !re.MatchString(earlier) {
		t.Errorf("batch id %q does not match expected format", earlier)
	}
	if earlier >= later {
		t.Errorf("batch ids not lexically sortable: %q !< %q", earlier, later)
	}
}

func TestHistoryItemOmitsEmptyOptionalFields(t *testing.T) {
	// A fresh install has no from_version; the key must be absent, not "".
	b, err := json.Marshal(historyItem{ID: "New.Pkg", Action: "install", ToVersion: "1.0", Status: historyStatusOK})
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, absent := range []string{"from_version", "error", "notes", "source", "name"} {
		if strings.Contains(js, absent) {
			t.Errorf("expected %q to be omitted from %s", absent, js)
		}
	}
	if !strings.Contains(js, "to_version") {
		t.Errorf("expected to_version present in %s", js)
	}
}

// itoa is a tiny local helper to keep the size-bound test readable without
// importing strconv just for a marker value.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
