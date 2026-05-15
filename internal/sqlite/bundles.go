package sqlite

import (
	"context"
	"encoding/json"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// ImportBundle rotates the Store's per-bundle Snapshot pointer and
// emits a `bundle.imported` event so hooks can react. Phase 2 dropped
// every SQL config table (migration 020); this method writes zero rows
// beyond the audit event. The sourcePath/sourceHash arguments are
// recorded in the event payload so downstream readers can attribute
// config state to its file of origin.
//
// TRANSITIONAL (Phase 2-bis / task #117): the per-project snapshot
// will move to agentruntime.ProjectRuntime in a follow-up commit. At
// that point this method narrows to EmitBundleImported (audit-only)
// and stops mutating Store state.
func (s *Store) ImportBundle(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error {
	// Capture the outgoing snapshot before rotation so the orphan
	// classification flow can resolve task.bucket_id → previous key
	// across the swap. previousSnapshot stays nil through the first
	// import because the empty seed has no useful id↔key mapping.
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
