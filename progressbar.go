package main

import (
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
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
		progress.WithGradient("#FF6BD6", "#6BFFC8"), // pink → mint gradient
		progress.WithWidth(width),
	)
	// Start active by default — screens that start in loading state need this
	// because init() is a value receiver and can't persist state changes.
	return progressBar{bar: p, active: true}
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
		barModel, barCmd := p.bar.Update(msg)
		p.bar = barModel.(progress.Model)
		cmd = tea.Batch(barCmd, tickProgress())
		return p, cmd

	case progress.FrameMsg:
		barModel, cmd := p.bar.Update(msg)
		p.bar = barModel.(progress.Model)
		return p, cmd
	}
	return p, nil
}

func (p progressBar) view() string {
	return p.bar.ViewAs(p.percent)
}
