package main

import "errors"

// cleanupTargetResultWire is the JSON-marshalable form of cleanupTargetResult,
// used to ferry results across the helper pipe. The in-memory result holds
// `[]error` which doesn't round-trip through encoding/json; this struct
// flattens errors to their messages.
type cleanupTargetResultWire struct {
	ID           string   `json:"id"`
	ResolvedPath string   `json:"resolved_path,omitempty"`
	SizeBytes    int64    `json:"size_bytes,omitempty"`
	FreedBytes   int64    `json:"freed_bytes,omitempty"`
	Files        int      `json:"files,omitempty"`
	Failed       int      `json:"failed,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	Skipped      int      `json:"skipped,omitempty"`
}

// cleanupResultToWire converts a domain result into the wire form. Error
// values lose their type identity (`errors.Is` checks won't survive the
// round trip), but the user-facing messages are preserved.
func cleanupResultToWire(r cleanupTargetResult) cleanupTargetResultWire {
	w := cleanupTargetResultWire{
		ID:           r.id,
		ResolvedPath: r.resolvedPath,
		SizeBytes:    r.sizeBytes,
		FreedBytes:   r.freedBytes,
		Files:        r.files,
		Failed:       r.failed,
		Skipped:      int(r.skipped),
	}
	if len(r.errors) > 0 {
		w.Errors = make([]string, 0, len(r.errors))
		for _, e := range r.errors {
			if e == nil {
				continue
			}
			w.Errors = append(w.Errors, e.Error())
		}
	}
	return w
}

// cleanupResultFromWire rehydrates a wire result. The original error types
// are gone — each message becomes a fresh errors.New value.
func cleanupResultFromWire(w cleanupTargetResultWire) cleanupTargetResult {
	r := cleanupTargetResult{
		id:           w.ID,
		resolvedPath: w.ResolvedPath,
		sizeBytes:    w.SizeBytes,
		freedBytes:   w.FreedBytes,
		files:        w.Files,
		failed:       w.Failed,
		skipped:      cleanupSkipReason(w.Skipped),
	}
	if len(w.Errors) > 0 {
		r.errors = make([]error, 0, len(w.Errors))
		for _, msg := range w.Errors {
			r.errors = append(r.errors, errors.New(msg))
		}
	}
	return r
}
