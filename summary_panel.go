package main

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// summaryFetchFunc is the signature for fetching package metadata.
// Injected for testability.
type summaryFetchFunc func(ctx context.Context, id, source, version string) (PackageDetail, error)

// summaryPanel renders a persistent side panel showing compact package info.
// It displays fields from the Package struct instantly and fetches additional
// metadata (publisher, license, description) via a debounced async call.
type summaryPanel struct {
	pkg       *Package       // currently focused package
	detail    *PackageDetail // fetched metadata (nil until loaded)
	loading   bool
	err       error
	noFetch   bool   // true when source doesn't support detail fetching
	fetchID   string // id+source of the pending fetch, to discard stale results
	cancelFn  context.CancelFunc
	fetchFunc summaryFetchFunc
	width     int
	height    int
	installed string // installed version override
	target    string // target version override
}

func newSummaryPanel() summaryPanel {
	return summaryPanel{
		fetchFunc: defaultSummaryFetch,
	}
}

func newSummaryPanelWith(fetch summaryFetchFunc) summaryPanel {
	return summaryPanel{
		fetchFunc: fetch,
	}
}

func defaultSummaryFetch(ctx context.Context, id, source, version string) (PackageDetail, error) {
	return showPackageCtx(ctx, id, source, version)
}

// canFetchDetails returns true if the package source supports winget show.
func canFetchDetails(source string) bool {
	s := strings.ToLower(source)
	return s == "winget" || s == "msstore"
}

// summaryDetailMsg delivers async-fetched metadata to the panel.
type summaryDetailMsg struct {
	fetchID string
	detail  PackageDetail
	err     error
}

type summaryFetchTickMsg struct {
	fetchID string
}

const summaryDebounceDelay = 200 * time.Millisecond

// focus updates the panel to show a different package. Instant fields render
// immediately; a debounced fetch is scheduled for full metadata.
func (p *summaryPanel) focus(pkg *Package, installed, target string) tea.Cmd {
	if pkg == nil {
		p.cancelPending()
		p.pkg = nil
		p.detail = nil
		p.loading = false
		p.err = nil
		p.noFetch = false
		return nil
	}

	// Same package — just update version overrides.
	key := pkg.ID + "|" + pkg.Source
	if p.pkg != nil && p.pkg.ID == pkg.ID && p.pkg.Source == pkg.Source {
		p.installed = installed
		p.target = target
		return nil
	}

	p.cancelPending()
	p.pkg = pkg
	p.detail = nil
	p.err = nil
	p.fetchID = key
	p.installed = installed
	p.target = target

	// Skip fetch for sources that don't support winget show.
	if !canFetchDetails(pkg.Source) {
		p.loading = false
		p.noFetch = true
		return nil
	}

	p.loading = true
	p.noFetch = false

	// Schedule a debounced fetch.
	return tea.Tick(summaryDebounceDelay, func(t time.Time) tea.Msg {
		return summaryFetchTickMsg{fetchID: key}
	})
}

// cancelPending cancels any in-flight fetch.
func (p *summaryPanel) cancelPending() {
	if p.cancelFn != nil {
		p.cancelFn()
		p.cancelFn = nil
	}
}

// update handles messages for the summary panel.
func (p *summaryPanel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case summaryFetchTickMsg:
		if msg.fetchID != p.fetchID || p.pkg == nil {
			return nil
		}
		p.cancelPending()
		ctx, cancel := context.WithCancel(context.Background())
		p.cancelFn = cancel
		id := p.pkg.ID
		source := p.pkg.Source
		fetchID := msg.fetchID
		fetch := p.fetchFunc
		return func() tea.Msg {
			d, err := fetch(ctx, id, source, "")
			return summaryDetailMsg{fetchID: fetchID, detail: d, err: err}
		}

	case summaryDetailMsg:
		if msg.fetchID != p.fetchID {
			return nil // stale result
		}
		p.loading = false
		p.cancelFn = nil
		if msg.err != nil {
			p.err = msg.err
		} else {
			p.detail = &msg.detail
		}
	}
	return nil
}

