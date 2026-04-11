package main

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"
)

type settingsScreen struct {
	cursor    int
	saved     bool
	dirty     bool
	errMsg    string
	diskState Settings // snapshot of settings on disk when screen was created
}

func newSettingsScreen() settingsScreen {
	disk := LoadSettings()
	return settingsScreen{
		diskState: disk,
		dirty:     !settingsEqual(appSettings, disk),
	}
}

func (s settingsScreen) init() tea.Cmd { return nil }

func (s settingsScreen) update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(settingDefs)-1 {
				s.cursor++
			}
		case "enter", "space", "right", "l":
			s.cycleForward()
		case "left", "h":
			s.cycleBackward()
		case "s":
			if err := SaveSettings(appSettings); err != nil {
				s.errMsg = "Save failed: " + err.Error()
			} else {
				s.saved = true
				s.dirty = false
				s.diskState = appSettings
				s.errMsg = ""
			}
		case "r":
			appSettings = DefaultSettings()
			s.saved = false
			s.dirty = !settingsEqual(appSettings, s.diskState)
			s.errMsg = ""
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			contentY := msg.Y - 9 // header + tab + title offset
			if contentY >= 0 && contentY < len(settingDefs) {
				s.cursor = contentY
				s.cycleForward()
			}
		}
	}
	return s, nil
}

func (s *settingsScreen) cycleForward() {
	def := settingDefs[s.cursor]
	switch def.stype {
	case settingToggle:
		cur := appSettings.getValue(def.key)
		if cur == "true" {
			appSettings.setValue(def.key, "false")
		} else {
			appSettings.setValue(def.key, "true")
		}
	case settingChoice:
		cur := appSettings.getValue(def.key)
		idx := 0
		for i, c := range def.choices {
			if c == cur {
				idx = i
				break
			}
		}
		idx = (idx + 1) % len(def.choices)
		appSettings.setValue(def.key, def.choices[idx])
	}
	s.saved = false
	s.dirty = !settingsEqual(appSettings, s.diskState)
	cache.invalidate()
}

func (s *settingsScreen) cycleBackward() {
	def := settingDefs[s.cursor]
	switch def.stype {
	case settingToggle:
		s.cycleForward() // toggle is the same either direction
		return
	case settingChoice:
		cur := appSettings.getValue(def.key)
		idx := 0
		for i, c := range def.choices {
			if c == cur {
				idx = i
				break
			}
		}
		idx--
		if idx < 0 {
			idx = len(def.choices) - 1
		}
		appSettings.setValue(def.key, def.choices[idx])
	}
	s.saved = false
	s.dirty = !settingsEqual(appSettings, s.diskState)
	cache.invalidate()
}

func (s settingsScreen) view(width, height int) string {
	panelWidth := width - 4

	// Build settings list.
	var lines []string
	for i, def := range settingDefs {
		cursor := cursorBlankStr
		labelStyle := itemStyle
		if i == s.cursor {
			cursor = cursorStr
			labelStyle = itemActiveStyle
		}

		val := appSettings.getValue(def.key)
		valDisplay := renderSettingValue(def, val)
		label := labelStyle.Render(fmt.Sprintf("%-24s", def.label))
		desc := itemDescStyle.Render(def.desc)
		lines = append(lines, fmt.Sprintf("%s%s %s  %s", cursor, label, valDisplay, desc))
	}

	// Settings panel.
	title := "Settings"
	if s.dirty {
		title = "Settings (unsaved)"
	}
	content := strings.Join(lines, "\n")
	panel := renderTitledPanel(title, content, panelWidth, len(lines), accent)

	// Detail panel for focused setting.
	detail := s.renderDetailPanel(panelWidth, height > 0 && height < 28)

	// Status line.
	var status string
	if s.errMsg != "" {
		status = "  " + errorStyle.Render(s.errMsg)
	} else if s.saved {
		status = "  " + successStyle.Render("Settings saved!")
	}

	// Config path.
	configLine := "  " + helpStyle.Render("Config: "+configPath())

	var b strings.Builder
	b.WriteString(panel + "\n")
	b.WriteString(detail + "\n")
	b.WriteString(configLine + "\n")
	if status != "" {
		b.WriteString(status + "\n")
	}
	return b.String()
}

func (s settingsScreen) helpKeys() []key.Binding {
	return []key.Binding{keyUp, keyDown, keyCycle, keySave, keyReset, keyTabs}
}

func renderSettingValue(def settingDef, val string) string {
	switch def.stype {
	case settingToggle:
		if val == "true" {
			return lipgloss.NewStyle().Bold(true).Foreground(success).Render("● ON ")
		}
		return lipgloss.NewStyle().Foreground(dim).Render("○ OFF")

	case settingChoice:
		matched := false
		var parts []string
		for _, c := range def.choices {
			display := def.choiceLabel(c)
			if c == val {
				matched = true
				parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(accent).Render("["+display+"]"))
			} else {
				parts = append(parts, helpStyle.Render(" "+display+" "))
			}
		}
		if !matched && val != "" {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(warning).Render("[v"+val+"]"))
		}
		return strings.Join(parts, "")
	}
	return val
}

func (s settingsScreen) renderDetailPanel(panelWidth int, compact bool) string {
	def := settingDefs[s.cursor]
	val := appSettings.getValue(def.key)

	var lines []string
	lines = append(lines, infoStyle.Render("Current: "+def.choiceLabel(valOrOnOff(def, val))))
	if hint := strings.TrimSpace(def.currentHint(val)); hint != "" {
		lines = append(lines, itemStyle.Render(hint))
	}
	if !compact {
		if detail := strings.TrimSpace(def.detail); detail != "" {
			lines = append(lines, "")
			for _, dl := range strings.Split(detail, "\n") {
				lines = append(lines, helpStyle.Render(dl))
			}
		}
	}

	content := strings.Join(lines, "\n")
	return renderTitledPanel(def.label, content, panelWidth, len(lines), secondary)
}

func valOrOnOff(def settingDef, val string) string {
	if def.stype == settingToggle {
		if val == "true" {
			return "on"
		}
		return "off"
	}
	return val
}
