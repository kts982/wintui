package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// captureHistory swaps historyRecordFn for a recorder and restores it after the
// test. Returns a pointer to the slice the write-points appended to.
func captureHistory(t *testing.T) *[]historyRecord {
	t.Helper()
	var recs []historyRecord
	orig := historyRecordFn
	historyRecordFn = func(rec historyRecord) (string, error) {
		recs = append(recs, rec)
		return rec.ID, nil
	}
	t.Cleanup(func() { historyRecordFn = orig })
	return &recs
}

func TestDominantAction(t *testing.T) {
	cases := []struct {
		name  string
		items []historyItem
		want  string
	}{
		{"empty", nil, ""},
		{"all upgrade", []historyItem{{Action: "upgrade"}, {Action: "upgrade"}}, "upgrade"},
		{"mixed", []historyItem{{Action: "install"}, {Action: "uninstall"}}, historyActionMixed},
	}
	for _, c := range cases {
		if got := dominantAction(c.items); got != c.want {
			t.Errorf("%s: dominantAction = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildTUIHistoryRecord(t *testing.T) {
	items := []batchItem{
		{action: retryOpUpgrade, status: batchDone, item: workspaceItem{pkg: Package{ID: "A", Name: "App A", Source: "winget"}, installed: "1.0", available: "2.0"}},
		{action: retryOpUpgrade, status: batchFailed, err: errors.New("boom"), item: workspaceItem{pkg: Package{ID: "B", Name: "App B", Source: "winget"}, installed: "1.0", available: "2.0"}},
		{action: retryOpInstall, status: batchPendingRestart, item: workspaceItem{pkg: Package{ID: "C", Name: "App C", Source: "winget"}}},
	}
	rec := buildTUIHistoryRecord(items, historyTriggerTUI, nil)

	if rec.Trigger != historyTriggerTUI {
		t.Errorf("trigger = %q, want %q", rec.Trigger, historyTriggerTUI)
	}
	if rec.Action != historyActionMixed {
		t.Errorf("action = %q, want mixed (upgrade+install)", rec.Action)
	}
	if len(rec.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(rec.Items))
	}
	if rec.Items[0].Status != historyStatusOK || rec.Items[0].FromVersion != "1.0" || rec.Items[0].ToVersion != "2.0" {
		t.Errorf("item[0] = %+v, want ok 1.0->2.0", rec.Items[0])
	}
	if rec.Items[1].Status != historyStatusError || rec.Items[1].Error != "boom" {
		t.Errorf("item[1] = %+v, want error 'boom'", rec.Items[1])
	}
	if rec.Items[2].Status != historyStatusPending || rec.Items[2].Action != "install" {
		t.Errorf("item[2] = %+v, want pending install", rec.Items[2])
	}
}

// Regression: when a user picks a specific (non-latest) version, the record's
// ToVersion must be that chosen version (what winget actually installs), not the
// available/latest version.
func TestBuildTUIHistoryRecordUsesSelectedVersion(t *testing.T) {
	item := workspaceItem{pkg: Package{ID: "A.Pkg", Source: "winget"}, installed: "1.0", available: "3.0"}
	items := []batchItem{{action: retryOpUpgrade, status: batchDone, item: item}}

	// User chose 2.5, not the latest 3.0.
	sel := map[string]string{item.key(): "2.5"}
	rec := buildTUIHistoryRecord(items, historyTriggerTUI, sel)
	if rec.Items[0].ToVersion != "2.5" {
		t.Errorf("ToVersion = %q, want 2.5 (the chosen version, not available 3.0)", rec.Items[0].ToVersion)
	}

	// No selection -> falls back to available.
	rec2 := buildTUIHistoryRecord(items, historyTriggerTUI, nil)
	if rec2.Items[0].ToVersion != "3.0" {
		t.Errorf("ToVersion = %q, want 3.0 (available, no version chosen)", rec2.Items[0].ToVersion)
	}
}

// Regression: an uninstall must record no to_version, even when a stale version
// pick lingers in selectedVersions (selections persist globally) or the package
// has an available update. Only from_version (the removed version) is meaningful.
func TestBuildTUIHistoryRecordUninstallHasNoToVersion(t *testing.T) {
	item := workspaceItem{pkg: Package{ID: "Gone.Pkg", Source: "winget"}, installed: "1.0", available: "2.0"}
	items := []batchItem{{action: retryOpUninstall, status: batchDone, item: item}}
	sel := map[string]string{item.key(): "2.5"} // stale pick from a prior detail-panel selection

	rec := buildTUIHistoryRecord(items, historyTriggerTUI, sel)
	if rec.Items[0].ToVersion != "" {
		t.Errorf("uninstall ToVersion = %q, want empty (no stale selected/available version)", rec.Items[0].ToVersion)
	}
	if rec.Items[0].FromVersion != "1.0" {
		t.Errorf("uninstall FromVersion = %q, want 1.0 (the removed version)", rec.Items[0].FromVersion)
	}
}

func TestUpgradePlannedWritesHistory(t *testing.T) {
	origStream := streamUpgradeFn
	origExit := cliExitCode
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error {
		if pkg.ID == "Bad.Pkg" {
			return errors.New("install failed")
		}
		return nil
	}
	t.Cleanup(func() { streamUpgradeFn = origStream; cliExitCode = origExit })
	recs := captureHistory(t)

	pkgs := []Package{
		{Name: "Good", ID: "Good.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
		{Name: "Bad", ID: "Bad.Pkg", Source: "winget", Version: "1.0", Available: "1.1"},
	}
	var buf bytes.Buffer
	if err := upgradePlanned(context.Background(), pkgs, 0, "visible", &buf); err != nil {
		t.Fatalf("upgradePlanned: %v", err)
	}

	if len(*recs) != 1 {
		t.Fatalf("history records = %d, want 1", len(*recs))
	}
	rec := (*recs)[0]
	if rec.Trigger != historyTriggerCLIAll {
		t.Errorf("trigger = %q, want %q", rec.Trigger, historyTriggerCLIAll)
	}
	if len(rec.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(rec.Items))
	}
	if rec.Items[0].Status != historyStatusOK || rec.Items[0].ToVersion != "2.0" {
		t.Errorf("item[0] = %+v, want ok ->2.0", rec.Items[0])
	}
	if rec.Items[1].Status != historyStatusError || rec.Items[1].Error == "" {
		t.Errorf("item[1] = %+v, want error with message", rec.Items[1])
	}
}

func TestUpgradeAutoTriggerIsCliAuto(t *testing.T) {
	origStream := streamUpgradeFn
	origExit := cliExitCode
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error { return nil }
	t.Cleanup(func() { streamUpgradeFn = origStream; cliExitCode = origExit })
	recs := captureHistory(t)

	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Auto.Pkg", "winget"): {UpdatePolicy: PolicyAuto},
	}
	raw := []Package{{Name: "Auto", ID: "Auto.Pkg", Source: "winget", Version: "1.0", Available: "2.0"}}

	var buf bytes.Buffer
	if err := upgradeAuto(context.Background(), raw, settings, &buf); err != nil {
		t.Fatalf("upgradeAuto: %v", err)
	}
	if len(*recs) != 1 || (*recs)[0].Trigger != historyTriggerCLIAuto {
		t.Fatalf("want one cli-auto record, got %+v", *recs)
	}
}

func TestUpgradePlannedEmptyWritesNoHistory(t *testing.T) {
	recs := captureHistory(t)
	var buf bytes.Buffer
	if err := upgradePlanned(context.Background(), nil, 0, "visible", &buf); err != nil {
		t.Fatalf("upgradePlanned: %v", err)
	}
	if len(*recs) != 0 {
		t.Fatalf("history records = %d, want 0 for an empty plan", len(*recs))
	}
}

func TestRecordImportHistory(t *testing.T) {
	recs := captureHistory(t)
	recordImportHistory(historyTriggerTUIImport,
		[]string{"A.Pkg", "B.Pkg"},
		[]string{"winget", "winget"},
		[]string{"", "3.0"},
		[]error{nil, errors.New("nope")})

	if len(*recs) != 1 {
		t.Fatalf("records = %d, want 1", len(*recs))
	}
	rec := (*recs)[0]
	if rec.Trigger != historyTriggerTUIImport || rec.Action != historyActionInstall {
		t.Errorf("trigger/action = %q/%q, want tui-import/install", rec.Trigger, rec.Action)
	}
	if rec.Items[0].Status != historyStatusOK || rec.Items[0].FromVersion != "" {
		t.Errorf("item[0] = %+v, want ok with no from-version", rec.Items[0])
	}
	if rec.Items[1].Status != historyStatusError || rec.Items[1].ToVersion != "3.0" || rec.Items[1].Error != "nope" {
		t.Errorf("item[1] = %+v, want error ->3.0 'nope'", rec.Items[1])
	}
}

func TestExecuteImportWritesHistory(t *testing.T) {
	origInstall := importInstallFn
	origExit := cliExitCode
	importInstallFn = func(ctx context.Context, pkg Package, version string) (string, error) {
		if pkg.ID == "Bad.Pkg" {
			return "", errors.New("boom")
		}
		return "", nil
	}
	t.Cleanup(func() { importInstallFn = origInstall; cliExitCode = origExit })
	recs := captureHistory(t)

	pkgs := []importPkg{
		{Name: "Good", ID: "Good.Pkg", Source: "winget"},
		{Name: "Bad", ID: "Bad.Pkg", Source: "winget"},
	}
	var buf bytes.Buffer
	if err := executeImport(&buf, pkgs); err != nil {
		t.Fatalf("executeImport: %v", err)
	}

	if len(*recs) != 1 {
		t.Fatalf("records = %d, want 1", len(*recs))
	}
	rec := (*recs)[0]
	if rec.Trigger != historyTriggerCLIImport || rec.Action != historyActionInstall {
		t.Errorf("trigger/action = %q/%q, want cli-import/install", rec.Trigger, rec.Action)
	}
	byID := map[string]historyItem{}
	for _, it := range rec.Items {
		byID[it.ID] = it
	}
	if byID["Good.Pkg"].Status != historyStatusOK {
		t.Errorf("Good.Pkg status = %q, want ok", byID["Good.Pkg"].Status)
	}
	if byID["Bad.Pkg"].Status != historyStatusError || byID["Bad.Pkg"].Error == "" {
		t.Errorf("Bad.Pkg = %+v, want error with message", byID["Bad.Pkg"])
	}
}

func TestUpgradeIDsWritesHistoryWithSkips(t *testing.T) {
	origStream := streamUpgradeFn
	origExit := cliExitCode
	streamUpgradeFn = func(ctx context.Context, pkg Package, out io.Writer) error { return nil }
	t.Cleanup(func() { streamUpgradeFn = origStream; cliExitCode = origExit })
	recs := captureHistory(t)

	settings := DefaultSettings()
	settings.Packages = map[string]PackageOverride{
		packageRuleKey("Held.Pkg", "winget"): {UpdatePolicy: PolicyHold},
	}
	raw := []Package{
		{Name: "Good", ID: "Good.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
		{Name: "Held", ID: "Held.Pkg", Source: "winget", Version: "1.0", Available: "2.0"},
	}

	var buf bytes.Buffer
	if err := upgradeIDs(context.Background(), []string{"Good.Pkg", "Held.Pkg", "Missing.Pkg"}, raw, settings, &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}

	if len(*recs) != 1 {
		t.Fatalf("history records = %d, want 1", len(*recs))
	}
	rec := (*recs)[0]
	if rec.Trigger != historyTriggerCLIID {
		t.Errorf("trigger = %q, want %q", rec.Trigger, historyTriggerCLIID)
	}
	byID := map[string]historyItem{}
	for _, it := range rec.Items {
		byID[it.ID] = it
	}
	if byID["Good.Pkg"].Status != historyStatusOK {
		t.Errorf("Good.Pkg status = %q, want ok", byID["Good.Pkg"].Status)
	}
	if byID["Held.Pkg"].Status != historyStatusSkipped || byID["Held.Pkg"].Notes != "held by policy" {
		t.Errorf("Held.Pkg = %+v, want skipped/held by policy", byID["Held.Pkg"])
	}
	if byID["Missing.Pkg"].Status != historyStatusSkipped || byID["Missing.Pkg"].Notes != "no update available" {
		t.Errorf("Missing.Pkg = %+v, want skipped/no update available", byID["Missing.Pkg"])
	}
}
