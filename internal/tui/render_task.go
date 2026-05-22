package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/multilineform"
	"omakiten/internal/tui/components/picker"
)

func (m *Model) updateTaskScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.blockerPickerOpen {
		return m.updateBlockerPicker(msg)
	}

	if m.taskScreen == taskScreenView {
		// Disarm pending delete prompts on any key other than `d` so the
		// arm-then-confirm gate cannot accidentally fire after navigation.
		if msg.String() != "d" {
			m.taskDeletePendingID = 0
			m.commentDeletePendingID = 0
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return *m, tea.Quit
		case "esc":
			// Stack pop wins over close-to-board so multi-level drills
			// unwind one ancestor at a time; only a root-level task
			// (empty stack) closes the screen.
			if !m.popTaskViewStack() {
				m.closeTaskScreen("")
			}
		case "e":
			if task, ok := m.activeTask(); ok {
				m.openTaskEdit(task)
			}
		case "n":
			if task, ok := m.activeTask(); ok {
				m.openSubTaskCreate(task)
			}
		case "f":
			if task, ok := m.activeTask(); ok {
				m.openDescriptionScreen(task)
			}
		case "b":
			if _, ok := m.activeTask(); ok {
				m.openBlockerPicker()
			}
		case "c":
			if _, ok := m.activeTask(); ok {
				m.beginInput(modeComment, m.t("tui.input.comment_body"), "")
			}
		case "m":
			if _, ok := m.activeTask(); ok {
				m.beginInput(modeMove, m.t("tui.input.target_bucket_key"), "")
			}
		case "d":
			// Task delete only fires when the form column owns focus —
			// activity-column keys belong to comment navigation, and the
			// dedicated comment screen (Enter on a focused comment) owns
			// per-comment edit/delete. Surfacing two destructive verbs
			// behind the same key on different focus states was the bug
			// the user reported as "as vezes o botão estar visivel e as
			// vezes não".
			if m.taskFocus == taskFocusForm {
				if task, ok := m.activeTask(); ok {
					m.armOrConfirmTaskDelete(task)
				}
			}
		case "r":
			if err := m.refresh(); err != nil {
				m.status = err.Error()
			} else {
				m.status = m.t("tui.status.refreshed")
			}
		case "M":
			m.toggleMarkdownRendered()
		case "tab":
			m.toggleTaskFocus()
		case "shift+tab":
			m.toggleTaskFocus()
		case "J":
			m.moveActivityCursor(1)
		case "K":
			m.moveActivityCursor(-1)
		case "enter":
			switch m.taskFocus {
			case taskFocusActivity:
				m.openCommentScreen()
			case taskFocusSubtasks:
				m.drillIntoSubtask()
			}
		default:
			// Per-zone nav: each focus zone owns its own j/k/g/G/pgup/pgdn
			// semantics. The form zone delegates to its embedded
			// detailscreen viewport; activity has split semantics for
			// line-scroll vs card-cursor; sub-tasks moves a card cursor
			// just like activity.
			switch m.taskFocus {
			case taskFocusActivity:
				switch msg.String() {
				case "j", "down":
					m.moveActivityCursor(1)
				case "k", "up":
					m.moveActivityCursor(-1)
				case "pgdown", "ctrl+d":
					m.scrollActivityLines(m.activityViewportLines() / 2)
				case "pgup", "ctrl+u":
					m.scrollActivityLines(-m.activityViewportLines() / 2)
				case "home", "g":
					m.activityScroll = 0
				case "end", "G":
					m.activityScroll = 1 << 20
					m.clampActivityScroll()
				}
			case taskFocusSubtasks:
				switch msg.String() {
				case "j", "down":
					m.moveSubtaskCursor(1)
				case "k", "up":
					m.moveSubtaskCursor(-1)
				case "home", "g":
					m.subtaskCursor = 0
					m.subtaskScroll = 0
				case "end", "G":
					if children := m.directChildren(m.taskID); len(children) > 0 {
						m.subtaskCursor = len(children) - 1
						m.syncSubtaskScrollToCursor()
					}
				}
			default:
				m.taskView, _ = m.taskView.Update(msg, m.taskViewportHeight())
			}
		}
		return *m, nil
	}

	// Form-mode keys split into two layers: the verbs handled by the
	// surrounding screen (esc/ctrl+s/ctrl+b/tab/priority cycle) and the
	// raw text edits that the focused bubbles input owns. Anything not
	// claimed by the screen is forwarded to the active input's Update so
	// arrow keys, home/end, paste, word delete, etc. work natively.
	switch msg.String() {
	case "ctrl+c":
		return *m, tea.Quit
	case "esc":
		if m.taskScreen == taskScreenCreate {
			m.closeTaskScreen(m.t("tui.status.cancelled"))
		} else if task, ok := m.activeTask(); ok {
			m.openTaskView(task)
		} else {
			m.closeTaskScreen(m.t("tui.status.cancelled"))
		}
		return *m, nil
	case "ctrl+s":
		m.saveTaskForm()
		return *m, nil
	case "tab", "shift+tab":
		m.toggleTaskField()
		return *m, nil
	case "ctrl+b":
		if m.taskScreen == taskScreenEdit {
			m.openBlockerPicker()
		}
		return *m, nil
	}
	if m.taskField == taskFieldPriority {
		switch msg.String() {
		case "left", "h":
			m.cycleTaskPriority(-1)
			return *m, nil
		case "right", "l":
			m.cycleTaskPriority(1)
			return *m, nil
		}
		// Other keys are no-ops on the priority enum so they don't
		// accidentally feed into a blurred input below.
		return *m, nil
	}
	var cmd tea.Cmd
	switch m.taskField {
	case taskFieldTitle:
		m.taskTitleInput, cmd = m.taskTitleInput.Update(msg)
	case taskFieldDescription:
		// Modifier-Enter is wired into the textarea's KeyMap.InsertNewline
		// (see newTaskDescriptionInput), so a bare Enter still falls through
		// to the form-level ctrl+s/save plumbing because nothing matches it.
		m.taskDescriptionInput, cmd = m.taskDescriptionInput.Update(msg)
	}
	return *m, cmd
}

