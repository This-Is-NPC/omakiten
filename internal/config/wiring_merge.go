package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// repoLocalWiringFilename is the wiring YAML expected at the root of a
// discovered `.omakiten/` repo-local directory. Loaders read it only when
// it exists; absence falls through to the global wiring untouched.
const repoLocalWiringFilename = "omakiten.yaml"

// readWiringWithRepoLocal reads the user-global wiring at globalPath and,
// when repoLocalDir is non-empty and `<repoLocalDir>/omakiten.yaml` exists,
// merges that overlay into the global wiring by the entry-merge rules
// documented in mergeWiringMaps. The merged YAML is decoded with strict
// known-fields so unknown sections in either layer still fail loudly.
func readWiringWithRepoLocal(globalPath, repoLocalDir string) (wiring, error) {
	baseBytes, err := os.ReadFile(globalPath)
	if err != nil {
		return wiring{}, err
	}
	if repoLocalDir == "" {
		return decodeWiring(baseBytes)
	}
	overlayPath := joinRepoLocalYAML(repoLocalDir)
	overlayBytes, err := os.ReadFile(overlayPath)
	if err != nil {
		if os.IsNotExist(err) {
			return decodeWiring(baseBytes)
		}
		return wiring{}, fmt.Errorf("read repo-local wiring %s: %w", overlayPath, err)
	}
	merged, err := mergeWiringYAML(baseBytes, overlayBytes)
	if err != nil {
		return wiring{}, fmt.Errorf("merge repo-local wiring: %w", err)
	}
	return decodeWiring(merged)
}

func joinRepoLocalYAML(repoLocalDir string) string {
	return filepath.Join(repoLocalDir, repoLocalWiringFilename)
}

func decodeWiring(data []byte) (wiring, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w wiring
	if err := dec.Decode(&w); err != nil {
		return wiring{}, err
	}
	return w, nil
}

// mergeWiringYAML parses base and overlay as YAML maps, applies the
// entry-merge rules in mergeWiringMaps, and re-marshals the result. The
// returned bytes are intended to be decoded strictly into the `wiring`
// struct so unknown fields surface even after the merge.
func mergeWiringYAML(base, overlay []byte) ([]byte, error) {
	baseMap, err := unmarshalYAMLMap(base)
	if err != nil {
		return nil, fmt.Errorf("parse base wiring: %w", err)
	}
	overlayMap, err := unmarshalYAMLMap(overlay)
	if err != nil {
		return nil, fmt.Errorf("parse overlay wiring: %w", err)
	}
	merged := mergeWiringMaps(baseMap, overlayMap)
	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("marshal merged wiring: %w", err)
	}
	return out, nil
}

func unmarshalYAMLMap(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

// mergeWiringMaps applies the entry-merge contract:
//
//   - workflows[]: identity key = `key`. Overlay entries replace base
//     entries with the same key; new entries append.
//   - personas[]: identity key = `slug`. Same shape as workflows.
//   - projects[]: identity key = `slug`. Same shape as workflows.
//   - mcp_commands: top-level map. Overlay key replaces the base entry
//     entirely (no deeper merge inside the per-command spec).
//   - config: deep-merge by nested key. Maps recurse; scalars/slices in
//     overlay replace the base value at that path.
//   - skills / laws / templates: top-level string lists. Union by value
//     (treat the slug itself as the identity).
//   - version, kit, anything else: overlay replaces base when present.
//
// `*_disabled` lists in overlay remove entries from the merged result
// after the overlay-merge pass:
//
//   - workflows_disabled / personas_disabled / projects_disabled: remove
//     entries whose identity matches a listed slug/key.
//   - mcp_commands_disabled: drop map keys.
//   - skills_disabled / laws_disabled / templates_disabled: remove
//     strings.
//
// Disable lists are stripped from the merged output so the strict YAML
// decode does not see them as unknown wiring fields.
func mergeWiringMaps(base, overlay map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}

	disabled := map[string][]string{}
	for key, section := range map[string]string{
		"workflows_disabled":    "workflows",
		"personas_disabled":     "personas",
		"projects_disabled":     "projects",
		"mcp_commands_disabled": "mcp_commands",
		"skills_disabled":       "skills",
		"laws_disabled":         "laws",
		"templates_disabled":    "templates",
	} {
		if raw, ok := overlay[key]; ok {
			disabled[section] = toStringSlice(raw)
			delete(overlay, key)
		}
	}

	for k, v := range overlay {
		switch k {
		case "workflows":
			out[k] = mergeListByIdentity(out[k], v, "key")
		case "personas", "projects":
			out[k] = mergeListByIdentity(out[k], v, "slug")
		case "mcp_commands":
			out[k] = mergeMapsReplaceKey(out[k], v)
		case "config":
			out[k] = deepMergeAny(out[k], v)
		case "skills", "laws", "templates":
			out[k] = unionStringList(out[k], v)
		default:
			out[k] = v
		}
	}

	for section, slugs := range disabled {
		if len(slugs) == 0 {
			continue
		}
		switch section {
		case "workflows":
			out["workflows"] = removeListByIdentity(out["workflows"], "key", slugs)
		case "personas":
			out["personas"] = removeListByIdentity(out["personas"], "slug", slugs)
		case "projects":
			out["projects"] = removeListByIdentity(out["projects"], "slug", slugs)
		case "mcp_commands":
			out["mcp_commands"] = removeMapKeys(out["mcp_commands"], slugs)
		case "skills", "laws", "templates":
			out[section] = removeStringValues(out[section], slugs)
		}
	}

	return out
}

