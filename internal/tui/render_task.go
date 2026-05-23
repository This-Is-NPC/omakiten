package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/config"
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
		case "a", "n":
			// §D.14 binds `a` to "add sub-task"; `n` retained as alias
			// during the rebind cycle so muscle memory from the first
			// landing keeps working — drop it after a release.
			if task, ok := m.activeTask(); ok {
				m.openSubTaskCreate(task)
			}
		case "s":
			// §D.14 — `s` focuses the sub-tasks pane. Idempotent when
			// already focused so re-pressing is harmless. When the task
			// has no children the focus would trap j/k against an empty
			// list, so leave the focus where it was and surface a status
			// hint so the press still acknowledges.
			children := m.directChildren(m.taskID)
			if len(children) == 0 {
				m.status = m.t("tui.status.no_subtasks_to_focus")
			} else {
				m.taskFocus = taskFocusSubtasks
				if m.subtaskCursor < 0 {
					m.subtaskCursor = 0
				}
			}
		case " ":
			// §D.14 — space on the sub-tasks pane shortcuts the focused
			// child into the workflow's final bucket. Goes through the
			// full transition engine so guards still fire; the error
			// surfaces inline like any other failed move.
			if m.taskFocus == taskFocusSubtasks {
				m.sendFocusedSubtaskToDone()
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
		if m.taskScreen == taskScreenEdit && m.taskEditFormDirty() {
			if !m.taskEscPendingDiscard {
				// First esc on a dirty edit arms the discard prompt
				// rather than closing immediately — the user gets one
				// chance to recover the work before it disappears.
				m.taskEscPendingDiscard = true
				m.status = m.t("tui.taskedit.dirty_discard_prompt")
				return *m, nil
			}
		}
		m.taskEscPendingDiscard = false
		if m.taskScreen == taskScreenCreate {
			m.closeTaskScreen(m.t("tui.status.cancelled"))
		} else if task, ok := m.activeTask(); ok {
			m.openTaskView(task)
		} else {
			m.closeTaskScreen(m.t("tui.status.cancelled"))
		}
		return *m, nil
	case "ctrl+s":
		m.taskEscPendingDiscard = false
		m.saveTaskForm()
		return *m, nil
	case "tab":
		m.taskEscPendingDiscard = false
		m.cycleTaskField(1)
		return *m, nil
	case "shift+tab":
		m.taskEscPendingDiscard = false
		m.cycleTaskField(-1)
		return *m, nil
	case "ctrl+b":
		m.taskEscPendingDiscard = false
		if m.taskScreen == taskScreenEdit {
			m.openBlockerPicker()
		}
		return *m, nil
	}
	// Any other key dismisses the dirty-discard arm so the next esc
	// behaves as "arm again", not "discard".
	m.taskEscPendingDiscard = false
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
	case taskFieldTags:
		m.taskTagsInput, cmd = m.taskTagsInput.Update(msg)
	case taskFieldParent:
		m.taskParentInput, cmd = m.taskParentInput.Update(msg)
		// Clear any stale lookup error so the next blur recomputes
		// against the current input — typing a fresh id mid-edit
		// shouldn't leave the previous "not found" hint visible.
		m.taskParentLookupError = ""
	}
	return *m, cmd
}

// taskEditFormDirty reports whether the form values have diverged from
// the snapshot captured at openTaskEdit. Used by the esc dirty-check:
// "clean" closes immediately, "dirty" arms a confirm prompt so the
// user doesn't blow away an in-flight edit with a stray esc.
func (m *Model) taskEditFormDirty() bool {
	if !m.taskEditInitial.active {
		return false
	}
	if strings.TrimSpace(m.taskTitleInput.Value()) != strings.TrimSpace(m.taskEditInitial.title) {
		return true
	}
	if strings.TrimSpace(m.taskDescriptionInput.Value()) != strings.TrimSpace(m.taskEditInitial.description) {
		return true
	}
	if m.taskPriority != m.taskEditInitial.priority {
		return true
	}
	if normalizeTagsCSV(m.taskTagsInput.Value()) != normalizeTagsCSV(m.taskEditInitial.tagsCSV) {
		return true
	}
	if strings.TrimSpace(m.taskParentInput.Value()) != strings.TrimSpace(m.taskEditInitial.parent) {
		return true
	}
	return false
}