func (m *Model) updateBlockerPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return *m, tea.Quit
	}
	candidates := m.blockerPickerCandidates()
	rowCount := len(candidates)

	// Delegate navigation + esc/space/ctrl+s recognition to the picker
	// sub-model. We still own the side-effects (close/save/toggle) because
	// those touch parent fields (m.blockerPickerChecks, status, refresh).
	var cmd tea.Cmd
	m.blockerPicker, cmd = m.blockerPicker.Update(msg, rowCount, scrollDataRows(m.blockerPickerViewportRows()))

	switch m.blockerPicker.LastEvent() {
	case picker.EventCancel:
		m.closeBlockerPicker(m.t("tui.status.cancelled"))
	case picker.EventSelect:
		m.saveBlockerPicker()
	case picker.EventToggle:
		if m.blockerPicker.Cursor >= 0 && m.blockerPicker.Cursor < rowCount {
			taskID := candidates[m.blockerPicker.Cursor].ID
			if m.blockerPickerChecks == nil {
				m.blockerPickerChecks = map[int64]bool{}
			}
			m.blockerPickerChecks[taskID] = !m.blockerPickerChecks[taskID]
		}
	}
	return *m, cmd
}

// blockerPickerViewportRows returns how many candidate rows fit in the picker
// panel after the screen chrome and the panel's internal header rows.
func (m Model) blockerPickerViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 2 task-screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 6 panel header rows (kicker/hint/blank/metaRow/blank/separator) = 13.
	chrome := 13
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m Model) blockerPickerCandidates() []domain.Task {
	candidates := make([]domain.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task.ID != m.blockerPickerTaskID {
			candidates = append(candidates, task)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return candidates
}

func (m *Model) openTaskCreate() {
	m.taskScreen = taskScreenCreate
	m.taskID = 0
	m.taskCreateParentID = nil
	m.taskTitleInput = newTaskTitleInput()
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.resizeTaskDescriptionInput()
	// Default the form to the priority flagged `default: true` in
	// config.priorities, falling back to the middle entry when none is
	// flagged. priorityZero falls through to the storage column DEFAULT.
	m.taskPriority = m.defaultPriorityID()
	m.taskField = taskFieldTitle
	m.applyTaskFieldFocus()
	m.status = m.t("tui.status.new_task")
	m.moveMode = false
}

// openSubTaskCreate boots the create form pre-attached to a parent.
// Triggered by `n` from inside the detail view; the parent FK is held
// in taskCreateParentID, surfaced as a breadcrumb in the form header,
// and routed through TaskService.AddSub on save.
func (m *Model) openSubTaskCreate(parent domain.Task) {
	m.openTaskCreate()
	parentID := parent.ID
	m.taskCreateParentID = &parentID
	m.status = fmt.Sprintf(m.t("tui.status.new_subtask_fmt"), parent.ID, parent.Title)
}

// defaultPriorityID returns the id flagged `default: true` in the active
// priorities table, falling back to the middle entry. PriorityZero when
// no priorities are loaded so the form silently defers to the storage
// column DEFAULT.
func (m Model) defaultPriorityID() domain.Priority {
	if len(m.priorities) == 0 {
		return domain.PriorityZero
	}
	for _, p := range m.priorities {
		if p.Default {
			return domain.Priority(p.ID)
		}
	}
	return domain.Priority(m.priorities[len(m.priorities)/2].ID)
}

func (m *Model) openTaskView(task domain.Task) {
	m.taskScreen = taskScreenView
	m.taskID = task.ID
	m.taskTitleInput = newTaskTitleInput()
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.taskField = taskFieldTitle
	m.status = ""
	m.moveMode = false
	m.taskView = detailscreen.New(0)
	m.activityScroll = 0
	m.activityCursor = -1
	m.subtaskCursor = -1
	m.subtaskScroll = 0
	m.taskFocus = taskFocusForm
	m.descriptionScreenOpen = false
	m.descriptionScreen = detailscreen.New(0)
	// Stack reset is policy: any external open (board / table / graph
	// enter) starts a fresh drill history. Sub-task drills and stack
	// pops save/restore taskViewStack around this call to preserve
	// their breadcrumb.
	m.taskViewStack = nil
	if err := m.refreshTaskActivity(task.ID); err != nil {
		m.status = err.Error()
	}
}

// refreshTaskActivity loads the unified activity feed (comments + system
// events) for the given task using the configured task_activity sort order.
// Stored separately from m.comments so the global comments slice keeps
// powering badges/metrics without leaking system events into them.
func (m *Model) refreshTaskActivity(taskID int64) error {
	if taskID <= 0 || m.repos.Events == nil {
		m.activity = nil
		m.activityForTask = 0
		return nil
	}
	// Validator guarantees task_activity.sort.order is set in the
	// bundle; direct field access is safe.
	order := m.views.TaskActivity.Sort.Order
	events, err := m.repos.Events.ListTaskActivity(m.ctx, m.project.ID, taskID, order)
	if err != nil {
		return err
	}
	m.activity = events
	m.activityForTask = taskID
	return nil
}

func (m *Model) openTaskEdit(task domain.Task) {
	// Surface the policy hint at the press of `e` instead of waiting for
	// ctrl+s. Without this gate the user types a whole edit just to be
	// rejected at save time, which feels like a bait-and-switch — the
	// same enforcement that runs in the service is mirrored here so the
	// form simply does not open when the bucket forbids edits. The
	// service still re-checks before persisting, so this is a UX gate
	// only, not a security boundary.
	if allowed, hint := m.canEditTask(task.ID); !allowed {
		m.status = hint
		return
	}
	m.taskScreen = taskScreenEdit
	m.taskID = task.ID
	m.taskTitleInput = newTaskTitleInput()
	m.taskTitleInput.SetValue(task.Title)
	// Position the caret at the end so the field opens "ready to extend"
	// rather than overwriting the first char on the next keystroke.
	m.taskTitleInput.SetCursor(len(task.Title))
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.taskDescriptionInput.SetValue(task.Description)
	// Calibrate the persistent textarea geometry BEFORE CursorEnd so
	// the end-of-content scroll is computed against the wrap width
	// the user will actually see. Without this the persistent
	// viewport keeps the bubbles default (40 cols / 6 rows) and any
	// downstream Update(msg) operates against that stale wrap; the
	// render path's per-call SetWidth on a copy can't repair it
	// because the copy is discarded. See multilineform.Resize for
	// the full explanation.
	m.resizeTaskDescriptionInput()
	m.taskDescriptionInput.CursorEnd()
	m.taskPriority = task.Priority
	m.taskField = taskFieldTitle
	m.applyTaskFieldFocus()
	m.status = m.t("tui.status.editing_task")
	m.moveMode = false
}

// resizeTaskDescriptionInput keeps the persistent task description
// textarea sized to match what renderTaskDescriptionField will pass
// to multilineform.Render. Without this, the textarea retains the
// bubbles package-default geometry and the first keystroke after
// focus desyncs the viewport — the field appears to vanish. Called
// from every entry point that mutates m.taskDescriptionInput so the
// invariant holds across create / edit transitions.
func (m *Model) resizeTaskDescriptionInput() {
	multilineform.Resize(
		&m.taskDescriptionInput,
		m.taskFormWidth(),
		taskDescriptionInputHeight,
		m.styles.multilineFormTheme(),
	)
}

func (m *Model) closeTaskScreen(status string) {
	m.blockerPickerOpen = false
	m.blockerPickerTaskID = 0
	m.blockerPicker.Cursor = 0
	m.blockerPickerChecks = nil
	m.taskScreen = taskScreenClosed
	m.taskID = 0
	m.taskCreateParentID = nil
	m.taskTitleInput = newTaskTitleInput()
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.taskPriority = domain.PriorityZero
	m.taskField = taskFieldTitle
	m.status = status
	m.moveMode = false
	m.taskView = detailscreen.New(0)
	m.activity = nil
	m.activityForTask = 0
	m.activityScroll = 0
	m.activityCursor = -1
	m.subtaskCursor = -1
	m.subtaskScroll = 0
	m.taskViewStack = nil
	m.taskFocus = taskFocusForm
	m.commentScreenOpen = false
	m.commentScreenID = 0
	m.commentScreen = detailscreen.New(0)
	m.descriptionScreenOpen = false
	m.descriptionScreen = detailscreen.New(0)
}

func (m *Model) toggleTaskField() {
	switch m.taskField {
	case taskFieldTitle:
		m.taskField = taskFieldDescription
	case taskFieldDescription:
		m.taskField = taskFieldPriority
	default:
		m.taskField = taskFieldTitle
	}
	m.applyTaskFieldFocus()
}

// applyTaskFieldFocus mirrors m.taskField onto the bubbles inputs so the
// caret only blinks in the focused field. Without this, both inputs would
// render carets simultaneously which is visually ambiguous.
func (m *Model) applyTaskFieldFocus() {
	if m.taskField == taskFieldTitle {
		m.taskTitleInput.Focus()
	} else {
		m.taskTitleInput.Blur()
	}
	if m.taskField == taskFieldDescription {
		m.taskDescriptionInput.Focus()
	} else {
		m.taskDescriptionInput.Blur()
	}
}

// cycleTaskPriority advances the form's priority cursor through the
// configured table by `delta` steps (+1 for ←/h, -1 for →/l per the
// reverse-order convention used by the priority field). Clamped at
// both ends. The cycle order is the slice order of m.priorities, which
// renderers (board badges) and the SQL sort also follow — so cycling
// "right" always feels like raising urgency.
func (m *Model) cycleTaskPriority(delta int) {
	if len(m.priorities) == 0 {
		return
	}
	idx := 0
	for i, p := range m.priorities {
		if domain.Priority(p.ID) == m.taskPriority {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.priorities) {
		idx = len(m.priorities) - 1
	}
	m.taskPriority = domain.Priority(m.priorities[idx].ID)
}

func (m *Model) openBlockerPicker() {
	if m.taskID <= 0 {
		return
	}
	m.blockerPickerOpen = true
	m.blockerPickerTaskID = m.taskID
	m.blockerPicker = picker.New(picker.Multi)
	m.blockerPickerChecks = map[int64]bool{}
	for _, dep := range m.dependencies {
		if dep.TaskID == m.taskID {
			m.blockerPickerChecks[dep.DependsOnTaskID] = true
		}
	}
}

func (m *Model) closeBlockerPicker(status string) {
	m.blockerPickerOpen = false
	m.blockerPickerTaskID = 0
	m.blockerPicker = picker.Model{}
	m.blockerPickerChecks = nil
	m.status = status
}

func (m *Model) saveBlockerPicker() {
	if m.blockerPickerTaskID <= 0 {
		m.closeBlockerPicker("")
		return
	}
	desired := make([]int64, 0, len(m.blockerPickerChecks))
	for taskID, checked := range m.blockerPickerChecks {
		if checked {
			desired = append(desired, taskID)
		}
	}
	if err := app.NewDependencyService(m.repos.Dependencies).SyncBlockers(m.ctx, m.project, m.blockerPickerTaskID, desired); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.closeBlockerPicker(m.t("tui.status.blockers_saved"))
}

// armOrConfirmTaskDelete is the arm-then-confirm gate for hard task deletion.
// First press records the task ID and surfaces a status hint; a second `d` on
// the same task fires TaskService.Delete, which enforces the bucket
// permissions.task.delete policy and operations.delete.guards before
// hard-deleting with cascade. The service emits the system task.removed event
// and writes one ActivityLog row (visible in Stats › Logs) on every call.
func (m *Model) armOrConfirmTaskDelete(task domain.Task) {
	if task.ID <= 0 {
		return
	}
	// Confirm path runs the service (which re-checks policy) — this gate
	// only covers the arm path so the hint surfaces on the first `d` when
	// the bucket forbids delete, instead of letting the user "Confirm
	// delete..." just to be rejected on the second press.
	if m.taskDeletePendingID == task.ID {
		m.executeTaskDelete(task.ID)
		return
	}
	if allowed, hint := m.canDeleteTask(task.ID); !allowed {
		m.status = hint
		// Mirror the service path: the bucket-permission denial that
		// TaskService.Delete emits at line task_service.go:237 must fire
		// here too so hooks (notification.show, exec, …) see every guard hit
		// regardless of which entry point caught it.
		m.repos.Workflow.Evaluator().EmitViolated(m.ctx, m.project.ID, domain.EventEntityTask, task.ID,
			app.GuardOperationTaskDelete, app.GuardRulePermissions, hint,
			map[string]any{"task_id": task.ID, "entity": app.EntityTask, "operation": app.PermissionDelete})
		return
	}
	m.taskDeletePendingID = task.ID
	m.commentDeletePendingID = 0
	m.status = fmt.Sprintf(m.t("tui.confirm.task_delete_fmt"), task.ID, task.Title)
}

// executeTaskDelete runs the TaskService.Delete call and reconciles UI state
// after the cascade. On success the task screen closes (the row is gone) and
// a refresh repopulates board/table; on guard violation the policy hint
// surfaces in the status badge while pending state is cleared so the user
// can retry intentionally rather than re-confirming a stale arm.
func (m *Model) executeTaskDelete(taskID int64) {
	m.taskDeletePendingID = 0
		if _, err := app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot()).Delete(m.ctx, m.project, taskID); err != nil {
		m.status = err.Error()
		return
	}
	if m.taskScreen == taskScreenView && m.taskID == taskID {
		m.closeTaskScreen("")
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.task_deleted_fmt"), taskID)
}

func (m *Model) saveTaskForm() {
	title := strings.TrimSpace(m.taskTitleInput.Value())
	if title == "" {
		m.status = m.t("tui.status.task_title_required")
		return
	}
	description := strings.TrimSpace(m.taskDescriptionInput.Value())

	var task domain.Task
	var err error
	switch m.taskScreen {
	case taskScreenCreate:
		// Add takes the priority as a label string so CLI/MCP/TUI all
		// share one input boundary; the form already holds the resolved
		// id, so we map it back through priorityLabel to keep the
		// service signature uniform across surfaces.
		label := m.priorityLabel(m.taskPriority)
		taskService := app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot())
		if m.taskCreateParentID != nil {
			task, err = taskService.AddSub(m.ctx, m.project, *m.taskCreateParentID, title, description, label, "")
		} else {
			task, err = taskService.Add(m.ctx, m.project, title, description, label, "")
		}
	case taskScreenEdit:
		current, ok := m.activeTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		update := domain.TaskUpdate{Title: &title, Description: &description}
		if m.taskPriority != domain.PriorityZero {
			p := m.taskPriority
			update.Priority = &p
		}
		task, err = app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot()).Edit(m.ctx, m.project, current.ID, update)
	default:
		return
	}
	if err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if m.selectTaskByID(task.ID) {
		m.openTaskView(task)
	}
	m.status = m.t("tui.status.saved")
}

func (m Model) renderTaskScreen() string {
	if m.blockerPickerOpen {
		return m.renderBlockerPicker()
	}
	switch m.taskScreen {
	case taskScreenCreate:
		title := m.t("tui.kicker.new_task")
		if m.taskCreateParentID != nil {
			if parent, ok := m.taskByID(*m.taskCreateParentID); ok {
				title = fmt.Sprintf(m.t("tui.kicker.new_subtask_fmt"), parent.ID, parent.Title)
			}
		}
		return m.renderTaskForm(title)
	case taskScreenEdit:
		// Mirrors the comment-edit kicker pattern (`Edit comment · #N`)
		// so both write surfaces read as the same shape.
		return m.renderTaskForm(fmt.Sprintf(m.t("tui.kicker.edit_task_fmt"), m.taskID))
	case taskScreenView:
		return m.renderTaskView()
	default:
		return ""
	}
}

// taskDescriptionInlineCap is the maximum number of rendered lines the
// task description occupies inside the form column before it gets
// elided with a "+N more · f to focus" hint. Long descriptions read
// in the dedicated overlay (openDescriptionScreen) instead of pushing
// activity + sub-tasks below the fold.
const taskDescriptionInlineCap = 12

// subtasksPanelMinInner is the minimum inner width the sub-tasks
// column needs to render a readable task card. Mirrors the board's
// minColumnInner so card geometry stays identical across surfaces.
// Used only by the stacked fallback — side-by-side gives the
// sub-tasks pane the same outer width as the form column, which is
// always wider than this floor by construction (minLeftCol = 60).
const subtasksPanelMinInner = 28

func (m Model) renderTaskView() string {
	task, ok := m.activeTask()
	if !ok {
		return m.renderPanel(m.t("tui.empty.task_not_found_refresh"))
	}
	available := m.availableWidth()
	// Sub-tasks pane is always rendered so its empty state ("no
	// sub-tasks. press n to add one.") is visible even on leaf tasks;
	// hiding it confused users into thinking the column had collapsed
	// or the screen was missing the new pane entirely.
	layout := m.computeTaskViewLayout(available, true)
	details := m.renderTaskDetailsBox(task, layout)
	children := m.directChildren(task.ID)

	// Sub-tasks column: board-style cards via the shared
	// columnFrame + taskCard helpers. Always rendered so the empty
	// state is visible on leaf tasks — the pane communicates "no
	// sub-tasks yet" instead of going missing.
	subtasksBox := m.renderSubtasksPanel(children, layout)

	// Activity column: existing pane, wrapped in a fixed box. In
	// side-by-side mode the activity box is stretched to match the
	// combined height of the form + sub-tasks stack so the right
	// rail reads as a single tall column "ocupando todo o espaço"
	// rather than a content-sized box floating at the top.
	commentsCellText := m.renderTaskCommentsCell(task.ID)
	commentsLines := gridtable.WrapLines(strings.Split(commentsCellText, "\n"), layout.activityWidth)
	if layout.kind == taskViewSideBySide {
		leftHeight := lipgloss.Height(details) + lipgloss.Height(subtasksBox)
		// Subtract 2 for the activity box's own top + bottom borders
		// so the FINAL box (borders + content) matches the left stack.
		desired := leftHeight - 2
		for len(commentsLines) < desired {
			commentsLines = append(commentsLines, "")
		}
	}
	commentsBox := renderFixedBox(commentsLines, layout.activityWidth, m.styles.border)

	rendered := joinTaskViewSections(layout, details, subtasksBox, commentsBox)
	return m.applyTaskViewScroll(rendered)
}

// renderTaskDetailsBox builds the form column's bordered detailscreen:
// kicker (with optional drill breadcrumb), title / bucket / priority /
// comments / tags rows, blockers list, and the capped inline
// description. Extracted from renderTaskView so the same box can be
// measured ahead of time when computing scroll offsets for a focus
// change in the stacked layout.
func (m Model) renderTaskDetailsBox(task domain.Task, layout taskViewLayout) string {
	blockers := m.blockersForTask(task.ID)
	taskTags := m.tagsForTask(task.ID)
	tagLine := ""
	if len(taskTags) > 0 {
		tagNames := make([]string, len(taskTags))
		for i, t := range taskTags {
			tagNames[i] = t.Label
		}
		tagLine = strings.Join(tagNames, " · ")
	}
	taskKickerLabel := fmt.Sprintf(m.t("tui.kicker.task_fmt"), task.ID)
	if trail := m.taskBreadcrumbTrail(); trail != "" {
		taskKickerLabel = taskKickerLabel + "  " + trail
	}
	taskKicker := m.styles.kicker(taskKickerLabel)
	if m.taskFocus == taskFocusForm {
		taskKicker = m.styles.kickerFocused(taskKickerLabel)
	}
	detail := m.taskView.Reset(layout.formValueWidth).
		Custom(taskKicker).
		Row(m.t("tui.row.title"), task.Title).
		Row(m.t("tui.row.bucket"), task.BucketKey).
		Row(m.t("tui.row.priority"), m.priorityLabel(task.Priority)).
		Row(m.t("tui.row.comments"), fmt.Sprintf("%d", m.commentCount(task.ID))).
		Row(m.t("tui.row.tags"), tagLine).
		KickerCount(m.t("tui.row.blockers"), len(blockers))
	if len(blockers) == 0 {
		detail = detail.Span(m.styles.hint.Render(m.t("tui.empty.blockers")))
	} else {
		for _, blocker := range blockers {
			detail = detail.Span(m.renderTaskReference(blocker))
		}
	}
	detail = detail.Kicker(m.t("tui.kicker.description"))
	detail = detail.Span(m.renderTaskDescriptionInline(task.Description, layout.formValueWidth))
	return detail.View(0, m.styles.border, m.styles.hint)
}

// taskFocusedSectionOffset computes the line index inside the joined
// renderTaskView output where the currently-focused section starts.
// In side-by-side layout every section reads at the top of the
// rendered string, so the offset is always 0 — the join is a single
// horizontal row. In stacked layout the form, sub-tasks, and
// activity boxes are separated by a "\n\n" run; the offset for each
// is the cumulative height of the preceding boxes plus a blank
// separator. Used by applyTaskFocus to land a freshly-focused zone
// at the top of the viewport on tab.
func (m Model) taskFocusedSectionOffset() int {
	if m.taskFocus == taskFocusForm {
		return 0
	}
	task, ok := m.activeTask()
	if !ok {
		return 0
	}
	layout := m.computeTaskViewLayout(m.availableWidth(), true)
	if layout.kind != taskViewStacked {
		return 0
	}
	details := m.renderTaskDetailsBox(task, layout)
	formHeight := lipgloss.Height(details)
	if m.taskFocus == taskFocusSubtasks {
		// "\n\n" between sections yields one extra blank line between
		// the form box's last row and the sub-tasks box's first row.
		return formHeight + 1
	}
	subtasks := m.renderSubtasksPanel(m.directChildren(task.ID), layout)
	subHeight := lipgloss.Height(subtasks)
	return formHeight + 1 + subHeight + 1
}

// taskViewLayout records the per-section widths and packing decision
// the renderer made for the current terminal geometry. computeTaskViewLayout
// owns the policy; renderTaskView and its helpers consume the
// resolved widths so the math lives in one place.
//
// The layout has exactly two shapes, picked per render:
//
//   - SideBySide: form column and sub-tasks pane stacked vertically
//     in a left column; activity pane occupies a right rail that
//     reads as a single tall column running the full height of the
//     left stack.
//   - Stacked: every section in its own full-width row (task → sub
//     → activity). Used when the left column would shrink below a
//     readable width.
type taskViewLayout struct {
	available int

	// formValueWidth is the value column inside the detailscreen grid
	// for the form pane (label width + spacer + borders subtracted).
	formValueWidth int
	// formBoxWidth is the on-screen width of the form pane including
	// label + value + borders. The sub-tasks pane uses the same width
	// so the left column reads as a vertical stack of equal-width
	// boxes.
	formBoxWidth int

	// subtasksWidth is the outer width of the sub-tasks column.
	subtasksWidth  int
	subtasksInner  int
	subtasksCard   int
	subtasksInner2 int // content inside card

	// activityWidth is the outer width of the activity pane.
	activityWidth int

	// kind dictates the horizontal join shape.
	kind taskViewLayoutKind
}

// taskViewLayoutKind enumerates the two packing shapes the task
// detail screen can take. Picked per render based on whether the
// left column can still afford a readable width after carving the
// activity rail off the right.
type taskViewLayoutKind int

const (
	// taskViewStacked stacks every section vertically. The narrow
	// terminal case — form, sub-tasks, activity each occupy a
	// full-width row.
	taskViewStacked taskViewLayoutKind = iota
	// taskViewSideBySide packs form + sub-tasks stacked in a left
	// column with activity occupying a right rail that runs the
	// full height of that stack.
	taskViewSideBySide
)

// computeTaskViewLayout picks per-section widths and packing kind for
// the current terminal width. Side-by-side: activity rail = 40% of
// available clamped to [44, 96]; remaining width minus the spacer
// belongs to the left column (form on top, sub-tasks below) so the
// two share the same outer width and read as a stack. Falls back to
// fully stacked when the left column would shrink below 60 cols —
// the readable floor for the form's title + bucket + description
// rows.
func (m Model) computeTaskViewLayout(available int, _ bool) taskViewLayout {
	const (
		spacer      = 2
		minLeftCol  = 60 // floor for form / sub-tasks shared outer width
		minActivity = taskCommentsPanelMinWidth
	)
	formLabelChrome := detailscreen.LabelWidth + 1 + 2 // label + spacer + borders

	layout := taskViewLayout{available: available}
	activityWidth := m.activityPanelWidth()

	// On-screen budget accounting: activity pane on-screen width is
	// `activityWidth + 2` because renderFixedBox adds its own 2-col
	// border outside the `width` arg. Subtract those 2 here so the
	// rail + spacer + left column actually fit `available` instead
	// of overflowing by 2 cells on every render.
	leftCol := available - (activityWidth + 2) - spacer
	if leftCol >= minLeftCol {
		layout.kind = taskViewSideBySide
		layout.activityWidth = activityWidth
		layout.formBoxWidth = leftCol
		formValue := leftCol - formLabelChrome
		if formValue < 16 {
			formValue = 16
		}
		layout.formValueWidth = formValue
		layout.subtasksWidth = leftCol
		layout.subtasksInner = leftCol - 2
		layout.subtasksCard = layout.subtasksInner - 2
		layout.subtasksInner2 = layout.subtasksCard - 2
		return layout
	}

	layout.kind = taskViewStacked
	// Stacked: every section spans the same outer width so the three
	// boxes read as a single vertical stack with aligned edges. The
	// form value width is derived by subtracting the label + borders
	// from that shared outer width; sub-tasks + activity get the
	// outer width directly.
	outer := available
	if outer < 36 {
		outer = 36
	}
	// Outer = on-screen width including borders. Each renderer needs
	// the inner content width: form column uses
	// `formBoxWidth = valueW + 16` (label + separator + 2 borders);
	// renderFixedBox + kanbanColumnSized add 2 cols of border outside
	// their `width` arg, so subtasks/activity widths are `outer - 2`.
	formValue := outer - formLabelChrome
	if formValue < 16 {
		formValue = 16
	}
	layout.formValueWidth = formValue
	layout.formBoxWidth = formValue + formLabelChrome
	innerOuter := outer - 2
	layout.activityWidth = innerOuter
	layout.subtasksWidth = outer
	layout.subtasksInner = innerOuter
	if layout.subtasksInner < subtasksPanelMinInner {
		layout.subtasksInner = subtasksPanelMinInner
	}
	layout.subtasksCard = layout.subtasksInner - 2
	layout.subtasksInner2 = layout.subtasksCard - 2
	_ = minActivity
	return layout
}

// joinTaskViewSections packs the three pre-rendered section boxes
// according to the layout decision. SideBySide builds a vertical
// stack on the left (form on top of sub-tasks) then puts activity
// in a right rail; Stacked emits a single column of full-width
// boxes. The two helpers are surfaced separately because lipgloss
// JoinVertical / JoinHorizontal have no concept of "fall back to
// stacking on narrow"; the policy lives here.
func joinTaskViewSections(layout taskViewLayout, form, subtasks, activity string) string {
	switch layout.kind {
	case taskViewSideBySide:
		leftCol := lipgloss.JoinVertical(lipgloss.Left, form, subtasks)
		return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "  ", activity)
	default: // taskViewStacked
		return strings.Join([]string{form, subtasks, activity}, "\n\n")
	}
}

