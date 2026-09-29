package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	knowledgegraph "omakiten/internal/graph"
	"omakiten/internal/keynav"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/board"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/description"
	"omakiten/internal/tui/screens/entitydetail"
	"omakiten/internal/tui/screens/entitylist"
	"omakiten/internal/tui/screens/graph"
	"omakiten/internal/tui/screens/home"
	"omakiten/internal/tui/screens/insights"
	"omakiten/internal/tui/screens/knowledge"
	"omakiten/internal/tui/screens/logs"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/plans"
	projectscreen "omakiten/internal/tui/screens/project"
	"omakiten/internal/tui/screens/projectresume"
	"omakiten/internal/tui/screens/relationshippicker"
	settingsscreen "omakiten/internal/tui/screens/settings"
	"omakiten/internal/tui/screens/settingspicker"
	"omakiten/internal/tui/screens/stats"
	"omakiten/internal/tui/screens/studio"
	"omakiten/internal/tui/screens/table"
	"omakiten/internal/tui/screens/taskdetail"
	"omakiten/internal/tui/screens/taskform"
)

// This file is the root half of the screen host contract: it binds live host
// deps onto each extracted screen, hands the bound instance to the descriptor
// factory, dispatches keys into it, and folds the semantic Outcome back into
// root state. Every extraction cohort adds its screen to the three switches
// below (bind / fetch / store) and nothing else.

// hostedScreenProvider is the capability newHostedScreenFactory needs from the
// host adapter: the live, dep-bound instance for a screen id. Extracted screens
// are stateful values owned by the root Model, so the factory must return the
// SAME instance the update path mutates rather than constructing a fresh one.
type hostedScreenProvider interface {
	screenhost.Host
	hostedScreen(id screenhost.ID) (screenhost.Screen, bool)
}

// newHostedScreenFactory resolves an extracted screen from the host adapter.
func newHostedScreenFactory(id screenhost.ID) screenhost.Factory {
	return func(host screenhost.Host) screenhost.Screen {
		provider, ok := host.(hostedScreenProvider)
		if !ok {
			panic(fmt.Sprintf("tui: screen %q requires the root screen host adapter", id))
		}
		screen, ok := provider.hostedScreen(id)
		if !ok {
			panic(fmt.Sprintf("tui: screen %q is not hosted by the root model", id))
		}
		return screen
	}
}

// hostedScreen returns the live, dep-bound instance for an extracted screen.
func (h modelScreenHost) hostedScreen(id screenhost.ID) (screenhost.Screen, bool) {
	return h.model.hostedScreen(id)
}

func (m Model) hostedScreen(id screenhost.ID) (screenhost.Screen, bool) {
	if screen, ok := m.entityListScreens[id]; ok {
		return m.boundEntityListScreen(screen), true
	}
	if descriptor, ok := entitylist.ForID(id); ok {
		return m.boundEntityListScreen(entitylist.New(descriptor)), true
	}
	if screen, ok := m.hostedTopScreen(id); ok {
		return screen, true
	}
	return m.hostedSettingsScreen(id)
}

func (m Model) hostedTopScreen(id screenhost.ID) (screenhost.Screen, bool) {
	switch id {
	case screenhost.Home:
		return m.homeScreen, true
	case screenhost.TasksBoard:
		return m.boundBoardScreen(), true
	case screenhost.TasksTable:
		return m.boundTableScreen(), true
	case screenhost.TasksGraph:
		return m.boundGraphScreen(), true
	case screenhost.TasksPlans:
		return m.boundPlansScreen(), true
	case screenhost.PlanGoal:
		return m.boundPlanGoalScreen(), true
	case screenhost.PlanNetwork:
		return m.planNetworkScreen, true
	case screenhost.Project:
		return m.boundProjectScreen(), true
	case screenhost.ProjectForm:
		return m.boundProjectFormScreen(), true
	case screenhost.ProjectResume:
		return m.boundProjectResumeScreen(), true
	case screenhost.ProjectKnowledge:
		return m.projectKnowledgeScreen, true
	case screenhost.StatsGeneral:
		return m.boundStatsScreen(), true
	case screenhost.StatsLogs:
		return m.boundLogsScreen(), true
	case screenhost.StatsInsights:
		return m.boundInsightsScreen(), true
	case screenhost.StudioWorkflow, screenhost.StudioCommands, screenhost.StudioPersonas, screenhost.StudioHooks:
		return m.boundStudioScreen(id), true
	case screenhost.TaskForm:
		return m.taskFormScreen.Bind(m.taskFormDeps()), true
	case screenhost.TaskDetail:
		return m.boundTaskDetailScreen(), true
	case screenhost.TaskDescription:
		return m.boundDescriptionScreen(), true
	case screenhost.CommentDetail:
		return m.boundCommentDetailScreen(), true
	}
	return nil, false
}

func (m Model) hostedSettingsScreen(id screenhost.ID) (screenhost.Screen, bool) {
	switch id {
	case screenhost.SettingsGeneral:
		return m.boundSettingsGeneralScreen(), true
	case screenhost.SettingsGuards:
		return m.boundSettingsGuardsScreen(), true
	case screenhost.EntityDetail:
		return m.boundEntityDetailScreen(), true
	case screenhost.ThemePicker:
		screen := m.themePickerScreen
		if screen.ID() != id {
			screen = settingspicker.New(settingspicker.Theme)
		}
		return screen, true
	case screenhost.ConfigPicker:
		screen := m.configPickerScreen
		if screen.ID() != id {
			screen = settingspicker.New(settingspicker.Config)
		}
		return screen, true
	case screenhost.SubtaskKitPicker:
		screen := m.subtaskKitPickerScreen
		if screen.ID() != id {
			screen = settingspicker.New(settingspicker.SubtaskKit)
		}
		return screen, true
	case screenhost.PersonaSkills:
		screen := m.personaSkillsScreen
		if screen.ID() != id {
			screen = relationshippicker.New(relationshippicker.PersonaSkills)
		}
		return screen, true
	case screenhost.TemplateDefault:
		screen := m.templateDefaultScreen
		if screen.ID() != id {
			screen = relationshippicker.New(relationshippicker.TemplateDefault)
		}
		return screen, true
	}
	return nil, false
}

// storeScreen writes an updated screen value back into root state. The type
// switch is the only place root knows a screen's concrete type; everything else
// goes through the screenhost.Screen interface.
func (m *Model) storeScreen(screen screenhost.Screen) {
	if m.storeSpecializedScreen(screen) {
		return
	}
	switch typed := screen.(type) {
	case home.Screen:
		m.homeScreen = typed
	case studio.Screen:
		m.studioScreen = typed
	case board.Screen:
		m.boardScreen = typed
	case table.Screen:
		m.tableScreen = typed
	case graph.Screen:
		m.graphScreen = typed
	case plans.Screen:
		m.plansScreen = typed
	case plans.GoalScreen:
		m.planGoalReaderScreen = typed
	case plannetwork.Screen:
		m.planNetworkScreen = typed
	case projectscreen.Screen:
		m.projectScreen = typed
	case projectscreen.FormScreen:
		m.projectFormReaderScreen = typed
	case projectresume.Screen:
		m.projectResumeScreen = typed
	case knowledge.Screen:
		m.projectKnowledgeScreen = typed
	case stats.Screen:
		m.statsScreen = typed
	case logs.Screen:
		m.logsScreen = typed
	case insights.Screen:
		m.insightsScreen = typed
	}
}

