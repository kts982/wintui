package main

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type executionLog struct {
	vp      viewport.Model
	lines   []string
	current []string
	follow  bool
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

func (l executionLog) fullOutput() string {
	return strings.Join(l.lines, "\n")
}

func (l executionLog) currentOutput() string {
	return strings.Join(l.current, "\n")
}

func (l executionLog) helpKeys() []key.Binding {
	bindings := []key.Binding{keyScroll}
	if !l.follow {
		bindings = append(bindings, keyFollow)
	}
	bindings = append(bindings, keyEscCancel)
	return bindings
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

func (l *executionLog) setSize(width, height int) {
	l.vp.SetWidth(width - 8)
	vpH := height - 12
	if vpH < 5 {
		vpH = 5
	}
	l.vp.SetHeight(vpH)
}

func (l executionLog) view(width, height int) string {
	return indentBlock(l.vp.View(), 2)
}

func (l *executionLog) sync() {
	l.vp.SetContent(strings.Join(l.lines, "\n"))
	if l.follow {
		l.vp.GotoBottom()
	}
}