// renderTaskDescriptionInline returns the description block as rendered
// inside the form column: either the empty-state hint, the full markdown
// when it fits within taskDescriptionInlineCap, or the truncated head
// with a `+N more · f to focus` cue pointing at the description overlay.
// Long descriptions live in renderDescriptionScreen instead so the form
// column does not push activity + sub-tasks below the fold.
func (m Model) renderTaskDescriptionInline(description string, width int) string {
	body := strings.TrimSpace(description)
	if body == "" {
		return m.styles.hint.Render(m.t("tui.empty.task_no_description"))
	}
	rendered := m.renderBodyMarkdown(description, width)
	lines := strings.Split(rendered, "\n")
	if len(lines) <= taskDescriptionInlineCap {
		return rendered
	}
	head := strings.Join(lines[:taskDescriptionInlineCap], "\n")
	cue := m.styles.hint.Render(fmt.Sprintf(m.t("tui.task.description_more_fmt"), len(lines)-taskDescriptionInlineCap))
	return head + "\n" + cue
}

// renderSubtasksPanel renders the sub-tasks pane as a board-style
// column: bordered kicker + per-child task card via the shared
// renderColumnFrame + renderTaskCard helpers. Cards mirror the board's
// visual language so a sub-task here reads identically to its row on
// the kanban surface — cursor + selection styling included. Empty
// state renders the same boxed pane with a hint line so leaf tasks
// still show the column instead of dropping it from the layout.
func (m Model) renderSubtasksPanel(children []domain.Task, layout taskViewLayout) string {
	finalKey := m.workflow.FinalBucketKey()
	done := 0
	for _, child := range children {
		if child.BucketKey == finalKey {
			done++
		}
	}

	focused := m.taskFocus == taskFocusSubtasks
	headerLabel := fmt.Sprintf("%s · %d/%d done", strings.ToUpper(m.t("tui.row.sub_tasks")), done, len(children))
	var header string
	if focused {
		header = m.styles.hintAccent.Render("▸ " + headerLabel)
	} else {
		header = m.styles.info.Render("// " + headerLabel)
	}

	cards := make([]string, len(children))
	heights := make([]int, len(children))
	for i, child := range children {
		selected := focused && i == m.subtaskCursor
		cards[i] = m.renderTaskCard(taskCardSpec{
			ID:         child.ID,
			Title:      child.Title,
			Badges:     m.taskBoardBadges(child),
			Selected:   selected,
			Archived:   child.State == domain.TaskStateArchived,
			BoxWidth:   layout.subtasksCard,
			InnerWidth: layout.subtasksInner2,
		})
		heights[i] = strings.Count(cards[i], "\n") + 1
	}

	emptyLine := ""
	if len(children) == 0 {
		emptyLine = m.styles.empty.Width(layout.subtasksInner).Render(m.t("tui.empty.sub_tasks"))
	}

	body := m.renderColumnFrame(columnSpec{
		Header:       header,
		Rule:         m.hRule(layout.subtasksInner),
		EmptyLine:    emptyLine,
		Cards:        cards,
		CardHeights:  heights,
		ScrollOffset: m.subtaskScroll,
		Viewport:     m.subtasksViewportRows(),
	})
	return m.styles.kanbanColumnSized(layout.subtasksInner, 0).Render(body)
}

