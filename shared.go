package main

import (
	"fmt"
	"strings"
	"time"
)

// scrollWindow calculates visible range for long lists.
func scrollWindow(cursor, total, maxVisible int) (start, end int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}

func formatBatchResults(ids []string, errs []error) string {
	var b strings.Builder
	for i, id := range ids {
		if i >= len(errs) {
			break
		}
		if errs[i] == nil {
			b.WriteString(successStyle.Render("  ✓ ") + id + "\n")
		} else {
			reason := errs[i].Error()
			b.WriteString(errorStyle.Render("  ✗ ") + id + "\n")
			b.WriteString("    " + helpStyle.Render(reason) + "\n")
		}
	}
	return b.String()
}

func batchResultCounts(errs []error) (successCount, failCount int) {
	for _, err := range errs {
		if err == nil {
			successCount++
		} else {
			failCount++
		}
	}
	return successCount, failCount
}

// humanDuration formats a duration as a short human-readable string.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func packageSummary(pkgs []Package) string {
	total := len(pkgs)
	winget, msstore, system := 0, 0, 0
	for _, p := range pkgs {
		switch identityKind(p) {
		case "winget":
			winget++
		case "msstore":
			msstore++
		case "system":
			system++
		}
	}

	other := total - winget - msstore - system
	if winget == 0 && msstore == 0 && system == 0 {
		return fmt.Sprintf("%d package(s) installed.", total)
	}

	var parts []string
	if winget > 0 {
		parts = append(parts, fmt.Sprintf("%d winget", winget))
	}
	if msstore > 0 {
		parts = append(parts, fmt.Sprintf("%d msstore", msstore))
	}
	if system > 0 {
		parts = append(parts, fmt.Sprintf("%d system", system))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d other", other))
	}

	return fmt.Sprintf("%d installed (%s)", total, strings.Join(parts, ", "))
}
