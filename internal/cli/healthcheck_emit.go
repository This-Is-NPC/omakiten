package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// healthCheckEventStore is the narrow port `runUpdate` / `runTUI`
// emit through. Mirrors the subset of *sqlite.Store the emit helpers
// need so tests can stub pass/fail without spinning up a real DB. The
// production wiring is *sqlite.Store, which implements this shape
// out of the box.
type healthCheckEventStore interface {
	RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, payload string) error
}

// healthCheckPayloadCap limits validator_raw_excerpt to keep the
// activity row bounded — broken bundles can produce multi-KB
// validator output and the audit table is not the place to mirror
// the whole envelope. The truncated copy is enough to triage the
// failure; the full output is already in the user-facing error
// envelope.
const healthCheckPayloadCap = 2048

// emitHealthCheckEvent serialises payload to JSON and writes a row to
// the events table via store.RecordEntityEvent. Activity-write
// failures are logged to stderr and swallowed (#369 AC 3): the
// update flow's contract is the binary swap, not the audit trail. A
// nil store is a no-op so direct unit tests can wire `nil` for the
// emit branch they do not exercise.
func emitHealthCheckEvent(ctx context.Context, store healthCheckEventStore, eventType string, payload map[string]any) {
	if store == nil {
		return
	}
	if raw, ok := payload["validator_raw_excerpt"].(string); ok && len(raw) > healthCheckPayloadCap {
		payload["validator_raw_excerpt"] = raw[:healthCheckPayloadCap]
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check emit: marshal %s: %v\n", eventType, err)
		return
	}
	if err := store.RecordEntityEvent(ctx, "system", 0, 0, eventType, string(body)); err != nil {
		fmt.Fprintf(os.Stderr, "health-check emit: record %s: %v\n", eventType, err)
	}
}
