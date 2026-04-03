package main

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// listFilter provides type-to-filter for package lists.
// Press / to activate, esc to clear, type to filter.
type listFilter struct {
	input  textinput.Model
	active bool
	query  string
}

func newListFilter() listFilter {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	styles := ti.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(accent)
	styles.Cursor.Color = accent
	ti.SetStyles(styles)
	ti.Prompt = "/ "
	ti.SetWidth(30)
	return listFilter{input: ti}
}

// activate opens the filter input.
func (f listFilter) activate() listFilter {
	f.active = true
	f.input.SetValue(f.query)
	f.input.Focus()
	return f
}

// deactivate closes the filter and clears it.
func (f listFilter) deactivate() listFilter {
	f.active = false
	f.query = ""
	f.input.SetValue("")
	f.input.Blur()
	return f
}

// apply stores the current input value as the filter query and deactivates.
func (f listFilter) apply() listFilter {
	f.query = f.input.Value()
	f.active = false
	f.input.Blur()
	return f
}
