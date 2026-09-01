package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// writeFileAtomic publishes data at path via a uniquely named temp file in the
// same directory followed by os.Rename, so readers only ever observe either
// the previous complete file or the new complete file.
//
// The temp name is unique per call (os.CreateTemp), never a fixed
// "<file>.tmp": with a shared temp name two concurrent writers — a TUI session
// and a CLI command both saving settings.json, for instance — collide on
// Windows (sharing violations on the rename) or, worse, one renames the
// other's half-written temp into place. On any failure the temp file is
// removed so nothing accumulates in the state directory; sweepStaleAtomicTemps
// handles the one case this cannot (the process dying mid-write).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, atomicTempPattern(base))
	if err != nil {
		return err
	}
	tmp := f.Name()
	discard := func() { _ = os.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		discard()
		return err
	}
	if err := f.Close(); err != nil {
		discard()
		return err
	}
	// CreateTemp creates 0600; restore the permission the callers always used.
	// On Windows this only toggles the read-only attribute and is harmless.
	if perm != 0 {
		_ = os.Chmod(tmp, perm)
	}
	if err := renameWithRetry(tmp, path); err != nil {
		discard()
		return err
	}
	return nil
}

// atomicTempPattern is the os.CreateTemp pattern for base: ".<base>-*.tmp".
func atomicTempPattern(base string) string { return "." + base + "-*.tmp" }

// sweepStaleAtomicTemps removes temp files that a crashed or killed writer
// left behind in dir for the given base names (e.g. "settings.json"), if they
// are older than maxAge. Only files matching our own ".<base>-*.tmp" pattern
// are touched; a temp younger than maxAge may belong to a writer that is
// mid-publish right now. Returns the number removed.
func sweepStaleAtomicTemps(dir string, bases []string, maxAge time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		matched := false
		for _, base := range bases {
			if strings.HasPrefix(name, "."+base+"-") && strings.HasSuffix(name, ".tmp") {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			removed++
		}
	}
	return removed
}

// Windows sharing errors that a retry resolves. Spelled numerically so this
// file builds on every GOOS; they only ever match on Windows.
const (
	errnoSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	errnoLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

func isSharingError(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == errnoSharingViolation || errno == errnoLockViolation)
}

// isTransientReadError: a read blocked by a concurrent writer surfaces as a
// sharing/lock violation. ERROR_ACCESS_DENIED on a read is an ACL problem and
// will not go away — retrying it would only stall the caller (the TUI Update
// goroutine, for the per-package rule editors).
func isTransientReadError(err error) bool { return isSharingError(err) }

// isTransientRenameError: besides sharing violations, a replace-rename onto
// a file another process momentarily has open fails with ERROR_ACCESS_DENIED
// even though it succeeds a millisecond later, so ACCESS_DENIED must be
// retried here. The price is that a genuinely permanent denial (read-only
// target, ACL) burns the bounded budget before erroring.
func isTransientRenameError(err error) bool {
	return isSharingError(err) || errors.Is(err, fs.ErrPermission)
}

// fsRetryAttempts / fsRetryBackoff bound the retry budget: 2, 4, 8, 16, 32 ms
// then 50 ms, over 12 attempts ≈ 410 ms total. Contention from a concurrent
// WinTUI process normally lasts milliseconds; a file another program keeps
// open still surfaces as an error well under half a second.
const fsRetryAttempts = 12

func fsRetryBackoff(n int) time.Duration {
	d := time.Duration(2<<uint(n)) * time.Millisecond
	if d > 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	return d
}

// renameWithRetry is os.Rename with a bounded retry on transient Windows
// sharing errors; without it two WinTUI processes saving state at the same
// moment would surface spurious "Access is denied" save errors.
func renameWithRetry(oldpath, newpath string) error {
	var err error
	for n := 0; n < fsRetryAttempts; n++ {
		if err = os.Rename(oldpath, newpath); err == nil {
			return nil
		}
		if !isTransientRenameError(err) {
			return err
		}
		time.Sleep(fsRetryBackoff(n))
	}
	return err
}

// readFileWithRetry is os.ReadFile with the same bounded retry for transient
// sharing errors. Missing files and permanent errors are returned
// immediately; a read that keeps failing returns the last error so the caller
// can refuse to act on data it never saw.
func readFileWithRetry(path string) ([]byte, error) {
	var (
		b   []byte
		err error
	)
	for n := 0; n < fsRetryAttempts; n++ {
		if b, err = os.ReadFile(path); err == nil {
			return b, nil
		}
		if !isTransientReadError(err) {
			return nil, err
		}
		time.Sleep(fsRetryBackoff(n))
	}
	return nil, err
}
