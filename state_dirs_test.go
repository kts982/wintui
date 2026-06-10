package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestStateDirErrorsRecordAndSelfHeal(t *testing.T) {
	t.Cleanup(func() {
		recordStateDirResult("test dir", nil)
		recordStateDirResult("other dir", nil)
	})

	recordStateDirResult("test dir", fmt.Errorf("access is denied"))
	recordStateDirResult("other dir", fmt.Errorf("disk full"))
	got := stateDirErrors()
	if len(got) != 2 || got[0] != "other dir: disk full" || got[1] != "test dir: access is denied" {
		t.Fatalf("stateDirErrors() = %#v, want sorted purpose-prefixed entries", got)
	}

	recordStateDirResult("test dir", nil)
	got = stateDirErrors()
	if len(got) != 1 || got[0] != "other dir: disk full" {
		t.Fatalf("stateDirErrors() after self-heal = %#v, want only the remaining failure", got)
	}
}

func TestCheckStateDirsReportsRecordedFailure(t *testing.T) {
	t.Cleanup(func() { recordStateDirResult("test dir", nil) })

	if c := checkStateDirs(); c.Status != "PASS" {
		// The probe runs the real path helpers; on a healthy dev machine
		// this must be PASS.
		t.Fatalf("checkStateDirs() = %+v, want PASS on a healthy machine", c)
	}

	recordStateDirResult("test dir", fmt.Errorf("access is denied"))
	c := checkStateDirs()
	if c.Status != "FAIL" || !strings.Contains(c.Details, "test dir: access is denied") {
		t.Fatalf("checkStateDirs() = %+v, want FAIL mentioning the recorded error", c)
	}
}
