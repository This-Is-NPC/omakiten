package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/multilineform"
)

// beginInput puts the model into a modal text-input state. Used when the
// user presses 'c' (comment-add embedded in the activity column) or 'm'
// (move target bucket key). Comment editing has its own full-screen
// overlay (`commentScreenEditing`) and does not go through this entry
// point. modeComment uses a bubbles textarea for multi-line composition;
// modeMove uses a bubbles textinput so cursor / word-jump / kill-line all
// work natively.
func (m *Model) beginInput(mode inputMode, status, prefill string) {
	m.mode = mode
	m.status = status
	m.moveMode = false
	switch mode {
	case modeComment, modePlanGoal:
		m.commentInput = newCommentInput()
		m.commentInput.SetValue(prefill)
		// Calibrate the persistent textarea geometry BEFORE CursorEnd so
		// the end-of-content scroll is computed against the same wrap
		// width that renderCommentInput will pass into multilineform.Render.
		// Without this, the persistent viewport keeps the bubbles default
		// (40 cols / 6 rows) and the first keystroke desyncs yOffset.
		// See multilineform.Resize for the full explanation; mirrors
		// resizeTaskDescriptionInput and openCommentEdit.
		multilineform.Resize(
			&m.commentInput,
			m.commentInputWidth(),
			commentInputHeight,
			m.styles.multilineFormTheme(),
		)
		m.commentInput.CursorEnd()
		m.commentInput.Focus()
		m.moveInput.Reset()
		// Embedded comment input shrinks activityViewportLines by ~9 rows.
		// Re-sync so the focused card stays inside the new budget.
		m.syncActivityScrollToCursor()
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
	if m.mode == modeComment || m.mode == modePlanGoal {
		bindings := newCommentInputBindings()
		switch {
		case key.Matches(msg, bindings.Cancel):
			m.cancelInput()
			return m, nil
		case m.mode == modePlanGoal && msg.String() == "ctrl+s":
			// Plan goal body is a full-panel multi-line editor; Enter
			// stays bound to "insert newline" so markdown lists and
			// paragraphs land naturally. Save is ctrl+s instead — same
			// keystroke as the dedicated comment-edit overlay.
			m.submitInput()
			return m, nil
		case m.mode == modeComment && key.Matches(msg, bindings.Save):
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
	m.commentEditID = 0
	m.planGoalEditingID = 0
	m.planAssignTaskID = 0
	m.moveInputTargetID = 0
	m.commentInput = newCommentInput()
	m.moveInput = newMoveInput()
	m.status = m.t("tui.status.cancelled")
	// Closing the embedded comment input restores the full activityViewportLines
	// budget; re-sync so the focused card is positioned against the new height.
	m.syncActivityScrollToCursor()
}

// submitInput resolves the modal input by dispatching to the appropriate
// service. modeComment → comment-add; modeMove → move-task by bucket key;
// modeCommentEdit → comment-rewrite (workflow-aware so bucket policy fires).
// Errors set m.status; the mode is always cleared on the way out so the
// model returns to normal navigation regardless of outcome.
func (m *Model) submitInput() {
	var input string
	switch m.mode {
	case modeComment, modePlanGoal:
		input = m.commentInput.Value()
	default:
		input = strings.TrimSpace(m.moveInput.Value())
	}
	// modePlanGoal accepts an empty body (a deliberate clear), so the
	// input-required guard only fires for the other modal flavours.
	if m.mode != modePlanGoal {
		input = strings.TrimSpace(input)
		if input == "" {
			m.status = m.t("tui.status.input_required")
			return
		}
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
		commentSvc := app.NewCommentService(m.repos.Comments, m.repos.activeSnapshot())
		_, err = commentSvc.Add(m.ctx, m.project, task.ID, input, "human", nil)
	case modeMove:
		var task domain.Task
		var ok bool
		if m.moveInputTargetID > 0 {
			// `m` from the sub-tasks pane (or any caller that named an
			// explicit target via beginMoveInputForTask) MUST rewrite that
			// row — not whatever selectedTask happens to return. The
			// pre-fix code always routed through selectedTask, which
			// resolves to the open task screen's parent while a task is
			// open, so every sub-task move quietly hit the parent instead.
			task, ok = m.taskByID(m.moveInputTargetID)
		} else {
			task, ok = m.selectedTask()
		}
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		savedTask, err = app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot()).Move(m.ctx, m.project, task.ID, input)
		selectSavedTask = true
	case modePlanGoal:
		if m.repos.Plans == nil || m.planGoalEditingID == 0 {
			err = domain.NewError(domain.ErrValidation, "plan goal editor has no target plan", nil)
			break
		}
		planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
		_, err = planSvc.UpdateGoalBody(m.ctx, m.project, m.planGoalEditingID, input)
	case modePlanAssign:
		if m.repos.Tasks == nil || m.planAssignTaskID == 0 {
			err = domain.NewError(domain.ErrValidation, "plan assignee editor has no target task", nil)
			break
		}
		taskSvc := app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot())
		_, _, err = taskSvc.Assign(m.ctx, m.project, m.planAssignTaskID, input)
	}

	if err != nil {
		m.status = err.Error()
		m.mode = modeNormal
		m.commentEditID = 0
		m.planGoalEditingID = 0
		m.planAssignTaskID = 0
		m.moveInputTargetID = 0
		m.commentInput = newCommentInput()
		m.moveInput = newMoveInput()
		m.syncActivityScrollToCursor()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		if selectSavedTask && m.selectTaskByID(savedTask.ID) {
			m.taskID = savedTask.ID
		}
		m.status = m.t("tui.status.saved")
	}
	if m.mode == modePlanGoal || m.mode == modePlanAssign {
		m.reloadPlanNetwork()
	}
	if m.taskID > 0 && m.taskScreen == taskScreenView {
		if err := m.refreshTaskActivity(m.taskID); err != nil {
			m.status = err.Error()
		}
	}
	m.mode = modeNormal
	m.commentEditID = 0
	m.planGoalEditingID = 0
	m.planAssignTaskID = 0
	m.moveInputTargetID = 0
	m.commentInput = newCommentInput()
	m.moveInput = newMoveInput()
	m.syncActivityScrollToCursor()
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
		m.status = m.t("tui.status.no_target_column")
		return
	}
	task, ok := m.selectedTask()
	if !ok {
		m.status = m.t("tui.status.no_selected_task")
		return
	}
	target := m.workflow.Buckets[targetColIdx]
	if _, err := app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot()).Move(m.ctx, m.project, task.ID, target.Key); err != nil {
		m.status = err.Error()
		m.moveMode = false
		return
	}
	m.colIdx = targetColIdx
	m.moveMode = false
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		m.status = fmt.Sprintf(m.t("tui.status.task_moved_fmt"), task.ID, target.Key)
	}
	m.selectTaskByID(task.ID)
	m.syncFocusedColumnScroll()
}
