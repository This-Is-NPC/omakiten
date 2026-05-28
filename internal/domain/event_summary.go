package domain

import (
	"strings"
)

// SummarizeEvent renders a single-line human-readable detail string
// for a Logs inspector row. The output is the right-most column the
// Logs view shows (5-column generic layout: time · type · entity ·
// who · detail) and the value the CLI / MCP shapes serialise under a
// `summary` field.
//
// Constraints:
//   - Pure. No clock, no I/O, no goroutine state — feed it any
//     EventRow and you get the same string back.
//   - Never panics. Malformed Payload JSON falls back to the raw
//     payload string (condensed); a row with an unknown event_type
//     falls back to "<event_type> <payload-condensed>".
//   - Never empty. Every branch returns at least the event_type as a
//     last resort so the Logs grid never shows a blank cell.
//
// Implementation: each known event_type registers a summariser in its
// per-category file's `init()`. The parity tests in
// event_summary_test.go fail when a new event_type lands in event.go
// without a corresponding register() call.
func SummarizeEvent(row EventRow) string {
	if fn, ok := summarizers[row.EventType]; ok {
		return fn(row)
	}
	return unknownFallback(row)
}

// summarizers is the per-event_type dispatch table populated by
// init() in event_summary_<category>.go. Lookup is O(1); the
// duplicate-registration panic in register() guarantees a single
// owner per event_type.
var summarizers = map[string]func(EventRow) string{}

// register installs an arm in the summarizers table. Panics on
// duplicate registration so two categories cannot silently shadow
// one another — the panic surfaces at process startup, not at the
// first emission that hits the conflicting arm.
func register(eventType string, fn func(EventRow) string) {
	if _, dup := summarizers[eventType]; dup {
		panic("duplicate summarizer registered for " + eventType)
	}
	summarizers[eventType] = fn
}

// unknownFallback renders rows whose event_type is not registered.
// Per AC#3 the output is `event_type + " " + payload-condensed` so
// the row is still useful in the Logs grid.
func unknownFallback(row EventRow) string {
	et := strings.TrimSpace(row.EventType)
	if et == "" {
		et = "event"
	}
	payload := strings.TrimSpace(row.Payload)
	if payload == "" {
		return et
	}
	// Re-encode without surrounding whitespace if it parses as JSON;
	// otherwise just collapse runs of whitespace. Either way the
	// result fits on one line.
	if cond, ok := condenseJSON(payload); ok {
		return et + " " + cond
	}
	return et + " " + condenseLine(payload)
}
