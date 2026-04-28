package main

import (
	"reflect"
	"strings"
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

func TestCycleFocusedUpdatePolicyOnInstalledPackage(t *testing.T) {
	original := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = original
		cache = originalCache
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{{
		pkg:       Package{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"},
		installed: "130.0",
	}}

	next, _ := ws.update(keyMsg("t"))
	got := next.(workspaceScreen)
	o := appSettings.getOverride("Mozilla.Firefox", "winget")
	if o.UpdatePolicy != PolicyAuto {
		t.Fatalf("policy after first t = %q, want auto", o.UpdatePolicy)
	}
	if len(got.items) != 1 || got.items[0].upgradeable {
		t.Fatalf("items after auto = %#v, want installed item retained", got.items)
	}

	next, _ = got.update(keyMsg("t"))
	got = next.(workspaceScreen)
	o = appSettings.getOverride("Mozilla.Firefox", "winget")
	if o.UpdatePolicy != PolicyHold {
		t.Fatalf("policy after second t = %q, want hold", o.UpdatePolicy)
	}

	_, _ = got.update(keyMsg("t"))
	o = appSettings.getOverride("Mozilla.Firefox", "winget")
	if !o.isEmpty() {
		t.Fatalf("policy after third t = %#v, want cleared ask/default", o)
	}
	if appSettings.hasOverride("Mozilla.Firefox", "winget") {
		t.Fatal("expected empty ask policy to remove package override")
	}
}

func TestCycleFocusedUpdatePolicyHoldRemovesUpdateImmediately(t *testing.T) {
	original := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = original
		cache = originalCache
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{{
		pkg:         Package{Name: "Git", ID: "Git.Git", Source: "winget", Version: "2.0", Available: "2.1"},
		upgradeable: true,
		installed:   "2.0",
		available:   "2.1",
	}}

	next, _ := ws.update(keyMsg("t")) // ask -> auto
	got := next.(workspaceScreen)
	next, _ = got.update(keyMsg("t")) // auto -> hold
	got = next.(workspaceScreen)

	if got.hiddenUpgrades != 1 {
		t.Fatalf("hiddenUpgrades = %d, want 1 held package", got.hiddenUpgrades)
	}
	if len(got.items) != 1 {
		t.Fatalf("len(items) = %d, want 1 installed item", len(got.items))
	}
	if got.items[0].upgradeable {
		t.Fatalf("held package still upgradeable: %#v", got.items[0])
	}
	if got.items[0].pkg.ID != "Git.Git" {
		t.Fatalf("item ID = %q, want Git.Git", got.items[0].pkg.ID)
	}
}

func TestCycleFocusedUpdatePolicyIgnoresSearchResults(t *testing.T) {
	original := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = original
		cache = originalCache
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.searchResults = []Package{{Name: "Firefox", ID: "Mozilla.Firefox", Source: "winget", Version: "130.0"}}

	next, _ := ws.update(keyMsg("t"))
	got := next.(workspaceScreen)
	if appSettings.hasOverride("Mozilla.Firefox", "winget") {
		t.Fatal("search result should not receive a package policy override")
	}
	if len(got.searchResults) != 1 {
		t.Fatalf("searchResults = %#v, want unchanged", got.searchResults)
	}
}

func TestCycleFocusedUpdatePolicyIgnoresNonWingetSources(t *testing.T) {
	original := appSettings
	originalCache := cache
	appSettings = DefaultSettings()
	cache = &packageCache{ttl: 2 * time.Minute, diskTTL: 24 * time.Hour}
	t.Cleanup(func() {
		appSettings = original
		cache = originalCache
	})

	ws := newWorkspaceScreen()
	ws.state = workspaceReady
	ws.items = []workspaceItem{{
		pkg:       Package{Name: "Some MSIX App", ID: "SomeMsix_abc123", Source: ""},
		installed: "1.0",
	}}

	next, _ := ws.update(keyMsg("t"))
	got := next.(workspaceScreen)
	if appSettings.hasOverride("SomeMsix_abc123", "") {
		t.Fatal("non-winget package should not receive a policy override")
	}
	if len(got.items) != 1 {
		t.Fatalf("items = %#v, want unchanged", got.items)
	}
}

func TestRenderItemTextShowsPolicyBadges(t *testing.T) {
	original := appSettings
	appSettings = DefaultSettings()
	appSettings.Packages = map[string]PackageOverride{
		packageRuleKey("Auto.Pkg", "winget"): {UpdatePolicy: PolicyAuto},
		packageRuleKey("Held.Pkg", "winget"): {UpdatePolicy: PolicyHold},
	}
	t.Cleanup(func() { appSettings = original })

	ws := newWorkspaceScreen()
	auto := stripANSI(ws.renderItemText(workspaceItem{
		pkg:       Package{Name: "Auto", ID: "Auto.Pkg", Source: "winget", Version: "1.0"},
		installed: "1.0",
	}, 80, false))
	if !strings.Contains(auto, "[AUTO]") {
		t.Fatalf("auto row = %q, want [AUTO] badge", auto)
	}

	held := stripANSI(ws.renderItemText(workspaceItem{
		pkg:       Package{Name: "Held", ID: "Held.Pkg", Source: "winget", Version: "1.0"},
		installed: "1.0",
	}, 80, false))
	if !strings.Contains(held, "[HOLD]") {
		t.Fatalf("held row = %q, want [HOLD] badge", held)
	}
}
