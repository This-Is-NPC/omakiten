package tui

import (
	"encoding/json"
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
// editor is repointed, the registry is replaced, refresh() re-queries the
// task snapshot, and a bundle.swapped event is emitted so the hooks engine
// can react (e.g., surface the orphan-migration notification when the new
// workflow lost buckets the previous one had).
func (m *Model) reloadBundle(path string) error {
	fromWorkflow := m.workflow.Key

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

	if err := m.refresh(); err != nil {
		return err
	}

	if m.project.ID != 0 {
		m.emitBundleSwapped(fromWorkflow, m.workflow.Key)
	}
	return nil
}

// emitBundleSwapped records bundle.swapped with the orphan preview folded
// into the payload. Failures are swallowed: the swap itself already
// succeeded, and a missing event must not crash the TUI mid-render.
func (m *Model) emitBundleSwapped(fromKey, toKey string) {
	report, err := m.repos.Orphans.PreviewOrphanedTasks(m.ctx, m.project.ID)
	if err != nil {
		// Preview failed but the swap already committed. Best we can do
		// is surface the partial state — emit the event with zero orphans
		// and leave the message in m.status for the user.
		report = domain.OrphanReport{WorkflowKey: toKey}
	}
	payload := struct {
		FromWorkflow string              `json:"from_workflow"`
		ToWorkflow   string              `json:"to_workflow"`
		OrphanCount  int                 `json:"orphan_count"`
		HasOrphans   bool                `json:"has_orphans"`
		Groups       []domain.OrphanGroup `json:"groups,omitempty"`
	}{
		FromWorkflow: fromKey,
		ToWorkflow:   toKey,
		OrphanCount:  report.Total,
		HasOrphans:   report.Total > 0,
		Groups:       report.Groups,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = m.repos.Events.RecordEntityEvent(m.ctx, domain.EventEntitySystem, 0, m.project.ID, domain.EventTypeBundleSwapped, string(raw))
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
