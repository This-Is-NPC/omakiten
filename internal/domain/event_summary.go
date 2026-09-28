package domain

import (
	"strings"
)

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
