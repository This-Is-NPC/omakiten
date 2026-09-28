package tui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/taskvalidation"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/taskdetail"
	"omakiten/internal/tui/screens/taskform"
)

func (m *Model) openTaskCreate() {
	priority := m.defaultPriorityID()
	m.status = m.t("tui.status.new_task")
	m.blurActiveScreen()
	m.taskFormGeneration++
	m.taskFormScreen = taskform.New().Bind(m.taskFormDeps()).Open(taskform.Payload{
		Mode: taskform.Create, Generation: m.taskFormGeneration,
		Kicker: m.t("tui.kicker.new_task"), Values: taskform.Values{Priority: strconv.Itoa(int(priority))},
		Priorities: m.taskFormPriorities()}, m.screenFrame())
	m.pushScreen(screenhost.TaskForm)
}

func (m *Model) openSubTaskCreate(parent domain.Task) {
	m.openTaskCreate()
	parentID := parent.ID
	payload := m.taskFormScreen.Payload()
	payload.CreateParentID = &parentID
	payload.Kicker = fmt.Sprintf(m.t("tui.kicker.new_subtask_fmt"), parent.ID, parent.Title)
	m.taskFormScreen = m.taskFormScreen.Open(payload, m.screenFrame())
	m.status = fmt.Sprintf(m.t("tui.status.new_subtask_fmt"), parent.ID, parent.Title)
}

func (m Model) defaultPriorityID() domain.Priority {
	if len(m.priorities) == 0 {
		return domain.PriorityZero
	}
	for _, priority := range m.priorities {
		if priority.Default {
			return domain.Priority(priority.ID)
		}
	}
	return domain.Priority(m.priorities[len(m.priorities)/2].ID)
}

func (m *Model) openTaskView(task domain.Task) {
	m.status = ""
	m.blurActiveScreen()
	m.openTaskDetail(task, nil, !m.inTaskDetail())
}

func (m Model) loadTaskActivity(taskID int64) ([]domain.Event, error) {
	if taskID <= 0 {
		return nil, nil
	}
	if m.repos.Events == nil {
		comments := m.commentsForTask(taskID)
		events := make([]domain.Event, len(comments))
		for i, comment := range comments {
			events[i] = commentToEvent(comment)
		}
		return events, nil
	}
	return m.repos.Events.ListTaskActivity(m.ctx, m.project.ID, taskID, m.views.TaskActivity.Sort.Order)
}

func (m *Model) openTaskEdit(task domain.Task) {
	if allowed, hint := m.canEditTask(task.ID); !allowed {
		m.status = hint
		return
	}
	if !m.inTaskDetail() || m.taskDetailScreen.Payload().Task.ID != task.ID {
		m.openTaskView(task)
	}
	tagsCSV := m.loadTaskTagsCSV(task.ID)
	parentValue := ""
	if task.ParentID != nil {
		parentValue = strconv.FormatInt(*task.ParentID, 10)
	}
	m.status = m.t("tui.status.editing_task")
	m.blurActiveScreen()
	m.taskFormGeneration++
	m.taskFormScreen = taskform.New().Bind(m.taskFormDeps()).Open(taskform.Payload{
		Mode: taskform.Edit, TaskID: task.ID, Generation: m.taskFormGeneration,
		Kicker:     fmt.Sprintf(m.t("tui.kicker.edit_task_fmt"), task.ID),
		Values:     taskform.Values{Title: task.Title, Description: task.Description, Priority: strconv.Itoa(int(task.Priority)), TagsCSV: tagsCSV, Parent: parentValue},
		Priorities: m.taskFormPriorities()}, m.screenFrame())
	m.pushScreen(screenhost.TaskForm)
}

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

func (m *Model) closeTaskScreen(status string) {
	if m.inTaskDetail() {
		m.taskDetailGeneration++
		m.popScreen()
	}
	m.status = status
	m.blurActiveScreen()
}

