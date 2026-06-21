package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func withHistory(t *testing.T, recs []historyRecord) {
	t.Helper()
	orig := historyLoadFn
	historyLoadFn = func() (historyEnvelope, error) {
		return historyEnvelope{Version: historyEnvelopeVersion, Records: recs}, nil
	}
	t.Cleanup(func() { historyLoadFn = orig })
}

func histRecord(id, trigger, action string, ts time.Time, items ...historyItem) historyRecord {
	return historyRecord{ID: id, Timestamp: ts, Trigger: trigger, Action: action, Items: items, Summary: summarize(items)}
}

func resetExit(t *testing.T) {
	t.Helper()
	orig := cliExitCode
	cliExitCode = 0
	t.Cleanup(func() { cliExitCode = orig })
}

var (
	tEarlier = time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	tLater   = time.Date(2026, 6, 21, 18, 0, 0, 0, time.UTC)
)

func TestHistoryBatchesTableNewestFirst(t *testing.T) {
	resetExit(t)
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIAll, "upgrade", tEarlier,
			historyItem{ID: "Mozilla.Firefox", Name: "Firefox", Action: "upgrade", FromVersion: "1.0", ToVersion: "2.0", Status: historyStatusOK}),
		histRecord("b2", historyTriggerTUI, "install", tLater,
			historyItem{ID: "Foo.Bar", Name: "Foo", Action: "install", ToVersion: "3.0", Status: historyStatusOK}),
	})

	var buf bytes.Buffer
	if err := runHistory(historyOptions{Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Firefox") || !strings.Contains(out, "Foo") {
		t.Fatalf("expected both batches in output:\n%s", out)
	}
	// Newest first: the tLater "install" batch must appear before the tEarlier one.
	if strings.Index(out, "install") > strings.Index(out, "upgrade") {
		t.Errorf("expected newest-first ordering (install before upgrade):\n%s", out)
	}
	if !strings.Contains(out, "2 batch(es)") {
		t.Errorf("missing summary line:\n%s", out)
	}
	if cliExitCode != 0 {
		t.Errorf("cliExitCode = %d, want 0", cliExitCode)
	}
}

func TestHistoryBatchesJSON(t *testing.T) {
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIAll, "upgrade", tLater,
			historyItem{ID: "A.Pkg", Action: "upgrade", Status: historyStatusOK},
			historyItem{ID: "B.Pkg", Action: "upgrade", Status: historyStatusError, Error: "boom"}),
	})

	var buf bytes.Buffer
	if err := runHistory(historyOptions{JSON: true, Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	var payload historyBatchesJSON
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if payload.View != "batches" || payload.Count != 1 {
		t.Fatalf("view/count = %q/%d, want batches/1", payload.View, payload.Count)
	}
	b := payload.Batches[0]
	if b.OK != 1 || b.Failed != 1 || len(b.PackageIDs) != 2 {
		t.Errorf("batch = %+v, want ok=1 failed=1 2 ids", b)
	}
}

func TestHistoryTimelineFiltersByID(t *testing.T) {
	resetExit(t)
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIAll, "upgrade", tEarlier,
			historyItem{ID: "Mozilla.Firefox", Name: "Firefox", Action: "upgrade", FromVersion: "1.0", ToVersion: "2.0", Status: historyStatusOK},
			historyItem{ID: "Other.Pkg", Action: "upgrade", Status: historyStatusOK}),
		histRecord("b2", historyTriggerCLIAll, "upgrade", tLater,
			historyItem{ID: "Mozilla.Firefox", Name: "Firefox", Action: "upgrade", FromVersion: "2.0", ToVersion: "3.0", Status: historyStatusOK}),
	})

	var buf bytes.Buffer
	if err := runHistory(historyOptions{ID: "Mozilla.Firefox", Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Other.Pkg") {
		t.Errorf("timeline leaked another package:\n%s", out)
	}
	if !strings.Contains(out, "2.0") || !strings.Contains(out, "3.0") {
		t.Errorf("expected both version transitions:\n%s", out)
	}
	if !strings.Contains(out, "2 action(s) recorded") {
		t.Errorf("missing timeline summary:\n%s", out)
	}
	if cliExitCode != 0 {
		t.Errorf("cliExitCode = %d, want 0", cliExitCode)
	}
}

