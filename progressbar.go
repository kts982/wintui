package main

import (
	"image/color"
	"time"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
)

// progressBar wraps the bubbles progress component with auto-tick for
// indeterminate loading animations (we don't know real progress from winget).
type progressBar struct {
	bar     progress.Model
	percent float64
	active  bool
}

type progressTickMsg time.Time

func newProgressBar(width int) progressBar {
	p := progress.New(
		progress.WithColors(progressColors()...),
		progress.WithWidth(width),
	)
	// Start active by default — screens that start in loading state need this
	// because init() is a value receiver and can't persist state changes.
	return progressBar{bar: p, active: true}
}

// progressColors returns the two endpoint colors the progress bar
// blends between, sourced from the active palette's LogoStops. Kept
// as a func so newProgressBar and applyTheme stay in sync.
func progressColors() []color.Color {
	return []color.Color{
		activePalette.LogoStops[0],
		activePalette.LogoStops[1],
	}
}

// applyTheme rebuilds the underlying progress.Model with current
// palette colors while preserving width, percent, and active state.
// The bubbles progress API has no setter for colors — WithColors is
// only applied at construction — so we re-construct.
func (p progressBar) applyTheme() progressBar {
	w := p.bar.Width()
	p.bar = progress.New(
		progress.WithColors(progressColors()...),
		progress.WithWidth(w),
	)
	return p
}

// start begins the animated progress.
func (p progressBar) start() (progressBar, tea.Cmd) {
	p.active = true
	p.percent = 0
	return p, tickProgress()
}

// stop ends the animation and sets to 100%.
func (p progressBar) stop() progressBar {
	p.active = false
	p.percent = 1.0
	return p
}

func tickProgress() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return progressTickMsg(t)
	})
}

func (p progressBar) update(msg tea.Msg) (progressBar, tea.Cmd) {
	switch msg.(type) {
	case progressTickMsg:
		if !p.active {
			return p, nil
		}
		// Ease-out: slow down as it approaches 90% (never reaches 100% until done)
		remaining := 0.92 - p.percent
		if remaining < 0.01 {
			remaining = 0.01
		}
		p.percent += remaining * 0.06
		if p.percent > 0.92 {
			p.percent = 0.92
		}
		// Also update the progress bar animation frames
		var cmd tea.Cmd
		var barCmd tea.Cmd
		p.bar, barCmd = p.bar.Update(msg)
		cmd = tea.Batch(barCmd, tickProgress())
		return p, cmd

	case progress.FrameMsg:
		var cmd tea.Cmd
		p.bar, cmd = p.bar.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p progressBar) view() string {
	return p.bar.ViewAs(p.percent)
}
