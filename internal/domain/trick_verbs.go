package domain

// ReservedTrickVerbs is the closed set of verb names the TUI palette
// owns: `nav` resolves to a screen route via the palette ScreenRegistry
// and `op` opens an entity by id. Verb dispatch is hard-coded in the
// palette handler (see internal/tui/palette/handler.go); a user-defined
// hook that filters on these verbs would either silently lose to the
// built-in handler or rebind expected behaviour, so the config validator
// rejects HookSpec.When["verb"] entries that match this set.
//
// Adding a built-in verb means appending it here AND adding the dispatch
// arm in the palette handler. Removing one is a breaking change for any
// user config that relied on the built-in routing.
var ReservedTrickVerbs = []string{"nav", "op"}

// IsReservedTrickVerb reports whether v matches one of the
// ReservedTrickVerbs entries. Used by config validation.
func IsReservedTrickVerb(v string) bool {
	for _, r := range ReservedTrickVerbs {
		if r == v {
			return true
		}
	}
	return false
}
