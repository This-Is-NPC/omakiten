package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// reloadBundle re-resolves the bundle at path through the BundleCache
// (Phase 3e) and rewires every bundle-derived field on the Model in
// place. The cache's Reload builds a fresh per-project Snapshot,
// fires Store.EmitBundleImported (audit-only — no Store-side
// rotation), and swaps the ProjectRuntime entry so in-flight callers
// keep the previous pointer. On error nothing on the Model changes
// so the caller can surface the failure and let the user retry.
// bundle.swapped continues to fire so the hooks engine can react
// (e.g., the orphan-migration notification when the new workflow
// lost buckets the previous one had).
//
// Repositories.Cache MUST be wired — Phase 3e dropped the
// ConfigService.Import fallback so the TUI never reaches the SQL
// config-write path. Production composition (cli/tui.go) always
// installs the cache; tests use newPickerModel-style helpers that do
// the same.
func (m *Model) reloadBundle(path string) error {
	if m.repos.Cache == nil {
		return fmt.Errorf("tui: Repositories.Cache is required for hot-reload (Phase 3e dropped the ConfigService.Import fallback)")
	}
	fromWorkflow := m.workflow.Key
	fromPath := m.repos.Editor.Path()

	pr, err := m.repos.Cache.Reload(m.ctx, m.repos.ProjectID, path)
	if err != nil {
		return err
	}
	bundle := *pr.Bundle
	registry := pr.EnumRegistry

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
	// Phase 2-bis Round-2 deleted WorkflowService.SetRegistry — the
	// service captures its Snapshot at construction and mutating it via
	// a setter would re-introduce the shared-pointer drift the refactor
	// removed. The cache rebuild produced a fresh WorkflowService bound
	// to the rotated Snapshot; swap the long-lived TUI reference at the
	// same point the rest of the snapshot-derived state rotates.
	m.repos.Workflow = pr.Workflow
	m.notifications = bundle.Notifications
	m.tokenBadgeYellow, m.tokenBadgeRed = bundle.Config.TUI.TokenBadge.Effective()

	if err := m.refresh(); err != nil {
		return err
	}

	suppressed := m.suppressNextSwapEmit
	m.suppressNextSwapEmit = false
	if m.project.ID != 0 && !suppressed {
		m.emitBundleSwapped(fromWorkflow, m.workflow.Key, fromPath)
	}
	return nil
}

// emitBundleSwapped records bundle.swapped with the orphan preview folded
// into the payload. When the report carries orphans, the previous config
// path is stashed on the model so an esc-press on the resulting prompt
// reverts the swap. Failures are swallowed: the swap itself already
// succeeded, and a missing event must not crash the TUI mid-render.
func (m *Model) emitBundleSwapped(fromKey, toKey, fromPath string) {
	report, err := m.repos.Orphans.PreviewOrphanedTasks(m.ctx, m.project.ID, m.repos.activeSnapshot(), m.repos.activePreviousSnapshot())
	if err != nil {
		// Preview failed but the swap already committed. Best we can do
		// is surface the partial state — emit the event with zero orphans
		// and leave the message in m.status for the user.
		report = domain.OrphanReport{WorkflowKey: toKey}
	}
	if report.Total > 0 {
		m.pendingSwapRevertPath = fromPath
	} else {
		m.pendingSwapRevertPath = ""
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

// revertConfigSwap re-imports the previous bundle and rewrites .active to
// match. Called when the user dismisses the orphan-migration notification
// without picking an action — the contract is "no decision = no commit".
// The next reloadBundle on the revert path skips its own bundle.swapped
// emit so the user is not bounced through an immediate second prompt.
func (m *Model) revertConfigSwap() {
	if m.pendingSwapRevertPath == "" {
		return
	}
	path := m.pendingSwapRevertPath
	m.pendingSwapRevertPath = ""
	m.suppressNextSwapEmit = true
	if err := m.reloadBundle(path); err != nil {
		m.status = fmt.Sprintf("Config swap cancel failed: %v", err)
		return
	}
	base := filepath.Base(path)
	if err := paths.SetActiveConfig(base); err != nil {
		m.status = err.Error()
		return
	}
	display := strings.TrimSuffix(base, filepath.Ext(base))
	m.status = fmt.Sprintf("Config swap cancelled — restored %s", display)
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