func (m *Model) storeSpecializedScreen(screen screenhost.Screen) bool {
	switch typed := screen.(type) {
	case entitylist.Screen:
		if m.entityListScreens == nil {
			m.entityListScreens = map[screenhost.ID]entitylist.Screen{}
		}
		m.entityListScreens[typed.ID()] = typed
	case entitydetail.Screen:
		m.entityDetailScreen = typed
	case settingsscreen.Screen:
		if typed.ID() == screenhost.SettingsGuards {
			m.settingsGuardsScreen = typed
		} else {
			m.settingsGeneralScreen = typed
		}
	case settingspicker.Screen:
		switch typed.ID() {
		case screenhost.ThemePicker:
			m.themePickerScreen = typed
		case screenhost.ConfigPicker:
			m.configPickerScreen = typed
		case screenhost.SubtaskKitPicker:
			m.subtaskKitPickerScreen = typed
		}
	case relationshippicker.Screen:
		if typed.ID() == screenhost.PersonaSkills {
			m.personaSkillsScreen = typed
		} else {
			m.templateDefaultScreen = typed
		}
	case taskform.Screen:
		m.taskFormScreen = typed
	case taskdetail.Screen:
		m.taskDetailScreen = typed
	case description.Screen:
		m.descriptionReaderScreen = typed
	case commentdetail.Screen:
		m.commentDetailScreen = typed
	default:
		return false
	}
	return true
}

// dispatchActiveScreen sends a message to the active hosted screen and folds
// its semantic outcome into root-owned state.
func (m *Model) dispatchActiveScreen(msg tea.Msg) (tea.Cmd, bool) {
	screen, ok := m.activeHostedScreenLive()
	if !ok {
		return nil, false
	}
	if m.interceptDeniedKey(screen, msg) {
		return nil, true
	}
	m.studioOutcomeGeneration = m.studioRuntimeGeneration
	m.studioOutcomeGenerationSet = true
	return m.applyScreenOutcome(screen.Update(m.screenFrame(), msg)), true
}

func (m *Model) dispatchOwnedScreenKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	screen, ok := m.activeHostedScreenLive()
	if !ok {
		return nil, false
	}
	owner, ok := screen.(screenhost.KeyOwner)
	if !ok || !owner.OwnsKey(msg) {
		return nil, false
	}
	if m.denyProductKey(screen, msg.String()) {
		return nil, true
	}
	m.studioOutcomeGeneration = m.studioRuntimeGeneration
	m.studioOutcomeGenerationSet = true
	return m.applyScreenOutcome(screen.Update(m.screenFrame(), msg)), true
}

// applyScreenOutcome folds a screen's semantic outcome into root state. Screens
// report intent — navigate, go back, reload, set status, quit — and the root
// remains the only writer of navigation, status and process lifecycle.
func (m *Model) applyScreenOutcome(outcome screenhost.Outcome) tea.Cmd {
	if command, stop := m.prepareScreenOutcome(outcome); stop {
		return command
	}
	for _, handler := range []func(screenhost.Outcome) (tea.Cmd, bool){
		m.applyNavigationOutcome,
		m.applyProjectOutcome,
		m.applyRelationshipOutcome,
		m.applyTaskOutcome,
		m.applyEntityOutcome,
		m.applyReloadOutcome,
	} {
		if command, handled := handler(outcome); handled {
			return command
		}
	}
	return outcome.Command
}

func (m *Model) prepareScreenOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	studioGeneration := m.consumeStudioOutcomeGeneration()
	if m.denyProductAction(outcome) {
		return outcome.Command, true
	}
	taskFormBlockerOverlay := m.inTaskFormBlockerOverlay()
	m.clearPendingTaskDelete(outcome)
	if !m.acceptPreparedScreenOutcome(outcome) {
		return outcome.Command, true
	}
	if m.rejectStaleStudioOutcome(outcome, studioGeneration) {
		return outcome.Command, true
	}
	m.storePreparedScreen(outcome.Screen, taskFormBlockerOverlay)
	return nil, false
}

func (m *Model) consumeStudioOutcomeGeneration() uint64 {
	generation := m.studioRuntimeGeneration
	if m.studioOutcomeGenerationSet {
		generation = m.studioOutcomeGeneration
		m.studioOutcomeGenerationSet = false
	}
	return generation
}

func (m *Model) clearPendingTaskDelete(outcome screenhost.Outcome) {
	if _, ok := outcome.Screen.(taskdetail.Screen); ok && outcome.Action.Kind != screenhost.ActionDeleteTaskFromDetail {
		m.taskDeletePendingID = 0
	}
}

func (m *Model) acceptPreparedScreenOutcome(outcome screenhost.Outcome) bool {
	if isRelationshipAction(outcome.Action.Kind) && !m.acceptRelationshipOutcome(outcome) {
		return false
	}
	if isTaskFormAction(outcome.Action.Kind) && !m.acceptTaskFormOutcome(outcome) {
		return false
	}
	return true
}

func (m *Model) rejectStaleStudioOutcome(outcome screenhost.Outcome, generation uint64) bool {
	screen, ok := outcome.Screen.(studio.Screen)
	if !ok || generation == m.studioRuntimeGeneration {
		return false
	}
	// A Studio apply may rotate the runtime from inside its Reload callback. Its
	// returned screen belongs to the old generation and must not overwrite the
	// freshly reset root session.
	if message := screen.State().ApplyMessage; message != "" {
		m.status = message
	}
	return true
}

func (m *Model) storePreparedScreen(screen screenhost.Screen, taskFormBlockerOverlay bool) {
	if screen == nil {
		return
	}
	m.storeScreen(screen)
	detail, ok := screen.(taskdetail.Screen)
	if ok && taskFormBlockerOverlay && detail.State().Mode == taskdetail.ModeNormal {
		m.closeTaskFormBlockerOverlay(m.t("tui.status.cancelled"))
	}
}

func (m *Model) applyNavigationOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionSetStatus:
		m.status = outcome.Action.Status
	case screenhost.ActionOpenTask:
		if task, ok := m.taskByID(outcome.Action.TaskID); ok {
			m.openTaskView(task)
		}
	case screenhost.ActionMoveTask:
		if task, ok := m.taskByID(outcome.Action.TaskID); ok {
			m.beginMoveInputForTask(task)
		}
	case screenhost.ActionCreateTask:
		m.openTaskCreate()
	case screenhost.ActionEditTask:
		if task, ok := m.taskByID(outcome.Action.TaskID); ok {
			m.openTaskEdit(task)
		}
	case screenhost.ActionMoveTaskToBucket:
		m.applyBoardMove(outcome.Action.TaskID, outcome.Action.BucketKey)
	case screenhost.ActionNavigate:
		m.navigateToScreen(outcome.Action.Target)
	case screenhost.ActionBack:
		m.applyBackOutcome(outcome.Screen)
		m.popScreen()
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) applyBackOutcome(screen screenhost.Screen) {
	picker, ok := screen.(settingspicker.Screen)
	if !ok {
		return
	}
	switch picker.Payload().Kind {
	case settingspicker.Theme:
		m.status = m.t("tui.status.theme_picker_cancelled")
	case settingspicker.Config:
		m.status = m.t("tui.status.config_picker_cancelled")
	case settingspicker.SubtaskKit:
		m.status = m.t("tui.status.subtask_kit_picker_cancelled")
	}
}