// setSize updates the panel dimensions.
func (p *summaryPanel) setSize(width, height int) {
	p.width = max(width, 0)
	p.height = max(height, 0)
}

// view renders the summary panel, clipped to fit within the allocated height.
func (p summaryPanel) view() string {
	if p.width < 4 || p.height < 3 {
		return ""
	}
	if p.pkg == nil {
		return p.renderEmpty()
	}

	// Inner dimensions: border takes 2, padding(0,1) takes 2.
	innerWidth := max(p.width-6, 8)
	maxLines := max(p.height-2, 3) // available content lines inside border

	var lines []string

	// Package name + ID (always shown).
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accent).Render(p.pkg.Name))
	lines = append(lines, helpStyle.Render(p.pkg.ID))
	lines = append(lines, helpStyle.Render(strings.Repeat("─", innerWidth)))

	// Version info.
	if p.installed != "" {
		lines = append(lines, p.field("Installed", p.installed))
	} else if p.pkg.Version != "" {
		lines = append(lines, p.field("Version", p.pkg.Version))
	}
	if p.pkg.Available != "" {
		lines = append(lines, p.field("Available", p.pkg.Available))
	}
	if p.target != "" && p.target != p.pkg.Available {
		lines = append(lines, p.field("Target", p.target))
	}
	if p.pkg.Source != "" {
		lines = append(lines, p.field("Source", p.pkg.Source))
	}

	// Fetched metadata.
	if p.detail != nil {
		if p.detail.Publisher != "" {
			lines = append(lines, p.field("Publisher", p.detail.Publisher))
		}
		if p.detail.InstallerType != "" {
			lines = append(lines, p.field("Type", p.detail.InstallerType))
		}
		if p.detail.License != "" {
			lines = append(lines, p.field("License", p.detail.License))
		}
		if p.detail.Homepage != "" {
			lines = append(lines, p.field("Homepage", truncate(p.detail.Homepage, innerWidth-10)))
		}

		if p.detail.Description != "" {
			lines = append(lines, "")
			lines = append(lines, helpStyle.Render(strings.Repeat("─", innerWidth)))
			descLines := strings.Split(wordWrap(p.detail.Description, innerWidth), "\n")
			for _, dl := range descLines {
				lines = append(lines, helpStyle.Render(dl))
			}
		}
	} else if p.loading {
		lines = append(lines, "")
		lines = append(lines, helpStyle.Render("Loading..."))
	} else if p.noFetch {
		lines = append(lines, "")
		lines = append(lines, helpStyle.Render("Package managed outside winget."))
		lines = append(lines, helpStyle.Render("Extended details not available."))
	} else if p.err != nil {
		lines = append(lines, "")
		lines = append(lines, helpStyle.Render("Details unavailable"))
	}

	// Clip to available height.
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	content := strings.Join(lines, "\n")

	style := lipgloss.NewStyle().
		Width(max(p.width-2, 1)).
		Height(max(p.height-2, 1)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dim).
		Padding(0, 1)

	return style.Render(content)
}

func (p summaryPanel) renderEmpty() string {
	style := lipgloss.NewStyle().
		Width(max(p.width-2, 1)).
		Height(max(p.height-2, 1)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dim).
		Padding(0, 1).
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center)

	return style.Render(helpStyle.Render("No package selected"))
}

func (p summaryPanel) field(label, value string) string {
	return lipgloss.NewStyle().Foreground(secondary).Render(label+": ") +
		lipgloss.NewStyle().Foreground(bright).Render(value)
}

func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	var lines []string
	var line string
	for _, word := range words {
		if line == "" {
			line = word
		} else if len(line)+1+len(word) <= width {
			line += " " + word
		} else {
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
