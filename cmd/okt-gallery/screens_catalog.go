package main

import (
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/board"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/description"
	"omakiten/internal/tui/screens/entitydetail"
	"omakiten/internal/tui/screens/entitylist"
	"omakiten/internal/tui/screens/graph"
	"omakiten/internal/tui/screens/home"
	"omakiten/internal/tui/screens/insights"
	"omakiten/internal/tui/screens/logs"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/plans"
	"omakiten/internal/tui/screens/project"
	"omakiten/internal/tui/screens/projectresume"
	"omakiten/internal/tui/screens/relationshippicker"
	"omakiten/internal/tui/screens/settings"
	"omakiten/internal/tui/screens/settingspicker"
	"omakiten/internal/tui/screens/stats"
	"omakiten/internal/tui/screens/studio"
	"omakiten/internal/tui/screens/table"
	"omakiten/internal/tui/screens/taskdetail"
	"omakiten/internal/tui/screens/taskform"
)

// screenEntries is one gallery entry per screenhost.ID. Each `screen:` value is
// a STRING LITERAL — the coverage gate's AST walk only counts BasicLit values
// on that key, so a loop that wrote screen: s.id would look uncovered forever.
//
// Scenarios are the recorded golden states; mounting goes through
// screenfixture.Drive — the same path screentest.Record paints with.
func screenEntries() []entry {
	homeScenarios := home.FixtureScenarios()
	boardScenarios := board.FixtureScenarios()
	tableScenarios := table.FixtureScenarios()
	graphScenarios := graph.FixtureScenarios()
	plansList := plans.FixtureScenariosFor(screenhost.TasksPlans)
	planGoal := plans.FixtureScenariosFor(screenhost.PlanGoal)
	planNetwork := plannetwork.FixtureScenarios()
	projectOverview := project.FixtureScenariosFor(screenhost.Project)
	projectForm := project.FixtureScenariosFor(screenhost.ProjectForm)
	projectResume := projectresume.FixtureScenarios()
	statsGeneral := stats.FixtureScenariosFor(screenhost.StatsGeneral)
	logsScenarios := logs.FixtureScenarios()
	insightsScenarios := insights.FixtureScenarios()
	studioCommands := studio.FixtureScenariosFor(screenhost.StudioCommands)
	studioWorkflow := studio.FixtureScenariosFor(screenhost.StudioWorkflow)
	studioPersonas := studio.FixtureScenariosFor(screenhost.StudioPersonas)
	studioHooks := studio.FixtureScenariosFor(screenhost.StudioHooks)
	settingsGeneral := settings.FixtureScenariosFor(screenhost.SettingsGeneral)
	settingsLaws := entitylist.FixtureScenariosFor(screenhost.SettingsLaws)
	settingsPersonas := entitylist.FixtureScenariosFor(screenhost.SettingsPersonas)
	settingsSkills := entitylist.FixtureScenariosFor(screenhost.SettingsSkills)
	settingsTemplates := entitylist.FixtureScenariosFor(screenhost.SettingsTemplates)
	settingsTags := entitylist.FixtureScenariosFor(screenhost.SettingsTags)
	settingsGuards := settings.FixtureScenariosFor(screenhost.SettingsGuards)
	entityDetail := entitydetail.FixtureScenarios()
	themePicker := settingspicker.FixtureScenariosFor(screenhost.ThemePicker)
	configPicker := settingspicker.FixtureScenariosFor(screenhost.ConfigPicker)
	subtaskKit := settingspicker.FixtureScenariosFor(screenhost.SubtaskKitPicker)
	personaSkills := relationshippicker.FixtureScenariosFor(screenhost.PersonaSkills)
	templateDefault := relationshippicker.FixtureScenariosFor(screenhost.TemplateDefault)
	taskForm := taskform.FixtureScenarios()
	taskDetail := taskdetail.FixtureScenarios()
	taskDescription := description.FixtureScenarios()
	commentDetail := commentdetail.FixtureScenarios()

	return []entry{
		{
			name: "home", pkg: "internal/tui/screens/home", screen: "home", minWidth: 80, minHeight: 24,
			title: "Home", desc: "Project picker. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(homeScenarios), scenarios: screenScenarios(homeScenarios),
		},
		{
			name: "tasks.board", pkg: "internal/tui/screens/board", screen: "tasks.board", minWidth: 80, minHeight: 24,
			title: "Tasks › Board", desc: "Kanban carousel. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(boardScenarios), scenarios: screenScenarios(boardScenarios),
		},
		{
			name: "tasks.table", pkg: "internal/tui/screens/table", screen: "tasks.table", minWidth: 80, minHeight: 24,
			title: "Tasks › Table", desc: "Compact task table. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(tableScenarios), scenarios: screenScenarios(tableScenarios),
		},
		{
			name: "tasks.graph", pkg: "internal/tui/screens/graph", screen: "tasks.graph", minWidth: 80, minHeight: 24,
			title: "Tasks › Graph", desc: "Dependency graph. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(graphScenarios), scenarios: screenScenarios(graphScenarios),
		},
		{
			name: "tasks.plans", pkg: "internal/tui/screens/plans", screen: "tasks.plans", minWidth: 80, minHeight: 24,
			title: "Tasks › Plans", desc: "Plan rollups list. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(plansList), scenarios: screenScenarios(plansList),
		},
		{
			name: "plans.goal", pkg: "internal/tui/screens/plans", screen: "plans.goal", minWidth: 80, minHeight: 24,
			title: "Plans › Goal", desc: "Plan goal reader. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(planGoal), scenarios: screenScenarios(planGoal),
		},
		{
			name: "plans.network", pkg: "internal/tui/screens/plannetwork", screen: "plans.network", minWidth: 80, minHeight: 24,
			title: "Plans › Network", desc: "Wave network. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(planNetwork), scenarios: screenScenarios(planNetwork),
		},
		{
			name: "project", pkg: "internal/tui/screens/project", screen: "project", minWidth: 80, minHeight: 24,
			title: "Project", desc: "Project overview. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(projectOverview), scenarios: screenScenarios(projectOverview),
		},
		{
			name: "project.form", pkg: "internal/tui/screens/project", screen: "project.form", minWidth: 80, minHeight: 24,
			title: "Project › Form", desc: "Project form. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(projectForm), scenarios: screenScenarios(projectForm),
		},
		{
			name: "project.resume", pkg: "internal/tui/screens/projectresume", screen: "project.resume", minWidth: 80, minHeight: 24,
			title: "Project › Resume", desc: "Resume projection. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(projectResume), scenarios: screenScenarios(projectResume),
		},
		{
			name: "stats.general", pkg: "internal/tui/screens/stats", screen: "stats.general", minWidth: 80, minHeight: 24,
			title: "Stats › General", desc: "Budget tables. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(statsGeneral), scenarios: screenScenarios(statsGeneral),
		},
		{
			name: "stats.logs", pkg: "internal/tui/screens/logs", screen: "stats.logs", minWidth: 80, minHeight: 24,
			title: "Stats › Logs", desc: "Activity log. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(logsScenarios), scenarios: screenScenarios(logsScenarios),
		},
		{
			name: "stats.insights", pkg: "internal/tui/screens/insights", screen: "stats.insights", minWidth: 80, minHeight: 24,
			title: "Stats › Insights", desc: "Insight cards. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(insightsScenarios), scenarios: screenScenarios(insightsScenarios),
		},
		{
			name: "studio.commands", pkg: "internal/tui/screens/studio", screen: "studio.commands", minWidth: 80, minHeight: 24,
			title: "Studio › Commands", desc: "Command editor. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(studioCommands), scenarios: screenScenarios(studioCommands),
		},
		{
			name: "studio.workflow", pkg: "internal/tui/screens/studio", screen: "studio.workflow", minWidth: 80, minHeight: 24,
			title: "Studio › Workflow", desc: "Buckets with guards as rows. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(studioWorkflow), scenarios: screenScenarios(studioWorkflow),
		},
		{
			name: "studio.personas", pkg: "internal/tui/screens/studio", screen: "studio.personas", minWidth: 80, minHeight: 24,
			title: "Studio › Personas", desc: "Wired persona roster and reverse command index. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(studioPersonas), scenarios: screenScenarios(studioPersonas),
		},
		{
			name: "studio.hooks", pkg: "internal/tui/screens/studio", screen: "studio.hooks", minWidth: 80, minHeight: 24,
			title: "Studio › Hooks", desc: "Configured HookSpecs with per-index hook.executed history. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(studioHooks), scenarios: screenScenarios(studioHooks),
		},
		{
			name: "settings.general", pkg: "internal/tui/screens/settings", screen: "settings.general", minWidth: 80, minHeight: 24,
			title: "Settings › General", desc: "General settings. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsGeneral), scenarios: screenScenarios(settingsGeneral),
		},
		{
			name: "settings.laws", pkg: "internal/tui/screens/entitylist", screen: "settings.laws", minWidth: 80, minHeight: 24,
			title: "Settings › Laws", desc: "Laws grid. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsLaws), scenarios: screenScenarios(settingsLaws),
		},
		{
			name: "settings.personas", pkg: "internal/tui/screens/entitylist", screen: "settings.personas", minWidth: 80, minHeight: 24,
			title: "Settings › Personas", desc: "Personas grid. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsPersonas), scenarios: screenScenarios(settingsPersonas),
		},
		{
			name: "settings.skills", pkg: "internal/tui/screens/entitylist", screen: "settings.skills", minWidth: 80, minHeight: 24,
			title: "Settings › Skills", desc: "Skills grid. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsSkills), scenarios: screenScenarios(settingsSkills),
		},
		{
			name: "settings.templates", pkg: "internal/tui/screens/entitylist", screen: "settings.templates", minWidth: 80, minHeight: 24,
			title: "Settings › Templates", desc: "Templates grid. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsTemplates), scenarios: screenScenarios(settingsTemplates),
		},
		{
			name: "settings.tags", pkg: "internal/tui/screens/entitylist", screen: "settings.tags", minWidth: 80, minHeight: 24,
			title: "Settings › Tags", desc: "Tags grid. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsTags), scenarios: screenScenarios(settingsTags),
		},
		{
			name: "settings.guards", pkg: "internal/tui/screens/settings", screen: "settings.guards", minWidth: 80, minHeight: 24,
			title: "Settings › Guards", desc: "Guard settings. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(settingsGuards), scenarios: screenScenarios(settingsGuards),
		},
		{
			name: "entity.detail", pkg: "internal/tui/screens/entitydetail", screen: "entity.detail", minWidth: 80, minHeight: 24,
			title: "Entity detail", desc: "Entity reader. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(entityDetail), scenarios: screenScenarios(entityDetail),
		},
		{
			name: "settings.theme-picker", pkg: "internal/tui/screens/settingspicker", screen: "settings.theme-picker", minWidth: 80, minHeight: 24,
			title: "Settings › Theme picker", desc: "Theme picker. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(themePicker), scenarios: screenScenarios(themePicker),
		},
		{
			name: "settings.config-picker", pkg: "internal/tui/screens/settingspicker", screen: "settings.config-picker", minWidth: 80, minHeight: 24,
			title: "Settings › Config picker", desc: "Config picker. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(configPicker), scenarios: screenScenarios(configPicker),
		},
		{
			name: "settings.subtask-kit-picker", pkg: "internal/tui/screens/settingspicker", screen: "settings.subtask-kit-picker", minWidth: 80, minHeight: 24,
			title: "Settings › Subtask kit", desc: "Subtask kit picker. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(subtaskKit), scenarios: screenScenarios(subtaskKit),
		},
		{
			name: "settings.persona-skills", pkg: "internal/tui/screens/relationshippicker", screen: "settings.persona-skills", minWidth: 80, minHeight: 24,
			title: "Settings › Persona skills", desc: "Persona skills picker. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(personaSkills), scenarios: screenScenarios(personaSkills),
		},
		{
			name: "settings.template-default", pkg: "internal/tui/screens/relationshippicker", screen: "settings.template-default", minWidth: 80, minHeight: 24,
			title: "Settings › Template default", desc: "Template default picker. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(templateDefault), scenarios: screenScenarios(templateDefault),
		},
		{
			name: "tasks.form", pkg: "internal/tui/screens/taskform", screen: "tasks.form", minWidth: 80, minHeight: 24,
			title: "Tasks › Form", desc: "Task form. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(taskForm), scenarios: screenScenarios(taskForm),
		},
		{
			name: "tasks.detail", pkg: "internal/tui/screens/taskdetail", screen: "tasks.detail", minWidth: 80, minHeight: 24,
			title: "Tasks › Detail", desc: "Task detail. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(taskDetail), scenarios: screenScenarios(taskDetail),
		},
		{
			name: "tasks.description", pkg: "internal/tui/screens/description", screen: "tasks.description", minWidth: 80, minHeight: 24,
			title: "Tasks › Description", desc: "Description reader. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(taskDescription), scenarios: screenScenarios(taskDescription),
		},
		{
			name: "comments.detail", pkg: "internal/tui/screens/commentdetail", screen: "comments.detail", minWidth: 80, minHeight: 24,
			title: "Comments › Detail", desc: "Comment detail. Scenarios are recorded golden states; resize live. Scenarios are the recorded golden states the fit gate already holds, mounted through screenfixture.Drive so the gallery and the golden share one Build path; resize the frame live to watch the same body at other geometries.",
			new: newScreenDemo(commentDetail), scenarios: screenScenarios(commentDetail),
		},
	}
}