// normalizeTagsCSV folds a CSV tag list into a canonical sorted shape
// so reorder-only edits (e.g. "a, b" → "b, a") don't trip the dirty
// check. Empty fields are dropped; whitespace around each token is
// trimmed.
func normalizeTagsCSV(csv string) string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
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
	m.taskTagsInput = newTaskTagsInput()
	m.taskParentInput = newTaskParentInput()
	m.taskParentLookupError = ""
	m.taskEditInitial = taskEditSnapshot{}
	m.taskEscPendingDiscard = false
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

	// §E Tags + Parent sections. Tags is the CSV projection of the
	// current attached tag set; Parent is the FK id as decimal (empty =
	// root). Both are captured into taskEditInitial so esc can detect
	// "dirty" without re-querying the DB.
	tagsCSV := m.loadTaskTagsCSV(task.ID)
	m.taskTagsInput = newTaskTagsInput()
	m.taskTagsInput.SetValue(tagsCSV)
	m.taskTagsInput.SetCursor(len(tagsCSV))

	parentValue := ""
	if task.ParentID != nil {
		parentValue = strconv.FormatInt(*task.ParentID, 10)
	}
	m.taskParentInput = newTaskParentInput()
	m.taskParentInput.SetValue(parentValue)
	m.taskParentInput.SetCursor(len(parentValue))
	m.taskParentLookupError = ""

	m.taskEditInitial = taskEditSnapshot{
		active:      true,
		title:       task.Title,
		description: task.Description,
		priority:    task.Priority,
		tagsCSV:     tagsCSV,
		parent:      parentValue,
	}
	m.taskEscPendingDiscard = false

	m.taskField = taskFieldTitle
	m.applyTaskFieldFocus()
	m.status = m.t("tui.status.editing_task")
	m.moveMode = false
}

// loadTaskTagsCSV reads the active tag set for taskID and returns the
// canonical CSV projection used by the §E Tags section. Sorted by name
// so the dirty-check stays order-insensitive. Lookup failures fall
// back to an empty string — Tags is a soft field, the form should
// still open.
func (m Model) loadTaskTagsCSV(taskID int64) string {
	if m.repos.Tags == nil {
		return ""
	}
	tags, err := m.repos.Tags.ListTaskTags(m.ctx, m.project.ID, taskID)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
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
	m.taskTagsInput = newTaskTagsInput()
	m.taskParentInput = newTaskParentInput()
	m.taskParentLookupError = ""
	m.taskEditInitial = taskEditSnapshot{}
	m.taskEscPendingDiscard = false
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

// taskFieldOrder is the §E section rotation: Title → Description →
// Priority → Tags → Parent. cycleTaskField (Tab / Shift+Tab) walks this
// slice; declaring it once keeps the forward and reverse paths in sync.
var taskFieldOrder = []taskFormField{
	taskFieldTitle,
	taskFieldDescription,
	taskFieldPriority,
	taskFieldTags,
	taskFieldParent,
}

// cycleTaskField rotates the active section by delta steps through
// taskFieldOrder, wrapping at both ends. delta=+1 for Tab, delta=-1
// for Shift+Tab. The blur side-effect on Parent runs through
// applyTaskFieldFocus so the lookup hint fires regardless of which
// direction the user leaves the field.
func (m *Model) cycleTaskField(delta int) {
	idx := 0
	for i, f := range taskFieldOrder {
		if f == m.taskField {
			idx = i
			break
		}
	}
	previous := m.taskField
	idx = (idx + delta + len(taskFieldOrder)) % len(taskFieldOrder)
	m.taskField = taskFieldOrder[idx]
	// Blur-time parent validation: when the cursor leaves Parent, run
	// the lookup so the next render surfaces the hint. The save-time
	// anti-cycle check still runs separately — this only catches the
	// "exists + same project" precondition the user can act on
	// immediately.
	if previous == taskFieldParent && m.taskField != taskFieldParent {
		m.validateParentInputOnBlur()
	}
	m.applyTaskFieldFocus()
}

// applyTaskFieldFocus mirrors m.taskField onto the bubbles inputs so the
// caret only blinks in the focused field. Without this, multiple inputs
// would render carets simultaneously which is visually ambiguous.
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
	if m.taskField == taskFieldTags {
		m.taskTagsInput.Focus()
	} else {
		m.taskTagsInput.Blur()
	}
	if m.taskField == taskFieldParent {
		m.taskParentInput.Focus()
	} else {
		m.taskParentInput.Blur()
	}
}

