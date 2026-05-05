package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCleanupResultWireRoundTrip(t *testing.T) {
	original := cleanupTargetResult{
		id:           "windows_temp",
		resolvedPath: `C:\Windows\Temp`,
		sizeBytes:    1024 * 1024,
		freedBytes:   512 * 1024,
		files:        4,
		failed:       1,
		errors:       []error{errors.New("locked file foo.tmp"), errors.New("access denied")},
		skipped:      cleanupSkipNone,
	}

	wire := cleanupResultToWire(original)
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back cleanupTargetResultWire
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := cleanupResultFromWire(back)

	if got.id != original.id || got.resolvedPath != original.resolvedPath ||
		got.sizeBytes != original.sizeBytes || got.freedBytes != original.freedBytes ||
		got.files != original.files || got.failed != original.failed ||
		got.skipped != original.skipped {
		t.Errorf("round-trip lost fields: got %#v want %#v", got, original)
	}
	if len(got.errors) != len(original.errors) {
		t.Fatalf("error count = %d, want %d", len(got.errors), len(original.errors))
	}
	for i := range original.errors {
		if got.errors[i].Error() != original.errors[i].Error() {
			t.Errorf("error %d: got %q want %q", i, got.errors[i], original.errors[i])
		}
	}
}

func TestCleanupResultWireDropsNilErrors(t *testing.T) {
	r := cleanupTargetResult{
		id:     "user_temp",
		errors: []error{errors.New("real"), nil, errors.New("also real")},
	}
	w := cleanupResultToWire(r)
	if len(w.Errors) != 2 {
		t.Errorf("nil error should be dropped, got %d entries: %v", len(w.Errors), w.Errors)
	}
}

func TestCleanupResultWireOmitsZeroFields(t *testing.T) {
	// A purely-skipped result (no work done, no errors) should serialize to
	// a minimal JSON object — the wire format relies on omitempty so the
	// pipe doesn't carry useless zero values for every action.
	r := cleanupTargetResult{id: "go_build", skipped: cleanupSkipMissing}
	w := cleanupResultToWire(r)
	data, _ := json.Marshal(w)

	var raw map[string]any
	json.Unmarshal(data, &raw)

	// id and skipped are the only meaningful fields.
	for _, expected := range []string{"id", "skipped"} {
		if _, ok := raw[expected]; !ok {
			t.Errorf("expected %q in wire JSON, got %v", expected, raw)
		}
	}
	for _, unwanted := range []string{"resolved_path", "size_bytes", "freed_bytes", "files", "failed", "errors"} {
		if _, ok := raw[unwanted]; ok {
			t.Errorf("unexpected %q in wire JSON: %v", unwanted, raw[unwanted])
		}
	}
}

func TestCleanupResultWirePreservesSkipReason(t *testing.T) {
	for _, reason := range []cleanupSkipReason{
		cleanupSkipNone, cleanupSkipUnresolved, cleanupSkipMissing,
		cleanupSkipGuarded, cleanupSkipNotElevated,
	} {
		r := cleanupTargetResult{id: "x", skipped: reason}
		w := cleanupResultToWire(r)
		back := cleanupResultFromWire(w)
		if back.skipped != reason {
			t.Errorf("skip reason %d round-tripped as %d", reason, back.skipped)
		}
	}
}