// taskBreadcrumbTrail formats the drill-down ancestor chain for display
// next to the task kicker. Returns "" for top-level views (no stack).
// Truncates to the last 3 ancestors with a leading ellipsis so deep
// trees do not push the kicker off-screen.
func (m Model) taskBreadcrumbTrail() string {
	if len(m.taskViewStack) == 0 {
		return ""
	}
	max := 3
	stack := m.taskViewStack
	prefix := ""
	if len(stack) > max {
		prefix = "… "
		stack = stack[len(stack)-max:]
	}
	parts := make([]string, 0, len(stack))
	for i := len(stack) - 1; i >= 0; i-- {
		parts = append(parts, fmt.Sprintf("#%d", stack[i]))
	}
	return m.styles.hint.Render(prefix + "← " + strings.Join(parts, " ← "))
}

// subtasksViewportRows returns the line budget the sub-tasks column
// has after the surrounding screen chrome. Mirrors activityViewportLines
// so both right-rail panes scroll against the same vertical budget.
func (m Model) subtasksViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 7 // header(2) + leading blank(1) + footer(2) + panel header(kicker+rule) (2)
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

// (border colors stay neutral on purpose — focus is signalled by the
// kicker style, not the panel border, so the screen stays calm visually.)

// taskViewportHeight returns how many lines of detail-view content can fit
// between the header and footer. Returns 0 when the terminal height is unknown
// or too small to bother scrolling, in which case the caller should render the
// full content (the terminal will scroll natively if needed).
func (m Model) taskViewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 5 // header(2) + leading blank(1) + footer(2)
	if m.status != "" {
		chrome++
	}
	h := m.height - chrome
	if h < 8 {
		return 0
	}
	return h
}

