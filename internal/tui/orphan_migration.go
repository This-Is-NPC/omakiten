package tui

import (
	"fmt"

	"omakiten/internal/contract"
)

// handleOrphanMigrationAction applies the kitten_orphan_migration confirm
// path through operation.Service.MigrateOrphans so TUI shares the same
// preview/confirm contract as CLI. Empty ActionID paths (skip) are
// labeled dismissals — they record nothing and leave tasks on the
// inactive bucket until the user re-triggers.
//
// On migrate it calls MigrateOrphans(confirmed=false) for the preview
// gate, then confirmed=true to apply. Swap-time emitBundleSwapped also
// previews via the facade so the notification can appear before this
// keypress.
func (m *Model) handleOrphanMigrationAction(action ActionMsg) {
	if action.ActionID != "migrate" {
		m.status = fmt.Sprintf(m.t("tui.status.notification_fmt"), action.Slug, action.ActionID)
		return
	}
	if m.tuiSurfaceDenied("orphans.migrate") {
		m.status = m.surfaceDeniedMessage("orphans.migrate")
		return
	}

	svc := m.repos.operationService()
	if svc == nil {
		m.status = fmt.Sprintf(m.t("tui.status.notification_skipped_fmt"), action.ActionID)
		return
	}

	m.emitConfirmationGranted(action)

	selector := contract.ProjectSelector{ProjectID: m.project.ID}
	preview, err := svc.MigrateOrphans(m.ctx, contract.MigrateOrphansInput{
		ProjectSelector: selector,
	})
	if err != nil {
		m.status = err.Error()
		return
	}
	if preview.Report.Total == 0 {
		m.status = fmt.Sprintf(m.t("tui.status.tasks_migrated_fmt"), 0)
		return
	}

	resp, err := svc.MigrateOrphans(m.ctx, contract.MigrateOrphansInput{
		ProjectSelector: selector,
		Confirmed:       true,
	})
	if err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.tasks_migrated_fmt"), resp.Report.Total)
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	}
}