func (m *Model) applyProjectOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionOpenPlanGoal:
		m.openPlanGoal(outcome.Action.PlanSlug)
	case screenhost.ActionOpenPlanNetwork:
		m.openPlanNetwork(outcome.Action.PlanSlug)
	case screenhost.ActionSavePlanGoal:
		m.savePlanNetworkGoal(outcome.Action)
	case screenhost.ActionSetTaskAssignee:
		m.assignPlanNetworkTask(outcome.Action)
	case screenhost.ActionSelectProject:
		m.selectHomeProjectID(outcome.Action.ProjectID)
	case screenhost.ActionCreateProject:
		m.status = "Create a project with: okt init --name MyProject --slug my-project"
	case screenhost.ActionEditProject:
		m.editHomeProject(outcome.Action.ProjectID)
	case screenhost.ActionPrepareProjectDelete:
		m.prepareHomeProjectDelete(outcome.Action.ProjectID)
	case screenhost.ActionDeleteProject:
		return m.executeHomeProjectDeleteAction(outcome.Action), true
	case screenhost.ActionOpenProjectForm:
		m.openProjectForm()
	case screenhost.ActionOpenProjectResume:
		m.openProjectResume()
	case screenhost.ActionOpenProjectKnowledge:
		m.openProjectKnowledge()
	case screenhost.ActionOpenProjectComment:
		m.openProjectComment(outcome.Action.CommentID)
	case screenhost.ActionOpenThemePicker:
		m.openThemePicker()
	case screenhost.ActionOpenConfigPicker:
		m.openConfigPicker()
	case screenhost.ActionOpenSubtaskKitPicker:
		m.openSubtaskKitPicker()
	case screenhost.ActionOpenConfigEditor:
		return m.openConfigEditorOutcome(outcome)
	case screenhost.ActionApplySettingsPicker:
		m.applySettingsPicker(outcome.Action)
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) openProjectForm() {
	m.projectFormReaderScreen = m.boundProjectFormScreen().Apply(m.projectScreen.Payload())
	m.pushScreen(screenhost.ProjectForm)
}

func (m *Model) editHomeProject(projectID int64) {
	if m.selectHomeProjectID(projectID) {
		m.openProjectView()
	}
}

func (m *Model) openProjectKnowledge() {
	snapshot := m.projectScreen.Payload().Knowledge
	m.projectKnowledgeScreen = m.projectKnowledgeScreen.Apply(snapshot, knowledgegraph.KnowledgeViews(snapshot))
	m.pushScreen(screenhost.ProjectKnowledge)
}

func (m *Model) openConfigEditorOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	if m.repos.Editor == nil {
		m.status = m.t("tui.status.editor_unavailable")
		return outcome.Command, true
	}
	return runExternalEditor(m.repos.Editor.Path()), true
}

func (m *Model) applyRelationshipOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionSelectRelationship:
	case screenhost.ActionSavePersonaSkills:
		m.savePersonaSkills(outcome.Screen.(relationshippicker.Screen))
	case screenhost.ActionSelectTemplateDefault:
		m.saveTemplateDefault(outcome.Action)
	case screenhost.ActionCreateRelationship:
		return m.scaffoldRelationshipSkill(outcome.Screen.(relationshippicker.Screen)), true
	case screenhost.ActionCancelRelationshipPicker:
		if outcome.Screen.ID() == screenhost.TemplateDefault {
			m.status = m.t("tui.status.default_picker_cancelled")
		}
		m.relationshipPickerGeneration++
		m.popScreen()
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) applyTaskOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionSaveTaskForm:
		m.saveTaskFormAction(outcome.Action)
	case screenhost.ActionCancelTaskForm:
		m.cancelTaskForm(outcome.Action)
	case screenhost.ActionLookupTaskParent:
		m.lookupTaskFormParent(outcome.Action)
	case screenhost.ActionOpenTaskBlockers:
		m.openTaskFormBlockerOverlay()
	case screenhost.ActionCloseTaskDetail:
		m.closeTaskDetail(outcome.Action)
	case screenhost.ActionRefreshTaskDetail:
		m.refreshTaskDetail(outcome.Action, m.t("tui.status.refreshed"))
	case screenhost.ActionCreateSubtask, screenhost.ActionOpenNestedTask, screenhost.ActionOpenTaskComment,
		screenhost.ActionAddTaskComment, screenhost.ActionMoveTaskFromDetail, screenhost.ActionSaveTaskBlockers,
		screenhost.ActionDeleteTaskFromDetail, screenhost.ActionArchiveTaskFromDetail, screenhost.ActionUnarchiveTaskFromDetail,
		screenhost.ActionOpenTaskDescription, screenhost.ActionSaveComment, screenhost.ActionDeleteComment,
		screenhost.ActionCompleteSubtask, screenhost.ActionToggleTaskMarkdown:
		return m.applyTaskDetailOutcome(outcome)
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) applyTaskDetailOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionCreateSubtask:
		if task, ok := m.taskByID(outcome.Action.TaskID); ok {
			m.openSubTaskCreate(task)
		}
	case screenhost.ActionOpenNestedTask:
		m.openNestedTaskDetail(outcome.Action)
	case screenhost.ActionOpenTaskComment:
		m.openTaskDetailComment(outcome.Action)
	case screenhost.ActionAddTaskComment:
		m.addTaskDetailComment(outcome.Action)
	case screenhost.ActionMoveTaskFromDetail:
		m.moveTaskFromDetail(outcome.Action)
	case screenhost.ActionSaveTaskBlockers:
		m.saveTaskDetailBlockers(outcome.Action)
	case screenhost.ActionDeleteTaskFromDetail:
		if task, ok := m.taskByID(outcome.Action.TaskID); ok {
			m.armOrConfirmTaskDelete(task)
		}
	case screenhost.ActionArchiveTaskFromDetail:
		m.executeTaskArchiveFromDetail(outcome.Action)
	case screenhost.ActionUnarchiveTaskFromDetail:
		m.executeTaskUnarchiveFromDetail(outcome.Action)
	case screenhost.ActionOpenTaskDescription:
		if task, ok := m.taskByID(outcome.Action.TaskID); ok {
			m.openDescriptionScreen(task)
		}
	case screenhost.ActionSaveComment:
		m.saveCommentDetail(outcome.Action)
	case screenhost.ActionDeleteComment:
		m.deleteCommentDetail(outcome.Action)
	case screenhost.ActionCompleteSubtask:
		m.completeTaskDetailSubtask(outcome.Action)
	case screenhost.ActionToggleTaskMarkdown:
		m.toggleMarkdownRendered()
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) applyEntityOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionOpenEntity:
		m.clearDeletePrompt("")
		if outcome.Action.Value == "" {
			m.status = m.t("tui.status.nothing_to_open")
			return outcome.Command, true
		}
		m.openEntityDetail(entityKindFromListAction(outcome.Action.EntityKind), outcome.Action.Value)
	case screenhost.ActionCreateEntity:
		m.clearDeletePrompt("")
		return m.openEntityCreate(entityKindFromListAction(outcome.Action.EntityKind)), true
	case screenhost.ActionEditEntity:
		m.clearDeletePrompt("")
		if outcome.Action.Value == "" {
			m.status = m.t("tui.status.nothing_to_edit")
			return outcome.Command, true
		}
		return m.openEntityEditor(entityKindFromListAction(outcome.Action.EntityKind), outcome.Action.Value), true
	case screenhost.ActionPrepareEntityDelete:
		m.prepareEntityDelete(outcome.Action)
	case screenhost.ActionDeleteEntity:
		m.deleteEntity(entityKindFromListAction(outcome.Action.EntityKind), outcome.Action.Value)
	case screenhost.ActionCancelEntityDelete:
		m.cancelEntityDelete()
	case screenhost.ActionOpenPersonaSkills:
		m.clearDeletePrompt("")
		if outcome.Action.Value == "" {
			m.status = m.t("tui.status.no_persona_selected")
			return outcome.Command, true
		}
		m.openPersonaPicker(outcome.Action.Value)
	case screenhost.ActionOpenTemplateDefault:
		m.openTemplateDefaultOutcome(outcome.Action.Value)
	case screenhost.ActionTemplateCreateHint:
		m.status = m.t("tui.status.template_add_hint")
	case screenhost.ActionTemplateDeleteHint:
		m.status = m.t("tui.status.template_remove_hint")
	case screenhost.ActionPrepareTagDelete, screenhost.ActionDeleteTag, screenhost.ActionDeleteOrphanTags,
		screenhost.ActionPrepareTagMerge, screenhost.ActionMergeTags:
		return m.applyTagOutcome(outcome)
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) cancelEntityDelete() {
	if screen, ok := m.entityListScreens[screenhost.SettingsTags]; ok {
		m.entityListScreens[screenhost.SettingsTags] = screen.CancelArmed()
	}
	m.clearDeletePrompt(m.t("tui.status.delete_cancelled"))
}