func taskViewPageStep(viewport int) int {
	step := viewport / 2
	if step < 4 {
		return 4
	}
	return step
}

// applyTaskViewScroll slices the rendered detail content to the available
// viewport based on m.taskView.Viewport.Scroll, appending an indicator when content is
// hidden above or below.
func (m Model) applyTaskViewScroll(content string) string {
	viewport := m.taskViewportHeight()
	lines := strings.Split(content, "\n")
	if viewport <= 0 || len(lines) <= viewport {
		return "\n" + indentBlock(content, 2)
	}
	// Overflow: reserve one row for the footer indicator.
	return "\n" + indentBlock(m.taskView.Viewport.View(lines, viewport-1, m.styles.hint), 2)
}

func (m Model) renderTaskReference(task domain.Task) string {
	meta := m.styles.hint.Render(fmt.Sprintf("%s · %s", task.BucketKey, m.priorityLabel(task.Priority)))
	return m.styles.hintAccent.Render(fmt.Sprintf("#%d", task.ID)) + " " + task.Title + "  " + meta
}

// moveSubtaskCursor advances the cursor in the sub-tasks pane by the
// given delta. Wraps from "no selection" (-1) to first or last card
// depending on direction so a single keypress always lands on a real
// row. Mirrors moveActivityCursor's behaviour so the two panes feel
// identical under j/k.
func (m *Model) moveSubtaskCursor(delta int) {
	children := m.directChildren(m.taskID)
	rows := len(children)
	if rows == 0 {
		m.subtaskCursor = -1
		return
	}
	if m.subtaskCursor < 0 {
		if delta > 0 {
			m.subtaskCursor = 0
		} else {
			m.subtaskCursor = rows - 1
		}
		m.syncSubtaskScrollToCursor()
		return
	}
	next := m.subtaskCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= rows {
		next = rows - 1
	}
	m.subtaskCursor = next
	m.syncSubtaskScrollToCursor()
}

