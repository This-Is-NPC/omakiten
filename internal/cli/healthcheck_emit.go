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
//
// The caller's `payload` map is never mutated; when ANY string field
// exceeds `healthCheckPayloadCap` the helper marshals from a locally
// truncated copy. The closed-set design used to target a single
// magic key (`validator_raw_excerpt`); the generalisation lets
// future emissions ship oversized strings without re-editing the
// cap branch (Open/Closed — Martin).
func emitHealthCheckEvent(ctx context.Context, store healthCheckEventStore, eventType string, payload map[string]any) {
	if store == nil {
		return
	}
	out := truncateOversizedPayload(payload, healthCheckPayloadCap)
	body, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check emit: marshal %s: %v\n", eventType, err)
		return
	}
	if err := store.RecordEntityEvent(ctx, "system", 0, 0, eventType, string(body)); err != nil {
		fmt.Fprintf(os.Stderr, "health-check emit: record %s: %v\n", eventType, err)
	}
}

// truncateOversizedPayload returns the original map untouched when no
// string field exceeds cap. Otherwise it returns a defensive shallow
// copy with every oversized string clipped to cap bytes — the caller
// keeps its original payload, the audit-row JSON stays bounded, and
// the helper does not have to enumerate the closed set of keys.
func truncateOversizedPayload(payload map[string]any, cap int) map[string]any {
	var clone map[string]any
	for k, v := range payload {
		s, ok := v.(string)
		if !ok || len(s) <= cap {
			continue
		}
		if clone == nil {
			clone = make(map[string]any, len(payload))
			for kk, vv := range payload {
				clone[kk] = vv
			}
		}
		clone[k] = s[:cap]
	}
	if clone == nil {
		return payload
	}
	return clone
}