func (m *Model) openTaskFormBlockerOverlay() {
	if m.taskDetailScreen.Payload().Task.ID <= 0 {
		return
	}
	outcome := m.boundTaskDetailScreen().Update(m.screenFrame(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	detail, ok := outcome.Screen.(taskdetail.Screen)
	if !ok || detail.State().Mode != taskdetail.ModeBlockers {
		return
	}
	m.taskDetailScreen = detail
	m.pushScreen(screenhost.TaskDetail)
}

func (m *Model) closeTaskFormBlockerOverlay(status string) {
	m.status = status
	m.popScreen()
}

func (m *Model) armOrConfirmTaskDelete(task domain.Task) {
	if task.ID <= 0 {
		return
	}
	if m.taskDeletePendingID == task.ID {
		m.executeTaskDelete(task.ID)
		return
	}
	if allowed, hint := m.canDeleteTask(task.ID); !allowed {
		m.status = hint
		if svc, ok := m.requireOps(); ok {
			svc.EmitGuardViolated(m.ctx, m.project.ID, domain.EventEntityTask, task.ID,
				"task.delete", "permissions", hint,
				map[string]any{"task_id": task.ID, "entity": "task", "operation": "delete"})
		}
		return
	}
	m.taskDeletePendingID = task.ID
	descendants, err := m.repos.Tasks.CountDescendants(m.ctx, m.project.ID, task.ID)
	if err != nil || descendants == 0 {
		m.status = fmt.Sprintf(m.t("tui.confirm.task_delete_fmt"), task.ID, task.Title)
		return
	}
	m.status = fmt.Sprintf(m.t("tui.confirm.delete_subtree_fmt"), task.ID, task.Title, descendants, descendants+1)
}

func (m *Model) executeTaskDelete(taskID int64) {
	m.taskDeletePendingID = 0
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	if _, err := svc.DeleteTask(m.ctx, contract.DeleteTaskInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          taskID,
		Confirmed:       true}); err != nil {
		m.status = err.Error()
		return
	}
	if m.inTaskDetail() && m.taskDetailScreen.Payload().Task.ID == taskID {
		m.closeTaskScreen("")
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.task_deleted_fmt"), taskID)
}

func (m *Model) executeTaskArchiveFromDetail(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	svc := m.repos.operationService()
	if svc == nil {
		m.status = m.t("tui.status.archive_unavailable")
		return
	}
	if _, err := svc.ArchiveTask(m.ctx, contract.ArchiveTaskInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: m.project.ID},
		TaskID:          action.TaskID}); err != nil {
		m.status = err.Error()
		return
	}
	m.reloadTaskDetailAfterLifecycle(action, fmt.Sprintf(m.t("tui.status.task_archived_fmt"), action.TaskID))
}

func (m *Model) executeTaskUnarchiveFromDetail(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	svc := m.repos.operationService()
	if svc == nil {
		m.status = m.t("tui.status.unarchive_unavailable")
		return
	}
	if _, err := svc.UnarchiveTask(m.ctx, contract.ArchiveTaskInput{
		ProjectSelector: contract.ProjectSelector{ProjectID: m.project.ID},
		TaskID:          action.TaskID}); err != nil {
		m.status = err.Error()
		return
	}
	m.reloadTaskDetailAfterLifecycle(action, fmt.Sprintf(m.t("tui.status.task_unarchived_fmt"), action.TaskID))
}

// reloadTaskDetailAfterLifecycle refreshes the board snapshot, then reloads the
// open task detail by primary-key lookup so archived rows remain visible even
// when includeArchived is off.
func (m *Model) reloadTaskDetailAfterLifecycle(action screenhost.Action, status string) {
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if m.repos.Tasks == nil {
		m.status = status
		return
	}
	task, err := m.repos.Tasks.GetTaskByID(m.ctx, m.project.ID, action.TaskID, nil)
	if err != nil {
		m.status = err.Error()
		return
	}
	activity, err := m.loadTaskActivity(action.TaskID)
	if err != nil {
		m.status = err.Error()
		return
	}
	payload := m.taskDetailPayload(task, m.taskDetailScreen.Payload().Stack, activity)
	m.taskDetailScreen = m.taskDetailScreen.FinishOperation().Replace(payload)
	m.status = status
}