// syncSubtaskScrollToCursor advances subtaskScroll so the focused
// sub-task card stays inside the viewport budget. Cheap O(n) scan;
// the typical task has a handful of direct children.
func (m *Model) syncSubtaskScrollToCursor() {
	if m.subtaskCursor < 0 {
		return
	}
	children := m.directChildren(m.taskID)
	if m.subtaskCursor >= len(children) {
		return
	}
	viewport := m.subtasksViewportRows()
	if viewport <= 0 {
		m.subtaskScroll = 0
		return
	}
	// Each card occupies ~4 rows on screen (border + content + badges).
	// The estimate keeps the cursor visible without the cost of measuring
	// every rendered card; off-by-one drift is corrected by clampScroll
	// on the next render.
	const cardRowsEstimate = 4
	cursorTop := m.subtaskCursor * cardRowsEstimate
	cursorBottom := cursorTop + cardRowsEstimate
	if cursorTop < m.subtaskScroll {
		m.subtaskScroll = cursorTop
	}
	if cursorBottom > m.subtaskScroll+viewport {
		m.subtaskScroll = cursorBottom - viewport
	}
	if m.subtaskScroll < 0 {
		m.subtaskScroll = 0
	}
}

// activeSubtask returns the sub-task currently under the cursor in
// the sub-tasks pane. Falls back to false when the pane has no
// selection or the index drifted past the end (e.g. after refresh
// dropped a child); callers render no-op for that case.
func (m Model) activeSubtask() (domain.Task, bool) {
	if m.subtaskCursor < 0 {
		return domain.Task{}, false
	}
	children := m.directChildren(m.taskID)
	if m.subtaskCursor >= len(children) {
		return domain.Task{}, false
	}
	return children[m.subtaskCursor], true
}

