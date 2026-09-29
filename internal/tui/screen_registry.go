package tui

import (
	"omakiten/internal/tui/palette"
	"omakiten/internal/tui/screenhost"
)

var registeredScreens = screenDescriptors()
var screenRegistry = mustScreenRegistry(registeredScreens)

func init() { topOrder, subsByTop, topLabels, subLabels = navigationTables() }

func screenDescriptors() []screenhost.DescriptorSpec {
	return []screenhost.DescriptorSpec{
		hostedHomeDescriptor(),
		hostedDescriptor(screenhost.TasksBoard, screenhost.TopTasks, 1, 1, "TASKS", "board", "11", "tui.palette.route.tasks_board", "tasks_board", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.TasksTable, screenhost.TopTasks, 1, 2, "TASKS", "table", "12", "tui.palette.route.tasks_table", "tasks_table", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.TasksGraph, screenhost.TopTasks, 1, 3, "TASKS", "graph", "13", "tui.palette.route.tasks_graph", "tasks_graph", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.TasksPlans, screenhost.TopTasks, 1, 4, "TASKS", "plans", "14", "tui.palette.route.tasks_plans", "tasks_plans", screenhost.ReloadBundle),
		hostedDetailDescriptor(screenhost.PlanGoal, screenhost.TopTasks, "TASKS", "goal", "plan_goal", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.PlanNetwork, screenhost.TopTasks, "TASKS", "network", "plan_network", screenhost.ReloadPlan),
		hostedProjectDescriptor(),
		hostedDetailDescriptor(screenhost.ProjectForm, screenhost.TopTasks, "TASKS", "project form", "project_form", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.ProjectResume, screenhost.TopTasks, "TASKS", "resume", "project_resume", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.ProjectKnowledge, screenhost.TopTasks, "TASKS", "knowledge", "project_knowledge", screenhost.ReloadManual),
		hostedDescriptor(screenhost.StatsGeneral, screenhost.TopStats, 2, 1, "STATS", "general", "21", "tui.palette.route.stats_general", "stats_general", screenhost.ReloadStats),
		hostedDescriptor(screenhost.StatsLogs, screenhost.TopStats, 2, 2, "STATS", "logs", "22", "tui.palette.route.stats_logs", "stats_logs", screenhost.ReloadLogs),
		hostedDescriptor(screenhost.StatsInsights, screenhost.TopStats, 2, 3, "STATS", "insights", "23", "tui.palette.route.stats_insights", "stats_insights", screenhost.ReloadInsights),
		hostedDescriptor(screenhost.StudioWorkflow, screenhost.TopStudio, 3, 1, "STUDIO", "workflow", "31", "tui.palette.route.studio_workflow", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.StudioCommands, screenhost.TopStudio, 3, 2, "STUDIO", "commands", "32", "tui.palette.route.studio_commands", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.StudioPersonas, screenhost.TopStudio, 3, 3, "STUDIO", "personas", "33", "tui.palette.route.studio_personas", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.StudioHooks, screenhost.TopStudio, 3, 4, "STUDIO", "hooks", "34", "tui.palette.route.studio_hooks", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsGeneral, screenhost.TopSettings, 4, 1, "SETTINGS", "general", "41", "tui.palette.route.settings_general", "settings_general", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsLaws, screenhost.TopSettings, 4, 2, "SETTINGS", "laws", "42", "tui.palette.route.settings_laws", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsPersonas, screenhost.TopSettings, 4, 3, "SETTINGS", "personas", "43", "tui.palette.route.settings_personas", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsSkills, screenhost.TopSettings, 4, 4, "SETTINGS", "skills", "44", "tui.palette.route.settings_skills", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsTemplates, screenhost.TopSettings, 4, 5, "SETTINGS", "templates", "45", "tui.palette.route.settings_templates", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsTags, screenhost.TopSettings, 4, 6, "SETTINGS", "tags", "46", "tui.palette.route.settings_tags", "settings_tags", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsGuards, screenhost.TopSettings, 4, 7, "SETTINGS", "guards", "47", "tui.palette.route.settings_guards", "settings_guards", screenhost.ReloadBundle),
		hostedDetailDescriptor(screenhost.EntityDetail, screenhost.TopSettings, "SETTINGS", "detail", "entity_detail", screenhost.ReloadBundle),
		hostedDetailDescriptor(screenhost.ThemePicker, screenhost.TopSettings, "SETTINGS", "theme", "settings_picker", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.ConfigPicker, screenhost.TopSettings, "SETTINGS", "config", "settings_picker", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.SubtaskKitPicker, screenhost.TopSettings, "SETTINGS", "subtask kit", "settings_picker", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.PersonaSkills, screenhost.TopSettings, "SETTINGS", "persona skills", "persona-skills", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.TemplateDefault, screenhost.TopSettings, "SETTINGS", "template default", "template-default", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.TaskForm, screenhost.TopTasks, "TASKS", "task form", "task_form", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.TaskDetail, screenhost.TopTasks, "TASKS", "task detail", "task_detail", screenhost.ReloadTaskActivity),
		hostedDetailDescriptor(screenhost.TaskDescription, screenhost.TopTasks, "TASKS", "description", "task_description", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.CommentDetail, screenhost.TopTasks, "TASKS", "comment", "comment_view", screenhost.ReloadManual),
	}

}