// validateParentInputOnBlur runs the §E §7 parent-id lookup: the value
// must parse as an int64, name a task in the active project, and not
// refer to the task being edited. Anti-cycle is intentionally deferred
// to save time so the user can keep typing without the form rejecting
// transient ids. Sets taskParentLookupError when invalid; clears it
// when valid or empty.
func (m *Model) validateParentInputOnBlur() {
	value := strings.TrimSpace(m.taskParentInput.Value())
	if value == "" {
		m.taskParentLookupError = ""
		return
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		m.taskParentLookupError = m.t("tui.taskedit.parent_lookup_invalid")
		return
	}
	if m.taskID > 0 && id == m.taskID {
		m.taskParentLookupError = m.t("tui.taskedit.parent_lookup_self")
		return
	}
	tasks, err := m.repos.Tasks.ListTasks(m.ctx, m.project.ID, domain.TaskFilter{IncludeArchived: true}, m.repos.activeSnapshot())
	if err != nil {
		// Don't punish the user for an IO blip — the save-time check
		// will catch any real problem.
		m.taskParentLookupError = ""
		return
	}
	for _, t := range tasks {
		if t.ID == id {
			m.taskParentLookupError = ""
			return
		}
	}
	m.taskParentLookupError = m.t("tui.taskedit.parent_lookup_not_found")
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
	// Sub-tasks ride the FK ON DELETE CASCADE, so the prompt needs to
	// announce the full subtree size — otherwise a one-key confirm can
	// silently take out a multi-level branch. CountDescendants walks
	// the recursive CTE; a failure here degrades to the root-only
	// prompt rather than blocking the verb.
	descendants, err := m.repos.Tasks.CountDescendants(m.ctx, m.project.ID, task.ID)
	if err != nil || descendants == 0 {
		m.status = fmt.Sprintf(m.t("tui.confirm.task_delete_fmt"), task.ID, task.Title)
		return
	}
	m.status = fmt.Sprintf(m.t("tui.confirm.delete_subtree_fmt"),
		task.ID, task.Title, descendants, descendants+1)
}

