package main

import (
	"context"
	"testing"
)

// TUI workspace batch write-point: finishBatch must record exactly one "tui"
// record built from the modal items, and nothing for an empty batch.
func TestFinishBatchRecordsHistory(t *testing.T) {
	recs := captureHistory(t)
	s := workspaceScreen{
		ctx: context.Background(),
		modal: &execModal{items: []batchItem{
			{action: retryOpUpgrade, status: batchDone, item: workspaceItem{pkg: Package{ID: "A.Pkg", Name: "App A", Source: "winget"}, installed: "1.0", available: "2.0"}},
			{action: retryOpUpgrade, status: batchFailed, item: workspaceItem{pkg: Package{ID: "B.Pkg", Source: "winget"}, installed: "1.0", available: "2.0"}},
		}},
	}
	s.finishBatch()

	if len(*recs) != 1 {
		t.Fatalf("records = %d, want 1", len(*recs))
	}
	rec := (*recs)[0]
	if rec.Trigger != historyTriggerTUI {
		t.Errorf("trigger = %q, want %q", rec.Trigger, historyTriggerTUI)
	}
	if len(rec.Items) != 2 || rec.Items[0].ID != "A.Pkg" || rec.Items[1].ID != "B.Pkg" {
		t.Errorf("items = %+v", rec.Items)
	}
}

func TestFinishBatchEmptyWritesNoHistory(t *testing.T) {
	recs := captureHistory(t)
	s := workspaceScreen{ctx: context.Background(), modal: &execModal{}}
	s.finishBatch()
	if len(*recs) != 0 {
		t.Errorf("empty batch must record nothing, got %+v", *recs)
	}
}

// TUI import normal completion (importDone) records the whole batch.
func TestImportCompletionRecordsHistory(t *testing.T) {
	recs := captureHistory(t)
	m := newImportModel()
	m.active = true
	m.state = importInstalling
	m.batchIDs = []string{"A.Pkg"}
	m.batchSources = []string{"winget"}
	m.batchVersions = []string{""}
	m.batchTotal = 1
	m.batchCurrent = 0

	next, _, _ := m.update(singleImportInstallDoneMsg{index: 0}, nil)
	if next.state != importDone {
		t.Fatalf("state = %v, want importDone", next.state)
	}
	if len(*recs) != 1 || (*recs)[0].Trigger != historyTriggerTUIImport {
		t.Fatalf("want one tui-import record, got %+v", *recs)
	}
	if len((*recs)[0].Items) != 1 || (*recs)[0].Items[0].ID != "A.Pkg" {
		t.Errorf("items = %+v", (*recs)[0].Items)
	}
}

// Regression for the review finding: pressing Esc mid-import must still record
// the installs that already completed (not silently drop them).
func TestImportEscRecordsCompletedHistory(t *testing.T) {
	recs := captureHistory(t)
	m := newImportModel()
	m.active = true
	m.state = importInstalling
	m.cancel = func() {}
	m.batchIDs = []string{"Done.Pkg", "InFlight.Pkg"}
	m.batchSources = []string{"winget", "winget"}
	m.batchVersions = []string{"", ""}
	m.batchTotal = 2
	m.batchCurrent = 1 // one finished; the second is in flight
	m.batchErrs = []error{nil}

	m.update(keyMsg("esc"), nil)

	if len(*recs) != 1 {
		t.Fatalf("records = %d, want 1 (the completed install)", len(*recs))
	}
	rec := (*recs)[0]
	if rec.Trigger != historyTriggerTUIImport {
		t.Errorf("trigger = %q, want %q", rec.Trigger, historyTriggerTUIImport)
	}
	if len(rec.Items) != 1 || rec.Items[0].ID != "Done.Pkg" {
		t.Errorf("items = %+v, want only the completed Done.Pkg (in-flight excluded)", rec.Items)
	}
}
