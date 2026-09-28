package tui

import (
	"omakiten/internal/tui/palette"
	"omakiten/internal/tui/screenhost"
)

type registeredScreen struct {
	descriptor screenhost.DescriptorSpec
	legacyNav  navState
}

var registeredScreens = legacyScreenDescriptors()
var screenRegistry = mustScreenRegistry(registeredScreens)

func init() {
	topOrder, subsByTop, topLabels, subLabels = legacyNavigationTables()
}

func legacyScreenDescriptors() []registeredScreen {
	return []registeredScreen{
		hostedHomeDescriptor(),
		hostedDescriptor(screenhost.TasksBoard, screenhost.TopTasks, topTasks, subBoard, 1, 1, "TASKS", "board", "11", "tui.palette.route.tasks_board", "tasks_board", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.TasksTable, screenhost.TopTasks, topTasks, subTable, 1, 2, "TASKS", "table", "12", "tui.palette.route.tasks_table", "tasks_table", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.TasksGraph, screenhost.TopTasks, topTasks, subGraph, 1, 3, "TASKS", "graph", "13", "tui.palette.route.tasks_graph", "tasks_graph", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.TasksPlans, screenhost.TopTasks, topTasks, subPlans, 1, 4, "TASKS", "plans", "14", "tui.palette.route.tasks_plans", "tasks_plans", screenhost.ReloadBundle),
		hostedDetailDescriptor(screenhost.PlanGoal, screenhost.TopTasks, topTasks, subPlans, "TASKS", "goal", "plan_goal", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.PlanNetwork, screenhost.TopTasks, topTasks, subPlans, "TASKS", "network", "plan_network", screenhost.ReloadPlan),
		hostedProjectDescriptor(),
		hostedDetailDescriptor(screenhost.ProjectForm, screenhost.TopTasks, topTasks, subBoard, "TASKS", "project form", "project_form", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.ProjectResume, screenhost.TopTasks, topTasks, subBoard, "TASKS", "resume", "project_resume", screenhost.ReloadManual),
		hostedDescriptor(screenhost.StatsGeneral, screenhost.TopStats, topStats, subStatsGeneral, 2, 1, "STATS", "general", "21", "tui.palette.route.stats_general", "stats_general", screenhost.ReloadStats),
		hostedDescriptor(screenhost.StatsLogs, screenhost.TopStats, topStats, subStatsLogs, 2, 2, "STATS", "logs", "22", "tui.palette.route.stats_logs", "stats_logs", screenhost.ReloadLogs),
		hostedDescriptor(screenhost.StatsInsights, screenhost.TopStats, topStats, subStatsInsights, 2, 3, "STATS", "insights", "23", "tui.palette.route.stats_insights", "stats_insights", screenhost.ReloadInsights),
		hostedDescriptor(screenhost.StudioWorkflow, screenhost.TopStudio, topStudio, subStudioWorkflow, 3, 1, "STUDIO", "workflow", "31", "tui.palette.route.studio_workflow", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.StudioCommands, screenhost.TopStudio, topStudio, subStudioCommands, 3, 2, "STUDIO", "commands", "32", "tui.palette.route.studio_commands", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.StudioPersonas, screenhost.TopStudio, topStudio, subStudioPersonas, 3, 3, "STUDIO", "personas", "33", "tui.palette.route.studio_personas", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.StudioHooks, screenhost.TopStudio, topStudio, subStudioHooks, 3, 4, "STUDIO", "hooks", "34", "tui.palette.route.studio_hooks", "studio", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsGeneral, screenhost.TopSettings, topSettings, subSettingsGeneral, 4, 1, "SETTINGS", "general", "41", "tui.palette.route.settings_general", "settings_general", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsLaws, screenhost.TopSettings, topSettings, subSettingsLaws, 4, 2, "SETTINGS", "laws", "42", "tui.palette.route.settings_laws", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsPersonas, screenhost.TopSettings, topSettings, subSettingsPersonas, 4, 3, "SETTINGS", "personas", "43", "tui.palette.route.settings_personas", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsSkills, screenhost.TopSettings, topSettings, subSettingsSkills, 4, 4, "SETTINGS", "skills", "44", "tui.palette.route.settings_skills", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsTemplates, screenhost.TopSettings, topSettings, subSettingsTemplates, 4, 5, "SETTINGS", "templates", "45", "tui.palette.route.settings_templates", "settings_entity", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsTags, screenhost.TopSettings, topSettings, subSettingsTags, 4, 6, "SETTINGS", "tags", "46", "tui.palette.route.settings_tags", "settings_tags", screenhost.ReloadBundle),
		hostedDescriptor(screenhost.SettingsGuards, screenhost.TopSettings, topSettings, subSettingsGuards, 4, 7, "SETTINGS", "guards", "47", "tui.palette.route.settings_guards", "settings_guards", screenhost.ReloadBundle),
		hostedDetailDescriptor(screenhost.EntityDetail, screenhost.TopSettings, topSettings, subSettingsLaws, "SETTINGS", "detail", "entity_detail", screenhost.ReloadBundle),
		hostedDetailDescriptor(screenhost.ThemePicker, screenhost.TopSettings, topSettings, subSettingsGeneral, "SETTINGS", "theme", "settings_picker", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.ConfigPicker, screenhost.TopSettings, topSettings, subSettingsGeneral, "SETTINGS", "config", "settings_picker", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.SubtaskKitPicker, screenhost.TopSettings, topSettings, subSettingsGeneral, "SETTINGS", "subtask kit", "settings_picker", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.PersonaSkills, screenhost.TopSettings, topSettings, subSettingsPersonas, "SETTINGS", "persona skills", "persona-skills", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.TemplateDefault, screenhost.TopSettings, topSettings, subSettingsTemplates, "SETTINGS", "template default", "template-default", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.TaskForm, screenhost.TopTasks, topTasks, subBoard, "TASKS", "task form", "task_form", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.TaskDetail, screenhost.TopTasks, topTasks, subBoard, "TASKS", "task detail", "task_detail", screenhost.ReloadTaskActivity),
		hostedDetailDescriptor(screenhost.TaskDescription, screenhost.TopTasks, topTasks, subBoard, "TASKS", "description", "task_description", screenhost.ReloadManual),
		hostedDetailDescriptor(screenhost.CommentDetail, screenhost.TopTasks, topTasks, subBoard, "TASKS", "comment", "comment_view", screenhost.ReloadManual),
	}

}

