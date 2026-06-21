package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatLine(t *testing.T) {
	if got := heartbeatLine(3 * time.Minute); got != "  … still working (3m elapsed)\n" {
		t.Errorf("heartbeatLine = %q", got)
	}
}

// With no ticks (nil channel never fires in select), streamUpgradeOutput must
// behave byte-for-byte like the pre-heartbeat loop: relay lines with a two-space
// indent, skip progress sentinels and blank lines, then return the terminal err.
func TestStreamUpgradeOutputNoTickByteIdentical(t *testing.T) {
	outChan := make(chan string, 4)
	outChan <- "line one"
	outChan <- progressLineSentinel(50) // sentinel: must be skipped
	outChan <- ""                       // blank: must be skipped
	outChan <- "line two"
	close(outChan)
	errChan := make(chan error, 1)
	errChan <- nil

	var buf bytes.Buffer
	if err := streamUpgradeOutput(&buf, outChan, errChan, nil, func() time.Duration { return 0 }); err != nil {
		t.Fatalf("err = %v", err)
	}
	want := "  line one\n  line two\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

// The terminal error must propagate after the channel closes.
func TestStreamUpgradeOutputReturnsErr(t *testing.T) {
	outChan := make(chan string)
	close(outChan)
	errChan := make(chan error, 1)
	errChan <- errSentinel

	err := streamUpgradeOutput(&bytes.Buffer{}, outChan, errChan, nil, func() time.Duration { return 0 })
	if err != errSentinel {
		t.Fatalf("err = %v, want errSentinel", err)
	}
}

var errSentinel = &heartbeatTestErr{}

type heartbeatTestErr struct{}

func (*heartbeatTestErr) Error() string { return "boom" }

// A tick must emit a heartbeat, ordered before subsequent output. Driven via
// unbuffered channels so the sequence is deterministic (each send rendezvouses
// with the loop's select; the buffer is read only after the loop returns).
func TestStreamUpgradeOutputHeartbeat(t *testing.T) {
	outChan := make(chan string)
	errChan := make(chan error, 1)
	ticks := make(chan time.Time)

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- streamUpgradeOutput(&buf, outChan, errChan, ticks, func() time.Duration { return 5 * time.Minute })
	}()

	ticks <- time.Unix(0, 0) // loop emits a heartbeat, returns to select
	outChan <- "after tick"  // loop emits the line
	errChan <- nil           // buffered, ready for the close path
	close(outChan)           // loop sees ok=false, returns <-errChan
	if err := <-done; err != nil {
		t.Fatalf("err = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "still working (5m elapsed)") {
		t.Errorf("missing heartbeat in %q", out)
	}
	if !strings.Contains(out, "  after tick") {
		t.Errorf("missing output line in %q", out)
	}
	if strings.Index(out, "still working") > strings.Index(out, "after tick") {
		t.Errorf("heartbeat should precede the line: %q", out)
	}
}
