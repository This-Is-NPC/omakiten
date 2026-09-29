package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/board"
	"omakiten/internal/tui/screens/stats"
)

func TestScreenRegistryRepresentsEveryLegacyRouteOnce(t *testing.T) {
	t.Parallel()

	want := []screenhost.ID{
		screenhost.Home,
		screenhost.TasksBoard,
		screenhost.TasksTable,
		screenhost.TasksGraph,
		screenhost.TasksPlans,
		screenhost.PlanGoal,
		screenhost.PlanNetwork,
		screenhost.Project,
		screenhost.ProjectForm,
		screenhost.ProjectResume,
		screenhost.ProjectKnowledge,
		screenhost.StatsGeneral,
		screenhost.StatsLogs,
		screenhost.StatsInsights,
		screenhost.StudioWorkflow,
		screenhost.StudioCommands,
		screenhost.StudioPersonas,
		screenhost.StudioHooks,
		screenhost.SettingsGeneral,
		screenhost.SettingsLaws,
		screenhost.SettingsPersonas,
		screenhost.SettingsSkills,
		screenhost.SettingsTemplates,
		screenhost.SettingsTags,
		screenhost.SettingsGuards,
		screenhost.EntityDetail,
		screenhost.ThemePicker,
		screenhost.ConfigPicker,
		screenhost.SubtaskKitPicker,
		screenhost.PersonaSkills,
		screenhost.TemplateDefault,
		screenhost.TaskForm,
		screenhost.TaskDetail,
		screenhost.TaskDescription,
		screenhost.CommentDetail,
	}

	descriptors := screenRegistry.All()
	if len(descriptors) != len(want) {
		t.Fatalf("descriptor count = %d, want %d", len(descriptors), len(want))
	}
	seen := make(map[screenhost.ID]int, len(descriptors))
	for _, descriptor := range descriptors {
		seen[descriptor.ID]++
		if descriptor.Factory == nil {
			t.Errorf("descriptor %q has nil factory", descriptor.ID)
			continue
		}
		screen := descriptor.Factory(modelScreenHost{frame: screenhost.NewFrame(screenhost.FrameOptions{}), model: Model{}})
		if screen.ID() != descriptor.ID {
			t.Errorf("descriptor %q factory returned screen %q", descriptor.ID, screen.ID())
		}
	}
	for _, id := range want {
		if seen[id] != 1 {
			t.Errorf("screen %q represented %d times, want once", id, seen[id])
		}
	}
}

func TestScreenRegistryDerivesLegacyNavigationTables(t *testing.T) {
	t.Parallel()

	if !reflect.DeepEqual(topOrder, []screenhost.TopID{screenhost.TopTasks, screenhost.TopStats, screenhost.TopStudio, screenhost.TopSettings}) {
		t.Fatalf("topOrder = %v, want Tasks/Stats/Studio/Settings", topOrder)
	}
	wantSubs := map[screenhost.TopID][]screenhost.ID{
		screenhost.TopTasks:    {screenhost.TasksBoard, screenhost.TasksTable, screenhost.TasksGraph, screenhost.TasksPlans},
		screenhost.TopStats:    {screenhost.StatsGeneral, screenhost.StatsLogs, screenhost.StatsInsights},
		screenhost.TopStudio:   {screenhost.StudioWorkflow, screenhost.StudioCommands, screenhost.StudioPersonas, screenhost.StudioHooks},
		screenhost.TopSettings: {screenhost.SettingsGeneral, screenhost.SettingsLaws, screenhost.SettingsPersonas, screenhost.SettingsSkills, screenhost.SettingsTemplates, screenhost.SettingsTags, screenhost.SettingsGuards},
	}
	if !reflect.DeepEqual(subsByTop, wantSubs) {
		t.Fatalf("subsByTop = %v, want %v", subsByTop, wantSubs)
	}
}

func TestScreenRegistryOwnsNamedDriftMetadata(t *testing.T) {
	t.Parallel()

	cases := map[screenhost.ID]struct {
		code   string
		help   string
		reload screenhost.ReloadPolicy
	}{
		screenhost.TasksPlans:     {code: "14", help: "tasks_plans", reload: screenhost.ReloadBundle},
		screenhost.Project:        {code: "15", help: "project", reload: screenhost.ReloadBundle},
		screenhost.StatsInsights:  {code: "23", help: "stats_insights", reload: screenhost.ReloadInsights},
		screenhost.StudioWorkflow: {code: "31", help: "studio", reload: screenhost.ReloadBundle},
		screenhost.StudioCommands: {code: "32", help: "studio", reload: screenhost.ReloadBundle},
		screenhost.StudioPersonas: {code: "33", help: "studio", reload: screenhost.ReloadBundle},
		screenhost.StudioHooks:    {code: "34", help: "studio", reload: screenhost.ReloadBundle},
		screenhost.SettingsGuards: {code: "47", help: "settings_guards", reload: screenhost.ReloadBundle},
	}
	for id, want := range cases {
		descriptor, ok := screenRegistry.ByID(id)
		if !ok {
			t.Errorf("descriptor %q missing", id)
			continue
		}
		if descriptor.Palette.Code != want.code {
			t.Errorf("%s palette code = %q, want %q", id, descriptor.Palette.Code, want.code)
		}
		if !reflect.DeepEqual(descriptor.HelpKeys, []string{want.help}) {
			t.Errorf("%s help = %v, want [%s]", id, descriptor.HelpKeys, want.help)
		}
		if descriptor.Reload != want.reload {
			t.Errorf("%s reload = %v, want %v", id, descriptor.Reload, want.reload)
		}
	}
}

func TestBoardDescriptorResolvesHostedScreen(t *testing.T) {
	t.Parallel()

	m := Model{styles: newStyles(config.Theme{}), width: 100, height: 40, navigation: screenhost.TasksBoard}
	descriptor, ok := screenRegistry.ByID(screenhost.TasksBoard)
	if !ok {
		t.Fatal("tasks.board descriptor missing")
	}
	m.boardScreen = board.New()
	screen := descriptor.Factory(modelScreenHost{frame: m.screenFrame(), model: m})
	if screen.ID() != screenhost.TasksBoard {
		t.Fatalf("factory screen id = %q", screen.ID())
	}
}

// TestHostedScreenFactoryResolvesTheLiveInstance pins the extraction seam: an
// extracted screen's factory must hand back the instance the root Model holds
// (with its cursor, filter and loaded projection), never a fresh one.
func TestHostedScreenFactoryResolvesTheLiveInstance(t *testing.T) {
	t.Parallel()

	m := Model{styles: newStyles(config.Theme{}), width: 100, height: 40, navigation: screenhost.StatsGeneral}
	m.statsScreen = stats.New()
	m.statsScreen = m.boundStatsScreen().Update(m.screenFrame(), tea.KeyMsg{Type: tea.KeyRight}).Screen.(stats.Screen)

	descriptor, ok := screenRegistry.ByID(screenhost.StatsGeneral)
	if !ok {
		t.Fatal("stats.general descriptor missing")
	}
	screen := descriptor.Factory(modelScreenHost{frame: m.screenFrame(), model: m})
	resolved, ok := screen.(stats.Screen)
	if !ok {
		t.Fatalf("factory returned %T, want the extracted stats screen", screen)
	}
	if resolved.Period() != m.statsScreen.Period() {
		t.Fatalf("factory period = %q, want the live instance's %q", resolved.Period(), m.statsScreen.Period())
	}
	if got, want := screen.View(m.screenFrame()), m.renderCurrentView(); got != want {
		t.Fatalf("hosted factory render drift\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
