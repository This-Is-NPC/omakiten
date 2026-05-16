package sqlite

import (
	"context"
	"encoding/json"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// EmitBundleImported records a bundle.imported audit event without
// rotating any Store-side snapshot pointer. Phase 2-bis Round-2
// retired the ImportBundle method that previously coupled rotation
// and audit emission; the runtime composition root now owns the
// snapshot pair on ProjectRuntime and calls this method purely to
// keep the bundle.imported hook surface alive.
//
// The payload mirrors the shape downstream listeners expect (path,
// hash, workflow_key + element counts) so existing bundle.imported
// hooks continue to fire on every successful bundle load.
func (s *Store) EmitBundleImported(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error {
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
