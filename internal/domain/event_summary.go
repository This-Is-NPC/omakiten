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
// Dispatch goes through the YAML-loaded EventDefByKey table — each
// definition's Formatter is the function its event_summary_*.go file
// installed via registerFormatter() at init. Rows whose event_type is
// absent from the registry (or whose definition lacks a formatter) fall
// back to unknownFallback so the Logs grid still renders something
// useful.
func SummarizeEvent(row EventRow) string {
	if def, ok := EventDefByKey[row.EventType]; ok && def.Formatter != nil {
		return def.Formatter(row)
	}
	return unknownFallback(row)
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
