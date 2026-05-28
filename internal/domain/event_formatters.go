package domain

// FormatterID is the typed wrapper for the stable string id that ties a
// YAML event-registry entry (definitions.<key>.formatter) to the Go
// function that renders an EventRow into a human-friendly summary.
//
// The wrapper is a `type X string` so consumers can keep using string
// literals where convenient but the formatter-registry boundary checks
// have a distinct compile-time type — accidentally passing an
// EventCategory or a free-form metric tag where a FormatterID is
// expected fails at compile time instead of silently parsing.
type FormatterID string

// formatterRegistry maps a stable FormatterID (declared in YAML via
// formatter:) to the Go function that renders an EventRow into a
// human-friendly summary. Populated by event_summary_*.go init() calls
// in Phase 1; resolved by the YAML event-registry loader at boot.
var formatterRegistry = map[FormatterID]func(EventRow) string{}

// registerFormatter binds a formatter id to its implementation. Panics on
// duplicate id — every event_summary_*.go init() must claim a unique id so
// the YAML loader has an unambiguous lookup.
func registerFormatter(id FormatterID, fn func(EventRow) string) {
	if _, exists := formatterRegistry[id]; exists {
		panic("event_formatters: duplicate formatter id " + string(id))
	}
	formatterRegistry[id] = fn
}

// ResolveFormatter returns the formatter bound to id and true on hit,
// (nil, false) on miss. The YAML loader uses this to bind EventDef.Formatter
// and panic at boot for unknown ids.
func ResolveFormatter(id FormatterID) (func(EventRow) string, bool) {
	fn, ok := formatterRegistry[id]
	return fn, ok
}
