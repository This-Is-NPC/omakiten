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
			m.closeTaskScreen("")
		case "e":
			if task, ok := m.activeTask(); ok {
				m.openTaskEdit(task)
			}
		case "b":
			if _, ok := m.activeTask(); ok {
				m.openBlockerPicker()
			}
		case "c":
			if _, ok := m.activeTask(); ok {
				m.beginInput(modeComment, "Comment body", "")
			}
		case "m":
			if _, ok := m.activeTask(); ok {
				m.beginInput(modeMove, "Target bucket key", "")
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
				m.status = "Refreshed"
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
			if m.taskFocus == taskFocusActivity {
				m.openCommentScreen()
			}
		default:
			// Activity-column nav has split semantics (line scroll for the
			// body, card cursor for J/K) so the activity branch stays
			// inline. The form-column branch is a plain viewport — delegate
			// to the embedded sub-model.
			if m.taskFocus == taskFocusActivity {
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
			} else {
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
			m.closeTaskScreen("Cancelled")
		} else if task, ok := m.activeTask(); ok {
			m.openTaskView(task)
		} else {
			m.closeTaskScreen("Cancelled")
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
		m.closeBlockerPicker("Cancelled")
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
	m.taskTitleInput = newTaskTitleInput()
	m.taskDescriptionInput = newTaskDescriptionInput()
	m.resizeTaskDescriptionInput()
	// Default the form to the priority flagged `default: true` in
	// config.priorities, falling back to the middle entry when none is
	// flagged. priorityZero falls through to the storage column DEFAULT.
	m.taskPriority = m.defaultPriorityID()
	m.taskField = taskFieldTitle
	m.applyTaskFieldFocus()
	m.status = "New task"
	m.moveMode = false
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
	m.taskFocus = taskFocusForm
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
	m.status = "Editing task"
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
	m.taskFocus = taskFocusForm
	m.commentScreenOpen = false
	m.commentScreenID = 0
	m.commentScreen = detailscreen.New(0)
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
	m.closeBlockerPicker("Blockers saved")
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
		m.repos.Workflow.EmitGuardViolated(m.ctx, m.project.ID, domain.EventEntityTask, task.ID,
			app.GuardOperationTaskDelete, app.GuardRulePermissions, hint,
			map[string]any{"task_id": task.ID, "entity": app.EntityTask, "operation": app.PermissionDelete})
		return
	}
	m.taskDeletePendingID = task.ID
	m.commentDeletePendingID = 0
	m.status = fmt.Sprintf("Confirm delete task #%d %q. Press d again; esc cancels.", task.ID, task.Title)
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
	m.status = fmt.Sprintf("Deleted task #%d", taskID)
}

func (m *Model) saveTaskForm() {
	title := strings.TrimSpace(m.taskTitleInput.Value())
	if title == "" {
		m.status = "Task title is required"
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
		task, err = app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, m.repos.activeSnapshot()).Add(m.ctx, m.project, title, description, label, "")
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
	m.status = "Saved"
}

func (m Model) renderTaskScreen() string {
	if m.blockerPickerOpen {
		return m.renderBlockerPicker()
	}
	switch m.taskScreen {
	case taskScreenCreate:
		return m.renderTaskForm("New task")
	case taskScreenEdit:
		// Mirrors the comment-edit kicker pattern (`Edit comment · #N`)
		// so both write surfaces read as the same shape.
		return m.renderTaskForm(fmt.Sprintf("Edit task · #%d", m.taskID))
	case taskScreenView:
		return m.renderTaskView()
	default:
		return ""
	}
}

func (m Model) renderTaskView() string {
	task, ok := m.activeTask()
	if !ok {
		return m.renderPanel("Task not found. Refresh with r or return to the board.")
	}
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

	taskKicker := m.styles.kicker(fmt.Sprintf("Task · #%d", task.ID))
	if m.taskFocus == taskFocusForm {
		taskKicker = m.styles.kickerFocused(fmt.Sprintf("Task · #%d", task.ID))
	}

	commentsCellText := m.renderTaskCommentsCell(task.ID)

	available := m.availableWidth()
	// Side-by-side layout needs: details(label+1+value+2 borders) + 2 spacer + comments(inner+2 borders).
	// Below this threshold, stack vertically and let each block use the full width.
	const minWideValueWidth = 24
	activityWidth := m.activityPanelWidth()
	wideThreshold := detailscreen.LabelWidth + 1 + minWideValueWidth + 2 + 2 + activityWidth + 2

	var valueWidth int
	if available < wideThreshold {
		valueWidth = available - detailscreen.LabelWidth - 1 - 2
		if valueWidth < 16 {
			valueWidth = 16
		}
	} else {
		valueWidth = available - (activityWidth + 2) - 2 - (detailscreen.LabelWidth + 1) - 2
		if valueWidth < minWideValueWidth {
			valueWidth = minWideValueWidth
		}
		if valueWidth > 120 {
			valueWidth = 120
		}
	}

	detail := m.taskView.Reset(valueWidth).
		Custom(taskKicker).
		Row("Title", task.Title).
		Row("Bucket", task.BucketKey).
		Row("Priority", m.priorityLabel(task.Priority)).
		Row("Comments", fmt.Sprintf("%d", m.commentCount(task.ID))).
		Row("Tags", tagLine).
		KickerCount("Blockers", len(blockers))
	if len(blockers) == 0 {
		detail = detail.Span(m.styles.hint.Render("No blockers. Press b to add one."))
	} else {
		for _, blocker := range blockers {
			detail = detail.Span(m.renderTaskReference(blocker))
		}
	}
	detail = detail.Kicker("Description")
	if strings.TrimSpace(task.Description) == "" {
		detail = detail.Span(m.styles.hint.Render("No description"))
	} else {
		detail = detail.Span(m.renderBodyMarkdown(task.Description, valueWidth))
	}
	details := detail.View(0, m.styles.border, m.styles.hint)

	var rendered string
	if available < wideThreshold {
		commentsWidth := available - 2
		if commentsWidth < 36 {
			commentsWidth = 36
		}
		commentsBox := renderFixedBox(gridtable.WrapLines(strings.Split(commentsCellText, "\n"), commentsWidth), commentsWidth, m.styles.border)
		rendered = details + "\n\n" + commentsBox
	} else {
		commentsBox := renderFixedBox(gridtable.WrapLines(strings.Split(commentsCellText, "\n"), activityWidth), activityWidth, m.styles.border)
		rendered = lipgloss.JoinHorizontal(lipgloss.Top, details, "  ", commentsBox)
	}

	return m.applyTaskViewScroll(rendered)
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

func (m Model) renderBlockerPicker() string {
	task, ok := m.taskByID(m.blockerPickerTaskID)
	if !ok {
		return m.renderPanel("Task not found. Press esc to return.")
	}

	header := []string{
		m.styles.kicker(fmt.Sprintf("Blockers · #%d", task.ID)),
		m.styles.hint.Render("up/down: move · space: toggle · ctrl+s: save · esc: cancel"),
		"",
		m.styles.metaRow("Task", task.Title, metaRowLabelWidth),
		"",
	}
	candidates := m.blockerPickerCandidates()
	if len(candidates) == 0 {
		empty := []string{m.styles.hint.Render("No other tasks are available to block this task.")}
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
		m.formHint("ctrl+s saves", "tab switches field", "←/→ cycles priority", "esc cancels"),
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
