package main

import (
	"errors"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Elevation does not always run as the user who started WinTUI. Under Windows
// Administrator protection it runs as a hidden, profile-separated
// ADMIN_<user> account; for a standard user it runs as whichever admin typed
// their credentials into the prompt. That account cannot see the user's
// per-user installs, and whatever per-user state it installs lands in its own
// profile, invisible to the user. The routing guard in winget.go keeps such
// actions off the helper in that case.

// currentUserSID is the SID of the account running WinTUI. A variable so
// tests can pin it.
var currentUserSID = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Uid, nil
}

// linkedTokenUserSID returns the user SID of this process's linked token,
// which is the token an elevation would use. A classic split-token admin's
// linked token is its own elevated token; under Administrator protection it
// belongs to the hidden admin account. A standard user has none (error).
// Only meaningful in an unelevated process.
var linkedTokenUserSID = func() (string, error) {
	var linked windows.Token
	var n uint32
	err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenLinkedToken,
		(*byte)(unsafe.Pointer(&linked)), uint32(unsafe.Sizeof(linked)), &n)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(windows.Handle(linked)) }()
	return tokenUserSID(linked)
}

// tokenUserSID reads TokenUser from t as a string SID. It goes through
// windows.GetTokenInformation and syscall.SID.String, both already linked for
// IsElevated and os/user, so the binary's syscall surface stays unchanged.
func tokenUserSID(t windows.Token) (string, error) {
	size := uint32(64)
	buf := make([]byte, size)
	for {
		var n uint32
		err := windows.GetTokenInformation(t, windows.TokenUser, &buf[0], size, &n)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || n <= size {
			return "", err
		}
		size = n
		buf = make([]byte, size)
	}
	tu := (*windows.Tokenuser)(unsafe.Pointer(&buf[0]))
	return (*syscall.SID)(unsafe.Pointer(tu.User.Sid)).String()
}

// predictSeparateAccount decides from the two SIDs whether an elevation will
// run as another account. No linked token means a standard user, whose
// elevation always runs as some admin. If even our own SID is unknown, assume
// separate: the guard then only keeps actions non-elevated first, which is
// always safe.
func predictSeparateAccount(self string, selfErr error, linked string, linkedErr error) bool {
	if selfErr != nil || linkedErr != nil {
		return true
	}
	return !strings.EqualFold(self, linked)
}

var (
	elevationAccountOnce     sync.Once
	elevationAccountSeparate bool
)

// predictedSeparateElevationAccount reports, before any prompt, whether
// elevating from this unelevated process will run as another account. The
// linked token cannot change for the life of the process, so it is read once.
// A variable so tests can pin it.
var predictedSeparateElevationAccount = func() bool {
	elevationAccountOnce.Do(func() {
		self, selfErr := currentUserSID()
		linked, linkedErr := linkedTokenUserSID()
		elevationAccountSeparate = predictSeparateAccount(self, selfErr, linked, linkedErr)
	})
	return elevationAccountSeparate
}
