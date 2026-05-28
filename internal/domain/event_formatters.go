package domain

// formatterRegistry maps a stable string id (declared in YAML via formatter:)
// to the Go function that renders an EventRow into a human-friendly summary.
// Populated by event_summary_*.go init() calls in Phase 1; resolved by the
// YAML event-registry loader at boot.
var formatterRegistry = map[string]func(EventRow) string{}

// registerFormatter binds a formatter id to its implementation. Panics on
// duplicate id — every event_summary_*.go init() must claim a unique id so
// the YAML loader has an unambiguous lookup.
func registerFormatter(id string, fn func(EventRow) string) {
	if _, exists := formatterRegistry[id]; exists {
		panic("event_formatters: duplicate formatter id " + id)
	}
	formatterRegistry[id] = fn
}

// ResolveFormatter returns the formatter bound to id and true on hit,
// (nil, false) on miss. The YAML loader uses this to bind EventDef.Formatter
// and panic at boot for unknown ids.
func ResolveFormatter(id string) (func(EventRow) string, bool) {
	fn, ok := formatterRegistry[id]
	return fn, ok
}
