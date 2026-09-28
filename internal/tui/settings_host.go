package tui

import (
	"fmt"

	settingsscreen "omakiten/internal/tui/screens/settings"
)

func (m Model) boundSettingsGeneralScreen() settingsscreen.Screen {
	screen := m.settingsGeneralScreen
	if screen.ID() == "" {
		screen = settingsscreen.NewGeneral()
	}
	return m.boundSettingsScreen(screen)
}

func (m Model) boundSettingsGuardsScreen() settingsscreen.Screen {
	screen := m.settingsGuardsScreen
	if screen.ID() == "" {
		screen = settingsscreen.NewGuards()
	}
	return m.boundSettingsScreen(screen)
}

func (m Model) boundSettingsScreen(screen settingsscreen.Screen) settingsscreen.Screen {
	scope := m.t("tui.settings.runtime.scope_global")
	if m.repos.RepoLocalDir != "" {
		scope = fmt.Sprintf(m.t("tui.settings.runtime.scope_local_fmt"), m.repos.RepoLocalDir)
	}
	return screen.Bind(settingsscreen.Payload{
		Runtime:  settingsscreen.Runtime{Version: m.repos.Version, Scope: scope, ConfigPath: m.repos.ConfigPath, DBPath: m.repos.DBPath},
		Workflow: m.workflow, ThemeKey: m.theme.Key, Languages: m.languages, Snapshot: m.repos.activeSnapshot()}, nil)
}
