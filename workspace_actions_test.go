package main

import (
	"reflect"
	"testing"
	"time"
)

func TestBeginApplyActionBuildsMixedBatch(t *testing.T) {
	ws := newWorkspaceScreen()

	install := workspaceItem{pkg: Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"}}
	upgrade := workspaceItem{
		pkg:         Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.0", Available: "2.1"},
		upgradeable: true,
		installed:   "2.0",
		available:   "2.1",
	}
	uninstall := workspaceItem{pkg: Package{Name: "Legacy Tool", ID: "Contoso.Legacy", Source: "winget", Version: "1.0"}}

	ws.installQueue = []workspaceItem{install}
	ws.installQueueMap[install.key()] = true
	ws.items = []workspaceItem{upgrade, uninstall}
	ws.selected[upgrade.key()] = true
	ws.selected[uninstall.key()] = true

	next, _ := ws.beginApplyAction()
	got := next.(workspaceScreen)

	if got.state != workspaceConfirm {
		t.Fatalf("state = %v, want workspaceConfirm", got.state)
	}
	if got.modal == nil {
		t.Fatal("modal = nil, want batch review modal")
	}
	if got.modal.action != retryOpApply {
		t.Fatalf("modal.action = %q, want apply", got.modal.action)
	}

	var actions []retryOp
	for _, item := range got.modal.items {
		actions = append(actions, item.action)
	}
	want := []retryOp{retryOpInstall, retryOpUpgrade, retryOpUninstall}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("batch actions = %#v, want %#v", actions, want)
	}
}

func TestBeginActionInstallFallsBackToFocusedSearchResult(t *testing.T) {
	ws := newWorkspaceScreen()
	ws.searchResults = []Package{{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget"}}

	next, _ := ws.beginAction(retryOpInstall)
	got := next.(workspaceScreen)

	if got.modal == nil {
		t.Fatal("modal = nil, want install review modal")
	}
	if got.modal.action != retryOpInstall {
		t.Fatalf("modal.action = %q, want install", got.modal.action)
	}
	if len(got.modal.items) != 1 {
		t.Fatalf("len(modal.items) = %d, want 1", len(got.modal.items))
	}
	if got.modal.items[0].action != retryOpInstall {
		t.Fatalf("modal.items[0].action = %q, want install", got.modal.items[0].action)
	}
	if got.modal.items[0].item.pkg.ID != "Mozilla.Firefox" {
		t.Fatalf("modal.items[0].item.pkg.ID = %q, want Mozilla.Firefox", got.modal.items[0].item.pkg.ID)
	}
}

func TestSortBatchItemsMovesSelfUpgradeToEnd(t *testing.T) {
	origExe := currentExecutablePath
	origEval := evalSymlinksPath
	currentExecutablePath = func() (string, error) {
		return `C:\Users\ktsio\AppData\Local\Microsoft\WinGet\Links\wintui.exe`, nil
	}
	evalSymlinksPath = func(path string) (string, error) { return path, nil }
	t.Cleanup(func() {
		currentExecutablePath = origExe
		evalSymlinksPath = origEval
	})

	items := []batchItem{
		newBatchItem(retryOpUpgrade, workspaceItem{pkg: Package{ID: selfPackageID, Source: "winget"}}),
		newBatchItem(retryOpInstall, workspaceItem{pkg: Package{ID: "Mozilla.Firefox", Source: "winget"}}),
	}

	got := sortBatchItems(items)

	if got[0].item.pkg.ID != "Mozilla.Firefox" {
		t.Fatalf("got[0].item.pkg.ID = %q, want Mozilla.Firefox", got[0].item.pkg.ID)
	}
	if got[1].item.pkg.ID != selfPackageID {
		t.Fatalf("got[1].item.pkg.ID = %q, want %s", got[1].item.pkg.ID, selfPackageID)
	}
	if !isSelfUpgradeBatchItem(got[1]) {
		t.Fatal("expected trailing batch item to be detected as self-upgrade")
	}
}

func TestWorkspaceDataFromDiskPrimesCacheForOverrideRebuild(t *testing.T) {
	originalCache := cache
	originalSettings := appSettings
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	appSettings = DefaultSettings()
	t.Cleanup(func() {
		cache = originalCache
		appSettings = originalSettings
	})

	ws := newWorkspaceScreen()
	msg := workspaceDataMsg{
		installed: []Package{
			{ID: "Hidden.Pkg", Name: "Hidden", Version: "1.0", Source: "winget"},
		},
		upgradeable: []Package{
			{ID: "Hidden.Pkg", Name: "Hidden", Version: "1.0", Available: "2.0.0", Source: "winget"},
		},
		fromDisk: true,
		savedAt:  time.Now().Add(-10 * time.Minute),
	}

	next, _ := ws.update(msg)
	got := next.(workspaceScreen)
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("Hidden.Pkg", "winget"): {Ignore: true},
	}

	got.rebuildItemsFromCache()
	if got.hiddenUpgrades != 1 {
		t.Fatalf("hiddenUpgrades = %d, want 1", got.hiddenUpgrades)
	}
	for _, item := range got.items {
		if item.upgradeable && item.pkg.ID == "Hidden.Pkg" && item.pkg.Source == "winget" {
			t.Fatal("expected ignored disk-cached upgrade to disappear after rebuild")
		}
	}
}

func TestDeduplicateUninstallItemsCollapseDuplicateIDs(t *testing.T) {
	comet1 := workspaceItem{pkg: Package{Name: "Comet", ID: "Perplexity.Comet", Source: "winget", Version: "1.0"}}
	comet2 := workspaceItem{pkg: Package{Name: "Comet", ID: "Perplexity.Comet", Source: "winget", Version: "2.0"}}
	firefox := workspaceItem{pkg: Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"}}

	items := []batchItem{
		newBatchItem(retryOpUninstall, comet1),
		newBatchItem(retryOpUninstall, comet2),
		newBatchItem(retryOpUninstall, firefox),
	}

	got := deduplicateUninstallItems(items)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (duplicates collapsed)", len(got))
	}
	if got[0].item.pkg.ID != "Perplexity.Comet" {
		t.Fatalf("got[0].pkg.ID = %q, want Perplexity.Comet", got[0].item.pkg.ID)
	}
	if !got[0].allVersions {
		t.Fatal("got[0].allVersions = false, want true (duplicate ID detected)")
	}
	if got[1].item.pkg.ID != "Mozilla.Firefox" {
		t.Fatalf("got[1].pkg.ID = %q, want Mozilla.Firefox", got[1].item.pkg.ID)
	}
	if got[1].allVersions {
		t.Fatal("got[1].allVersions = true, want false (unique ID)")
	}
}

func TestDeduplicatePreservesNonUninstallItems(t *testing.T) {
	pkg := workspaceItem{pkg: Package{Name: "Git", ID: "Git.Git", Source: "winget"}}

	items := []batchItem{
		newBatchItem(retryOpInstall, pkg),
		newBatchItem(retryOpUpgrade, pkg),
	}

	got := deduplicateUninstallItems(items)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (non-uninstall items preserved)", len(got))
	}
}
