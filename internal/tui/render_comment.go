package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/viewport"
)

// renderCommentScreen renders a focused, full-width view of a single comment
// so long bodies can be read end-to-end. Mirrors the renderTaskView visual
// language: kicker · #ID, label rows, body in a viewport with the same
// applyTaskViewScroll indicator. The body uses the full availableWidth (no
// activity column) precisely so very long comments fit without sideways pinch.
// When `commentScreenEditing` is true the same overlay flips into a
// dedicated edit form so the user gets a task-edit-like experience instead
// of the prior inline activity-column input.
func (m Model) renderCommentScreen() string {
	comment, ok := m.activeComment()
	if !ok {
		notFound := []string{
			m.styles.kicker(fmt.Sprintf("Comment · #%d", m.commentScreenID)),
			"",
			m.styles.hint.Render("Comment not found. Press esc to return."),
		}
		return m.renderPanel(strings.Join(notFound, "\n"))
	}
	if m.commentScreenEditing {
		return m.renderCommentEditScreen(comment)
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
		// Pass the whole body as a single spanned row so gridtable.Render
		// wraps it inline; emitting one row per line would draw a horizontal
		// border between every wrapped line and read like a price list.
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
// activity cursor still on the comment they were reading. Editing state
// is also reset so re-opening the same comment lands on the read view.
func (m *Model) closeCommentScreen() {
	m.commentScreenOpen = false
	m.commentScreenID = 0
	m.commentScreen = detailscreen.New(0)
	m.commentScreenEditing = false
	m.commentEditID = 0
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
	if m.commentScreenEditing {
		return m.updateCommentEditScreen(msg)
	}
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
// When the deleted comment is the one currently displayed in the dedicated
// comment screen, that overlay closes here — the alternative ("not found"
// placeholder) is a worse UX and would require the caller to re-detect
// success via a status-string sniff.
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
	if m.commentScreenOpen && m.commentScreenID == commentID {
		m.closeCommentScreen()
	}
	m.status = fmt.Sprintf("Deleted comment #%d", commentID)
}

// openCommentEdit pivots the active comment overlay into edit mode: same
// screen, but the read-only detail body is replaced with a full-width
// textarea pre-filled with the existing body. ctrl+s saves through
// CommentService.Edit (workflow-aware so bucket policy fires), esc cancels
// back to the read view. This replaced the older flow which closed the
// overlay and routed through the embedded inline activity input — the
// user reported that as feeling like an unrelated context switch.
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
	if !m.commentScreenOpen {
		m.commentScreenOpen = true
		m.commentScreenID = comment.ID
		m.commentScreen = detailscreen.New(0)
	}
	m.commentScreenEditing = true
	m.commentEditID = comment.ID
	m.commentInput = newCommentInput()
	m.commentInput.SetValue(comment.Body)
	m.commentInput.SetWidth(m.commentEditScreenInnerWidth())
	m.commentInput.SetHeight(m.commentEditScreenInnerHeight())
	m.commentInput.CursorEnd()
	m.commentInput.Focus()
	m.status = fmt.Sprintf("Editing comment #%d", comment.ID)
	m.moveMode = false
}

// commentEditScreenInnerWidth returns the textarea inner width (after the
// outer panel's border + padding) used by the dedicated comment edit
// screen. Mirrors the task form's width budget so editing a long comment
// feels just like editing a task description.
func (m Model) commentEditScreenInnerWidth() int {
	width := m.availableWidth() - 8
	if width < 24 {
		width = 24
	}
	return width
}

// commentEditScreenInnerHeight returns the textarea height inside the
// dedicated comment edit screen. The textarea is roomier than the task
// description (comments are prose, not summaries) but stays well under
// the terminal height — the user flagged a previous full-screen sizing
// as overshooting visible space. Cap is half the task viewport, floored
// to a sane minimum so a tiny terminal still shows enough rows to type.
func (m Model) commentEditScreenInnerHeight() int {
	const (
		minHeight     = 8
		preferredCap  = 16
		terminalShare = 2 // half of taskViewportHeight at most
	)
	available := m.taskViewportHeight()
	if available <= 0 {
		return minHeight
	}
	h := available / terminalShare
	if h > preferredCap {
		h = preferredCap
	}
	if h < minHeight {
		h = minHeight
	}
	// Clamp to leave room for kicker + hint + blank + borders + footer
	// (~8 rows of chrome around the textarea). When the terminal is too
	// short to fit the preferred height + chrome, scale the textarea
	// down so the form never exceeds the visible viewport.
	const chromeBudget = 8
	maxByTerminal := available - chromeBudget
	if maxByTerminal > 0 && h > maxByTerminal {
		h = maxByTerminal
	}
	if h < minHeight {
		h = minHeight
	}
	return h
}

// renderCommentEditScreen renders the dedicated full-width edit form.
// Layout matches the task edit screen (kicker · hint · bordered textarea
// · footer cues come from renderFooter). Border switches to the accent
// color so the active surface is unambiguous.
func (m Model) renderCommentEditScreen(comment domain.Comment) string {
	width := m.availableWidth() - 4
	innerWidth := m.commentEditScreenInnerWidth()
	innerHeight := m.commentEditScreenInnerHeight()

	input := m.commentInput
	input.Cursor.Style = m.styles.cursor
	input.SetWidth(innerWidth)
	input.SetHeight(innerHeight)
	style := m.styles.multilineInput.Width(width).Height(innerHeight + 2).BorderForeground(m.styles.hintAccent.GetForeground())

	lines := []string{
		m.styles.kicker(fmt.Sprintf("Edit comment · #%d", comment.ID)),
		m.styles.hint.Render("ctrl+s saves · esc cancels · alt+enter/shift+enter newline · arrows/home/end navigate"),
		"",
		style.Render(input.View()),
	}
	return "\n" + indentBlock(strings.Join(lines, "\n"), 2)
}

// updateCommentEditScreen handles keystrokes while the dedicated comment
// edit overlay is active. Save (ctrl+s) and cancel (esc) are intercepted
// here; everything else forwards to the textarea so cursor/word/kill-line
// edits work natively. modeCommentEdit no longer exists — the editing
// state lives entirely on `commentScreenEditing`.
func (m *Model) updateCommentEditScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	bindings := newCommentInputBindings()
	switch {
	case msg.String() == "ctrl+c":
		return *m, tea.Quit
	case msg.String() == "ctrl+s":
		m.saveCommentEdit()
		return *m, nil
	case key.Matches(msg, bindings.Cancel):
		m.cancelCommentEdit()
		return *m, nil
	}
	var cmd tea.Cmd
	m.commentInput, cmd = m.commentInput.Update(msg)
	return *m, cmd
}

