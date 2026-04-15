package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (s workspaceScreen) updateSearch(msg tea.KeyPressMsg) (screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.searchActive = false
		s.searchInput.Blur()
		return s, nil
	case "enter":
		query := strings.TrimSpace(s.searchInput.Value())
		s.searchActive = false
		s.searchInput.Blur()
		if query == "" {
			// Empty search clears results.
			s.searchResults = nil
			s.searchQuery = ""
			return s, nil
		}
		s.searchLoading = true
		s.searchQuery = query
		ctx := s.ctx
		return s, func() tea.Msg {
			results, err := searchPackagesCtx(ctx, query)
			return searchResultsMsg{query: query, results: results, err: err}
		}
	default:
		var cmd tea.Cmd
		s.searchInput, cmd = s.searchInput.Update(msg)
		return s, cmd
	}
}
