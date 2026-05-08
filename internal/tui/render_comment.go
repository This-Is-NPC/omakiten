package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
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
	// Disarm comment-delete on any non-`d` press so the second `d` is the
	// only way to confirm. esc cancels arm-only without closing the view.
	if msg.String() != "d" {
		m.commentDeletePendingID = 0
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "e":
		if comment, ok := m.activeComment(); ok {
			m.openCommentEdit(comment)
		}
		return m, nil
	case "d":
		if comment, ok := m.activeComment(); ok {
			m.armOrConfirmCommentDelete(comment)
			// Confirmed delete closes the screen because the comment is
			// gone — drop the modal immediately so the user does not see
			// a "not found" placeholder.
			if m.commentDeletePendingID == 0 && m.status != "" && strings.HasPrefix(m.status, "Deleted comment") {
				m.closeCommentScreen()
			}
		}
		return m, nil
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

// focusedActivityComment returns the comment under the activity cursor when
// the activity column is focused and the cursor lands on a comment-typed
// event. System events (task.created/moved/etc.) return false so callers
// like the d/E keybindings can fall through to task-level operations.
func (m Model) focusedActivityComment() (domain.Comment, bool) {
	if m.taskFocus != taskFocusActivity || m.activityCursor < 0 {
		return domain.Comment{}, false
	}
	events := m.activityForTaskInView(m.taskID)
	if m.activityCursor >= len(events) {
		return domain.Comment{}, false
	}
	ev := events[m.activityCursor]
	if ev.EventType != domain.EventTypeComment {
		return domain.Comment{}, false
	}
	return eventToComment(ev), true
}

// armOrConfirmCommentDelete is the arm-then-confirm gate for hard comment
// deletion. The first `d` press records the comment ID; a second `d` on the
// same comment fires CommentService.Remove, which enforces the bucket
// permissions.comment.delete policy (inheriting from permissions.task when
// not declared). The service emits the system comment.removed event with
// the body snapshot for audit and writes one ActivityLog row per call.
func (m *Model) armOrConfirmCommentDelete(comment domain.Comment) {
	if comment.ID <= 0 {
		return
	}
	if m.commentDeletePendingID == comment.ID {
		m.executeCommentDelete(comment.ID)
		return
	}
	if allowed, hint := m.canDeleteComment(comment.TaskID); !allowed {
		m.status = hint
		return
	}
	m.commentDeletePendingID = comment.ID
	m.taskDeletePendingID = 0
	m.status = fmt.Sprintf("Confirm delete comment #%d. Press d again; esc cancels.", comment.ID)
}

// executeCommentDelete runs the CommentService.Remove call (workflow-aware so
// bucket policy is enforced) and refreshes the activity feed so the deleted
// card disappears. On guard violation the policy hint surfaces in the status
// badge while pending state is cleared so the user retries intentionally.
func (m *Model) executeCommentDelete(commentID int64) {
	m.commentDeletePendingID = 0
	if _, err := app.NewCommentServiceWithWorkflow(m.repos.Comments, m.repos.Workflow).Remove(m.ctx, m.project, commentID); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if m.taskID > 0 {
		if err := m.refreshTaskActivity(m.taskID); err != nil {
			m.status = err.Error()
			return
		}
	}
	m.activityCursor = -1
	m.status = fmt.Sprintf("Deleted comment #%d", commentID)
}

// openCommentEdit opens the modal text input pre-filled with the comment
// body so the user can rewrite it inline. The actual write happens in
// submitInput on enter — modeCommentEdit threads commentEditID through so
// the saved input lands on the right row. We close the dedicated comment
// screen first so the embedded edit input renders unambiguously inside
// the activity column of the underlying task view.
//
// Policy gate: the service re-checks bucket permissions before persisting,
// but surfacing the hint here means the user never types into a modal
// that is doomed to fail at save time.
func (m *Model) openCommentEdit(comment domain.Comment) {
	if comment.ID <= 0 {
		return
	}
	if allowed, hint := m.canEditComment(comment.TaskID); !allowed {
		m.status = hint
		return
	}
	if m.commentScreenOpen {
		m.closeCommentScreen()
	}
	m.commentEditID = comment.ID
	m.beginInput(modeCommentEdit, fmt.Sprintf("Edit comment #%d", comment.ID), comment.Body)
}

func (m Model) renderCommentInput() string {
	kicker := "New comment"
	if m.mode == modeCommentEdit && m.commentEditID > 0 {
		kicker = fmt.Sprintf("Edit comment · #%d", m.commentEditID)
	}
	lines := []string{
		m.styles.kicker(kicker),
		m.styles.hint.Render("enter saves · alt+enter/shift+enter newline · arrows/home/end navigate"),
	}
	if m.status != "" && m.status != "Comment body" && !strings.HasPrefix(m.status, "Edit comment") {
		lines = append(lines, m.styles.statusBadge(m.status))
	}
	width := m.commentInputWidth()
	innerWidth := width - 4
	if innerWidth < 8 {
		innerWidth = 8
	}
	input := m.commentInput
	input.SetWidth(innerWidth)
	input.SetHeight(commentInputHeight)
	style := m.styles.commentInput.Width(width).BorderForeground(m.styles.hintAccent.GetForeground())
	lines = append(lines, style.Render(input.View()))
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
