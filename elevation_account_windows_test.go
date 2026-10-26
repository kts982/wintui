package main

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPredictSeparateAccount(t *testing.T) {
	const self = "S-1-5-21-1-2-3-1001"
	errNoLinked := errors.New("no linked token")
	tests := []struct {
		name      string
		linked    string
		selfErr   error
		linkedErr error
		want      bool
	}{
		{"classic split token: linked token is our own", self, nil, nil, false},
		{"linked SID differs only in case", strings.ToLower(self), nil, nil, false},
		{"administrator protection: hidden admin account", "S-1-5-21-1-2-3-1002", nil, nil, true},
		{"standard user: no linked token", "", nil, errNoLinked, true},
		{"own SID unknown", self, errors.New("no user"), nil, true},
	}
	for _, tt := range tests {
		if got := predictSeparateAccount(self, tt.selfErr, tt.linked, tt.linkedErr); got != tt.want {
			t.Errorf("%s: predictSeparateAccount() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestTokenUserSIDMatchesCurrentUser checks the TokenUser buffer handling
// against os/user on the real process token.
func TestTokenUserSIDMatchesCurrentUser(t *testing.T) {
	got, err := tokenUserSID(windows.GetCurrentProcessToken())
	if err != nil {
		t.Fatalf("tokenUserSID: %v", err)
	}
	want, err := currentUserSID()
	if err != nil {
		t.Fatalf("currentUserSID: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("tokenUserSID() = %q, want %q", got, want)
	}
}

// TestLinkedTokenUserSIDShape only checks the shape: whether a linked token
// exists depends on how the test process was started (split-token admin,
// standard user, UAC off).
func TestLinkedTokenUserSIDShape(t *testing.T) {
	sid, err := linkedTokenUserSID()
	if err != nil {
		t.Skipf("no linked token in this environment: %v", err)
	}
	if !strings.HasPrefix(sid, "S-1-") {
		t.Fatalf("linkedTokenUserSID() = %q, want a string SID", sid)
	}
	self, _ := currentUserSID()
	t.Logf("linked token user %s, process user %s, predicted separate = %v", sid, self, predictedSeparateElevationAccount())
}
