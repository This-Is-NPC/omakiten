package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/tui/components/field"
)

// beginInput puts the model into a modal text-input state. Used when the
// user presses 'c' (comment-add embedded in the activity column) or 'm'
// (move target bucket key). Comment editing has its own full-screen
// screen and does not go through this entry point. modeComment uses a
// bubbles textarea for multi-line composition;
// modeMove uses a bubbles textinput so cursor / word-jump / kill-line all
// work natively.
func (m *Model) beginInput(mode inputMode, status, prefill string) {
	m.mode = mode
	m.status = status
	m.blurActiveScreen()
	switch mode {
	case modeComment:
		m.commentInput = newCommentInput()
		m.commentInput.SetValue(prefill)
		// Calibrate the persistent textarea geometry BEFORE CursorEnd so
		// the end-of-content scroll is computed against the same wrap
		// width that renderCommentInput will pass into field.RenderArea.
		// Without this, the persistent viewport keeps the bubbles default
		// (40 cols / 6 rows) and the first keystroke desyncs yOffset.
		// See field.Resize for the full explanation; mirrors
		// resizeTaskDescriptionInput and openCommentEdit.
		field.Resize(
			&m.commentInput,
			m.commentInputWidth(),
			commentInputHeight,
			m.styles.multilineFormTheme(),
		)
		m.commentInput.CursorEnd()
		m.commentInput.Focus()
		m.moveInput.Reset()
	default:
		m.moveInput = newMoveInput()
		m.moveInput.SetValue(prefill)
		m.moveInput.CursorEnd()
		m.moveInput.Focus()
	}
}

// updateInput is the per-keystroke handler while m.mode != modeNormal.
// Both modal surfaces are bubbles components — every key not claimed by
// the parent (Save / Cancel / quit) is forwarded so cursor movement,
// word-jump, kill-line, paste, etc. all work natively. Modifier-Enter
// (shift+enter / alt+enter / ctrl+j) is bound to KeyMap.InsertNewline
// inside `newCommentInput`, so a bare Enter consistently means "submit"
// across every modal mode without a hand-rolled rune-append loop.
func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.mode == modeComment {
		bindings := newCommentInputBindings()
		switch {
		case key.Matches(msg, bindings.Cancel):
			m.cancelInput()
			return m, nil
		case key.Matches(msg, bindings.Save):
			m.submitInput()
			return m, nil
		}
		var cmd tea.Cmd
		m.commentInput, cmd = m.commentInput.Update(msg)
		return m, cmd
	}
	bindings := newMoveInputBindings()
	switch {
	case key.Matches(msg, bindings.Cancel):
		m.cancelInput()
		return m, nil
	case key.Matches(msg, bindings.Save):
		m.submitInput()
		return m, nil
	}
	var cmd tea.Cmd
	m.moveInput, cmd = m.moveInput.Update(msg)
	return m, cmd
}

// cancelInput aborts the active modal without persisting anything,
// returning the model to normal navigation. Both bubbles inputs are
// recreated so the next beginInput starts from a clean slate (no
// leaked text, no leaked cursor position).
func (m *Model) cancelInput() {
	m.mode = modeNormal
	m.moveInputTargetID = 0
	m.commentInput = newCommentInput()
	m.moveInput = newMoveInput()
	m.status = m.t("tui.status.cancelled")
}

// submitInput resolves the modal input by dispatching to the appropriate
// service. modeComment → comment-add; modeMove → move-task by bucket key;
// modeCommentEdit → comment-rewrite (workflow-aware so bucket policy fires).
// Errors set m.status; the mode is always cleared on the way out so the
// model returns to normal navigation regardless of outcome.
func (m *Model) submitInput() {
	input := strings.TrimSpace(m.inputValue())
	if input == "" {
		m.status = m.t("tui.status.input_required")
		return
	}

	savedTask, selectSavedTask, err := m.submitInputOperation(input)
	if err != nil {
		m.status = err.Error()
		m.resetInput()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		if selectSavedTask {
			m.selectTaskByID(savedTask.ID)
		}
		m.status = m.t("tui.status.saved")
	}
	m.resetInput()
}

func (m Model) inputValue() string {
	if m.mode == modeComment {
		return m.commentInput.Value()
	}
	return m.moveInput.Value()
}

func (m *Model) submitInputOperation(input string) (domain.Task, bool, error) {
	switch m.mode {
	case modeComment:
		return domain.Task{}, false, m.submitCommentInput(input)
	case modeMove:
		return m.submitMoveInput(input)
	default:
		return domain.Task{}, false, nil
	}
}

func (m *Model) submitCommentInput(input string) error {
	task, ok := m.selectedTask()
	if !ok {
		return domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
	}
	svc, ok := m.requireOps()
	if !ok {
		return fmt.Errorf("%s", m.status)
	}
	_, err := svc.AddComment(m.ctx, operation.AddCommentInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          task.ID,
		Body:            input,
		AuthorType:      "human"})
	return err
}

func (m *Model) submitMoveInput(input string) (domain.Task, bool, error) {
	task, ok := m.moveInputTask()
	if !ok {
		return domain.Task{}, false, domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
	}
	svc, ok := m.requireOps()
	if !ok {
		return domain.Task{}, false, fmt.Errorf("%s", m.status)
	}
	_, err := svc.MoveTask(m.ctx, operation.MoveTaskInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          task.ID,
		BucketKey:       input})
	if err != nil {
		return domain.Task{}, false, err
	}
	task.BucketKey = input
	return task, true, nil
}

func (m *Model) moveInputTask() (domain.Task, bool) {
	if m.moveInputTargetID > 0 {
		return m.taskByID(m.moveInputTargetID)
	}
	return m.selectedTask()
}

func (m *Model) resetInput() {
	m.mode = modeNormal
	m.moveInputTargetID = 0
	m.commentInput = newCommentInput()
	m.moveInput = newMoveInput()
}
