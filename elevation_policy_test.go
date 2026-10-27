package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// withPolicySettings installs s as appSettings and pins portable evidence to
// the given IDs for the test.
func withPolicySettings(t *testing.T, s Settings, portableIDs ...string) {
	t.Helper()
	origSettings, origPortable := appSettings, portableInstalled
	t.Cleanup(func() { appSettings, portableInstalled = origSettings, origPortable })
	appSettings = s
	portableInstalled = func(id string) bool {
		for _, p := range portableIDs {
			if strings.EqualFold(p, id) {
				return true
			}
		}
		return false
	}
}

func TestUserScopeReason(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name     string
		global   InstallScope
		rule     *PackageOverride
		portable bool
		action   retryOp
		want     string
	}{
		{"no rule", ScopeDefault, nil, false, retryOpUpgrade, ""},
		{"user-scope rule", ScopeDefault, &PackageOverride{Scope: ScopeUser}, false, retryOpUpgrade, "user-scope rule"},
		{"global user scope", ScopeUser, nil, false, retryOpInstall, "user scope setting"},
		{"machine rule beats global user scope", ScopeUser, &PackageOverride{Scope: ScopeMachine}, false, retryOpInstall, ""},
		{"portable install", ScopeDefault, nil, true, retryOpUpgrade, "portable install"},
		{"elevate=true clears it for upgrade", ScopeDefault, &PackageOverride{Scope: ScopeUser, Elevate: &yes}, false, retryOpUpgrade, ""},
		{"elevate=true clears portable for install", ScopeDefault, &PackageOverride{Elevate: &yes}, true, retryOpInstall, ""},
		{"elevate=true never clears it for uninstall", ScopeDefault, &PackageOverride{Scope: ScopeUser, Elevate: &yes}, false, retryOpUninstall, "user-scope rule"},
		{"elevate=false alone is not user scope", ScopeDefault, &PackageOverride{Elevate: &no}, false, retryOpUpgrade, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			s.Scope = tt.global
			if tt.rule != nil {
				s.setOverride("Test.App", "winget", *tt.rule)
			}
			var portable []string
			if tt.portable {
				portable = []string{"Test.App"}
			}
			withPolicySettings(t, s, portable...)
			if got := userScopeReason(s, tt.action, "Test.App", "winget"); got != tt.want {
				t.Fatalf("userScopeReason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNoElevationReasonIncludesElevateFalse(t *testing.T) {
	no := false
	s := DefaultSettings()
	s.setOverride("Test.App", "winget", PackageOverride{Elevate: &no})
	withPolicySettings(t, s)
	if got := noElevationReason(s, retryOpUpgrade, "Test.App", "winget"); got != "elevate=false rule" {
		t.Fatalf("noElevationReason = %q, want the elevate=false rule", got)
	}
}

func TestIsPortableInstalledIgnoresCase(t *testing.T) {
	dirs := []string{"anthropic.claudecode_Microsoft.Winget.Source_8wekyb3d8bbwe"}
	if !isPortableInstalled("Anthropic.ClaudeCode", dirs) {
		t.Fatal("isPortableInstalled must match the package directory case-insensitively")
	}
}

// TestPackageRouteRefusesUserScopeInElevatedSession: an elevated WinTUI runs
// winget as administrator, so a user-scope package is refused before winget,
// except an install/upgrade with an explicit elevate=true rule.
func TestPackageRouteRefusesUserScopeInElevatedSession(t *testing.T) {
	yes := true
	tests := []struct {
		name       string
		rule       PackageOverride
		action     retryOp
		wantRefuse bool
	}{
		{"user-scope upgrade refused", PackageOverride{Scope: ScopeUser}, retryOpUpgrade, true},
		{"user-scope uninstall refused", PackageOverride{Scope: ScopeUser}, retryOpUninstall, true},
		{"elevate=true upgrade runs", PackageOverride{Scope: ScopeUser, Elevate: &yes}, retryOpUpgrade, false},
		{"elevate=true uninstall still refused", PackageOverride{Scope: ScopeUser, Elevate: &yes}, retryOpUninstall, true},
		{"machine-scope upgrade runs", PackageOverride{Scope: ScopeMachine}, retryOpUpgrade, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRouteHarness(t, false, nil)
			isElevated = func() bool { return true }
			s := DefaultSettings()
			s.setOverride("Test.App", "winget", tt.rule)
			withPolicySettings(t, s)

			_, err := drainRoute(runActionStreamForPackage(context.Background(), tt.action, "Test.App", "winget", string(tt.action), "--id", "Test.App"))
			_, refused := asElevationRefusal(err)
			if refused != tt.wantRefuse {
				t.Fatalf("refused = %v (err %v), want %v", refused, err, tt.wantRefuse)
			}
			starts, normal := h.counts()
			if starts != 0 {
				t.Fatalf("helper starts = %d in an elevated session, want 0", starts)
			}
			if wantNormal := map[bool]int{true: 0, false: 1}[tt.wantRefuse]; normal != wantNormal {
				t.Fatalf("winget runs = %d, want %d", normal, wantNormal)
			}
		})
	}
}

// TestPackageRouteNeverElevatesUserScope: silent + auto-elevate would send the
// action to the helper upfront, and a requires-admin failure would retry it
// there; neither may happen for a user-scope package or an elevate=false rule.
func TestPackageRouteNeverElevatesUserScope(t *testing.T) {
	no := false
	requiresAdmin := errors.New("This operation requires elevation")
	tests := []struct {
		name     string
		global   InstallScope
		rule     *PackageOverride
		portable bool
	}{
		{"user-scope rule", ScopeDefault, &PackageOverride{Scope: ScopeUser}, false},
		{"global user scope", ScopeUser, nil, false},
		{"portable install", ScopeDefault, nil, true},
		{"elevate=false rule", ScopeDefault, &PackageOverride{Elevate: &no}, false},
	}
	for _, tt := range tests {
		for _, action := range []retryOp{retryOpInstall, retryOpUpgrade, retryOpUninstall} {
			t.Run(tt.name+"/"+string(action), func(t *testing.T) {
				h := newRouteHarness(t, false, requiresAdmin)
				s := DefaultSettings()
				s.InstallMode = ModeSilent
				s.Scope = tt.global
				if tt.rule != nil {
					s.setOverride("Test.App", "winget", *tt.rule)
				}
				var portable []string
				if tt.portable {
					portable = []string{"Test.App"}
				}
				withPolicySettings(t, s, portable...)

				var err error
				if action == retryOpUninstall {
					_, out, errCh := uninstallPackageStreamCtx(context.Background(), Package{ID: "Test.App", Source: "winget"}, false)
					_, err = drainRoute(out, errCh)
				} else {
					_, err = drainRoute(runActionStreamForPackage(context.Background(), action, "Test.App", "winget", string(action), "--id", "Test.App"))
				}
				if err == nil {
					t.Fatal("route err = nil, want the requires-admin failure")
				}
				if starts, normal := h.counts(); starts != 0 || normal == 0 {
					t.Fatalf("helper starts = %d, winget runs = %d; want no helper and a non-elevated run", starts, normal)
				}
			})
		}
	}
}

// TestUninstallRouteUsesPackageRules: uninstall used to read only the global
// settings; an elevate=true rule must now reach it too.
func TestUninstallRouteUsesPackageRules(t *testing.T) {
	yes := true
	h := newRouteHarness(t, false, nil)
	s := DefaultSettings()
	s.setOverride("Test.App", "winget", PackageOverride{Elevate: &yes})
	withPolicySettings(t, s)

	_, out, errCh := uninstallPackageStreamCtx(context.Background(), Package{ID: "Test.App", Source: "winget"}, false)
	if _, err := drainRoute(out, errCh); err != nil {
		t.Fatalf("route err = %v", err)
	}
	if starts, _ := h.counts(); starts != 1 {
		t.Fatalf("helper starts = %d, want 1 (elevate=true uninstall goes to the helper first)", starts)
	}
}

func TestForcedElevationRefusesUserScope(t *testing.T) {
	yes := true
	tests := []struct {
		name      string
		separate  bool
		rule      PackageOverride
		action    retryOp
		wantStart int
	}{
		{"user-scope rule", false, PackageOverride{Scope: ScopeUser}, retryOpUpgrade, 0},
		{"elevate=true user-scope upgrade retries", false, PackageOverride{Scope: ScopeUser, Elevate: &yes}, retryOpUpgrade, 1},
		{"elevate=true user-scope uninstall does not", false, PackageOverride{Scope: ScopeUser, Elevate: &yes}, retryOpUninstall, 0},
		{"separate account keeps elevate=true user scope off the helper", true, PackageOverride{Scope: ScopeUser, Elevate: &yes}, retryOpUpgrade, 0},
		{"separate account: unknown scope retries", true, PackageOverride{}, retryOpUpgrade, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRouteHarness(t, tt.separate, nil)
			s := DefaultSettings()
			s.setOverride("Test.App", "winget", tt.rule)
			withPolicySettings(t, s)

			out, errCh, initErr := runForcedElevated(context.Background(), tt.action, "Test.App", "winget", string(tt.action), "--id", "Test.App")
			starts, _ := h.counts()
			if starts != tt.wantStart {
				t.Fatalf("helper starts = %d, want %d", starts, tt.wantStart)
			}
			if tt.wantStart == 0 {
				if initErr != nil {
					t.Fatalf("initErr = %v, want the refusal on the stream", initErr)
				}
				if _, err := drainRoute(out, errCh); !isRefusal(err) {
					t.Fatalf("stream err = %v, want an elevation refusal", err)
				}
			}
		})
	}
}

func isRefusal(err error) bool {
	_, ok := asElevationRefusal(err)
	return ok
}

// TestCtrlEExcludesUserScopeItems: Q2=B — the explicit elevated retry does
// not override a user-scope or elevate=false rule; the reason is shown.
func TestCtrlEExcludesUserScopeItems(t *testing.T) {
	forceNotElevated(t)
	no := false
	s := DefaultSettings()
	s.AutoElevate = false
	s.setOverride("User.Pkg", "winget", PackageOverride{Scope: ScopeUser})
	s.setOverride("NoElev.Pkg", "winget", PackageOverride{Elevate: &no})
	withPolicySettings(t, s)

	failed := func(id string) batchItem {
		bi := batchItem{
			action: retryOpUpgrade,
			item:   workspaceItem{pkg: Package{Name: id, ID: id, Source: "winget"}},
			output: "0x8a150019",
		}
		bi.markFailed(errors.New("this action requires administrator privileges (0x8a150019)"))
		return bi
	}

	m := newExecModal(retryOpUpgrade, []batchItem{failed("User.Pkg"), failed("NoElev.Pkg")})
	m.phase = execPhaseComplete
	if m.hasElevationCandidates() {
		t.Fatal("hasElevationCandidates = true; user-scope and elevate=false items must not be offered for an elevated retry")
	}
	if items := m.elevationCandidateItems(); len(items) != 0 {
		t.Fatalf("elevationCandidateItems = %d items, want 0", len(items))
	}
	_, body, actions := m.viewComplete()
	text := stripANSI(strings.Join(body, "\n"))
	for _, want := range []string{"Not elevated: user-scope rule.", "Not elevated: elevate=false rule."} {
		if !strings.Contains(text, want) {
			t.Fatalf("results modal lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(stripANSI(actions), "ctrl+e") {
		t.Fatalf("ctrl+e offered with no eligible items: %q", stripANSI(actions))
	}

	// A plain elevation candidate in the same batch is still retried.
	m = newExecModal(retryOpUpgrade, []batchItem{failed("User.Pkg"), failed("Admin.Tool")})
	items := m.elevationCandidateItems()
	if len(items) != 1 || items[0].item.pkg.ID != "Admin.Tool" {
		t.Fatalf("elevationCandidateItems = %#v, want only Admin.Tool", items)
	}
}

func TestBatchRefusalIsRecordedAsSkipped(t *testing.T) {
	refusal := refuseInElevatedSession(retryOpUpgrade, "user-scope rule")
	items := []batchItem{
		{action: retryOpUpgrade, item: workspaceItem{pkg: Package{ID: "Ok.Pkg"}}, status: batchDone},
		{action: retryOpUpgrade, item: workspaceItem{pkg: Package{ID: "User.Pkg"}}, status: batchSkipped, err: refusal},
	}
	rec := buildTUIHistoryRecord(items, historyTriggerTUI, nil)
	got := rec.Items[1]
	if got.Status != historyStatusSkipped || got.Error != "" || !strings.Contains(got.Notes, "user-scope rule") {
		t.Fatalf("history item = %+v, want skipped with a user-scope note and no error", got)
	}
	m := newExecModal(retryOpUpgrade, items)
	m.phase = execPhaseComplete
	title, body, _ := m.viewComplete()
	if text := stripANSI(strings.Join(body, "\n")); !strings.Contains(text, "1 succeeded, 1 skipped") {
		t.Fatalf("summary does not count the refusal as skipped:\n%s", text)
	}
	if title != "Upgrade Complete" {
		t.Fatalf("title = %q, want %q", title, "Upgrade Complete")
	}

	// Nothing ran: the rc2 VM run's elevated bat uninstall read "Uninstall
	// Complete / 0 succeeded, 1 skipped".
	only := newExecModal(retryOpUninstall, items[1:])
	only.phase = execPhaseComplete
	if title, _, _ := only.viewComplete(); title != "Uninstall Skipped" {
		t.Fatalf("all-skipped title = %q, want %q", title, "Uninstall Skipped")
	}
}

// TestUpgradeAllRefusalIsNotAFailure: a scheduled `upgrade --all` must stay
// exit 0 when a package is refused by policy; `upgrade --id` treats it like a
// hold (exit 1).
func TestUpgradeAllRefusalIsNotAFailure(t *testing.T) {
	origStream, origExit, origRecord := streamUpgradeFn, cliExitCode, historyRecordFn
	t.Cleanup(func() { streamUpgradeFn, cliExitCode, historyRecordFn = origStream, origExit, origRecord })
	streamUpgradeFn = func(_ context.Context, pkg Package, _ io.Writer) error {
		if pkg.ID == "User.Pkg" {
			return refuseInElevatedSession(retryOpUpgrade, "user-scope rule")
		}
		return nil
	}
	var recorded historyRecord
	historyRecordFn = func(rec historyRecord) (string, error) { recorded = rec; return "", nil }

	raw := []Package{
		{Name: "User", ID: "User.Pkg", Source: "winget", Version: "1", Available: "2"},
		{Name: "Other", ID: "Other.Pkg", Source: "winget", Version: "1", Available: "2"},
	}
	cliExitCode = 0
	var buf bytes.Buffer
	if err := upgradeAll(context.Background(), raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeAll: %v", err)
	}
	if cliExitCode != 0 {
		t.Fatalf("cliExitCode = %d after a policy refusal in upgrade --all, want 0", cliExitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "1/1 succeeded.") || !strings.Contains(out, "Skipped (run WinTUI non-elevated): User.Pkg") {
		t.Fatalf("summary = %q", out)
	}
	if len(recorded.Items) != 2 || recorded.Items[0].Status != historyStatusSkipped || recorded.Items[0].Notes == "" {
		t.Fatalf("history items = %+v, want User.Pkg skipped with a note", recorded.Items)
	}

	cliExitCode = 0
	buf.Reset()
	if err := upgradeIDs(context.Background(), []string{"User.Pkg"}, raw, DefaultSettings(), &buf); err != nil {
		t.Fatalf("upgradeIDs: %v", err)
	}
	if cliExitCode != 1 {
		t.Fatalf("cliExitCode = %d after refusing the named package, want 1", cliExitCode)
	}
}

func TestDoctorPrivilegesShowsASeparateElevationAccount(t *testing.T) {
	forceNotElevated(t)
	origElevator := globalElevator
	t.Cleanup(func() { globalElevator = origElevator })
	for _, separate := range []bool{false, true} {
		globalElevator = &elevationManager{}
		pinPredictedSeparateAccount(t, separate)
		got := strings.Contains(checkPrivileges().Details, "different account")
		if got != separate {
			t.Fatalf("separate=%v: Privileges row = %q", separate, checkPrivileges().Details)
		}
	}
}

// TestResultModalShowsElevationNotes: the results modal's key lines showed
// "Running without elevation: …" only when the reason happened to contain
// "failed" (the rc2 VM run: shown for an identity failure, hidden for
// "elevation was cancelled"), and never the elevate=true note.
func TestResultModalShowsElevationNotes(t *testing.T) {
	output := strings.Join([]string{
		"Running without elevation: elevation was cancelled",
		"Found hexyl [sharkdp.hexyl] Version 0.17.0",
		"This command requires administrator privileges to execute.",
		"Elevation required. Requesting...",
		"Automatic elevation failed: elevation was cancelled",
	}, "\n")
	got := extractKeyLogLines(output)
	if len(got) == 0 || got[0] != "Running without elevation: elevation was cancelled" {
		t.Fatalf("key lines = %q, want the fallback note first", got)
	}
	if got := extractKeyLogLines(elevateRuleSkippedNote + "\nFound delta"); len(got) != 1 || got[0] != elevateRuleSkippedNote {
		t.Fatalf("key lines = %q, want the elevate=true note", got)
	}
}