func (m *Model) saveTaskForm() {
	if len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.TaskForm {
		return
	}
	payload := m.taskFormScreen.Payload()
	values := m.taskFormScreen.Values()
	input, ok := m.prepareTaskFormSave(payload, values)
	if !ok {
		return
	}
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	task, err := m.applyTaskFormSave(svc, payload, input)
	if err != nil {
		m.status = err.Error()
		return
	}
	if err := m.syncTaskTagsFromForm(task.ID, input.tagsCSV); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if m.selectTaskByID(task.ID) {
		m.popScreen()
		m.taskFormGeneration++
		m.openTaskView(task)
	}
	m.status = m.t("tui.status.saved")
}

type taskFormSaveValues struct {
	title, description, parentValue, tagsCSV string
	priority                                 domain.Priority
	parentID                                 *int64
}

func (m *Model) prepareTaskFormSave(payload taskform.Payload, values taskform.Values) (taskFormSaveValues, bool) {
	input := taskFormSaveValues{title: values.Title, description: values.Description, parentValue: values.Parent, tagsCSV: values.TagsCSV}
	priorityID, _ := strconv.Atoi(values.Priority)
	input.priority = domain.Priority(priorityID)
	if err := taskvalidation.ValidateTitle(input.title); err != nil {
		m.status = m.t("tui.status.task_title_required")
		if !errors.Is(err, taskvalidation.ErrTitleRequired) {
			m.status = err.Error()
		}
		return taskFormSaveValues{}, false
	}
	if input.parentValue != "" {
		id, err := taskvalidation.PositiveID(input.parentValue)
		if err != nil {
			m.status = m.t("tui.taskedit.parent_lookup_invalid")
			return taskFormSaveValues{}, false
		}
		if payload.Mode == taskform.Edit && id == payload.TaskID {
			m.status = m.t("tui.taskedit.parent_lookup_self")
			return taskFormSaveValues{}, false
		}
		input.parentID = &id
	}
	if payload.Mode == taskform.Edit && input.parentID != nil && !m.validateTaskEditCycle(*input.parentID) {
		return taskFormSaveValues{}, false
	}
	return input, true
}

func (m *Model) applyTaskFormSave(svc contract.Operations, payload taskform.Payload, input taskFormSaveValues) (domain.Task, error) {
	switch payload.Mode {
	case taskform.Create:
		createParent := input.parentID
		if payload.CreateParentID != nil {
			createParent = payload.CreateParentID
		}
		resp, err := svc.CreateTask(m.ctx, contract.CreateTaskInput{ProjectSelector: m.projectSelector(), Title: input.title, Description: input.description, Priority: m.priorityLabel(input.priority), ParentID: createParent})
		if err != nil {
			return domain.Task{}, err
		}
		if resp.Task == nil {
			return domain.Task{}, fmt.Errorf("create did not return a task")
		}
		return m.loadTaskByID(resp.Task.ID)
	case taskform.Edit:
		return m.editTaskForm(svc, payload, input)
	default:
		return domain.Task{}, nil
	}
}

func (m *Model) editTaskForm(svc contract.Operations, payload taskform.Payload, input taskFormSaveValues) (domain.Task, error) {
	current, found := m.taskByID(payload.TaskID)
	if !found {
		return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
	}
	edit := contract.EditTaskInput{ProjectSelector: m.projectSelector(), TaskID: current.ID, Title: &input.title, Description: &input.description}
	if input.priority != domain.PriorityZero {
		label := m.priorityLabel(input.priority)
		edit.Priority = &label
	}
	currentParent := ""
	if current.ParentID != nil {
		currentParent = strconv.FormatInt(*current.ParentID, 10)
	}
	if input.parentValue != currentParent {
		edit.ParentID = contract.OptionalInt64{Set: true, Value: input.parentID}
	}
	if _, err := svc.EditTask(m.ctx, edit); err != nil {
		return domain.Task{}, err
	}
	return m.loadTaskByID(current.ID)
}