// drillIntoSubtask opens the sub-task currently under the cursor as
// the new task screen, pushing the current task ID onto the breadcrumb
// stack so esc pops back to the parent (preserving its scroll + focus).
// No-op when the cursor is empty or the child is no longer present in
// the loaded snapshot. openTaskView resets the stack as part of its
// "fresh open" policy, so the push is deferred until after the open
// runs and the slice is rebuilt from the captured parentID.
func (m *Model) drillIntoSubtask() {
	child, ok := m.activeSubtask()
	if !ok {
		return
	}
	stack := append([]int64(nil), m.taskViewStack...)
	stack = append(stack, m.taskID)
	m.openTaskView(child)
	m.taskViewStack = stack
}

// popTaskViewStack returns to the most recent ancestor task pushed
// onto the breadcrumb stack. Returns true when a pop happened so the
// caller can suppress the default "close to board" path. Restoring
// the ancestor goes through openTaskView so its activity feed reloads
// against the current snapshot — child mutations may have rippled
// into the parent's counts. openTaskView's stack-reset policy is
// neutralised by stashing the trimmed slice and restoring it after.
func (m *Model) popTaskViewStack() bool {
	if len(m.taskViewStack) == 0 {
		return false
	}
	trimmed := m.taskViewStack[:len(m.taskViewStack)-1]
	preserved := append([]int64(nil), trimmed...)
	last := m.taskViewStack[len(m.taskViewStack)-1]
	parent, ok := m.taskByID(last)
	if !ok {
		// Ancestor disappeared (deleted while we were drilled in);
		// abandon the stack and fall through to default close.
		m.taskViewStack = nil
		return false
	}
	m.openTaskView(parent)
	m.taskViewStack = preserved
	return true
}

