package config

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SettingsSource layer labels for Bundle.Sources.
//
// The loader stamps every leaf-path under `config:` with one of these
// values. The TUI settings viewer (#258) renders the label so users can
// distinguish "kit ships this" from "I changed this".
//
// Layering rule (last-writer-wins):
//   - SourceDefault: leaf value matches the kit baseline (the embedded
//     `defaults/config/<kit.key>.yaml` for the same Kit.Key the bundle
//     declares).
//   - SourceProject: leaf value differs from the kit baseline OR the
//     leaf path is absent from the kit baseline (user-only path).
//   - SourceEnv: an environment variable read by the loader supplied
//     the leaf. Reserved today — no env var is wired into the loader
//     yet. ApplyEnvOverlay is exported so tests and future loaders can
//     promote a path to this layer without touching the core diff.
const (
	SourceDefault = "default"
	SourceProject = "project"
	SourceEnv     = "env"
)

// computeSettingsSources walks `user` and `kit` as parallel YAML node
// trees and returns the per-leaf-path origin map keyed by dot-path. The
// dot-path shape matches the EffectiveTuples accessor: top-level YAML
// section followed by the in-section flattened key (`section.key`,
// `section[N].field`, `section`). Empty mappings and sequences emit a
// single leaf at the path with value `{}` / `[]` (mirrors the accessor's
// leaf rules so every flattened tuple has a matching source entry).
//
// Diff semantics: leaf value strings come from yaml.v3's parsed
// representation; identical strings are SourceDefault. Any divergence
// — including a leaf present in the user tree but absent from the kit
// — promotes to SourceProject. The function never panics on shape
// mismatch (e.g. user reshaped a mapping into a sequence); the divergent
// leaf is simply tagged SourceProject and the walk continues.
//
// When marshaling fails the function returns nil; the loader falls back
// to leaving Bundle.Sources empty and EffectiveTuples renders Source as
// SourceDefault per the accessor's safe-default rule.
func computeSettingsSources(user, kit Settings) map[string]string {
	userTree, err := marshalSettingsTree(user)
	if err != nil {
		return nil
	}
	kitTree, err := marshalSettingsTree(kit)
	if err != nil {
		// Falling through with an empty kit baseline would flag every
		// leaf as SourceProject, which is the wrong default for the
		// canonical case (user file == kit file). Returning nil leaves
		// EffectiveTuples to render Source as SourceDefault — the
		// conservative fallback when the kit YAML is unreadable for any
		// reason (e.g. binary stripped, future override scheme).
		return nil
	}

	out := map[string]string{}
	if userTree == nil {
		return out
	}

	// yaml.v3 mapping content is [k1, v1, k2, v2, ...]. Walk the user
	// tree in declaration order; for each top-level section, descend the
	// paired kit subtree (if any) and emit a source label per leaf path.
	for i := 0; i+1 < len(userTree.Content); i += 2 {
		sectionKey := userTree.Content[i]
		sectionVal := userTree.Content[i+1]
		if sectionKey.Kind != yaml.ScalarNode {
			continue
		}
		section := sectionKey.Value
		kitSection := mappingValueByKey(kitTree, section)
		diffLeaves(sectionVal, kitSection, section, out)
	}
	return out
}

// marshalSettingsTree marshals a Settings value to a YAML mapping node.
// Returns the top-level MappingNode (stripped of the document wrapper)
// so the diff walker can iterate sections without re-unwrapping every
// recursion.
func marshalSettingsTree(s Settings) (*yaml.Node, error) {
	var buf bytes.Buffer
	if err := yaml.NewEncoder(&buf).Encode(s); err != nil {
		return nil, fmt.Errorf("marshal settings: %w", err)
	}
	var root yaml.Node
	if err := yaml.NewDecoder(&buf).Decode(&root); err != nil {
		return nil, fmt.Errorf("decode settings node: %w", err)
	}
	return topMapping(&root), nil
}

// diffLeaves descends paired YAML nodes and emits one source label per
// leaf into `out`. The prefix accumulates the dot-path from the section
// root; the section name is prepended once before recursion so the
// emitted keys match the EffectiveTuples join shape.
func diffLeaves(user, kit *yaml.Node, prefix string, out map[string]string) {
	if user == nil {
		return
	}
	switch user.Kind {
	case yaml.ScalarNode:
		out[prefix] = classifyLeaf(user, kit)
	case yaml.MappingNode:
		if len(user.Content) == 0 {
			out[prefix] = classifyLeaf(user, kit)
			return
		}
		for i := 0; i+1 < len(user.Content); i += 2 {
			k := user.Content[i]
			v := user.Content[i+1]
			if k.Kind != yaml.ScalarNode {
				continue
			}
			kitChild := mappingValueByKey(kit, k.Value)
			diffLeaves(v, kitChild, joinPath(prefix, k.Value), out)
		}
	case yaml.SequenceNode:
		if len(user.Content) == 0 {
			out[prefix] = classifyLeaf(user, kit)
			return
		}
		for i, c := range user.Content {
			kitItem := sequenceItem(kit, i)
			diffLeaves(c, kitItem, joinPath(prefix, fmt.Sprintf("[%d]", i)), out)
		}
	case yaml.AliasNode:
		if user.Alias != nil {
			diffLeaves(user.Alias, kit, prefix, out)
		}
	case yaml.DocumentNode:
		if len(user.Content) == 1 {
			diffLeaves(user.Content[0], kit, prefix, out)
		}
	}
}

