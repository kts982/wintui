package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
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
	ti.PromptStyle = lipgloss.NewStyle().Foreground(accent)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(accent)
	ti.Prompt = "/ "
	ti.Width = 30
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

// matches returns true if the package matches the filter.
func (f listFilter) matches(p Package) bool {
	if f.query == "" {
		return true
	}
	q := strings.ToLower(f.query)
	return strings.Contains(strings.ToLower(p.Name), q) ||
		strings.Contains(strings.ToLower(p.ID), q)
}

// filterPackages returns only matching packages.
func (f listFilter) filterPackages(pkgs []Package) []Package {
	if f.query == "" {
		return pkgs
	}
	var out []Package
	for _, p := range pkgs {
		if f.matches(p) {
			out = append(out, p)
		}
	}
	return out
}

// view renders the filter input or active filter label.
func (f listFilter) view() string {
	if f.active {
		return "  " + f.input.View()
	}
	if f.query != "" {
		return "  " + lipgloss.NewStyle().Foreground(accent).Render("Filter: ") +
			helpStyle.Render(f.query) + "  " +
			helpStyle.Render("(/ edit • esc clear)")
	}
	return ""
}