func (m *Model) prepareEntityDelete(action screenhost.Action) {
	m.clearDeletePrompt("")
	m.status = fmt.Sprintf(m.t("tui.confirm.entity_delete_fmt"), strings.ToLower(entityKindFromListAction(action.EntityKind).String()), action.Value)
}

func (m *Model) openTemplateDefaultOutcome(value string) {
	m.clearDeletePrompt("")
	if value != "" {
		m.openTemplateDefaultPicker(value)
	}
}

func (m *Model) applyTagOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionPrepareTagDelete:
		m.prepareTagDelete(outcome.Action.Value)
	case screenhost.ActionDeleteTag:
		m.confirmTagDelete(outcome.Action.Value)
	case screenhost.ActionDeleteOrphanTags:
		m.deleteOrphanTags()
	case screenhost.ActionPrepareTagMerge:
		m.prepareTagMerge(outcome.Action.Value)
	case screenhost.ActionMergeTags:
		m.confirmTagMerge(outcome.Action.SourceValue, outcome.Action.Value)
	default:
		return nil, false
	}
	return outcome.Command, true
}

func (m *Model) applyReloadOutcome(outcome screenhost.Outcome) (tea.Cmd, bool) {
	switch outcome.Action.Kind {
	case screenhost.ActionReload:
		return m.reloadScreenOutcome(outcome), true
	case screenhost.ActionQuit:
		return tea.Quit, true
	default:
		return nil, false
	}
}

func (m *Model) reloadScreenOutcome(outcome screenhost.Outcome) tea.Cmd {
	if pickerScreen, ok := outcome.Screen.(settingspicker.Screen); ok {
		m.refreshSettingsPicker(pickerScreen)
		return outcome.Command
	}
	if _, ok := outcome.Screen.(home.Screen); ok {
		return m.homeReloadCmd(outcome.Action.Generation)
	}
	if _, ok := outcome.Screen.(projectscreen.Screen); ok {
		m.setRefreshStatus(m.refreshProjectSummary())
		return outcome.Command
	}
	if _, ok := outcome.Screen.(projectresume.Screen); ok {
		m.setRefreshStatus(m.refreshProjectResume())
		return outcome.Command
	}
	if _, ok := outcome.Screen.(knowledge.Screen); ok {
		if m.repos.Knowledge != nil {
			snapshot := m.repos.Knowledge(m.ctx, m.project)
			m.projectKnowledgeScreen = m.projectKnowledgeScreen.Apply(snapshot, knowledgegraph.KnowledgeViews(snapshot))
		}
		return outcome.Command
	}
	if err := m.refreshCurrentView(); err != nil {
		m.status = err.Error()
		if list, ok := outcome.Screen.(plans.Screen); ok {
			m.plansScreen = list.Apply(list.Rollups(), err)
		}
	} else if _, ok := outcome.Screen.(entitydetail.Screen); ok {
		m.refreshEntityDetailProjection()
	}
	return outcome.Command
}

func (m *Model) setRefreshStatus(err error) {
	if err != nil {
		m.status = err.Error()
		return
	}
	m.status = m.t("tui.status.refreshed")
}

func (m *Model) openProjectComment(commentID int64) {
	for _, event := range m.projectScreen.Activity() {
		if event.ID != commentID || event.EventType != domain.EventTypeComment {
			continue
		}
		m.openCommentScreen(eventToComment(event), true)
		return
	}
}

func (m Model) boundBoardScreen() board.Screen {
	return m.boardScreen.Bind(board.Deps{
		Projection: m.taskProjection(m.workflow),
		Tasks:      m.tasks, Workflow: m.workflow, Dependencies: m.dependencies,
		Comments: m.comments, View: m.views.Board, Priorities: m.priorities}, m.screenFrame())
}