// classifyLeaf returns SourceDefault when the user and kit leaves carry
// identical literal representations; otherwise SourceProject. Mappings
// and sequences are compared at their canonical empty markers (`{}` /
// `[]`); deeper structural divergence is already split into per-leaf
// recursion before this fires, so the caller has already drilled to a
// terminal node by the time we classify.
func classifyLeaf(user, kit *yaml.Node) string {
	if kit == nil {
		return SourceProject
	}
	userLit := nodeLiteral(user)
	kitLit := nodeLiteral(kit)
	if userLit == kitLit {
		return SourceDefault
	}
	return SourceProject
}

// nodeLiteral returns a comparable string for a leaf node. Scalars use
// the parsed Value (matching scalarLiteral's contract from the accessor);
// empty mappings and sequences use the same `{}` / `[]` markers the
// accessor emits so the diff agrees with the rendered tuple shape.
func nodeLiteral(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	switch node.Kind {
	case yaml.ScalarNode:
		return scalarLiteral(node)
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			return "{}"
		}
		// Mismatch: a populated mapping reaching classifyLeaf means the
		// counterpart was a scalar or absent. Compare the canonical
		// shape literal so the divergence is unambiguous.
		return "{...}"
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return "[]"
		}
		return "[...]"
	case yaml.AliasNode:
		if node.Alias != nil {
			return nodeLiteral(node.Alias)
		}
	}
	return ""
}

// mappingValueByKey returns the child node paired with `key` in a
// mapping, or nil when the key is absent or the receiver is not a
// mapping. Used to descend the kit tree in lockstep with the user tree.
func mappingValueByKey(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// sequenceItem returns the n-th element of a sequence, or nil when the
// index is out of bounds or the receiver is not a sequence. Used so the
// diff walker can pair user[i] with kit[i] when both layers happen to
// hold the same-length list (e.g. priorities, severities).
func sequenceItem(seq *yaml.Node, i int) *yaml.Node {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	if i < 0 || i >= len(seq.Content) {
		return nil
	}
	return seq.Content[i]
}

// EnvOverlayLookup is the signature ApplyEnvOverlay accepts to discover
// loader-relevant environment variables. Production callers pass
// `os.LookupEnv`; tests inject a deterministic map-backed lookup.
type EnvOverlayLookup func(name string) (string, bool)

// envOverlayBinding is one (env var → settings path) row consulted by
// ApplyEnvOverlay. The registry is empty today — the loader does not
// consume any environment variable to override config — but the type
// is exported so a future task can land a row without churning the
// signature.
type envOverlayBinding struct {
	envVar string
	path   string
}

// envOverlayRegistry is the canonical list ApplyEnvOverlay walks. Keep
// rows sorted by path so the overlay order is deterministic across
// iterations of the loader; today the slice is empty and the function
// is a no-op for production callers, but tests pass a non-empty
// `lookup` plus a synthetic binding to exercise the SourceEnv promotion
// path end-to-end.
var envOverlayRegistry = []envOverlayBinding{}

// ApplyEnvOverlay promotes any settings path supplied by an environment
// variable to SourceEnv in `sources`. The lookup parameter is the only
// way the function discovers values, so test callers can drive the
// promotion deterministically without mutating process state.
//
// The function mutates `sources` in place. Paths not present in the map
// are still recorded so the snapshot accessor sees an env entry even
// when the leaf was absent from the user YAML (the diff walker only
// emits user-tree leaves; env vars may introduce entirely new paths in
// future schemes).
//
// Today the registry is empty; the function returns without writing
// anything for production callers. It is wired into the loader so the
// hook is in place when the first env-var binding lands.
func ApplyEnvOverlay(sources map[string]string, lookup EnvOverlayLookup) {
	if sources == nil || lookup == nil {
		return
	}
	for _, binding := range envOverlayRegistry {
		if _, ok := lookup(binding.envVar); !ok {
			continue
		}
		sources[binding.path] = SourceEnv
	}
}

// applyEnvOverlayWithBindings is the test seam ApplyEnvOverlay's tests
// use to inject a synthetic binding without registering it in the
// production catalog. Production code calls ApplyEnvOverlay (zero-arg
// binding list); the env overlay test passes its own row.
func applyEnvOverlayWithBindings(sources map[string]string, lookup EnvOverlayLookup, bindings []envOverlayBinding) {
	if sources == nil || lookup == nil {
		return
	}
	for _, binding := range bindings {
		if _, ok := lookup(binding.envVar); !ok {
			continue
		}
		sources[binding.path] = SourceEnv
	}
}

// SourceFor returns the layer label recorded for dot-path `path`. Empty
// or missing paths fall back to SourceDefault — the conservative answer
// when the loader had no kit baseline to compare against (test bundles
// constructed via newTwoBucketBundle bypass LoadBundle entirely).
func (b Bundle) SourceFor(path string) string {
	if b.Sources == nil {
		return SourceDefault
	}
	if v, ok := b.Sources[strings.TrimSpace(path)]; ok && v != "" {
		return v
	}
	return SourceDefault
}
