package domain

// CommentOpPolicy is the polymorphic permission value for a single comment
// operation (create / edit / delete). It is the runtime form of a YAML value
// that may be EITHER a bare bool (`create: true`) OR a rule object
//
//	create:
//	  allow: true
//	  require_tags:   [needs-review]
//	  deny_tags:      [locked]
//	  require_any_tag: true
//
// A bare bool `b` is equivalent to the rule object `{allow: b}` — that
// equivalence is what keeps every pre-existing bare-bool comment config
// (the #404 create flag and all #389 edit/delete flags) parsing and
// resolving byte-for-byte unchanged.
//
// Semantics (see Evaluate): Allow is the base verdict (nil == true, the
// documented "no rule = allow"); a false Allow short-circuits to deny. The
// tag predicates further constrain an allowing policy against the operation's
// relevant tag set (create → request payload tags; edit/delete → the target
// comment's stored tags).
//
// Representation note: this type is reused as the value for BOTH the task and
// comment slots of EntityPermission (uniform representation), but only the
// comment side carries rule semantics. Task permissions read Allow only; the
// config validator rejects tag predicates declared under permissions.task.*,
// so task-permission semantics stay plain bool.
type CommentOpPolicy struct {
	// Allow is the base verdict. A nil pointer means "no rule declared at
	// this layer" so the resolver keeps walking the chain; a non-nil pointer
	// is a declared verdict (the layer wins). When the whole chain is nil the
	// implicit fallback is true (no rule = allow).
	Allow *bool `json:"allow,omitempty"`
	// RequireTags denies the op unless EVERY listed tag is present in the
	// evaluated tag set.
	RequireTags []string `json:"require_tags,omitempty"`
	// DenyTags denies the op if ANY listed tag is present.
	DenyTags []string `json:"deny_tags,omitempty"`
	// RequireAnyTag, when true, denies the op if the evaluated tag set is
	// empty (at least one tag of any name must be attached).
	RequireAnyTag *bool `json:"require_any_tag,omitempty"`
}

// declared reports whether this policy carries any rule at all. An undeclared
// (zero) policy behaves as "no rule at this layer" so the resolution chain
// keeps falling through, identical to a nil *bool in the legacy chain.
func (p CommentOpPolicy) declared() bool {
	return p.Allow != nil || len(p.RequireTags) > 0 || len(p.DenyTags) > 0 || p.RequireAnyTag != nil
}

// allowValue resolves the base Allow verdict with the implicit-true fallback.
func (p CommentOpPolicy) allowValue() bool {
	return p.Allow == nil || *p.Allow
}

// Evaluate returns the final allow/deny verdict for this policy against the
// operation's relevant tag set. The base verdict is Allow (default true); a
// false Allow short-circuits to deny before any tag predicate runs. When the
// base allows, the tag predicates are applied in order:
//
//	require_any_tag: true → deny if tags is empty
//	require_tags          → deny unless ALL are present
//	deny_tags             → deny if ANY is present
//
// tags is the set of tag names already normalized by the caller (create →
// payload tags; edit/delete → stored comment tags). A nil/empty set is valid
// input and only trips the require_* predicates.
func (p CommentOpPolicy) Evaluate(tags []string) bool {
	if !p.allowValue() {
		return false
	}
	if p.RequireAnyTag != nil && *p.RequireAnyTag && len(tags) == 0 {
		return false
	}
	present := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		present[t] = struct{}{}
	}
	for _, req := range p.RequireTags {
		if _, ok := present[req]; !ok {
			return false
		}
	}
	for _, deny := range p.DenyTags {
		if _, ok := present[deny]; ok {
			return false
		}
	}
	return true
}

// boolPolicy wraps a plain bool verdict as a fully-declared policy. Used by
// the snapshot mapper for legacy/task bool conversions and by the resolution
// chain's implicit-true terminal.
func boolPolicy(v bool) CommentOpPolicy {
	return CommentOpPolicy{Allow: &v}
}

// resolveCommentPolicy walks the candidate policy pointers in priority order
// and returns the first one that declares a rule (most-specific wins,
// mirroring resolveBool). A nil pointer is "no rule at this layer". When every
// candidate is nil/undeclared the implicit fallback is an allowing policy —
// the documented "no rule = allow" semantics.
func resolveCommentPolicy(candidates ...*CommentOpPolicy) CommentOpPolicy {
	for _, c := range candidates {
		if c != nil && c.declared() {
			return *c
		}
	}
	return boolPolicy(true)
}
