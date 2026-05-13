package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// reloadBundle re-imports the bundle at path, then updates every
// bundle-derived field on the Model in place. The DB swap (ConfigService.
// Import) runs first; if it errors nothing on the Model changes so the caller
// can surface the failure and let the user retry. Once Import succeeds the
// editor is repointed, the registry is replaced, and refresh() re-queries the
// task snapshot against the freshly-active workflow.
func (m *Model) reloadBundle(path string) error {
	cfgSvc := app.NewConfigService(m.repos.Config, m.repos.BundleStore)
	bundle, _, registry, err := cfgSvc.Import(m.ctx, path)
	if err != nil {
		return err
	}

	theme, err := loadActiveTheme(bundle, path)
	if err != nil {
		return err
	}

	m.repos.Editor.SetPath(path)
	m.theme = theme
	m.styles = newStyles(theme)
	m.markdown = newMarkdownRenderer(tokensFromTheme(theme))
	m.priorities = append([]config.PriorityDefinition(nil), bundle.Config.EffectivePriorities()...)
	m.severities = append([]config.SeverityDefinition(nil), bundle.Config.EffectiveSeverities()...)
	m.registry = registry
	m.repos.Workflow.SetRegistry(registry)
	m.notifications = bundle.Notifications
	m.tokenBadgeYellow, m.tokenBadgeRed = bundle.Config.TUI.TokenBadge.Effective()

	return m.refresh()
}

// loadActiveTheme resolves the theme yaml referenced by bundle.Config.Theme.
// Active and parses it. Mirrors the CLI's loadActiveThemeFromBundle so the TUI
// can revalidate the theme without depending on cli/.
func loadActiveTheme(bundle config.Bundle, configPath string) (config.Theme, error) {
	root := config.ConfigRootFromYAMLPath(configPath)
	active := bundle.Config.Theme.Active
	customPath := filepath.Join(root, "themes", "custom", active+".yaml")
	defaultPath := filepath.Join(root, "themes", active+".yaml")
	themePath := defaultPath
	if _, err := os.Stat(customPath); err == nil {
		themePath = customPath
	}
	theme, err := config.LoadTheme(themePath)
	if err != nil {
		return config.Theme{}, domain.NewError(domain.ErrConfigInvalid, "theme is invalid", map[string]any{"path": themePath, "error": fmt.Sprint(err)})
	}
	return theme, nil
}
