package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// cleanupTargetResult aggregates the per-target outcome of a scan or delete
// pass. The UI surfaces (sizeBytes / freedBytes / failed); the helper logs
// the captured errors when admin retries.
type cleanupTargetResult struct {
	id           string
	resolvedPath string
	sizeBytes    int64 // bytes that would be (or were) removed
	freedBytes   int64 // bytes actually removed; populated only by cleanupDelete
	files        int   // top-level entries considered for removal
	failed       int   // top-level entries we could not remove
	errors       []error
	skipped      cleanupSkipReason
}

// cleanupSkipReason describes why a target produced no work. Distinct from
// "ran but failed": skipped targets are benign — missing env, missing dir,
// or guarded by policy — and the UI surfaces them differently.
type cleanupSkipReason int

const (
	cleanupSkipNone        cleanupSkipReason = iota
	cleanupSkipUnresolved                    // pathFn returned "" (env var missing)
	cleanupSkipMissing                       // resolved path is not on disk
	cleanupSkipGuarded                       // tripped cleanupValidateRoot
	cleanupSkipNotElevated                   // requiresAdmin && !isElevated()
)

// cleanupMaxErrors caps per-target error retention. A target with thousands
// of locked files would otherwise blow up memory; the failed counter still
// reflects the true count so the UI can say "and 1,247 more".
const cleanupMaxErrors = 8

// cleanupScan walks def.pathFn() and reports the size that would be freed
// if the target were cleaned. Never deletes. Honors ctx cancellation;
// missing paths and unresolved env vars are benign skips, not errors.
func cleanupScan(ctx context.Context, def cleanupTargetDef) cleanupTargetResult {
	return cleanupRun(ctx, def, false)
}

// cleanupDelete walks def.pathFn() and removes each candidate entry per
// def.mode. Validates the resolved root against the guard list before
// touching anything; per-entry failures (locked files, permission denied)
// are captured in the result, not fatal.
func cleanupDelete(ctx context.Context, def cleanupTargetDef) cleanupTargetResult {
	return cleanupRun(ctx, def, true)
}

func cleanupRun(ctx context.Context, def cleanupTargetDef, doDelete bool) cleanupTargetResult {
	res := cleanupTargetResult{id: def.id}

	if def.pathFn == nil {
		res.skipped = cleanupSkipUnresolved
		return res
	}
	root := def.pathFn()
	res.resolvedPath = root
	if root == "" {
		res.skipped = cleanupSkipUnresolved
		return res
	}
	if doDelete {
		if err := cleanupValidateRoot(root); err != nil {
			res.skipped = cleanupSkipGuarded
			res.appendErr(err)
			return res
		}
		if def.requiresAdmin && !isElevated() {
			res.skipped = cleanupSkipNotElevated
			return res
		}
	}
	if ctx.Err() != nil {
		return res
	}

	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			res.skipped = cleanupSkipMissing
			return res
		}
		res.appendErr(err)
		return res
	}
	if !info.IsDir() {
		res.appendErr(fmt.Errorf("registered root %q is not a directory", root))
		return res
	}
	// Refuse to traverse if the root itself is a reparse point — we don't
	// know what's on the other side of the link.
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		res.skipped = cleanupSkipGuarded
		res.appendErr(fmt.Errorf("registered root %q is a reparse point", root))
		return res
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		res.appendErr(err)
		return res
	}

	var cutoff time.Time
	if def.minAge > 0 {
		cutoff = time.Now().Add(-def.minAge)
	}

	for _, e := range entries {
		if ctx.Err() != nil {
			return res
		}
		name := e.Name()
		path := filepath.Join(root, name)

		if def.mode == cleanupModeGlob && !cleanupMatchesAnyGlob(name, def.globs) {
			continue
		}

		entryInfo, err := os.Lstat(path)
		if err != nil {
			res.failed++
			res.appendErr(err)
			continue
		}
		// Never traverse off the registered root.
		if entryInfo.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			continue
		}
		if def.minAge > 0 && entryInfo.ModTime().After(cutoff) {
			continue
		}

		size := cleanupEntrySize(ctx, path, entryInfo)
		res.files++
		res.sizeBytes += size

		if !doDelete {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			res.failed++
			res.appendErr(err)
			continue
		}
		res.freedBytes += size
	}

	return res
}

// cleanupEntrySize sums the file bytes under path. For directories it walks
// recursively but never follows reparse points; ctx cancellation aborts the
// walk and returns whatever was tallied so far.
func cleanupEntrySize(ctx context.Context, path string, info os.FileInfo) int64 {
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || d == nil {
			return nil //nolint:nilerr // best-effort size walk: unreadable entries are skipped, not fatal
		}
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // entry vanished mid-walk: skip it, keep the tally
		}
		total += fi.Size()
		return nil
	})
	return total
}

func cleanupMatchesAnyGlob(name string, globs []string) bool {
	for _, g := range globs {
		if ok, err := filepath.Match(g, name); err == nil && ok {
			return true
		}
	}
	return false
}

func (r *cleanupTargetResult) appendErr(err error) {
	if err == nil || len(r.errors) >= cleanupMaxErrors {
		return
	}
	r.errors = append(r.errors, err)
}