func hostedHomeDescriptor() screenhost.DescriptorSpec {
	return screenhost.DescriptorSpec{
		ID: screenhost.Home, Placement: screenhost.Placement{Top: screenhost.TopHome}, TopLabel: "HOME", SubLabel: "home",
		Factory: newHostedScreenFactory(screenhost.Home), Chrome: screenhost.Chrome{Footer: true, Help: true}, Reload: screenhost.ReloadHome, HelpKeys: []string{"home"},
	}
}

func hostedProjectDescriptor() screenhost.DescriptorSpec {
	return screenhost.DescriptorSpec{
		ID: screenhost.Project, Placement: screenhost.Placement{Top: screenhost.TopTasks}, TopLabel: "TASKS", SubLabel: "project",
		Palette: screenhost.PaletteMetadata{Route: string(screenhost.Project), Code: "15", TitleKey: "tui.palette.route.project"},
		Factory: newHostedScreenFactory(screenhost.Project), Chrome: screenhost.Chrome{Footer: true, Help: true}, Reload: screenhost.ReloadBundle, HelpKeys: []string{"project"},
	}
}

func mustScreenRegistry(registered []screenhost.DescriptorSpec) screenhost.Registry {
	registry, err := screenhost.NewRegistry(registered)
	if err != nil {
		panic(err)
	}
	return registry
}

func standardChrome() screenhost.Chrome {
	return screenhost.Chrome{Navigation: true, Footer: true, Help: true}
}

// hostedDescriptor declares a cyclic route.
func hostedDescriptor(id screenhost.ID, top screenhost.TopID, topOrder, subOrder int, topLabel, subLabel, paletteCode, paletteTitle, helpKey string, reload screenhost.ReloadPolicy) screenhost.DescriptorSpec {
	return screenhost.DescriptorSpec{
		ID:        id,
		Placement: screenhost.Placement{Top: top, TopOrder: topOrder, SubOrder: subOrder, Cyclic: true},
		TopLabel:  topLabel,
		SubLabel:  subLabel,
		Palette:   screenhost.PaletteMetadata{Route: string(id), Code: paletteCode, TitleKey: paletteTitle},
		Factory:   newHostedScreenFactory(id),
		Chrome:    standardChrome(),
		Reload:    reload,
		HelpKeys:  []string{helpKey},
	}
}

func hostedDetailDescriptor(id screenhost.ID, top screenhost.TopID, topLabel, subLabel, helpKey string, reload screenhost.ReloadPolicy) screenhost.DescriptorSpec {
	return screenhost.DescriptorSpec{
		ID: id, Placement: screenhost.Placement{Top: top}, TopLabel: topLabel, SubLabel: subLabel,
		Factory: newHostedScreenFactory(id), Chrome: standardChrome(), Reload: reload, HelpKeys: []string{helpKey},
	}
}

// modelScreenHost presents immutable root context and live hosted screen
// instances to descriptor factories.
type modelScreenHost struct {
	frame screenhost.Frame
	model Model
}

func (h modelScreenHost) Frame() screenhost.Frame { return h.frame }
func (h modelScreenHost) Text(key string) string  { return h.model.t(key) }

func (m Model) screenFrame() screenhost.Frame {
	return screenhost.NewFrame(screenhost.FrameOptions{
		Width:       m.width,
		Height:      m.height,
		ProjectID:   m.project.ID,
		ProjectSlug: m.project.Slug,
		Status:      m.status,
		Focused:     !m.helpOpen && !m.paletteOpen && m.notification == nil,
		ChromeRows:  m.hostChromeRows(),
		Styles:      m.styles.screenStyles(),
		Markdown:    tokensFromTheme(m.theme),
		Text:        m.t,
	})
}

func (m Model) activeScreenDescriptor() (screenhost.DescriptorSpec, bool) {
	if n := len(m.screenStack); n > 0 {
		return screenRegistry.ByID(m.screenStack[n-1])
	}
	return screenRegistry.ByID(m.navigation)
}

func (m Model) activeReloadPolicy() screenhost.ReloadPolicy {
	descriptor, ok := m.activeScreenDescriptor()
	if !ok {
		return screenhost.ReloadBundle
	}
	return descriptor.Reload
}

func navigationTables() ([]screenhost.TopID, map[screenhost.TopID][]screenhost.ID, map[screenhost.TopID]string, map[screenhost.ID]string) {
	var tops []screenhost.TopID
	subs := map[screenhost.TopID][]screenhost.ID{}
	topNames := map[screenhost.TopID]string{}
	subNames := map[screenhost.ID]string{}
	for _, screen := range registeredScreens {
		descriptor := screen
		if !descriptor.Placement.Cyclic {
			continue
		}
		top := descriptor.Placement.Top
		if _, seen := topNames[top]; !seen {
			tops = append(tops, top)
			topNames[top] = descriptor.TopLabel
		}
		sub := descriptor.ID
		subs[top] = append(subs[top], sub)
		subNames[sub] = descriptor.SubLabel
	}
	return tops, subs, topNames, subNames
}

func paletteScreenDescriptors() []palette.ScreenDescriptor {
	var screens []palette.ScreenDescriptor
	for _, descriptor := range screenRegistry.All() {
		if descriptor.Palette.Code == "" {
			continue
		}
		screens = append(screens, palette.ScreenDescriptor{
			Code: descriptor.Palette.Code, Route: palette.Route(descriptor.Palette.Route), TitleKey: descriptor.Palette.TitleKey,
		})
	}
	return screens
}
