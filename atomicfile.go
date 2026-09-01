package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
// removed so nothing accumulates in the state directory.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+"-*.tmp")
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

// Windows ERROR_SHARING_VIOLATION. Spelled numerically so this file builds on
// every GOOS; the constant only ever matches on Windows.
const errnoSharingViolation = syscall.Errno(32)

// isTransientFSError reports whether err is the kind of momentary Windows
// contention that a retry resolves: ERROR_ACCESS_DENIED (fs.ErrPermission) or
// ERROR_SHARING_VIOLATION raised because another process has the file open
// for the few microseconds of its own read or replace-rename.
func isTransientFSError(err error) bool {
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == errnoSharingViolation
}

// fsRetryBackoff yields the sleep before retry attempt n (0-based): 2, 4, 6…
// ms, totalling well under a second across fsRetryAttempts. Long enough to
// outlast a concurrent WinTUI process reading or replacing the same file,
// short enough that a genuinely locked file (an editor holding settings.json
// without share-delete) still fails promptly with the real error.
const fsRetryAttempts = 25

func fsRetryBackoff(n int) time.Duration { return time.Duration(2+2*n) * time.Millisecond }

// renameWithRetry is os.Rename with a bounded retry on transient Windows
// sharing errors. A replace-rename onto a file another process currently has
// open fails with ERROR_ACCESS_DENIED even though the operation would succeed
// a millisecond later; without the retry, two WinTUI processes saving state at
// the same moment would surface spurious "Access is denied" save errors.
func renameWithRetry(oldpath, newpath string) error {
	var err error
	for n := 0; n < fsRetryAttempts; n++ {
		if err = os.Rename(oldpath, newpath); err == nil {
			return nil
		}
		if !isTransientFSError(err) {
			return err
		}
		time.Sleep(fsRetryBackoff(n))
	}
	return err
}

// readFileWithRetry is os.ReadFile with the same bounded retry for transient
// sharing errors. Missing files are returned immediately (callers treat
// fs.ErrNotExist as "use defaults"); a read that keeps failing returns the
// last error so the caller can refuse to act on data it never saw.
func readFileWithRetry(path string) ([]byte, error) {
	var (
		b   []byte
		err error
	)
	for n := 0; n < fsRetryAttempts; n++ {
		if b, err = os.ReadFile(path); err == nil {
			return b, nil
		}
		if errors.Is(err, fs.ErrNotExist) || !isTransientFSError(err) {
			return nil, err
		}
		time.Sleep(fsRetryBackoff(n))
	}
	return nil, err
}