// executeTaskDelete runs the TaskService.Delete call and reconciles UI state
// after the cascade. On success the task screen closes (the row is gone) and
// a refresh repopulates board/table; on guard violation the policy hint
// surfaces in the status badge while pending state is cleared so the user
// can retry intentionally rather than re-confirming a stale arm.
func (m *Model) executeTaskDelete(taskID int64) {
	m.taskDeletePendingID = 0
	if _, err := m.taskService(nil).Delete(m.ctx, m.project, taskID); err != nil {
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

	// §E Parent section: empty = root, otherwise int64. Bad input
	// (non-numeric, negative, self) is rejected here so the user gets
	// the inline hint instead of a service-layer cycle error.
	parentValue := strings.TrimSpace(m.taskParentInput.Value())
	var parentID *int64
	if parentValue != "" {
		id, parseErr := strconv.ParseInt(parentValue, 10, 64)
		if parseErr != nil || id <= 0 {
			m.status = m.t("tui.taskedit.parent_lookup_invalid")
			return
		}
		if m.taskScreen == taskScreenEdit && id == m.taskID {
			m.status = m.t("tui.taskedit.parent_lookup_self")
			return
		}
		parentID = &id
	}

	var task domain.Task
	var err error
	switch m.taskScreen {
	case taskScreenCreate:
		// Add takes the priority as a label string so CLI/MCP/TUI all
		// share one input boundary; the form already holds the resolved
		// id, so we map it back through priorityLabel to keep the
		// service signature uniform across surfaces.
		label := m.priorityLabel(m.taskPriority)
		taskService := m.taskService(nil)
		switch {
		case m.taskCreateParentID != nil:
			task, err = taskService.AddSub(m.ctx, m.project, *m.taskCreateParentID, title, description, label, "")
		case parentID != nil:
			// §E parent textinput on create routes through AddSub so
			// the service still enforces same-project + active +
			// cross-bucket invariants.
			task, err = taskService.AddSub(m.ctx, m.project, *parentID, title, description, label, "")
		default:
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
		// §E Parent re-parent — only ride the ChangeParent flag when
		// the value diverged from the snapshot so a no-op edit doesn't
		// trip the service's archived/cycle checks unnecessarily.
		if m.taskEditInitial.active && parentValue != strings.TrimSpace(m.taskEditInitial.parent) {
			update.ChangeParent = true
			update.NewParentID = parentID
		}
		task, err = m.taskService(nil).Edit(m.ctx, m.project, current.ID, update)
	default:
		return
	}
	if err != nil {
		m.status = err.Error()
		return
	}

	// §E Tags section. Diff the current CSV against the snapshot (edit)
	// or against an empty set (create) and replay the delta through
	// TagService.Add / TagService.Remove. Failures surface in status
	// but the row is already persisted, so the form still closes — the
	// user can re-open and retry the tag edit without losing the
	// title/description/priority/parent work.
	if tagErr := m.syncTaskTagsFromForm(task.ID); tagErr != nil {
		m.status = tagErr.Error()
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

// syncTaskTagsFromForm reconciles the persisted tag set for taskID with
// the §E Tags CSV input. On create the snapshot is empty so every
// parsed tag becomes an Add; on edit the diff against
// taskEditInitial.tagsCSV decides which tags are added or removed.
// Empty CSV entries are dropped; tag names go through the same
// normalization pass TagService.Add applies internally so dupes
// across whitespace / case differences collapse before the delta is
// computed.
func (m *Model) syncTaskTagsFromForm(taskID int64) error {
	if taskID <= 0 || m.repos.Tags == nil {
		return nil
	}
	wanted := parseTagsCSV(m.taskTagsInput.Value())
	initial := parseTagsCSV(m.taskEditInitial.tagsCSV)
	wantedSet := map[string]struct{}{}
	for _, name := range wanted {
		wantedSet[name] = struct{}{}
	}
	initialSet := map[string]struct{}{}
	for _, name := range initial {
		initialSet[name] = struct{}{}
	}

	snap := m.repos.activeSnapshot()
	tagSvc := app.NewTagServiceWithEvents(m.repos.Tags, m.repos.Events, snap)

	// Additions: anything in wanted but not in initial.
	for _, name := range wanted {
		if _, ok := initialSet[name]; ok {
			continue
		}
		if _, err := tagSvc.Add(m.ctx, m.project, app.TagEntityTask, taskID, name); err != nil {
			return err
		}
	}

	// Removals: anything in the live attached set but not in wanted.
	// Re-read so we pick up the ids — initialSet only holds names.
	current, err := m.repos.Tags.ListTaskTags(m.ctx, m.project.ID, taskID)
	if err != nil {
		return err
	}
	for _, tag := range current {
		if _, keep := wantedSet[tag.Name]; keep {
			continue
		}
		if err := tagSvc.Remove(m.ctx, m.project, app.TagEntityTask, taskID, tag.ID); err != nil {
			return err
		}
	}
	return nil
}

// parseTagsCSV splits a Tags CSV into a normalised slice. Trim
// whitespace, drop empties, lowercase via TagService's normalisation
// rule (snap-aware synonym pass happens inside TagService.Add — here
// we only collapse whitespace so the diff stays string-stable).
func parseTagsCSV(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
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

// taskBreadcrumbTrail formats the ancestor chain (parent → grandparent
// → …) for display next to the task kicker. Derived from each task's
// ParentID via taskByID — independent of the drill back-stack so the
// breadcrumb stays consistent regardless of how the user arrived (plan
// view, board, table, or drill from a parent). Truncated to the last
// three ancestors with a leading "…" when the tree is deeper so the
// kicker never pushes off-screen.
//
// The back-stack (taskViewStack) still owns "esc returns to parent"
// — see popTaskViewStack. They are intentionally separate: one
// represents the task tree, the other the navigation history.
func (m Model) taskBreadcrumbTrail() string {
	task, ok := m.activeTask()
	if !ok || task.ParentID == nil {
		return ""
	}
	const maxAncestors = 3
	ancestors := make([]int64, 0, maxAncestors+1)
	cursor := task.ParentID
	guard := 32 // defensive: bail out long before cycle protection in storage matters
	for cursor != nil && guard > 0 {
		ancestors = append(ancestors, *cursor)
		parent, ok := m.taskByID(*cursor)
		if !ok {
			break
		}
		cursor = parent.ParentID
		guard--
	}
	if len(ancestors) == 0 {
		return ""
	}
	prefix := ""
	visible := ancestors
	if len(visible) > maxAncestors {
		prefix = "… "
		visible = visible[:maxAncestors]
	}
	parts := make([]string, 0, len(visible))
	for _, id := range visible {
		parts = append(parts, fmt.Sprintf("#%d", id))
	}
	return m.styles.hint.Render(prefix + "← " + strings.Join(parts, " ← "))
}

// subtasksViewportRows returns the line budget the sub-tasks column
// has after the surrounding screen chrome. In side-by-side layout the
// pane owns its full vertical column, so the original chrome=7
// estimate stays accurate. In stacked layout the pane is one of three
// sections in a joined string (form box + sub-tasks box + activity
// box), so the budget must also subtract the form box height and the
// section separator — otherwise the cursor walks past the outer
// viewport slice without anything scrolling and the user sees no
// movement. Mirrors activityViewportLines for the side-by-side case.
func (m Model) subtasksViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 7 // header(2) + leading blank(1) + footer(2) + panel header(kicker+rule) (2)
	if m.status != "" {
		chrome++
	}

	// In stacked layout the form box sits above the sub-tasks pane in
	// the joined output; carve out its height + the "\n\n" separator
	// so the budget reflects the leftover space.
	if task, ok := m.activeTask(); ok {
		layout := m.computeTaskViewLayout(m.availableWidth(), true)
		if layout.kind == taskViewStacked {
			details := m.renderTaskDetailsBox(task, layout)
			formHeight := lipgloss.Height(details)
			// +1 = blank line between form and sub-tasks sections.
			chrome += formHeight + 1
		}
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
// the typical task has a handful of direct children. In stacked
// layout it ALSO advances m.taskView.Viewport.Scroll so the joined
// detail screen scrolls when the focused card falls below the outer
// slice — without that, j/k on a tall sub-tasks list appeared to do
// nothing because the outer slicer hid the new cursor position.
func (m *Model) syncSubtaskScrollToCursor() {
	if m.subtaskCursor < 0 {
		return
	}
	children := m.directChildren(m.taskID)
	if m.subtaskCursor >= len(children) {
		return
	}
	const cardRowsEstimate = 4

	viewport := m.subtasksViewportRows()
	if viewport > 0 {
		// Each card occupies ~4 rows on screen (border + content + badges).
		// The estimate keeps the cursor visible without the cost of measuring
		// every rendered card; off-by-one drift is corrected by clampScroll
		// on the next render.
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
	} else {
		m.subtaskScroll = 0
	}

	// Stacked layout: the sub-tasks panel is one of three sections
	// joined into a single string sliced by m.taskView.Viewport. The
	// cursor advance is invisible unless we also move the outer
	// viewport. taskFocusedSectionOffset already knows the line index
	// where the sub-tasks section starts inside the joined output.
	layout := m.computeTaskViewLayout(m.availableWidth(), true)
	if layout.kind != taskViewStacked {
		return
	}
	outerViewport := m.taskViewportHeight()
	if outerViewport <= 0 {
		return
	}
	// 3 = panel kicker(1) + rule(1) + leading blank inside the
	// sub-tasks box before the first card row. Matches the chrome
	// renderSubtasksPanel emits above the card list.
	const subtasksHeaderRows = 3
	sectionTop := m.taskFocusedSectionOffset()
	cursorOuterTop := sectionTop + subtasksHeaderRows + m.subtaskCursor*cardRowsEstimate
	cursorOuterBottom := cursorOuterTop + cardRowsEstimate
	if cursorOuterTop < m.taskView.Viewport.Scroll {
		m.taskView.Viewport.Scroll = cursorOuterTop
	}
	if cursorOuterBottom > m.taskView.Viewport.Scroll+outerViewport {
		m.taskView.Viewport.Scroll = cursorOuterBottom - outerViewport
	}
	if m.taskView.Viewport.Scroll < 0 {
		m.taskView.Viewport.Scroll = 0
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

// sendFocusedSubtaskToDone shortcuts the sub-task currently under the
// cursor into the workflow's final bucket via the full transition
// engine. §D.14 — `space` on the sub-tasks pane is the "mark this
// child done" verb so the user doesn't have to open the child to move
// it. Guards still fire (subtasks_complete on grandchildren, blockers,
// etc.); failures surface inline.
func (m *Model) sendFocusedSubtaskToDone() {
	child, ok := m.activeSubtask()
	if !ok {
		return
	}
	snap := m.repos.activeSnapshot()
	if snap == nil {
		return
	}
	finalKey := snap.Workflow().FinalBucketKey()
	if finalKey == "" {
		return
	}
	if child.BucketKey == finalKey {
		// Already in the final bucket — no transition fires, but the
		// keypress still needs feedback so the user knows the press was
		// received and the child is genuinely done.
		m.status = fmt.Sprintf(m.t("tui.status.subtask_already_done_fmt"), child.ID, finalKey)
		return
	}
	svc := m.taskService(snap)
	if _, err := svc.Move(m.ctx, m.project, child.ID, finalKey); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.subtask_sent_done_fmt"), child.ID, finalKey)
}

// taskService returns a TaskService bound to the current Model wiring.
// snap is the snapshot the service reads through — callers may pass
// nil to use the model's active snapshot, but most callers already
// have it on hand from a prior repos.activeSnapshot() call and pass it
// explicitly to avoid the second lookup.
func (m *Model) taskService(snap *config.Snapshot) *app.TaskService {
	if snap == nil {
		snap = m.repos.activeSnapshot()
	}
	return app.NewTaskService(m.repos.Tasks, m.repos.Workflow, m.registry, snap)
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
		"",
		m.renderTaskFormLabel(taskFieldTags, "Tags"),
		m.renderTaskTagsField(width),
		"",
		m.renderTaskFormLabel(taskFieldParent, "Parent"),
		m.renderTaskParentField(width),
	}
	if m.taskParentLookupError != "" {
		lines = append(lines, m.styles.hint.Render("  "+m.taskParentLookupError))
	}
	return m.renderPanel(strings.Join(lines, "\n"))
}

// renderTaskTagsField mirrors renderTaskTitleField but binds to the
// §E Tags single-line CSV input. Border tracks focus so the active
// section reads as the next-keystroke target.
func (m Model) renderTaskTagsField(width int) string {
	input := m.taskTagsInput
	input.Cursor.Style = m.styles.cursor
	innerWidth := width - 4
	if innerWidth < 8 {
		innerWidth = 8
	}
	input.Width = innerWidth
	style := styleWidthFromCache(m.inputStyleByWidth, m.styles.input, width)
	if m.taskField == taskFieldTags {
		style = style.BorderForeground(m.styles.hintAccent.GetForeground())
	}
	return style.Render(input.View())
}

// renderTaskParentField mirrors renderTaskTagsField for the §E Parent
// id input. The lookup-error hint is rendered by the caller (one line
// below the bordered box) so the error sits closest to the field that
// produced it without competing with the next section's label.
func (m Model) renderTaskParentField(width int) string {
	input := m.taskParentInput
	input.Cursor.Style = m.styles.cursor
	innerWidth := width - 4
	if innerWidth < 8 {
		innerWidth = 8
	}
	input.Width = innerWidth
	style := styleWidthFromCache(m.inputStyleByWidth, m.styles.input, width)
	if m.taskField == taskFieldParent {
		style = style.BorderForeground(m.styles.hintAccent.GetForeground())
	}
	return style.Render(input.View())
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
	style := styleWidthFromCache(m.inputStyleByWidth, m.styles.input, width)
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
	style := styleWidthFromCache(m.inputStyleByWidth, m.styles.input, m.taskFormWidth())
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
