package main

import (
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
	action      retryOp
	item        workspaceItem
	status      batchItemStatus
	err         error
	output      string // captured output for this item
	command     string // pre-rendered preview of the winget command (populated at modal open)
	allVersions bool   // uninstall all installed versions (duplicate ID)
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