func toStringSlice(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toListOfMaps(raw any) []map[string]any {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func identityOf(item map[string]any, identityKey string) string {
	if raw, ok := item[identityKey]; ok {
		if s, ok := raw.(string); ok {
			return s
		}
	}
	return ""
}

// mergeListByIdentity merges two slices of maps keyed by identityKey.
// Base order is preserved; matching overlay entries replace in place;
// new overlay entries append.
func mergeListByIdentity(baseRaw, overlayRaw any, identityKey string) any {
	base := toListOfMaps(baseRaw)
	overlay := toListOfMaps(overlayRaw)
	if len(base) == 0 && len(overlay) == 0 {
		return baseRaw
	}
	overlayByID := map[string]map[string]any{}
	overlayOrder := []string{}
	for _, item := range overlay {
		id := identityOf(item, identityKey)
		if id == "" {
			continue
		}
		if _, dup := overlayByID[id]; !dup {
			overlayOrder = append(overlayOrder, id)
		}
		overlayByID[id] = item
	}
	out := make([]any, 0, len(base)+len(overlayOrder))
	baseIDs := map[string]struct{}{}
	for _, item := range base {
		id := identityOf(item, identityKey)
		baseIDs[id] = struct{}{}
		if replacement, has := overlayByID[id]; has {
			out = append(out, replacement)
			continue
		}
		out = append(out, item)
	}
	for _, id := range overlayOrder {
		if _, already := baseIDs[id]; already {
			continue
		}
		out = append(out, overlayByID[id])
	}
	return out
}

// mergeMapsReplaceKey returns a map where overlay keys replace base keys
// entirely (no recursive merge of the per-key value), and keys present
// only in base are preserved.
func mergeMapsReplaceKey(baseRaw, overlayRaw any) any {
	base, _ := baseRaw.(map[string]any)
	overlay, _ := overlayRaw.(map[string]any)
	if base == nil && overlay == nil {
		return baseRaw
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// deepMergeAny recursively merges two values when both sides are maps;
// otherwise overlay replaces base. Used for the `config` section so
// nested settings (e.g. config.output.json_minified) merge per-key
// rather than wholesale.
func deepMergeAny(baseRaw, overlayRaw any) any {
	baseMap, baseOK := baseRaw.(map[string]any)
	overlayMap, overlayOK := overlayRaw.(map[string]any)
	if !baseOK || !overlayOK {
		return overlayRaw
	}
	out := map[string]any{}
	for k, v := range baseMap {
		out[k] = v
	}
	for k, v := range overlayMap {
		if existing, has := out[k]; has {
			out[k] = deepMergeAny(existing, v)
			continue
		}
		out[k] = v
	}
	return out
}

// unionStringList returns the deduplicated union of two string lists,
// preserving base order then appending only-overlay entries. Empty/nil
// lists short-circuit.
func unionStringList(baseRaw, overlayRaw any) any {
	base := toStringSlice(baseRaw)
	overlay := toStringSlice(overlayRaw)
	if len(base) == 0 && len(overlay) == 0 {
		return baseRaw
	}
	seen := map[string]struct{}{}
	out := make([]any, 0, len(base)+len(overlay))
	for _, s := range base {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range overlay {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func removeListByIdentity(raw any, identityKey string, drop []string) any {
	list := toListOfMaps(raw)
	if len(list) == 0 {
		return raw
	}
	dropSet := map[string]struct{}{}
	for _, s := range drop {
		dropSet[s] = struct{}{}
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		if _, gone := dropSet[identityOf(item, identityKey)]; gone {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeMapKeys(raw any, drop []string) any {
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return raw
	}
	out := map[string]any{}
	dropSet := map[string]struct{}{}
	for _, s := range drop {
		dropSet[s] = struct{}{}
	}
	for k, v := range m {
		if _, gone := dropSet[k]; gone {
			continue
		}
		out[k] = v
	}
	return out
}

func removeStringValues(raw any, drop []string) any {
	list := toStringSlice(raw)
	if len(list) == 0 {
		return raw
	}
	dropSet := map[string]struct{}{}
	for _, s := range drop {
		dropSet[s] = struct{}{}
	}
	out := make([]any, 0, len(list))
	for _, s := range list {
		if _, gone := dropSet[s]; gone {
			continue
		}
		out = append(out, s)
	}
	return out
}