func (m *Model) applyBoardMove(taskID int64, bucketKey string) {
	task, ok := m.taskByID(taskID)
	if !ok {
		m.status = m.t("tui.status.no_selected_task")
		return
	}
	if err := m.moveTask(taskID, bucketKey); err != nil {
		m.status = err.Error()
		m.boardScreen = m.boundBoardScreen().SelectTaskID(taskID, m.screenFrame())
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.boardScreen = m.boundBoardScreen().SelectTaskID(taskID, m.screenFrame())
	m.status = fmt.Sprintf(m.t("tui.status.task_moved_fmt"), task.ID, bucketKey)
}

func (m Model) boundPlansScreen() plans.Screen { return m.plansScreen }

func (m Model) boundPlanGoalScreen() plans.GoalScreen {
	return m.planGoalReaderScreen
}

func (m *Model) openPlanGoal(slug string) {
	if slug == "" || m.repos.Plans == nil {
		return
	}
	m.planGoalReaderScreen = m.boundPlanGoalScreen().Loading()
	show, err := m.loadPlanShow(m.ctx, m.project, slug)
	m.planGoalReaderScreen = m.boundPlanGoalScreen().Apply(show, err)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.pushScreen(screenhost.PlanGoal)
}

func (m *Model) openPlanNetwork(slug string) {
	if slug == "" || m.repos.Plans == nil {
		return
	}
	show, err := m.loadPlanShow(m.ctx, m.project, slug)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.planNetworkGeneration++
	m.applyPlanNetworkResult(planNetworkResultMsg{generation: m.planNetworkGeneration, slug: slug, payload: m.planNetworkPayload(show), open: true})
}

type planNetworkResultMsg struct {
	generation uint64
	slug       string
	payload    plannetwork.Payload
	open       bool
	status     string
	err        error
}

func (m *Model) applyPlanNetworkResult(result planNetworkResultMsg) {
	if result.generation != m.planNetworkGeneration {
		return
	}
	if result.err != nil {
		m.status = result.err.Error()
		return
	}
	if result.open {
		if result.slug == "" || result.payload.Show.Plan.Slug != result.slug {
			return
		}
		m.planNetworkScreen = m.planNetworkScreen.Open(result.payload)
		m.pushScreen(screenhost.PlanNetwork)
	} else {
		if !m.inPlanNetwork() || result.slug != m.planNetworkScreen.Show().Plan.Slug || result.payload.Show.Plan.Slug != result.slug {
			return
		}
		m.planNetworkScreen = m.planNetworkScreen.Apply(result.payload)
	}
	if result.status != "" {
		m.status = result.status
	}
}

func (m Model) planNetworkPayload(show domain.PlanShow) plannetwork.Payload {
	payload := plannetwork.Payload{Show: show, Tasks: make(map[int64]domain.Task, len(m.tasks))}
	for _, task := range m.tasks {
		payload.Tasks[task.ID] = task
	}
	if snap := m.repos.activeSnapshot(); snap != nil {
		wf := snap.Workflow()
		payload.FinalBucket = wf.FinalBucketKey()
		firstPosition := 0
		for _, bucket := range wf.Buckets {
			if payload.FirstBucket == "" || bucket.Position < firstPosition {
				payload.FirstBucket = bucket.Key
				firstPosition = bucket.Position
			}
		}
		if m.repos.Plans != nil && show.Plan.ID != 0 {
			if row, ok, err := m.repos.Plans.PeekNextClaimable(m.ctx, m.project.ID, show.Plan.ID, snap); err == nil && ok {
				payload.NextClaimableID = row.TaskID
			}
		}
	}
	return payload
}

func (m *Model) savePlanNetworkGoal(action screenhost.Action) {
	if m.repos.Plans == nil || action.PlanID == 0 || action.PlanSlug != m.planNetworkScreen.Show().Plan.Slug {
		return
	}
	m.planNetworkGeneration++
	generation := m.planNetworkGeneration
	svc, ok := m.requireOps()
	if !ok {
		m.applyPlanNetworkResult(planNetworkResultMsg{generation: generation, slug: action.PlanSlug, err: fmt.Errorf("%s", m.status)})
		return
	}
	goal := action.Value
	if _, err := svc.EditPlan(m.ctx, contract.EditPlanInput{
		ProjectSelector: m.projectSelector(),
		PlanID:          action.PlanID,
		GoalBody:        &goal}); err != nil {
		m.applyPlanNetworkResult(planNetworkResultMsg{generation: generation, slug: action.PlanSlug, err: err})
		return
	}
	m.reloadPlanNetworkScreen(generation, action.PlanSlug)
}

func (m *Model) assignPlanNetworkTask(action screenhost.Action) {
	if m.repos.Tasks == nil || action.TaskID == 0 || action.PlanSlug != m.planNetworkScreen.Show().Plan.Slug {
		return
	}
	m.planNetworkGeneration++
	generation := m.planNetworkGeneration
	svc, ok := m.requireOps()
	if !ok {
		m.applyPlanNetworkResult(planNetworkResultMsg{generation: generation, slug: action.PlanSlug, err: fmt.Errorf("%s", m.status)})
		return
	}
	if _, err := svc.AssignTask(m.ctx, contract.AssignTaskInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          action.TaskID,
		Assignee:        action.Value}); err != nil {
		m.applyPlanNetworkResult(planNetworkResultMsg{generation: generation, slug: action.PlanSlug, err: err})
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.reloadPlanNetworkScreen(generation, action.PlanSlug)
}

func (m *Model) reloadPlanNetworkScreen(generation uint64, slug string) {
	show, err := m.loadPlanShow(m.ctx, m.project, slug)
	if err != nil {
		m.applyPlanNetworkResult(planNetworkResultMsg{generation: generation, slug: slug, err: err})
		return
	}
	m.applyPlanNetworkResult(planNetworkResultMsg{generation: generation, slug: slug, payload: m.planNetworkPayload(show), status: m.t("tui.status.saved")})
}

func (m *Model) pushScreen(id screenhost.ID) {
	if current, ok := m.activeHostedScreenLive(); ok {
		m.storeScreen(current.Lifecycle(m.screenFrame(), screenhost.LifecycleLeave).Screen)
	}
	m.screenStack = append(m.screenStack, id)
	if next, ok := m.activeHostedScreenLive(); ok {
		m.storeScreen(next.Lifecycle(m.screenFrame(), screenhost.LifecycleEnter).Screen)
	}
}

func (m *Model) popScreen() {
	if n := len(m.screenStack); n > 0 {
		if current, ok := m.activeHostedScreenLive(); ok {
			m.storeScreen(current.Lifecycle(m.screenFrame(), screenhost.LifecycleLeave).Screen)
		}
		m.screenStack = m.screenStack[:n-1]
		if next, ok := m.activeHostedScreenLive(); ok {
			m.storeScreen(next.Lifecycle(m.screenFrame(), screenhost.LifecycleFocus).Screen)
		}
		return
	}
	m.popHistory()
}

// navigateToScreen moves the legacy nav cursor onto the requested screen,
// recording the departure point so ctrl+o can restore it. Unknown ids are
// ignored rather than clearing the current route.
func (m *Model) navigateToScreen(id screenhost.ID) {
	if _, ok := screenRegistry.ByID(id); !ok || id == m.navigation {
		return
	}
	m.pushHistory()
	m.navigation = id
}

func (m Model) taskFormDeps() taskform.Deps {
	return taskform.Deps{
		Theme: m.styles.taskEditTheme(),
		Labels: taskform.Labels{
			Title:       m.t("tui.form.label.title"),
			Description: m.t("tui.form.label.description"),
			Priority:    m.t("tui.form.label.priority"),
			Tags:        m.t("tui.form.label.tags"),
			Parent:      m.t("tui.form.label.parent")}}
}

func (m Model) inTaskDetail() bool {
	return len(m.screenStack) > 0 && m.screenStack[len(m.screenStack)-1] == screenhost.TaskDetail
}

func (m Model) inTaskFormBlockerOverlay() bool {
	n := len(m.screenStack)
	return n >= 2 && m.screenStack[n-1] == screenhost.TaskDetail && m.screenStack[n-2] == screenhost.TaskForm &&
		m.taskDetailScreen.State().Mode == taskdetail.ModeBlockers
}

func (m Model) boundTaskDetailScreen() taskdetail.Screen {
	return m.taskDetailScreen.Bind(m.taskDetailDeps())
}

func (m Model) acceptTaskDetailAction(action screenhost.Action) bool {
	return m.inTaskDetail() && action.Generation == m.taskDetailGeneration && action.TaskID > 0
}

func (m *Model) taskDetailPayload(task domain.Task, stack []int64, activity []domain.Event) taskdetail.Payload {
	return taskdetail.Payload{
		Generation: m.taskDetailGeneration,
		Task:       task, Projection: m.taskProjection(m.subtaskPanelWorkflow()), Tasks: m.tasks, Workflow: m.subtaskPanelWorkflow(),
		Dependencies: m.dependencies, Activity: activity,
		Stack: append([]int64(nil), stack...)}
}

func (m *Model) closeTaskDetail(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	stack := m.taskDetailScreen.Payload().Stack
	if len(stack) > 0 {
		parentID := stack[len(stack)-1]
		if parent, ok := m.taskByID(parentID); ok {
			m.openTaskDetail(parent, stack[:len(stack)-1], false)
		}
		return
	}
	m.taskDetailGeneration++
	m.popScreen()
}

func (m *Model) openNestedTaskDetail(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	child, ok := m.taskByID(action.TaskID)
	if !ok {
		return
	}
	payload := m.taskDetailScreen.Payload()
	stack := append(payload.Stack, payload.Task.ID)
	m.openTaskDetail(child, stack, false)
}

func (m *Model) openTaskDetail(task domain.Task, stack []int64, push bool) {
	m.taskDetailGeneration++
	activity, err := m.loadTaskActivity(task.ID)
	if err != nil {
		m.status = err.Error()
	}
	m.taskDetailScreen = taskdetail.New().Bind(m.taskDetailDeps()).Open(m.taskDetailPayload(task, stack, activity), m.screenFrame())
	if push {
		m.pushScreen(screenhost.TaskDetail)
	}
}

func (m Model) taskDetailDeps() taskdetail.Deps {
	return taskdetail.Deps{
		Priorities: m.priorities,
		TaskTags:   m.tagsForTask,
		MovePrompt: m.moveInputPromptForTask}
}

func (m Model) taskProjection(workflow domain.Workflow) taskprojection.Projection {
	return taskprojection.Build(taskprojection.Input{
		Tasks: m.tasks, Workflow: workflow, Dependencies: m.dependencies,
		Comments: m.comments, Priorities: m.priorities})
}

func (m *Model) refreshTaskDetail(action screenhost.Action, status string) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	activity, err := m.loadTaskActivity(action.TaskID)
	if err != nil {
		m.status = err.Error()
		return
	}
	task, ok := m.taskByID(action.TaskID)
	if !ok {
		return
	}
	payload := m.taskDetailPayload(task, m.taskDetailScreen.Payload().Stack, activity)
	m.taskDetailScreen = m.taskDetailScreen.FinishOperation().Replace(payload)
	m.status = status
}

