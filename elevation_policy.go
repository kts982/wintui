package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// elevationNotes collects what the elevation routes decided for one action
// that the user should know about — chiefly that it ran without elevation —
// for the action's history record. It rides on the action's context and is
// safe for concurrent use; a nil *elevationNotes reads as empty.
type elevationNotes struct {
	mu    sync.Mutex
	notes []string
}

type elevationNotesKey struct{}

// withElevationNotes returns ctx carrying a fresh elevationNotes.
func withElevationNotes(ctx context.Context) (context.Context, *elevationNotes) {
	n := &elevationNotes{}
	return context.WithValue(ctx, elevationNotesKey{}, n), n
}

func (n *elevationNotes) String() string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return strings.Join(n.notes, "; ")
}

// noteElevation records note on ctx's elevationNotes, if it has one, and
// returns it for the action's output.
func noteElevation(ctx context.Context, note string) string {
	if n, ok := ctx.Value(elevationNotesKey{}).(*elevationNotes); ok {
		n.mu.Lock()
		n.notes = append(n.notes, note)
		n.mu.Unlock()
	}
	return note
}

// portableInstalled reports whether pkgID has winget's portable package
// directory in the current profile — evidence the package is user-scope even
// without a rule. A var so tests can stub the filesystem.
var portableInstalled = func(pkgID string) bool {
	return isPortableInstalled(pkgID, wingetPortablePackageDirNames())
}

// userScopeReason reports why an action on a package must stay with the
// user's own non-elevated account — a user-scope rule, the global user-scope
// setting, or a portable install — or "" when nothing pins it to user scope.
//
// A per-package elevate=true rule clears it for install and upgrade: it is the
// most specific, deliberate input. It never clears it for uninstall, which
// winget refuses for a user-scope package when running as administrator.
func userScopeReason(s Settings, action retryOp, pkgID, source string) string {
	if action != retryOpUninstall {
		if elev := s.packageElevateOverride(pkgID, source); elev != nil && *elev {
			return ""
		}
	}
	if _, o, ok := s.lookupOverride(pkgID, source); ok && o.Scope == ScopeUser {
		return "user-scope rule"
	}
	if s.effectiveSettings(pkgID, source).Scope == ScopeUser {
		return "user scope setting"
	}
	if portableInstalled(pkgID) {
		return "portable install"
	}
	return ""
}

// noElevationReason is userScopeReason widened by an elevate=false rule: the
// reason WinTUI must not start elevation for the action, or "".
func noElevationReason(s Settings, action retryOp, pkgID, source string) string {
	if reason := userScopeReason(s, action, pkgID, source); reason != "" {
		return reason
	}
	if elev := s.packageElevateOverride(pkgID, source); elev != nil && !*elev {
		return "elevate=false rule"
	}
	return ""
}

// otherAccountReason explains a refused elevation of a possibly per-user
// package when elevation runs as another account.
const otherAccountReason = "elevation runs as a different account; set scope=machine to elevate"

// forcedElevationBlockReason reports why an explicit elevated retry (Ctrl+E)
// must not elevate the package, or "". Beyond noElevationReason it keeps a
// user-scope package off a helper that runs as another account, which the
// elevate=true escape hatch does not override there.
func forcedElevationBlockReason(s Settings, action retryOp, pkgID, source string) string {
	if reason := noElevationReason(s, action, pkgID, source); reason != "" {
		return reason
	}
	if globalElevator.separateAccount() && !retryHelperAllowed(s.effectiveSettings(pkgID, source), true) {
		return otherAccountReason
	}
	return ""
}

// elevationRefusal is returned in place of running an action that would run
// elevated against the package's rules. Batches record it as skipped, not as
// a failure: a policy refusal must not fail a scheduled run.
type elevationRefusal struct {
	msg  string
	note string // short form for history notes
}

func (e *elevationRefusal) Error() string { return e.msg }

// refuseInElevatedSession refuses an action on a user-scope package while
// WinTUI itself runs elevated: winget would run as administrator, and
// elevate=false cannot de-elevate a child of an elevated process.
func refuseInElevatedSession(action retryOp, reason string) error {
	if action == retryOpUninstall {
		return &elevationRefusal{
			msg:  fmt.Sprintf("skipped (%s): winget cannot uninstall a user-scope package as administrator; run WinTUI non-elevated to uninstall it", reason),
			note: "not run: " + reason + "; WinTUI was elevated",
		}
	}
	return &elevationRefusal{
		msg:  fmt.Sprintf("skipped (%s): WinTUI is running as administrator; run it non-elevated to keep this package in your user scope", reason),
		note: "not run: " + reason + "; WinTUI was elevated",
	}
}

// refuseForcedElevation refuses an explicit elevated retry (Ctrl+E).
func refuseForcedElevation(reason string) error {
	return &elevationRefusal{
		msg:  "not retried elevated: " + reason,
		note: "not retried elevated: " + reason,
	}
}

// asElevationRefusal returns err's elevationRefusal, if it is one.
func asElevationRefusal(err error) (*elevationRefusal, bool) {
	var r *elevationRefusal
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

// elevationRefusalCheck returns the refusal for running action on the package
// in this process, or nil when it may run.
func elevationRefusalCheck(s Settings, action retryOp, pkgID, source string) error {
	if !isElevated() {
		return nil
	}
	if reason := userScopeReason(s, action, pkgID, source); reason != "" {
		return refuseInElevatedSession(action, reason)
	}
	return nil
}
