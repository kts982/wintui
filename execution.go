package main

import (
	"strings"

	"charm.land/bubbles/v2/viewport"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type executionLog struct {
	vp       viewport.Model
	lines    []string
	current  []string
	follow   bool
	expanded bool
}

func newExecutionLog() executionLog {
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(10))
	vp.Style = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(accent)
	log := executionLog{
		vp:     vp,
		follow: true,
	}
	log.setSize(80, contentAreaHeightForWindow(80, 24, true))
	return log
}

func (l *executionLog) reset() {
	l.lines = nil
	l.current = nil
	l.follow = true
	l.expanded = false
	l.vp.SetContent("")
	l.vp.GotoTop()
}

func (l *executionLog) appendSection(header string) {
	l.current = nil
	if len(l.lines) > 0 {
		l.lines = append(l.lines, "")
	}
	l.lines = append(l.lines, header)
	l.sync()
}

func (l *executionLog) appendLine(line string) {
	l.current = append(l.current, line)
	l.lines = append(l.lines, line)
	l.sync()
}

func (l executionLog) currentOutput() string {
	return strings.Join(l.current, "\n")
}

func (l executionLog) hasOutput() bool {
	return len(l.lines) > 0
}

func (l *executionLog) setDoneExpanded(expanded bool) {
	l.expanded = expanded && l.hasOutput()
	if l.expanded {
		l.follow = true
		l.vp.GotoBottom()
	}
}

func (l *executionLog) update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k", "down", "j", "pgup", "pgdown":
			var cmd tea.Cmd
			l.vp, cmd = l.vp.Update(msg)
			l.follow = l.vp.AtBottom()
			return cmd, true
		case "f", "end":
			l.vp.GotoBottom()
			l.follow = true
			return nil, true
		}
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		l.vp, cmd = l.vp.Update(msg)
		l.follow = l.vp.AtBottom()
		return cmd, true
	}

	return nil, false
}

func (l *executionLog) doneUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if !l.hasOutput() {
		return nil, false
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok && keyMsg.String() == "l" {
		l.expanded = !l.expanded
		if l.expanded {
			l.follow = true
			l.vp.GotoBottom()
		}
		return nil, true
	}
	if l.expanded {
		return l.update(msg)
	}
	return nil, false
}

func (l *executionLog) setSize(width, height int) {
	l.vp.SetWidth(width - 8)
	vpH := height - 12
	if vpH < 5 {
		vpH = 5
	}
	l.vp.SetHeight(vpH)
}

func (l *executionLog) sync() {
	l.vp.SetContent(strings.Join(l.lines, "\n"))
	if l.follow {
		l.vp.GotoBottom()
	}
}