func (m *Model) addTaskDetailComment(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	if _, err := svc.AddComment(m.ctx, contract.AddCommentInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          action.TaskID,
		Body:            action.Value,
		AuthorType:      "human"}); err != nil {
		m.status = err.Error()
		return
	}
	m.refreshTaskDetail(action, m.t("tui.status.saved"))
}

func (m *Model) moveTaskFromDetail(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	if err := m.moveTask(action.TaskID, action.BucketKey); err != nil {
		m.status = err.Error()
		m.taskDetailScreen = m.taskDetailScreen.FinishOperation()
		return
	}
	m.refreshTaskDetail(screenhost.Action{TaskID: m.taskDetailScreen.Payload().Task.ID, Generation: action.Generation}, m.t("tui.status.saved"))
}

func (m *Model) saveTaskDetailBlockers(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	taskFormBlockerOverlay := m.inTaskFormBlockerOverlay()
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	if err := svc.SyncBlockers(m.ctx, contract.SyncBlockersInput{
		ProjectSelector: m.projectSelector(),
		TaskID:          action.TaskID,
		TaskIDs:         action.TaskIDs}); err != nil {
		m.status = err.Error()
		return
	}
	m.refreshTaskDetail(action, m.t("tui.status.blockers_saved"))
	if taskFormBlockerOverlay {
		m.closeTaskFormBlockerOverlay(m.t("tui.status.blockers_saved"))
	}
}

func (m *Model) completeTaskDetailSubtask(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	final := m.subtaskPanelWorkflow().FinalBucketKey()
	if final == "" {
		return
	}
	if err := m.moveTask(action.TaskID, final); err != nil {
		m.status = err.Error()
		return
	}
	m.refreshTaskDetail(screenhost.Action{TaskID: m.taskDetailScreen.Payload().Task.ID, Generation: action.Generation}, m.t("tui.status.saved"))
}

func (m *Model) openTaskDetailComment(action screenhost.Action) {
	if !m.acceptTaskDetailAction(action) {
		return
	}
	for _, event := range m.taskDetailScreen.Activity() {
		if event.ID == action.CommentID && event.EventType == domain.EventTypeComment {
			m.openCommentScreen(eventToComment(event), false)
			return
		}
	}
}

func (m Model) boundDescriptionScreen() description.Screen {
	return m.descriptionReaderScreen
}

func (m Model) boundCommentDetailScreen() commentdetail.Screen {
	return m.commentDetailScreen.Bind(commentdetail.Deps{
		EditTheme: m.styles.multilineFormTheme()})
}

func (m *Model) openDescriptionScreen(task domain.Task) {
	m.descriptionReaderScreen = m.boundDescriptionScreen().Open(task)
	m.pushScreen(screenhost.TaskDescription)
}

func (m *Model) openCommentScreen(comment domain.Comment, fromProject bool) {
	payload := commentdetail.Payload{Comment: comment}
	if fromProject {
		payload.EditDenied, payload.DeleteDenied = m.t("tui.status.permission_denied"), m.t("tui.status.permission_denied")
	} else {
		payload.Editable, payload.EditDenied = m.canEditComment(comment.TaskID)
		payload.Deletable, payload.DeleteDenied = m.canDeleteComment(comment.TaskID)
	}
	m.commentDetailScreen = m.boundCommentDetailScreen().Open(payload)
	m.pushScreen(screenhost.CommentDetail)
}

func (m Model) acceptCommentDetailAction(action screenhost.Action) bool {
	return len(m.screenStack) > 0 && m.screenStack[len(m.screenStack)-1] == screenhost.CommentDetail &&
		action.CommentID > 0 && action.CommentID == m.commentDetailScreen.Payload().Comment.ID
}

func (m *Model) saveCommentDetail(action screenhost.Action) {
	if !m.acceptCommentDetailAction(action) {
		return
	}
	existing := m.commentDetailScreen.Payload().Comment
	tagNames := make([]string, len(existing.Tags))
	for i, tag := range existing.Tags {
		tagNames[i] = tag.Name
	}
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	body := action.Value
	if _, err := svc.EditComment(m.ctx, contract.EditCommentInput{
		ProjectSelector: m.projectSelector(),
		CommentID:       action.CommentID,
		Body:            &body,
		Tags:            tagNames}); err != nil {
		m.commentDetailScreen = m.commentDetailScreen.Apply(commentdetail.Result{CommentID: action.CommentID, Err: err})
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.commentDetailScreen = m.commentDetailScreen.Apply(commentdetail.Result{CommentID: action.CommentID, Err: err})
		m.status = err.Error()
		return
	}
	if action.TaskID > 0 && m.commentParentIsTaskDetail() {
		if err := m.refreshCommentParentTask(action.TaskID); err != nil {
			m.commentDetailScreen = m.commentDetailScreen.Apply(commentdetail.Result{CommentID: action.CommentID, Err: err})
			m.status = err.Error()
			return
		}
	}
	saved := existing
	saved.Body = action.Value
	if m.repos.Comments != nil {
		if row, lookupErr := m.repos.Comments.CommentByID(m.ctx, m.project.ID, action.CommentID); lookupErr == nil {
			saved = row
		}
	}
	m.commentDetailScreen = m.commentDetailScreen.Apply(commentdetail.Result{CommentID: action.CommentID, Comment: saved, Saved: true})
	m.status = m.t("tui.status.saved")
}

