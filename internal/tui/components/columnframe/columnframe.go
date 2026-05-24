// Package columnframe assembles a header + rule + cardlist body for
// the kanban-column-style surfaces (board lane, subtask panel, plan-
// network wave). Replaces the top-level renderColumnFrame helper by
// embedding the cardlist Model directly — the scroll offset never
// crosses the public API of the chrome, so callers can no longer
// pass the wrong unit.
package columnframe

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/cardlist"
)

// Model holds the column chrome (header / rule / empty-state) plus
// the cardlist that owns cursor + scroll. List is exported so
// callers can pipe MoveCursor / JumpFirst / WithItems mutations
// through the embedded model and reassign the result.
type Model struct {
	Header    string
	Rule      string
	EmptyLine string
	List      cardlist.Model
}

// View renders header + rule + (empty-state line OR cardlist slice)
// joined by newlines. hintStyle is applied to the cardlist's
// ▲/▼ indicators.
//
// The order of operations mirrors the prior renderColumnFrame
// contract — surfaces that wrap the column in a bordered box
// (kanbanColumnSized) keep the same visual result without changing
// their chrome.
func (m Model) View(hintStyle lipgloss.Style) string {
	rows := []string{m.Header, m.Rule}
	if m.List.Len() == 0 {
		if m.EmptyLine != "" {
			rows = append(rows, m.EmptyLine)
		}
		return strings.Join(rows, "\n")
	}
	body := m.List.View(hintStyle)
	if body == "" {
		return strings.Join(rows, "\n")
	}
	rows = append(rows, body)
	return strings.Join(rows, "\n")
}
