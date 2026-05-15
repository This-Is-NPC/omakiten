package sqlite

import (
	"context"
	"encoding/json"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// ImportBundle rotates the in-memory provider snapshot to the given
// bundle and emits a `bundle.imported` event so hooks can react. Phase
// 2 of the config refactor dropped every SQL config table (migration
// 020) — this method writes zero rows beyond the audit event. The
// sourcePath/sourceHash arguments are recorded in the event payload so
// downstream readers can still attribute config state to its file of
// origin.
func (s *Store) ImportBundle(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error {
	if s.providers == nil {
		s.providers = config.NewInMemoryProviders(bundle)
	} else {
		// Clone the prior providers BEFORE swap so OrphanService can
		// resolve task.bucket_id → old key against the previous bundle.
		// The clone is independent of subsequent Swap calls on the
		// active providers, which keeps the orphan diff stable even
		// after the new snapshot is installed.
		s.previousProviders = s.providers.Clone()
		s.providers.Swap(&bundle)
	}
	// Mirror the bundle into the value-typed Snapshot so app services
	// that already consume Phase 2-bis Snapshot see the rotation in
	// lockstep with providers. previousSnapshot follows the same
	// "from the second import onward" gating as previousProviders.
	if s.snapshot != nil {
		s.previousSnapshot = s.snapshot
	}
	s.snapshot = config.BuildSnapshot(bundle)

	workflowKey := bundle.Config.Workflow.Active
	if workflowKey == "" && len(bundle.Workflows) > 0 {
		workflowKey = bundle.Workflows[0].Key
	}
	payload, _ := json.Marshal(map[string]any{
		"path":           sourcePath,
		"hash":           sourceHash,
		"workflow_key":   workflowKey,
		"workflow_count": len(bundle.Workflows),
		"persona_count":  len(bundle.Personas),
		"skill_count":    len(bundle.Skills),
		"law_count":      len(bundle.Laws),
		"template_count": len(bundle.Templates),
	})
	return s.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, 0, domain.EventTypeBundleImported, string(payload))
}