func hostedHomeDescriptor() registeredScreen {
	return registeredScreen{descriptor: screenhost.DescriptorSpec{
		ID: screenhost.Home, Placement: screenhost.Placement{Top: screenhost.TopHome}, TopLabel: "HOME", SubLabel: "home",
		Factory: newHostedScreenFactory(screenhost.Home), Chrome: screenhost.Chrome{Footer: true, Help: true}, Reload: screenhost.ReloadHome, HelpKeys: []string{"home"},
	}, legacyNav: navState{top: topHome, sub: subBoard}}
}

func hostedProjectDescriptor() registeredScreen {
	return registeredScreen{descriptor: screenhost.DescriptorSpec{
		ID: screenhost.Project, Placement: screenhost.Placement{Top: screenhost.TopTasks}, TopLabel: "TASKS", SubLabel: "project",
		Palette: screenhost.PaletteMetadata{Route: string(screenhost.Project), Code: "15", TitleKey: "tui.palette.route.project"},
		Factory: newHostedScreenFactory(screenhost.Project), Chrome: screenhost.Chrome{Footer: true, Help: true}, Reload: screenhost.ReloadBundle, HelpKeys: []string{"project"},
	}, legacyNav: navState{top: topTasks, sub: subBoard}}
}

func mustScreenRegistry(registered []registeredScreen) screenhost.Registry {
	descriptors := make([]screenhost.DescriptorSpec, 0, len(registered))
	for _, screen := range registered {
		descriptors = append(descriptors, screen.descriptor)
	}
	registry, err := screenhost.NewRegistry(descriptors)
	if err != nil {
		panic(err)
	}
	return registry
}