// saveCommentEdit persists the typed body via CommentService.Edit, then
// flips the overlay back to its read-only detail view so the user lands
// on the freshly-saved comment. Tags survive because Edit replaces them
// from the slice we pass — we re-pass the existing tag names from the
// loaded comment snapshot, mirroring the prior submitInput logic.
func (m *Model) saveCommentEdit() {
	body := strings.TrimSpace(m.commentInput.Value())
	if body == "" {
		m.status = "Input is required"
		return
	}
	if m.commentEditID <= 0 {
		m.status = "no comment selected"
		return
	}
	existing, err := m.findCommentByID(m.commentEditID)
	if err != nil {
		m.status = err.Error()
		return
	}
	tagNames := make([]string, len(existing.Tags))
	for i, t := range existing.Tags {
		tagNames[i] = t.Name
	}
	if _, err := app.NewCommentServiceWithWorkflow(m.repos.Comments, m.repos.Workflow).Edit(m.ctx, m.project, m.commentEditID, body, tagNames); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if m.taskID > 0 && m.taskScreen == taskScreenView {
		if err := m.refreshTaskActivity(m.taskID); err != nil {
			m.status = err.Error()
			return
		}
	}
	m.status = "Saved"
	m.exitCommentEditMode()
}

// cancelCommentEdit drops the unsaved buffer and returns to the read-only
// detail view. The comment screen overlay stays open so the user lands
// back on the same card they were reading.
func (m *Model) cancelCommentEdit() {
	m.status = "Cancelled"
	m.exitCommentEditMode()
}

// exitCommentEditMode returns the comment screen to its read-only state.
// commentInput is reset so a future open doesn't leak the prior body.
func (m *Model) exitCommentEditMode() {
	m.commentScreenEditing = false
	m.commentEditID = 0
	m.commentInput = newCommentInput()
}

func (m Model) renderCommentInput() string {
	lines := []string{
		m.styles.kicker("New comment"),
		m.styles.hint.Render("enter saves · alt+enter/shift+enter newline · arrows/home/end navigate"),
	}
	if m.status != "" && m.status != "Comment body" {
		lines = append(lines, m.styles.statusBadge(m.status))
	}
	width := m.commentInputWidth()
	innerWidth := width - 4
	if innerWidth < 8 {
		innerWidth = 8
	}
	input := m.commentInput
	input.Cursor.Style = m.styles.cursor
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
	wrapped := gridtable.WrapLines(strings.Split(body, "\n"), width)
	if len(wrapped) <= commentCardLineLimit {
		return strings.Join(wrapped, "\n")
	}
	visible := wrapped[:commentCardLineLimit]
	hidden := len(wrapped) - commentCardLineLimit
	hint := m.styles.hint.Render(fmt.Sprintf("↩ %d more lines — enter opens", hidden))
	return strings.Join(visible, "\n") + "\n" + hint
}
