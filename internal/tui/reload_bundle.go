package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// reloadBundle re-resolves the bundle at path through the BundleCache
// (Phase 3e) and rewires every bundle-derived field on the Model in
// place. The cache's Reload builds a fresh per-project Snapshot,
// records the bundle.imported audit event (audit-only — no
// Store-side rotation), and swaps the ProjectRuntime entry so
// in-flight callers
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
	if err := m.applyProjectRuntime(pr, path); err != nil {
		return err
	}

	suppressed := m.suppressNextSwapEmit
	m.suppressNextSwapEmit = false
	if m.project.ID != 0 && !suppressed {
		m.emitBundleSwapped(fromWorkflow, m.workflow.Key, fromPath)
	}
	return nil
}

// reloadBundleIfChanged is the passive hot-reload path used by the TUI's
// refresh tick. It lets the shared BundleCache stat the watched config sources
// and applies the rotated runtime only when Resolve actually rebuilt it.
func (m *Model) reloadBundleIfChanged() (bool, error) {
	if m.repos.Cache == nil || m.repos.ConfigPath == "" {
		return false, nil
	}
	before := m.repos.Cache.Get(m.repos.ProjectID)
	// Intentional asymmetry vs the MCP Service() marker re-resolve
	// (runtime.go): Resolve only re-stats m.repos.ConfigPath (and its
	// watched sources), never the active-profile marker (.active). A TUI
	// launched without --config therefore passively picks up in-place
	// edits to its bundle, but NOT an active-profile SWITCH — that is the
	// AC2 contract (the switch is observed on the next explicit reload,
	// not the refresh tick). Do not "fix" this into a marker re-stat.
	pr, err := m.repos.Cache.Resolve(m.ctx, m.repos.ProjectID, m.repos.ConfigPath)
	if err != nil {
		return false, err
	}
	if pr == nil || pr == before {
		return false, nil
	}
	path := pr.SourcePath
	if path == "" {
		path = m.repos.ConfigPath
	}
	if err := m.applyProjectRuntime(pr, path); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Model) applyProjectRuntime(pr *agentruntime.ProjectRuntime, path string) error {
	if pr == nil || pr.Snapshot == nil {
		return fmt.Errorf("tui: hot-reload returned an empty project runtime")
	}
	snap := pr.Snapshot
	settings := snap.Settings()
	registry := pr.EnumRegistry

	if err := snap.ThemeError(); err != nil {
		return domain.NewError(domain.ErrConfigInvalid, m.t("cli.err.theme_invalid"), map[string]any{
			"active": settings.Theme.Active,
			"error":  err.Error(),
		})
	}
	theme := snap.Theme()

	if m.repos.Editor != nil {
		m.repos.Editor.SetPath(path)
	}
	m.repos.ConfigPath = path
	m.theme = theme
	m.styles = newStyles(theme)
	m.markdown = newMarkdownRenderer(tokensFromTheme(theme))
	m.priorities = snap.Priorities()
	m.severities = snap.Severities()
	m.registry = registry
	// Phase 2-bis Round-2 deleted WorkflowService.SetRegistry — the
	// service captures its Snapshot at construction and mutating it via
	// a setter would re-introduce the shared-pointer drift the refactor
	// removed. The cache rebuild produced a fresh WorkflowService bound
	// to the rotated Snapshot; swap the long-lived TUI reference at the
	// same point the rest of the snapshot-derived state rotates.
	m.repos.Workflow = pr.Workflow
	m.repos.Catalog = snap.Catalog(config.SurfaceTUI)
	m.notifications = snap.Notifications()
	m.languages = settings.EffectiveLanguages()
	m.tokenBadgeYellow, m.tokenBadgeRed = settings.TUI.TokenBadge.Effective()

	if err := m.refresh(); err != nil {
		return err
	}
	if m.top == topSettings && (m.sub == subSettingsGeneral || m.sub == subSettingsGuards) {
		m.refreshSettingsGeneralLines()
	}
	// Rotate the trick-palette registry against the freshly-loaded
	// bundle so config.tricks.nav overrides edited in-session take
	// effect on the next Ctrl+K open. Best-effort: registry build
	// failure leaves the previous registry in place rather than
	// nil-ing it out and breaking palette dispatch.
	if reg, err := buildPaletteRegistry(m.repos); err == nil {
		m.paletteRegistry = reg
	}
	return nil
}

// emitBundleSwapped records bundle.swapped with the orphan preview folded
// into the payload. When the report carries orphans, the previous config
// path is stashed on the model so an esc-press on the resulting prompt
// reverts the swap. Failures are swallowed: the swap itself already
// succeeded, and a missing event must not crash the TUI mid-render.
//
// The preview routes through PreviewOrphanedCascade when either snapshot
// in the pair declares a sub-task kit so the rows shown match what
// `OrphanService.Migrate` would rebind — preview/migrate parity (#301
// review §11557 finding A1). Projects without a sub-task kit keep using
// the legacy `PreviewOrphanedTasks` entrypoint so the pre-cascade
// bundle.swapped payload stays byte-identical.
func (m *Model) emitBundleSwapped(fromKey, toKey, fromPath string) {
	current := m.repos.activeSnapshot()
	previous := m.repos.activePreviousSnapshot()
	var (
		report domain.OrphanReport
		err    error
	)
	if app.CascadeActive(current, previous) {
		report, err = m.repos.Orphans.PreviewOrphanedCascade(m.ctx, m.project.ID, app.NewOrphanCascadePlan(current, previous))
	} else {
		report, err = m.repos.Orphans.PreviewOrphanedTasks(m.ctx, m.project.ID, current, previous)
	}
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
		FromWorkflow string               `json:"from_workflow"`
		ToWorkflow   string               `json:"to_workflow"`
		OrphanCount  int                  `json:"orphan_count"`
		HasOrphans   bool                 `json:"has_orphans"`
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
		m.status = fmt.Sprintf(m.t("tui.status.config_swap_cancel_failed_fmt"), err)
		return
	}
	base := filepath.Base(path)
	if err := paths.SetActiveConfig(base); err != nil {
		m.status = err.Error()
		return
	}
	display := strings.TrimSuffix(base, filepath.Ext(base))
	m.status = fmt.Sprintf(m.t("tui.status.config_swap_cancelled_fmt"), display)
}
