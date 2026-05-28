package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// EffectiveTuple is one row of the flattened effective-configuration view.
// The TUI settings viewer (#258) consumes a slice of these to render the
// per-key audit list under screen `03 //settings`.
//
// Section is the top-level YAML block (the outermost mapping key inside
// `config:` — e.g. `mcp`, `context`, `theme`, `views`). Key is the
// dot-path within the section (e.g. `recent_comment_limit`, or
// `surface.padding.left` for nested blocks; numeric indexes are appended
// as `[0]` for sequences). Value is the canonical YAML literal for the
// scalar at that path — booleans render as `true`/`false`, numerics keep
// their natural representation, strings are emitted verbatim without
// surrounding quotes (the renderer is free to add quotes for display).
//
// Source is the per-leaf origin label (SourceDefault / SourceProject /
// SourceEnv) the LoadBundle merge stamped into Bundle.Sources. The
// accessor populates it from Snapshot.SourceFor; bundles built without
// LoadBundle (test fixtures) fall back to SourceDefault so every row
// carries a non-empty label. The TUI settings viewer (#258) renders
// this column directly.
type EffectiveTuple struct {
	Section string
	Key     string
	Value   string
	Source  string
}

// EffectiveTuples returns a deterministic, ordered slice covering every
// scalar reachable from Snapshot.Settings(). Top-level sections follow
// the YAML key order of the marshaled Settings (which matches the
// Settings struct field declaration order); within a section, dot-path
// keys are sorted ascending for stable rendering across runs.
//
// The flattener is intentionally exhaustive: maps recurse with
// `parent.child` keys, sequences recurse with `parent[N]` indices, and
// scalars emit at the leaf. Empty mappings and empty sequences are
// recorded explicitly (value `{}` and `[]` respectively) so a zero-valued
// block stays visible in the viewer instead of silently dropping.
//
// The accessor walks a yaml.v3 node tree so any field with a `yaml:"-"`
// tag (e.g. ActiveTheme, Warnings) is automatically excluded — the
// snapshot exposes only the user-facing configuration surface.
func (s *Snapshot) EffectiveTuples() []EffectiveTuple {
	if s == nil {
		return nil
	}
	raw, err := yaml.Marshal(s.settings)
	if err != nil {
		// Settings is a plain struct with yaml-friendly types; a marshal
		// failure would mean a schema regression. Return nil rather than
		// panic so the TUI shows an empty viewer and the loader-level
		// validator surfaces the underlying error elsewhere.
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil
	}
	doc := topMapping(&root)
	if doc == nil {
		return nil
	}

	out := make([]EffectiveTuple, 0, 32)
	// yaml.v3 mapping content is [k1, v1, k2, v2, ...]. Iterate pairs
	// in declaration order so top-level section order matches the
	// Settings struct field order.
	for i := 0; i+1 < len(doc.Content); i += 2 {
		sectionKey := doc.Content[i]
		sectionVal := doc.Content[i+1]
		if sectionKey.Kind != yaml.ScalarNode {
			continue
		}
		section := sectionKey.Value
		rows := flattenNode(sectionVal, "")
		// Within a section, sort by dot-path so render order is stable
		// regardless of yaml.v3 traversal ordering or future struct
		// reordering.
		sort.SliceStable(rows, func(a, b int) bool {
			return rows[a].key < rows[b].key
		})
		for _, r := range rows {
			out = append(out, EffectiveTuple{
				Section: section,
				Key:     r.key,
				Value:   r.value,
				Source:  s.SourceFor(joinPath(section, r.key)),
			})
		}
	}
	return out
}

// flatRow is the internal carrier for flattenNode — a (key, value)
// pair that the EffectiveTuples builder lifts into the public tuple
// shape with the section name attached.
type flatRow struct {
	key   string
	value string
}