func standardChrome() screenhost.Chrome {
	return screenhost.Chrome{Navigation: true, Footer: true, Help: true}
}

// hostedDescriptor registers an EXTRACTED screen: one that implements
// screenhost.Screen in its own package and is resolved from the root Model's
// live instance rather than rendered through the legacy adapter. Every
// extraction cohort converts its rows from legacyDescriptor to this.
func hostedDescriptor(id screenhost.ID, top screenhost.TopID, legacyTop topID, legacySub subID, topOrder, subOrder int, topLabel, subLabel, paletteCode, paletteTitle, helpKey string, reload screenhost.ReloadPolicy) registeredScreen {
	return registeredScreen{descriptor: screenhost.DescriptorSpec{
		ID:        id,
		Placement: screenhost.Placement{Top: top, TopOrder: topOrder, SubOrder: subOrder, Cyclic: true},
		TopLabel:  topLabel,
		SubLabel:  subLabel,
		Palette:   screenhost.PaletteMetadata{Route: string(id), Code: paletteCode, TitleKey: paletteTitle},
		Factory:   newHostedScreenFactory(id),
		Chrome:    standardChrome(),
		Reload:    reload,
		HelpKeys:  []string{helpKey},
	}, legacyNav: navState{top: legacyTop, sub: legacySub}}
}

func hostedDetailDescriptor(id screenhost.ID, top screenhost.TopID, legacyTop topID, legacySub subID, topLabel, subLabel, helpKey string, reload screenhost.ReloadPolicy) registeredScreen {
	return registeredScreen{descriptor: screenhost.DescriptorSpec{
		ID: id, Placement: screenhost.Placement{Top: top}, TopLabel: topLabel, SubLabel: subLabel,
		Factory: newHostedScreenFactory(id), Chrome: standardChrome(), Reload: reload, HelpKeys: []string{helpKey},
	}, legacyNav: navState{top: legacyTop, sub: legacySub}}
}

// legacyScreenHost presents immutable root context and live hosted screen
// instances to descriptor factories.
type legacyScreenHost struct {
	frame screenhost.Frame
	model Model
}

func (h legacyScreenHost) Frame() screenhost.Frame { return h.frame }
func (h legacyScreenHost) Text(key string) string  { return h.model.t(key) }

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

func (m Model) activeScreenDescriptor() (screenhost.Descriptor, bool) {
	if n := len(m.screenStack); n > 0 {
		return screenRegistry.ByID(m.screenStack[n-1])
	}
	if m.onHome() {
		return screenRegistry.ByID(screenhost.Home)
	}
	for _, screen := range registeredScreens {
		if screen.legacyNav == (navState{top: m.top, sub: m.sub}) {
			return screenRegistry.ByID(screen.descriptor.ID)
		}
	}
	return screenhost.Descriptor{}, false
}

func (m Model) activeReloadPolicy() screenhost.ReloadPolicy {
	descriptor, ok := m.activeScreenDescriptor()
	if !ok {
		return screenhost.ReloadBundle
	}
	return descriptor.Reload
}

func legacyNavigationTables() ([]topID, map[topID][]subID, map[topID]string, map[subID]string) {
	var tops []topID
	subs := map[topID][]subID{}
	topNames := map[topID]string{}
	subNames := map[subID]string{}
	for _, screen := range registeredScreens {
		descriptor := screen.descriptor
		if !descriptor.Placement.Cyclic {
			continue
		}
		top := screen.legacyNav.top
		if _, seen := topNames[top]; !seen {
			tops = append(tops, top)
			topNames[top] = descriptor.TopLabel
		}
		sub := screen.legacyNav.sub
		subs[top] = append(subs[top], sub)
		subNames[sub] = descriptor.SubLabel
	}
	return tops, subs, topNames, subNames
}

func legacyNavForScreen(id screenhost.ID) (navState, bool) {
	for _, screen := range registeredScreens {
		if screen.descriptor.ID == id {
			return screen.legacyNav, true
		}
	}
	return navState{}, false
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