func (m *Model) deleteCommentDetail(action screenhost.Action) {
	if !m.acceptCommentDetailAction(action) {
		return
	}
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	if _, err := svc.DeleteComment(m.ctx, contract.DeleteCommentInput{
		ProjectSelector: m.projectSelector(),
		CommentID:       action.CommentID,
		Confirmed:       true}); err != nil {
		m.commentDetailScreen = m.commentDetailScreen.Apply(commentdetail.Result{CommentID: action.CommentID, Err: err})
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if action.TaskID > 0 && m.commentParentIsTaskDetail() {
		if err := m.refreshCommentParentTask(action.TaskID); err != nil {
			m.status = err.Error()
			return
		}
	}
	m.popScreen()
	m.status = fmt.Sprintf(m.t("tui.status.comment_deleted_fmt"), action.CommentID)
}

func (m Model) commentParentIsTaskDetail() bool {
	return len(m.screenStack) >= 2 && m.screenStack[len(m.screenStack)-1] == screenhost.CommentDetail && m.screenStack[len(m.screenStack)-2] == screenhost.TaskDetail
}

func (m *Model) refreshCommentParentTask(taskID int64) error {
	activity, err := m.loadTaskActivity(taskID)
	if err != nil {
		return err
	}
	task, ok := m.taskByID(taskID)
	if !ok {
		return domain.NewError(domain.ErrTaskNotFound, "task not found", map[string]any{"task_id": taskID})
	}
	payload := m.taskDetailPayload(task, m.taskDetailScreen.Payload().Stack, activity)
	m.taskDetailScreen = m.taskDetailScreen.Replace(payload)
	return nil
}

func (m Model) taskFormPriorities() []taskform.PriorityOption {
	options := make([]taskform.PriorityOption, 0, len(m.priorities))
	for _, priority := range m.priorities {
		options = append(options, taskform.PriorityOption{Value: strconv.Itoa(priority.ID), Label: priority.Value})
	}
	return options
}

func isTaskFormAction(kind screenhost.ActionKind) bool {
	switch kind {
	case screenhost.ActionSaveTaskForm, screenhost.ActionCancelTaskForm, screenhost.ActionLookupTaskParent, screenhost.ActionOpenTaskBlockers:
		return true
	}
	return false
}

func (m Model) acceptTaskFormOutcome(outcome screenhost.Outcome) bool {
	screen, ok := outcome.Screen.(taskform.Screen)
	if !ok || len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.TaskForm {
		return false
	}
	payload := screen.Payload()
	if outcome.Action.Generation != m.taskFormGeneration || payload.Generation != m.taskFormGeneration || outcome.Action.TaskFormMode != payload.Mode.String() || outcome.Action.TaskID != payload.TaskID {
		return false
	}
	values := screen.Values()
	return outcome.Action.TaskTitle == values.Title && outcome.Action.TaskDescription == values.Description &&
		outcome.Action.TaskPriority == values.Priority && outcome.Action.TaskTagsCSV == values.TagsCSV && outcome.Action.TaskParent == values.Parent
}

func (m *Model) saveTaskFormAction(_ screenhost.Action) { m.saveTaskForm() }

func (m *Model) cancelTaskForm(_ screenhost.Action) {
	payload := m.taskFormScreen.Payload()
	m.popScreen()
	m.taskFormGeneration++
	if payload.Mode == taskform.Create {
		if payload.CreateParentID != nil {
			if parent, ok := m.taskByID(*payload.CreateParentID); ok {
				m.openTaskView(parent)
				return
			}
		}
		m.closeTaskScreen(m.t("tui.status.cancelled"))
		return
	}
	if task, ok := m.taskByID(payload.TaskID); ok {
		m.openTaskView(task)
		return
	}
	m.closeTaskScreen(m.t("tui.status.cancelled"))
}

func (m *Model) lookupTaskFormParent(action screenhost.Action) {
	result := taskform.LookupResult{Generation: action.Generation, Parent: action.TaskParent}
	id, err := strconv.ParseInt(action.TaskParent, 10, 64)
	if err != nil || id <= 0 {
		result.Err = fmt.Errorf("%s", m.t("tui.taskedit.parent_lookup_invalid"))
	} else if action.TaskFormMode == taskform.Edit.String() && id == action.TaskID {
		result.Err = fmt.Errorf("%s", m.t("tui.taskedit.parent_lookup_self"))
	} else if m.repos.Tasks != nil {
		tasks, listErr := m.repos.Tasks.ListTasks(m.ctx, m.project.ID, domain.TaskFilter{IncludeArchived: true}, m.repos.activeSnapshot())
		if listErr != nil {
			result.Err = listErr
		} else {
			for _, task := range tasks {
				if task.ID == id {
					result.Label = fmt.Sprintf("#%d %s", task.ID, task.Title)
					m.taskFormScreen = m.taskFormScreen.ApplyLookup(result)
					return
				}
			}
			result.Err = fmt.Errorf("%s", m.t("tui.taskedit.parent_lookup_not_found"))
		}
	}
	m.taskFormScreen = m.taskFormScreen.ApplyLookup(result)
}

func (m Model) boundEntityListScreen(screen entitylist.Screen) entitylist.Screen {
	kind := entityKindFromListAction(string(screen.Kind()))
	items := make([]entitylist.Item, m.entityCount(kind))
	for i := range items {
		items[i] = entitylist.Item{
			Slug:   m.entitySlugAt(kind, i),
			Label:  m.entityCardLabel(kind, i),
			Badges: m.entityBadges(kind, i)}
	}
	return screen.Bind(items, nil)
}

func entityKindFromListAction(kind string) entityKind {
	switch entitylist.Kind(kind) {
	case entitylist.KindPersonas:
		return entityKindPersona
	case entitylist.KindSkills:
		return entityKindSkill
	case entitylist.KindTemplates:
		return entityKindTemplate
	case entitylist.KindTags:
		return entityKindTag
	default:
		return entityKindLaw
	}
}

func entityKindFromDetail(kind entitydetail.Kind) entityKind {
	return entityKindFromListAction(string(kind))
}

func entityDetailKind(kind entityKind) entitydetail.Kind {
	switch kind {
	case entityKindPersona:
		return entitydetail.KindPersona
	case entityKindSkill:
		return entitydetail.KindSkill
	case entityKindTemplate:
		return entitydetail.KindTemplate
	default:
		return entitydetail.KindLaw
	}
}

func (m Model) boundEntityDetailScreen() entitydetail.Screen {
	return m.entityDetailScreen
}

func (m *Model) refreshEntityDetailProjection() {
	payload := m.entityDetailScreen.Payload()
	if payload.Slug == "" {
		return
	}
	m.entityDetailScreen = m.boundEntityDetailScreen().Open(m.entityDetailPayload(entityKindFromDetail(payload.Kind), payload.Slug))
}

func (m *Model) openEntityDetail(kind entityKind, slug string) {
	m.entityDetailScreen = m.boundEntityDetailScreen().Open(m.entityDetailPayload(kind, slug))
	m.status = ""
	if len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.EntityDetail {
		m.pushScreen(screenhost.EntityDetail)
	}
}

func (m Model) entityDetailPayload(kind entityKind, slug string) entitydetail.Payload {
	payload := entitydetail.Payload{Kind: entityDetailKind(kind), Slug: slug, Header: fmt.Sprintf("%s · %s", kind.String(), slug), BodyLabel: m.t("tui.kicker.body")}
	row := func(label, value string) entitydetail.Row { return entitydetail.Row{Label: label, Value: value} }
	switch kind {
	case entityKindLaw:
		return m.lawDetailPayload(payload, row, slug)
	case entityKindSkill:
		return m.skillDetailPayload(payload, row, slug)
	case entityKindPersona:
		return m.personaDetailPayload(payload, row, slug)
	case entityKindTemplate:
		return m.templateDetailPayload(payload, row, slug)
	}
	return payload
}

func (m Model) lawDetailPayload(payload entitydetail.Payload, row func(string, string) entitydetail.Row, slug string) entitydetail.Payload {
	law, ok := m.findLawBySlug(slug)
	if !ok {
		payload.NotFound = m.t("tui.empty.law_not_found")
		return payload
	}
	payload.Rows = []entitydetail.Row{row(m.t("tui.row.slug"), law.Key), row(m.t("tui.row.severity"), m.severityStyle(law.Severity).Render(m.severityLabel(law.Severity))), row(m.t("tui.row.source"), law.SourcePath)}
	payload.Body = law.Body
	return payload
}

func (m Model) skillDetailPayload(payload entitydetail.Payload, row func(string, string) entitydetail.Row, slug string) entitydetail.Payload {
	skill, ok := m.findSkillBySlug(slug)
	if !ok {
		payload.NotFound = m.t("tui.empty.skill_not_found")
		return payload
	}
	payload.Rows = []entitydetail.Row{row(m.t("tui.row.slug"), skill.Key), row(m.t("tui.row.name"), skill.Name), row(m.t("tui.row.description"), skill.Description), row(m.t("tui.row.source"), skill.SourcePath)}
	payload.Body = skill.Body
	return payload
}

func (m Model) personaDetailPayload(payload entitydetail.Payload, row func(string, string) entitydetail.Row, slug string) entitydetail.Payload {
	persona, ok := m.findPersonaBySlug(slug)
	if !ok {
		payload.NotFound = m.t("tui.empty.persona_not_found")
		return payload
	}
	skills := strings.Join(persona.SkillKeys, ", ")
	if skills == "" {
		skills = m.styles.hint.Render(m.t("tui.empty.none"))
	}
	payload.Rows = []entitydetail.Row{row(m.t("tui.row.slug"), persona.Key), row(m.t("tui.row.name"), persona.Name), row(m.t("tui.row.description"), persona.Description), row(m.t("tui.row.skills"), skills), row(m.t("tui.row.source"), persona.SourcePath)}
	payload.Body, payload.Extra = persona.Body, m.t("tui.entity_screen.persona_skill_pick")
	return payload
}

func (m Model) templateDetailPayload(payload entitydetail.Payload, row func(string, string) entitydetail.Row, slug string) entitydetail.Payload {
	template, ok := m.findTemplateBySlug(slug)
	if !ok {
		payload.NotFound = m.t("tui.empty.template_not_found")
		return payload
	}
	entity := template.Entity
	if entity == "" {
		entity = m.styles.hint.Render(m.t("tui.empty.none"))
	}
	defaultLabel := m.styles.hint.Render(m.t("tui.empty.none"))
	if template.Default != "" {
		text := template.Default
		if template.ProjectSlug != "" {
			text += "  (project: " + template.ProjectSlug + ")"
		} else {
			text += "  (global)"
		}
		defaultLabel = m.styles.badgeInfo.Render(strings.ToUpper(text))
	}
	payload.Rows = []entitydetail.Row{row(m.t("tui.row.slug"), template.Slug), row(m.t("tui.row.name"), template.Name), row(m.t("tui.row.description"), template.Description), row(m.t("tui.row.entity"), entity), row(m.t("tui.row.default"), defaultLabel), row(m.t("tui.row.source"), template.SourcePath)}
	payload.Body, payload.Extra = template.Body, m.t("tui.entity_screen.template_set_default")
	return payload
}

// screenFooterTokens returns a screen's declarations and appends the standard
// host trailer unless the screen owns its established full footer ordering.
func (m Model) screenFooterTokens(screen screenhost.Screen) []footerToken {
	frame := m.screenFrame()
	bindings := m.filterFooterBindings(screen, screen.Footer(frame))
	tokens := make([]footerToken, 0, len(bindings)+3)
	for _, binding := range bindings {
		tokens = append(tokens, footerToken{key: binding.Key, label: binding.Label, primary: binding.Primary})
	}
	if owner, ok := screen.(screenhost.FooterOwner); ok && owner.OwnsFooter() {
		return tokens
	}
	tokens = append(tokens,
		footerToken{key: keynav.Default.Tops.Primary(), label: m.t("tui.footer.tabs")},
		footerToken{key: keynav.Default.Zones.Primary(), label: m.t("tui.footer.zones")},
		footerToken{key: ",//", label: m.t("tui.footer.subs")},
		m.helpToken(),
	)
	return tokens
}

func (m Model) activeScreenBlocksHostInput() bool {
	screen, ok := m.activeHostedScreen()
	if !ok {
		return false
	}
	blocker, ok := screen.(screenhost.InteractionBlocker)
	return ok && blocker.BlocksHostInput()
}

func (m Model) activeScreenBlocksHelp() bool {
	screen, ok := m.activeHostedScreen()
	if !ok {
		return false
	}
	blocker, ok := screen.(screenhost.HelpBlocker)
	return ok && blocker.BlocksHelp()
}

func (m *Model) blurActiveScreen() {
	screen, ok := m.activeHostedScreen()
	if !ok {
		return
	}
	outcome := screen.Lifecycle(m.screenFrame(), screenhost.LifecycleBlur)
	if outcome.Screen != nil {
		m.storeScreen(outcome.Screen)
	}
}

// activeHostedScreen resolves the extracted screen behind the active route.
func (m Model) activeHostedScreen() (screenhost.Screen, bool) {
	descriptor, ok := m.activeScreenDescriptor()
	if !ok {
		return nil, false
	}
	return m.hostedScreen(descriptor.ID)
}

func (m *Model) activeHostedScreenLive() (screenhost.Screen, bool) {
	descriptor, ok := m.activeScreenDescriptor()
	if !ok {
		return nil, false
	}
	switch descriptor.ID {
	case screenhost.StudioWorkflow, screenhost.StudioCommands, screenhost.StudioPersonas, screenhost.StudioHooks:
		return m.boundStudioScreenLive(descriptor.ID), true
	default:
		return m.hostedScreen(descriptor.ID)
	}
}

// activeHostedScreenOK reports whether the active route is an extracted screen.
func (m Model) activeHostedScreenOK() bool {
	_, ok := m.activeHostedScreen()
	return ok
}

// activeScreenFooterTokens renders the active extracted screen's footer.
func (m Model) activeScreenFooterTokens() []footerToken {
	screen, ok := m.activeHostedScreen()
	if !ok {
		return nil
	}
	return m.screenFooterTokens(screen)
}

// screenHelpGroups returns a registered screen's context-help groups. Used by
// renderHelp to source the extracted screens' help in place, keeping the
// overall group order stable while cohorts migrate one screen at a time.
func (m Model) screenHelpGroups(id screenhost.ID) []screenhost.HelpGroup {
	screen, ok := m.hostedScreen(id)
	if !ok {
		return nil
	}
	return m.filterHostedHelpGroups(screen, screen.Help(m.screenFrame()))
}
