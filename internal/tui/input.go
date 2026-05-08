package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// beginInput puts the model into a modal text-input state. Used when the
// user presses 'c' (comment), 'e' on a focused comment (edit), or 'm'
// (move). Comment modes own a multi-line bubbles textarea (caret + arrow
// nav + paste); modeMove keeps the simple m.input string because a bucket
// key is a single short token.
func (m *Model) beginInput(mode inputMode, status, prefill string) {
	m.mode = mode
	m.status = status
	m.moveMode = false
	switch mode {
	case modeComment, modeCommentEdit:
		m.commentInput = newCommentInput()
		m.commentInput.SetValue(prefill)
		m.commentInput.SetWidth(m.commentInputWidth())
		m.commentInput.SetHeight(commentInputHeight)
		m.commentInput.CursorEnd()
		m.commentInput.Focus()
		m.input = ""
	default:
		m.input = prefill
	}
}

// updateInput is the per-keystroke handler while m.mode != modeNormal.
// Comment modes delegate every key to the textarea so arrow navigation,
// home/end, and paste all work; only enter (without modifiers) and esc
// are intercepted. modeMove keeps the legacy single-line append/backspace
// loop because the input is a short bucket key.
func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeNormal
		m.input = ""
		m.commentEditID = 0
		m.commentInput = newCommentInput()
		m.status = "Cancelled"
		return m, nil
	}
	if m.mode == modeComment || m.mode == modeCommentEdit {
		// Plain Enter saves; alt/shift/ctrl-Enter all emit a newline so
		// the user can compose multi-line bodies with the modifier their
		// terminal supports. bubbles' textarea does not recognise the
		// modifier-Enter strings as newlines on its own, so we intercept
		// them and inject "\n" directly. Every other key is forwarded.
		switch msg.String() {
		case "enter":
			m.submitInput()
			return m, nil
		case "alt+enter", "shift+enter", "ctrl+j", "alt+ctrl+j":
			m.commentInput.InsertString("\n")
			return m, nil
		}
		var cmd tea.Cmd
		m.commentInput, cmd = m.commentInput.Update(msg)
		return m, cmd
	}
	// modeMove: legacy single-line input.
	switch msg.String() {
	case "enter":
		m.submitInput()
	case "backspace", "ctrl+h":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		m.input += msg.String()
	}
	return m, nil
}

// submitInput resolves the modal input by dispatching to the appropriate
// service. modeComment → comment-add; modeMove → move-task by bucket key;
// modeCommentEdit → comment-rewrite (workflow-aware so bucket policy fires).
// Errors set m.status; the mode is always cleared on the way out so the
// model returns to normal navigation regardless of outcome.
func (m *Model) submitInput() {
	var input string
	if m.mode == modeComment || m.mode == modeCommentEdit {
		input = strings.TrimSpace(m.commentInput.Value())
	} else {
		input = strings.TrimSpace(m.input)
	}
	if input == "" {
		m.status = "Input is required"
		return
	}

	var savedTask domain.Task
	selectSavedTask := false
	var err error
	switch m.mode {
	case modeComment:
		task, ok := m.selectedTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		_, err = app.NewCommentService(m.repos.Comments).Add(m.ctx, m.project, task.ID, input, "human", nil)
	case modeCommentEdit:
		if m.commentEditID <= 0 {
			err = domain.NewError(domain.ErrValidation, "no comment selected", nil)
			break
		}
		// Tags stay untouched on edit-from-TUI: the modal only captures the
		// body. CommentService.Edit replaces tags from the slice we pass, so
		// passing nil would wipe them; we don't have a tag editor wired up
		// yet, so re-pass the existing tag names instead. The service
		// re-normalises them so round-tripping is safe.
		existing, listErr := m.findCommentByID(m.commentEditID)
		if listErr != nil {
			err = listErr
			break
		}
		tagNames := make([]string, len(existing.Tags))
		for i, t := range existing.Tags {
			tagNames[i] = t.Name
		}
		_, err = app.NewCommentServiceWithWorkflow(m.repos.Comments, m.repos.Workflow).Edit(m.ctx, m.project, m.commentEditID, input, tagNames)
	case modeMove:
		task, ok := m.selectedTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		savedTask, err = app.NewTaskService(m.repos.Tasks, m.repos.Workflow).Move(m.ctx, m.project, task.ID, input)
		selectSavedTask = true
	}

	if err != nil {
		m.status = err.Error()
		m.mode = modeNormal
		m.input = ""
		m.commentEditID = 0
		m.commentInput = newCommentInput()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		if selectSavedTask && m.selectTaskByID(savedTask.ID) {
			m.taskID = savedTask.ID
		}
		m.status = "Saved"
	}
	if m.taskID > 0 && m.taskScreen == taskScreenView {
		if err := m.refreshTaskActivity(m.taskID); err != nil {
			m.status = err.Error()
		}
	}
	m.mode = modeNormal
	m.input = ""
	m.commentEditID = 0
	m.commentInput = newCommentInput()
}

// findCommentByID looks the requested comment up in the loaded snapshot
// (m.comments is the full project comment list refreshed on each tick) so
// modeCommentEdit can preserve tags without an extra round-trip.
func (m Model) findCommentByID(commentID int64) (domain.Comment, error) {
	for _, c := range m.comments {
		if c.ID == commentID {
			return c, nil
		}
	}
	return domain.Comment{}, domain.NewError(domain.ErrValidation, "comment not found in active snapshot", map[string]any{"comment_id": commentID})
}

// moveSelectedToColumn moves the currently-selected task to the bucket at
// targetColIdx. Used by the board's left/right arrow keys when moveMode is
// active; the column index is resolved via the workflow's bucket list.
func (m *Model) moveSelectedToColumn(targetColIdx int) {
	if targetColIdx < 0 || targetColIdx >= len(m.workflow.Buckets) {
		m.status = "No target column"
		return
	}
	task, ok := m.selectedTask()
	if !ok {
		m.status = "No selected task"
		return
	}
	target := m.workflow.Buckets[targetColIdx]
	if _, err := app.NewTaskService(m.repos.Tasks, m.repos.Workflow).Move(m.ctx, m.project, task.ID, target.Key); err != nil {
		m.status = err.Error()
		m.moveMode = false
		return
	}
	m.colIdx = targetColIdx
	m.moveMode = false
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		m.status = fmt.Sprintf("Moved #%d to %s", task.ID, target.Key)
	}
	m.selectTaskByID(task.ID)
	m.syncFocusedColumnScroll()
}
