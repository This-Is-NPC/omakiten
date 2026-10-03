package domain

import (
	"sort"
	"strings"
)

// ExternalFormatter renders every external.<name> event: the kit declares
// the names, so one formatter serves them all.
const ExternalFormatter FormatterID = "external"

func init() {
	registerFormatter(ExternalFormatter, summarizeExternal)
}

// summarizeExternal names the event and lists its fields in key order.
func summarizeExternal(row EventRow) string {
	name := strings.TrimPrefix(row.EventType, EventTypeExternalPrefix)
	payload := decodePayload(row.Payload)
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{name}
	for _, key := range keys {
		parts = append(parts, key+"="+condenseLine(readString(payload, key)))
	}
	return strings.Join(parts, " ")
}
