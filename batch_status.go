package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// batchItemStatus tracks the execution state of a single package in a batch.
type batchItemStatus int

const (
	batchQueued batchItemStatus = iota
	batchRunning
	batchDone
	batchFailed
	batchPendingRestart
)

// batchItem pairs a workspace item with its execution status.
type batchItem struct {
	action        retryOp
	item          workspaceItem
	status        batchItemStatus
	err           error
	output        string    // captured output for this item
	command       string    // pre-rendered preview of the winget command (populated at modal open)
	allVersions   bool      // uninstall all installed versions (duplicate ID)
	progress      int       // latest progress percent (0-100) while running
	blockedByProc bool      // failed because a related process is running
	startedAt     time.Time // when this item entered the running state
	latestLine    string    // most recent streamed output line (for live status)
}

// statusIcon returns a styled icon for the batch item's current state.
func (b batchItem) statusIcon(sp spinner.Model) string {
	switch b.status {
	case batchQueued:
		return lipgloss.NewStyle().Foreground(dim).Render("·")
	case batchRunning:
		return sp.View()
	case batchDone:
		return lipgloss.NewStyle().Foreground(success).Bold(true).Render("✓")
	case batchFailed:
		return lipgloss.NewStyle().Foreground(danger).Bold(true).Render("✗")
	case batchPendingRestart:
		return lipgloss.NewStyle().Foreground(warning).Bold(true).Render("↺")
	}
	return " "
}

// statusText returns a short description for the batch item's state.
func (b batchItem) statusText() string {
	switch b.status {
	case batchQueued:
		return helpStyle.Render("queued")
	case batchRunning:
		return lipgloss.NewStyle().Foreground(accent).Render("running...")
	case batchDone:
		return ""
	case batchFailed:
		if b.err != nil {
			return errorStyle.Render(b.err.Error())
		}
		return errorStyle.Render("failed")
	case batchPendingRestart:
		return warnStyle.Render("restart to finish")
	}
	return ""
}

// liveStatus returns a compact live status string for a running batch item.
// Prefers an explicit percent, falls back to the latest output line, then to
// elapsed time since the item started. Callers should only invoke this for
// items in the batchRunning state.
func (b batchItem) liveStatus() string {
	if b.progress > 0 {
		return renderInlineProgress(b.progress)
	}
	elapsed := ""
	if !b.startedAt.IsZero() {
		elapsed = " " + helpStyle.Render(formatElapsed(time.Since(b.startedAt)))
	}
	if line := strings.TrimSpace(b.latestLine); line != "" {
		return helpStyle.Render(truncate(line, 60)) + elapsed
	}
	return lipgloss.NewStyle().Foreground(accent).Render("running...") + elapsed
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return ""
	}
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("(%ds)", s)
	}
	m := s / 60
	s = s % 60
	return fmt.Sprintf("(%dm%02ds)", m, s)
}

// batchCounters returns (completed, failed, total) from a slice of batch items.
func batchCounters(items []batchItem) (completed, failed, total int) {
	total = len(items)
	for _, item := range items {
		switch item.status {
		case batchDone:
			completed++
		case batchFailed:
			failed++
			completed++
		case batchPendingRestart:
			completed++
		}
	}
	return
}
