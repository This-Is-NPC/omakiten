package sqlutil

// AgentAttributedFilter is the canonical WHERE predicate that scopes an
// `events` aggregation to agent traffic only. Rows with an empty
// `agent_model` are non-agent activity (TUI human input, system
// internals) and would distort any per-model benchmark, so every
// model-attribution query must exclude them with exactly this clause.
//
// This is the single source of truth for the filter: `metrics.go` and
// `insights.go` both reference it instead of hand-writing `agent_model
// != ”` (or the equivalent `<> ”`), so the two query families can
// never drift onto divergent attribution rules. When the events table
// lives behind an alias, wrap it with AgentAttributedFilterFor.
const AgentAttributedFilter = "agent_model != ''"

// AgentAttributedFilterFor returns the attribution filter qualified by
// a table alias, e.g. AgentAttributedFilterFor("r") yields
// `r.agent_model != ”`. An empty alias returns the bare
// AgentAttributedFilter unchanged. Use this in queries that join
// `events` to itself (or to other tables) and therefore must prefix the
// column to disambiguate it.
func AgentAttributedFilterFor(alias string) string {
	if alias == "" {
		return AgentAttributedFilter
	}
	return alias + ".agent_model != ''"
}

// ConditionalCount builds a `SUM(CASE WHEN <predicate> THEN 1 ELSE 0
// END)` projection — the conditional-count idiom that tallies, in a
// single grouped pass, how many rows in each group satisfy a predicate.
// predicate is the raw boolean SQL evaluated per row (e.g.
// "event_type = ?"); any `?` placeholders it carries must be bound by
// the caller, in projection order, ahead of the WHERE-clause args.
//
// This is the single source of truth for the idiom: `metrics.go` and
// `insights.go` both compose their SELECT lists from ConditionalCount
// rather than copy-pasting the CASE expression, so a change to the
// counting shape (e.g. COUNT vs SUM, NULL handling) lands in one place.
func ConditionalCount(predicate string) string {
	return "SUM(CASE WHEN " + predicate + " THEN 1 ELSE 0 END)"
}

// ConditionalCounts maps ConditionalCount over a slice of predicates,
// preserving order. It is a convenience for the common case where a
// query emits one conditional count per metric bucket and the scan loop
// reads the results back in the same order. Returns an empty slice for
// an empty input.
func ConditionalCounts(predicates []string) []string {
	out := make([]string, len(predicates))
	for i, p := range predicates {
		out[i] = ConditionalCount(p)
	}
	return out
}