func TestHistoryTimelineCaseInsensitiveID(t *testing.T) {
	resetExit(t)
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIID, "upgrade", tLater,
			historyItem{ID: "Mozilla.Firefox", Action: "upgrade", Status: historyStatusOK}),
	})
	var buf bytes.Buffer
	if err := runHistory(historyOptions{ID: "mozilla.firefox", Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if cliExitCode != 0 || !strings.Contains(buf.String(), "1 action(s) recorded") {
		t.Fatalf("case-insensitive id match failed: exit=%d out=%q", cliExitCode, buf.String())
	}
}

func TestHistoryTimelineNoMatchExits1(t *testing.T) {
	resetExit(t)
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIAll, "upgrade", tLater,
			historyItem{ID: "Present.Pkg", Action: "upgrade", Status: historyStatusOK}),
	})
	var buf bytes.Buffer
	if err := runHistory(historyOptions{ID: "Absent.Pkg", Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if cliExitCode != 1 {
		t.Errorf("cliExitCode = %d, want 1 (selector predicate miss)", cliExitCode)
	}
	if !strings.Contains(buf.String(), "No history for") {
		t.Errorf("missing not-found message:\n%s", buf.String())
	}
}

func TestHistoryUnfilteredEmptyExitsZero(t *testing.T) {
	resetExit(t)
	withHistory(t, nil)
	var buf bytes.Buffer
	if err := runHistory(historyOptions{Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if cliExitCode != 0 {
		t.Errorf("cliExitCode = %d, want 0 (empty unfiltered history is not an error)", cliExitCode)
	}
	if !strings.Contains(buf.String(), "No action history yet") {
		t.Errorf("missing empty-state message:\n%s", buf.String())
	}
}

func TestHistoryFilteredEmptyExits1(t *testing.T) {
	resetExit(t)
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIAll, "upgrade", tLater,
			historyItem{ID: "A.Pkg", Action: "upgrade", Status: historyStatusOK}),
	})
	var buf bytes.Buffer
	if err := runHistory(historyOptions{FailedOnly: true, Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if cliExitCode != 1 {
		t.Errorf("cliExitCode = %d, want 1 (--failed-only matched nothing)", cliExitCode)
	}
	if !strings.Contains(buf.String(), "No matching history records") {
		t.Errorf("missing filtered-empty message:\n%s", buf.String())
	}
}

func TestHistoryFailedOnlyKeepsOnlyFailures(t *testing.T) {
	resetExit(t)
	withHistory(t, []historyRecord{
		histRecord("ok", historyTriggerCLIAll, "upgrade", tEarlier,
			historyItem{ID: "Clean.Pkg", Action: "upgrade", Status: historyStatusOK}),
		histRecord("bad", historyTriggerCLIAll, "upgrade", tLater,
			historyItem{ID: "Broken.Pkg", Action: "upgrade", Status: historyStatusError, Error: "x"}),
	})
	var buf bytes.Buffer
	if err := runHistory(historyOptions{FailedOnly: true, Limit: 20}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Clean.Pkg") {
		t.Errorf("--failed-only leaked a clean batch:\n%s", out)
	}
	if !strings.Contains(out, "Broken.Pkg") {
		t.Errorf("--failed-only dropped the failed batch:\n%s", out)
	}
}

func TestHistoryLimitCaps(t *testing.T) {
	resetExit(t)
	recs := make([]historyRecord, 0, 5)
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		recs = append(recs, histRecord("b", historyTriggerTUI, "upgrade", base.Add(time.Duration(i)*time.Hour),
			historyItem{ID: "P", Action: "upgrade", Status: historyStatusOK}))
	}
	withHistory(t, recs)
	var buf bytes.Buffer
	if err := runHistory(historyOptions{Limit: 2}, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "5 batch(es)") || !strings.Contains(out, "showing most recent 2") {
		t.Errorf("limit summary wrong:\n%s", out)
	}
}

func TestHistoryInvalidSinceErrors(t *testing.T) {
	withHistory(t, nil)
	var buf bytes.Buffer
	err := runHistory(historyOptions{Since: "lastweek", Limit: 20}, &buf)
	if err == nil || !strings.Contains(err.Error(), "invalid --since") {
		t.Fatalf("want invalid --since error, got %v", err)
	}
}

func TestHistoryLoadErrorPropagates(t *testing.T) {
	orig := historyLoadFn
	historyLoadFn = func() (historyEnvelope, error) { return historyEnvelope{}, errors.New("corrupt JSON") }
	t.Cleanup(func() { historyLoadFn = orig })

	var buf bytes.Buffer
	if err := runHistory(historyOptions{Limit: 20}, &buf); err == nil {
		t.Fatal("expected the load error to propagate (fang styles it, exit 1)")
	}
}

func TestCompleteHistoryIDs(t *testing.T) {
	withHistory(t, []historyRecord{
		histRecord("b1", historyTriggerCLIAll, "upgrade", tLater,
			historyItem{ID: "Mozilla.Firefox", Name: "Firefox", Action: "upgrade", Status: historyStatusOK},
			historyItem{ID: "Git.Git", Name: "Git", Action: "upgrade", Status: historyStatusOK}),
	})
	got, _ := completeHistoryIDs(nil, nil, "moz")
	if len(got) != 1 || !strings.HasPrefix(got[0], "Mozilla.Firefox\t") {
		t.Fatalf("completion for 'moz' = %v, want [Mozilla.Firefox\\tFirefox]", got)
	}
	// Second positional must not complete.
	if got2, _ := completeHistoryIDs(nil, []string{"already"}, ""); got2 != nil {
		t.Errorf("expected no completion for second positional, got %v", got2)
	}
}