func (m *Model) validateTaskEditCycle(parentID int64) bool {
	taskID := m.taskFormScreen.Payload().TaskID
	if m.repos.Tasks == nil || parentID == taskID {
		return true
	}
	cycle, err := m.repos.Tasks.IsDescendantOf(m.ctx, m.project.ID, parentID, taskID)
	if err != nil {
		m.status = err.Error()
		return false
	}
	if !cycle {
		return true
	}
	if conflict, ok := m.taskByID(parentID); ok {
		m.status = fmt.Sprintf("re-parent would create a cycle: conflicting ancestor #%d %q is already a descendant of this task", conflict.ID, conflict.Title)
	} else {
		m.status = fmt.Sprintf("re-parent would create a cycle: conflicting ancestor #%d is already a descendant of this task", parentID)
	}
	return false
}

func (m *Model) syncTaskTagsFromForm(taskID int64, tagsCSV string) error {
	if taskID <= 0 || m.repos.Tags == nil {
		return nil
	}
	snap := m.repos.activeSnapshot()
	wanted := taskvalidation.CanonicalTags(tagsCSV, snap.Synonyms())
	current, err := m.repos.Tags.ListTaskTags(m.ctx, m.project.ID, taskID)
	if err != nil {
		return err
	}
	wantedSet, currentSet := taskTagSets(wanted, current)
	svc, ok := m.requireOps()
	if !ok {
		return fmt.Errorf("%s", m.status)
	}
	if err := m.addMissingTaskTags(svc, taskID, wanted, currentSet); err != nil {
		return err
	}
	return m.removeStaleTaskTags(svc, taskID, current, wantedSet)
}

func taskTagSets(wanted []string, current []domain.Tag) (map[string]struct{}, map[string]struct{}) {
	wantedSet := map[string]struct{}{}
	for _, name := range wanted {
		wantedSet[name] = struct{}{}
	}
	currentSet := map[string]struct{}{}
	for _, tag := range current {
		currentSet[tag.Name] = struct{}{}
	}
	return wantedSet, currentSet
}

func (m *Model) addMissingTaskTags(svc contract.Operations, taskID int64, wanted []string, currentSet map[string]struct{}) error {
	for _, name := range wanted {
		if _, exists := currentSet[name]; exists {
			continue
		}
		if _, err := svc.AddTag(m.ctx, contract.AddTagInput{
			ProjectSelector: m.projectSelector(),
			EntityType:      "task",
			EntityID:        taskID,
			TagName:         name}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) removeStaleTaskTags(svc contract.Operations, taskID int64, current []domain.Tag, wantedSet map[string]struct{}) error {
	for _, tag := range current {
		if _, keep := wantedSet[tag.Name]; keep {
			continue
		}
		if _, err := svc.RemoveTag(m.ctx, contract.RemoveTagInput{
			ProjectSelector: m.projectSelector(),
			EntityType:      "task",
			EntityID:        taskID,
			TagID:           tag.ID,
			Confirmed:       true}); err != nil {
			return err
		}
	}
	return nil
}

func finalBucketSnapshotForTask(snap *config.Snapshot, task domain.Task) *config.Snapshot {
	if snap == nil {
		return nil
	}
	return snap.For(task)
}

func (m *Model) beginMoveInputForTask(task domain.Task) {
	m.moveInputTargetID = task.ID
	m.beginInput(modeMove, m.moveInputPromptForTask(task), "")
}

func (m Model) moveInputPromptForTask(task domain.Task) string {
	base := m.t("tui.input.target_bucket_key")
	snap := m.repos.activeSnapshot()
	if snap == nil || snap.For(task) == nil {
		return base
	}
	buckets := snap.For(task).Workflow().Buckets
	if len(buckets) == 0 {
		return base
	}
	keys := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		keys = append(keys, bucket.Key)
	}
	return fmt.Sprintf("%s — %s", base, strings.Join(keys, " · "))
}

func (m Model) subtaskPanelWorkflow() domain.Workflow {
	if snap := m.repos.activeSnapshot(); snap != nil {
		if sub, ok := snap.SubtaskKit(); ok {
			return sub.Workflow()
		}
	}
	return m.workflow
}