// flattenNode walks a yaml.v3 node tree and emits one flatRow per
// scalar leaf. The prefix accumulates the dot-path from the section
// root; empty prefix means the leaf sits directly under the section
// (the key becomes the empty string and the renderer treats the value
// as the section's own scalar).
func flattenNode(node *yaml.Node, prefix string) []flatRow {
	if node == nil {
		return nil
	}
	// yaml.v3 wraps the document root in a DocumentNode; unwrap once.
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		return flattenNode(node.Content[0], prefix)
	}
	switch node.Kind {
	case yaml.ScalarNode:
		return []flatRow{{key: prefix, value: scalarLiteral(node)}}
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			return []flatRow{{key: prefix, value: "{}"}}
		}
		out := make([]flatRow, 0, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				continue
			}
			out = append(out, flattenNode(v, joinPath(prefix, k.Value))...)
		}
		return out
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return []flatRow{{key: prefix, value: "[]"}}
		}
		out := make([]flatRow, 0, len(node.Content))
		for i, c := range node.Content {
			out = append(out, flattenNode(c, joinPath(prefix, fmt.Sprintf("[%d]", i)))...)
		}
		return out
	case yaml.AliasNode:
		if node.Alias != nil {
			return flattenNode(node.Alias, prefix)
		}
	}
	return nil
}

// joinPath glues a dot-path together, skipping the separator when the
// running prefix is empty (so the section's first segment doesn't pick
// up a leading dot) and when the next segment is a bracketed index (so
// `tag_synonyms[0]` reads naturally rather than `tag_synonyms.[0]`).
func joinPath(prefix, segment string) string {
	if prefix == "" {
		return segment
	}
	if strings.HasPrefix(segment, "[") {
		return prefix + segment
	}
	return prefix + "." + segment
}

// scalarLiteral returns the canonical YAML literal for a scalar node.
// The yaml.v3 parser already strips quoting and resolves the value to
// its native representation in node.Value; we surface that verbatim.
// Tagged scalars (!!null, !!bool, !!int, !!float, !!str) all flow
// through the same Value field — the tag governs how the literal would
// be re-parsed, not how it is rendered for the viewer. For null we
// render `null` explicitly so the viewer never shows an empty cell that
// reads as "missing" instead of "explicitly null".
func scalarLiteral(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Tag == "!!null" || (node.Value == "" && node.Tag == "") {
		return "null"
	}
	return node.Value
}

// topMapping resolves the outermost mapping node of a freshly-unmarshaled
// yaml document. The Settings struct always marshals to a mapping at the
// top, so this is a documented invariant rather than a guess; the helper
// exists so the EffectiveTuples caller stays free of the
// DocumentNode→MappingNode unwrap boilerplate.
func topMapping(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil
		}
		return topMapping(root.Content[0])
	}
	if root.Kind == yaml.MappingNode {
		return root
	}
	return nil
}

// EffectiveSectionKeys returns the deterministic list of top-level YAML
// sections present in the flattened tuples — handy for the TUI viewer to
// allocate column headers without re-scanning EffectiveTuples() on every
// frame. Order matches EffectiveTuples (Settings struct field order).
func (s *Snapshot) EffectiveSectionKeys() []string {
	tuples := s.EffectiveTuples()
	if len(tuples) == 0 {
		return nil
	}
	out := make([]string, 0, 8)
	seen := map[string]bool{}
	for _, t := range tuples {
		if seen[t.Section] {
			continue
		}
		seen[t.Section] = true
		out = append(out, t.Section)
	}
	return out
}

// effectiveTuplePathsForTest is an internal helper that returns the
// `section.key` paths in order. Exists to make the completeness test
// readable without re-implementing the join.
func (s *Snapshot) effectiveTuplePathsForTest() []string {
	tuples := s.EffectiveTuples()
	out := make([]string, len(tuples))
	for i, t := range tuples {
		out[i] = joinPath(t.Section, t.Key)
	}
	return out
}

// String renders the tuple in a single line for debugging. The format is
// `section.key = value` (or `section = value` for scalar sections); the
// renderer in the TUI uses the struct fields directly rather than this
// helper, but it keeps debug printf output legible.
func (t EffectiveTuple) String() string {
	var b strings.Builder
	b.WriteString(joinPath(t.Section, t.Key))
	b.WriteString(" = ")
	b.WriteString(t.Value)
	if t.Source != "" {
		b.WriteString(" (")
		b.WriteString(t.Source)
		b.WriteString(")")
	}
	return b.String()
}
