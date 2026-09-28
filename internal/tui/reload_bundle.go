package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/tui/screens/studio"
)

// reloadBundle stages a replacement model and commits it through the runtime port.
// A rejected candidate leaves the active model and its snapshot unchanged.
func (m *Model) reloadBundle(path string) error {
	if m.repos.Cache == nil {
		return fmt.Errorf("tui: Repositories.Cache is required for hot-reload")
	}
	fromWorkflow := m.workflow.Key
	fromPath := m.repos.Editor.Path()
	before := *m
	before.studioApplyDiff = nil
	_, err := m.repos.Cache.ApplyView(m.ctx, m.repos.ProjectID, path, func(pr *contract.RuntimeView) (func() error, error) {
		staged := before
		if err := staged.applyProjectRuntime(pr, path); err != nil {
			return nil, err
		}
		return func() error {
			*m = staged
			return nil
		}, nil
	})
	if err != nil {
		m.studioApplyDiff = nil
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
	before := m.repos.Cache.View(m.repos.ProjectID)
	modelBefore := *m
	modelBefore.studioApplyDiff = nil
	// Intentional asymmetry vs the agent Service() marker re-resolve
	// (runtime.go): Resolve only re-stats m.repos.ConfigPath (and its
	// watched sources), never the active-profile marker (.active). A TUI
	// launched without --config therefore passively picks up in-place
	// edits to its bundle, but NOT an active-profile SWITCH — that is the
	// AC2 contract (the switch is observed on the next explicit reload,
	// not the refresh tick). Do not "fix" this into a marker re-stat.
	_, changed, err := m.repos.Cache.ResolveApplyView(m.ctx, m.repos.ProjectID, m.repos.ConfigPath, func(pr *contract.RuntimeView) (func() error, error) {
		path := pr.SourcePath
		if path == "" {
			path = m.repos.ConfigPath
		}
		staged := modelBefore
		if err := staged.applyProjectRuntime(pr, path); err != nil {
			return nil, err
		}
		return func() error {
			*m = staged
			return nil
		}, nil
	})
	if err != nil {
		m.studioApplyDiff = nil
		return false, err
	}
	return changed && m.repos.Cache.View(m.repos.ProjectID) != before, nil
}

func (m *Model) applyProjectRuntime(pr *contract.RuntimeView, path string) error {
	if pr == nil || pr.Snapshot == nil {
		return fmt.Errorf("tui: hot-reload returned an empty project runtime")
	}
	snap := pr.Snapshot
	settings := snap.Settings()
	registry := pr.EnumRegistry

	if err := snap.ThemeError(); err != nil {
		return domain.NewError(domain.ErrConfigInvalid, m.t("cli.err.theme_invalid"), map[string]any{
			"active": settings.Theme.Active,
			"error":  err.Error()})
	}
	theme := snap.Theme()

	m.studioRuntimeGeneration++
	m.studioScreen = studio.New()
	m.repos.runtimeOverride = pr
	m.repos.Editor = pr.Editor
	if m.repos.Editor != nil && path != "" {
		m.repos.Editor.SetPath(path)
	}
	m.repos.ConfigPath = path
	m.theme = theme
	m.styles = newStyles(theme)
	m.priorities = snap.Priorities()
	m.severities = snap.Severities()
	m.registry = registry
	m.repos.Catalog = snap.Catalog(config.SurfaceTUI)
	m.notifications = snap.Notifications()
	m.languages = settings.EffectiveLanguages()
	m.tokenBadgeYellow, m.tokenBadgeRed = settings.TUI.TokenBadge.Effective()

	if err := m.refresh(); err != nil {
		return err
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

// clearProjectRuntime removes every bundle-derived handle when a project has
// no resolved runtime. In particular, retaining the previous Editor here
// would let Studio open and apply the previous project's draft.
func (m *Model) clearProjectRuntime() {
	m.studioRuntimeGeneration++
	m.studioScreen = studio.New()
	m.repos.runtimeOverride = nil
	m.repos.Editor = nil
	m.repos.ConfigPath = ""
	m.repos.Catalog = nil
	m.theme = config.Theme{}
	m.styles = newStyles(m.theme)
	m.priorities = nil
	m.severities = nil
	m.registry = domain.NewEnumRegistry(nil, nil)
	m.notifications = nil
	m.languages = config.LanguageSettings{}
	m.tokenBadgeYellow, m.tokenBadgeRed = 0, 0
}

// emitBundleSwapped records bundle.swapped with the orphan preview folded
// into the payload. When the report carries orphans, the previous config
// path is stashed on the model so an esc-press on the resulting prompt
// reverts the swap. Failures are swallowed: the swap itself already
// succeeded, and a missing event must not crash the TUI mid-render.
//
// The preview prefers operation.Service.MigrateOrphans(confirmed=false) so
// the TUI shares the facade contract with CLI. When the facade is
// unavailable (test fixtures without ProjectRuntime.Service) it falls
// back to the Orphans repository: PreviewOrphanedCascade when either
// snapshot declares a sub-task kit, otherwise PreviewOrphanedTasks —
// preview/migrate parity (#301 review §11557 finding A1).
func (m *Model) emitBundleSwapped(fromKey, toKey, fromPath string) {
	studioDiff := m.studioApplyDiff
	m.studioApplyDiff = nil
	report, err := m.previewOrphanReport(toKey)
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
		StudioDiff   []string             `json:"studio_diff,omitempty"`
	}{
		FromWorkflow: fromKey,
		ToWorkflow:   toKey,
		OrphanCount:  report.Total,
		HasOrphans:   report.Total > 0,
		Groups:       report.Groups,
		StudioDiff:   studioDiff}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = m.repos.Events.RecordEntityEvent(m.ctx, domain.EventEntitySystem, 0, m.project.ID, domain.EventTypeBundleSwapped, string(raw))
}

// previewOrphanReport runs the orphan preview used by bundle.swapped.
// Facade path first; repository fallback keeps lightweight fixtures working.
func (m *Model) previewOrphanReport(toKey string) (domain.OrphanReport, error) {
	if svc := m.repos.operationService(); svc != nil {
		resp, err := svc.MigrateOrphans(m.ctx, contract.MigrateOrphansInput{
			ProjectSelector: contract.ProjectSelector{ProjectID: m.project.ID}})
		if err != nil {
			return domain.OrphanReport{}, err
		}
		report := resp.Report
		if report.WorkflowKey == "" {
			report.WorkflowKey = toKey
		}
		return report, nil
	}
	return domain.OrphanReport{WorkflowKey: toKey}, nil
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
	if m.repos.Editor == nil {
		m.status = "active config editor is unavailable"
		return
	}
	if err := paths.SetActiveConfigInDir(m.repos.Editor.ConfigDir(), base); err != nil {
		m.status = err.Error()
		return
	}
	display := strings.TrimSuffix(base, filepath.Ext(base))
	m.status = fmt.Sprintf(m.t("tui.status.config_swap_cancelled_fmt"), display)
}
