package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/viewport"
)

// renderCommentScreen renders a focused, full-width view of a single comment
// so long bodies can be read end-to-end. Mirrors the renderTaskView visual
// language: kicker · #ID, label rows, body in a viewport with the same
// applyTaskViewScroll indicator. The body uses the full availableWidth (no
// activity column) precisely so very long comments fit without sideways pinch.
func (m Model) renderCommentScreen() string {
	comment, ok := m.activeComment()
	if !ok {
		notFound := []string{
			m.styles.kicker(fmt.Sprintf("Comment · #%d", m.commentScreenID)),
			"",
			m.styles.hint.Render("Comment not found. Press esc to return."),
		}
		return "\n" + indentBlock(m.styles.panel.Render(strings.Join(notFound, "\n")), 2)
	}

	tagLine := ""
	if len(comment.Tags) > 0 {
		names := make([]string, len(comment.Tags))
		for i, t := range comment.Tags {
			names[i] = t.Label
		}
		tagLine = strings.Join(names, " · ")
	}

	available := m.availableWidth()
	valueWidth := available - detailscreen.LabelWidth - 1 - 2
	if valueWidth < 24 {
		valueWidth = 24
	}
	if valueWidth > 120 {
		valueWidth = 120
	}

	screen := m.commentScreen.Reset(valueWidth).
		Custom(m.styles.kicker(fmt.Sprintf("Comment · #%d", comment.ID))).
		Row("Task", fmt.Sprintf("#%d", comment.TaskID)).
		Row("Author", strings.TrimSpace(comment.AuthorType)).
		Row("When", strings.TrimSpace(comment.CreatedAt))
	if tagLine != "" {
		screen = screen.Row("Tags", tagLine)
	}
	screen = screen.Kicker("Body")
	body := strings.TrimSpace(comment.Body)
	if body == "" {
		screen = screen.Span(m.styles.hint.Render("empty comment"))
	} else {
		// Pass the whole body as a single spanned row so renderGridTable wraps
		// it inline; emitting one row per line would draw a horizontal border
		// between every wrapped line and read like a price list.
		screen = screen.Span(strings.TrimRight(body, "\n"))
	}

	return "\n" + indentBlock(screen.View(m.taskViewportHeight(), m.styles.border, m.styles.hint), 2)
}

// openCommentScreen opens the dedicated comment detail view for the comment
// under the activity cursor. System events have no body to read, so they
// ignore Enter (the activity feed still shows them inline as one-liners).
// commentScreen viewport resets so the body always opens at the top.
func (m *Model) openCommentScreen() {
	events := m.activityForTaskInView(m.taskID)
	if m.activityCursor < 0 || m.activityCursor >= len(events) {
		return
	}
	ev := events[m.activityCursor]
	if ev.EventType != domain.EventTypeComment {
		return
	}
	m.commentScreenOpen = true
	m.commentScreenID = ev.ID
	m.commentScreen = detailscreen.New(0)
}

// closeCommentScreen returns the user to the task detail view with the
// activity cursor still on the comment they were reading.
func (m *Model) closeCommentScreen() {
	m.commentScreenOpen = false
	m.commentScreenID = 0
	m.commentScreen = detailscreen.New(0)
}

// activeComment returns the comment currently displayed in the comment screen,
// looked up from the loaded activity feed. Falls back to false when the
// screen is open but the underlying event has been removed (refresh after
// delete) — caller renders a "not found" placeholder in that case.
func (m Model) activeComment() (domain.Comment, bool) {
	if !m.commentScreenOpen || m.commentScreenID <= 0 {
		return domain.Comment{}, false
	}
	for _, ev := range m.activityForTaskInView(m.taskID) {
		if ev.ID == m.commentScreenID && ev.EventType == domain.EventTypeComment {
			return eventToComment(ev), true
		}
	}
	return domain.Comment{}, false
}

func (m Model) updateCommentScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	// Delegate scroll keys + esc to the embedded detailscreen sub-model.
	// EventCancel from esc closes the comment view and returns to the
	// underlying task screen with the activity cursor preserved.
	var cmd tea.Cmd
	m.commentScreen, cmd = m.commentScreen.Update(msg, m.taskViewportHeight())
	if m.commentScreen.LastEvent() == viewport.EventCancel {
		m.closeCommentScreen()
	}
	return m, cmd
}

func (m Model) renderCommentInput() string {
	lines := []string{
		m.styles.kicker("New comment"),
		m.styles.hint.Render("enter saves · alt+enter/shift+enter newline"),
	}
	if m.status != "" && m.status != "Comment body" {
		lines = append(lines, m.styles.statusBadge(m.status))
	}
	lines = append(lines, m.styles.commentInput.Width(m.commentInputWidth()).Render(m.input))
	return strings.Join(lines, "\n")
}

// renderCommentCardSelected renders a single comment card. focused controls
// the border accent (so the active card pops); long bodies are capped to
// commentCardLineLimit with a "↩ N more — enter opens" footer.
func (m Model) renderCommentCardSelected(comment domain.Comment, focused bool) string {
	header := m.styles.hintAccent.Render(comment.AuthorType)
	if strings.TrimSpace(comment.CreatedAt) != "" {
		header += m.styles.hint.Render(" · " + comment.CreatedAt)
	}
	contentWidth := m.commentCardContentWidth()
	body := strings.TrimSpace(comment.Body)
	if body == "" {
		body = m.styles.hint.Render("empty comment")
	} else {
		body = m.cappedCommentBody(comment.ID, body, contentWidth)
	}
	content := header + "\n" + body
	if len(comment.Tags) > 0 {
		badges := make([]string, len(comment.Tags))
		for i, tag := range comment.Tags {
			badges[i] = m.styles.badgeInfo.Render("#" + tag.Label)
		}
		content += "\n" + wrapBadges(badges, contentWidth)
	}
	style := m.styles.commentCard.Width(m.commentCardWidth())
	if focused {
		style = style.BorderForeground(m.styles.hintAccent.GetForeground())
	}
	return style.Render(content)
}

// cappedCommentBody wraps the body to the available width and truncates to
// commentCardLineLimit visible lines plus a "↩ N more lines — enter opens"
// footer. The cap is unconditional: long comments are read in the dedicated
// comment screen (Enter on a focused comment), where they can scroll freely.
func (m Model) cappedCommentBody(_ int64, body string, width int) string {
	wrapped := wrapLinesToWidth(strings.Split(body, "\n"), width)
	if len(wrapped) <= commentCardLineLimit {
		return strings.Join(wrapped, "\n")
	}
	visible := wrapped[:commentCardLineLimit]
	hidden := len(wrapped) - commentCardLineLimit
	hint := m.styles.hint.Render(fmt.Sprintf("↩ %d more lines — enter opens", hidden))
	return strings.Join(visible, "\n") + "\n" + hint
}