func (m Model) renderBlockerPicker() string {
	task, ok := m.taskByID(m.blockerPickerTaskID)
	if !ok {
		return m.renderPanel(m.t("tui.status.task_not_found"))
	}

	header := []string{
		m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.blockers_fmt"), task.ID)),
		m.styles.hint.Render(m.t("tui.picker.hint.blockers")),
		"",
		m.styles.metaRow(m.t("tui.row.task"), task.Title, metaRowLabelWidth),
		"",
	}
	candidates := m.blockerPickerCandidates()
	if len(candidates) == 0 {
		empty := []string{m.styles.hint.Render(m.t("tui.empty.blocker_picker"))}
		return m.renderPickerPanel(header, empty, 0, 0)
	}

	dataRows := make([]string, 0, len(candidates))
	for index, candidate := range candidates {
		marker := m.cursorMarker(m.blockerPicker.Cursor == index)
		check := m.styles.hint.Render("[ ]")
		if m.blockerPickerChecks[candidate.ID] {
			check = m.styles.hintAccent.Render("[x]")
		}
		meta := m.styles.hint.Render(fmt.Sprintf("%s · %s", candidate.BucketKey, m.priorityLabel(candidate.Priority)))
		dataRows = append(dataRows, fmt.Sprintf("%s %s #%d %s  %s", marker, check, candidate.ID, candidate.Title, meta))
	}
	return m.renderPickerPanel(header, dataRows, m.blockerPicker.Scroll, m.blockerPickerViewportRows())
}

func (m Model) renderTaskForm(title string) string {
	width := m.taskFormWidth()
	titleField := m.renderTaskTitleField(width)
	descriptionField := m.renderTaskDescriptionField(width)
	lines := []string{
		m.styles.kicker(title),
		m.formHint(m.t("tui.form.hint.ctrl_s_saves"), m.t("tui.form.hint.tab_switches_field"), m.t("tui.form.hint.priority_cycle"), m.t("tui.form.hint.esc_cancels")),
		"",
		m.renderTaskFormLabel(taskFieldTitle, "Title"),
		titleField,
		"",
		m.renderTaskFormLabel(taskFieldDescription, "Description"),
		descriptionField,
		"",
		m.renderTaskFormLabel(taskFieldPriority, "Priority"),
		m.renderTaskPriorityInput(),
	}
	return m.renderPanel(strings.Join(lines, "\n"))
}

// renderTaskTitleField renders the bubbles textinput inside the same boxed
// border as the rest of the form. Border color tracks focus: only the
// active field gets the accent color, every other field stays neutral so
// the user is never confused about where the next keystroke lands.
func (m Model) renderTaskTitleField(width int) string {
	input := m.taskTitleInput
	input.Cursor.Style = m.styles.cursor
	innerWidth := width - 4
	if innerWidth < 8 {
		innerWidth = 8
	}
	input.Width = innerWidth
	style := m.styles.input.Width(width)
	if m.taskField == taskFieldTitle {
		style = style.BorderForeground(m.styles.hintAccent.GetForeground())
	}
	return style.Render(input.View())
}

// renderTaskDescriptionField renders the description textarea via the
// shared multilineform leaf so its border, padding, and cursor accent
// stay aligned with the comment forms. The persistent textarea is
// calibrated at form-open time (openTaskCreate / openTaskEdit) so the
// internal viewport stays in sync with the on-screen geometry across
// keystrokes — see multilineform.Resize for the bug this guards.
func (m Model) renderTaskDescriptionField(width int) string {
	return multilineform.Render(
		m.taskDescriptionInput,
		width,
		taskDescriptionInputHeight,
		m.taskField == taskFieldDescription,
		m.styles.multilineFormTheme(),
	)
}

func (m Model) renderTaskPriorityInput() string {
	// Pull the cycle from the active priorities table so the user sees
	// exactly the labels they declared in config.priorities — including
	// any custom entries (e.g. "urgent" past "high"). Order follows the
	// slice order so the visual reads low→high left-to-right.
	var parts []string
	for _, lvl := range m.priorities {
		label := lvl.Value
		if domain.Priority(lvl.ID) == m.taskPriority {
			parts = append(parts, m.styles.hintAccent.Render("["+label+"]"))
		} else {
			parts = append(parts, m.styles.hint.Render(label))
		}
	}
	style := m.styles.input.Width(m.taskFormWidth())
	if m.taskField == taskFieldPriority {
		style = style.BorderForeground(m.styles.hintAccent.GetForeground())
	}
	return style.Render(strings.Join(parts, "  "))
}

func (m Model) renderTaskFormLabel(field taskFormField, label string) string {
	marker := " "
	if m.taskField == field {
		marker = ">"
	}
	return m.styles.hintAccent.Render(marker + " // " + strings.ToUpper(label))
}
