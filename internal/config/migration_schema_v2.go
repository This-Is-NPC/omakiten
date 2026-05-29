package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersionV2 is the schema version stamped on personas and skills by
// the v1→v2 migrator (task #268).
const SchemaVersionV2 = 2

// migrateSchemaV2 upgrades every <rootDir>/config/*.yaml from the schema-v1
// persona/skill model to schema v2. It mirrors migrateSchemaDefaults: walks
// the config dir, rewrites each profile in place through the yaml.Node API so
// every unrelated line and comment survives, and is idempotent — a profile
// already at v2 reads through to a byte-identical write that is skipped.
//
// Per-file transform (see migrateSchemaV2InFile):
//   - infers persona.skill_repertoire as the union of mcp_commands[].skills
//     across all commands that bind the persona;
//   - stamps persona.schema_version: 2;
//   - drops the deprecated top-level shared_skill_pool and preset_variants
//     keys if present.
func migrateSchemaV2(rootDir string) error {
	configDir := filepath.Join(rootDir, "config")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", configDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			continue
		}
		yamlPath := filepath.Join(configDir, name)
		if err := migrateSchemaV2InFile(yamlPath); err != nil {
			return err
		}
	}
	return nil
}

// migrateSchemaV2InFile applies the v1→v2 transform to a single yaml profile.
// Returns nil (no-op) on a file that is missing, unparseable, or already at
// v2 — the load path surfaces parse errors with full context elsewhere, and a
// migration must never block the upgrade on an unrelated authoring mistake.
func migrateSchemaV2InFile(yamlPath string) error {
	raw, err := readFileBounded(yamlPath, MaxWiringFileBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil
	}
	doc := documentMap(&root)
	if doc == nil {
		return nil
	}

	// 1. Drop deprecated top-level keys.
	changed := removeMapEntry(doc, "shared_skill_pool")
	if removeMapEntry(doc, "preset_variants") {
		changed = true
	}

	// 2. Build persona → union(skills) from mcp_commands.
	repertoires := inferRepertoiresFromCommands(doc)

	// 3. Upgrade each persona entry.
	personasNode := mapValueNode(doc, "personas")
	if personasNode != nil && personasNode.Kind == yaml.SequenceNode {
		for _, personaNode := range personasNode.Content {
			if migratePersonaNode(personaNode, repertoires) {
				changed = true
			}
		}
	}

	if !changed {
		return nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return fmt.Errorf("encode %s: %w", yamlPath, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close encoder for %s: %w", yamlPath, err)
	}
	return WriteAtomic(yamlPath, buf.Bytes())
}

// inferRepertoiresFromCommands walks the mcp_commands mapping and returns, per
// persona slug, the union of every command's `skills:` list that binds that
// persona. Slugs keep first-seen order so the injected skill_repertoire is
// deterministic. The reserved `global` key carries no persona and is ignored.
func inferRepertoiresFromCommands(doc *yaml.Node) map[string][]string {
	out := map[string][]string{}
	seen := map[string]map[string]struct{}{}

	commandsNode := mapValueNode(doc, "mcp_commands")
	if commandsNode == nil || commandsNode.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(commandsNode.Content); i += 2 {
		cmdName := commandsNode.Content[i].Value
		specNode := commandsNode.Content[i+1]
		if cmdName == MCPCommandsGlobalKey || specNode.Kind != yaml.MappingNode {
			continue
		}
		personaNode := mapValueNode(specNode, "persona")
		if personaNode == nil {
			continue
		}
		persona := strings.TrimSpace(personaNode.Value)
		if persona == "" {
			continue
		}
		skillsNode := mapValueNode(specNode, "skills")
		if skillsNode == nil || skillsNode.Kind != yaml.SequenceNode {
			continue
		}
		if seen[persona] == nil {
			seen[persona] = map[string]struct{}{}
		}
		for _, s := range skillsNode.Content {
			slug := strings.TrimSpace(s.Value)
			if slug == "" {
				continue
			}
			if _, dup := seen[persona][slug]; dup {
				continue
			}
			seen[persona][slug] = struct{}{}
			out[persona] = append(out[persona], slug)
		}
	}
	return out
}

// migratePersonaNode upgrades a single persona mapping node in place. Returns
// true when it mutated the node. Idempotent: a persona that already declares
// schema_version: 2 is left untouched (no skill_repertoire recomputation, no
// re-stamp) so a second run is a byte-identical no-op.
func migratePersonaNode(personaNode *yaml.Node, repertoires map[string][]string) bool {
	if personaNode.Kind != yaml.MappingNode {
		return false
	}
	if versionNode := mapValueNode(personaNode, "schema_version"); versionNode != nil {
		if strings.TrimSpace(versionNode.Value) == fmt.Sprintf("%d", SchemaVersionV2) {
			return false
		}
	}

	slugNode := mapValueNode(personaNode, "slug")
	if slugNode == nil {
		return false
	}
	slug := strings.TrimSpace(slugNode.Value)

	changed := false

	// skill_repertoire = union of per-command skills referencing this persona.
	if mapValueNode(personaNode, "skill_repertoire") == nil {
		if slugs := repertoires[slug]; len(slugs) > 0 {
			appendMapEntry(personaNode, "skill_repertoire", stringSeqNode(slugs))
			changed = true
		}
	}

	// schema_version: 2
	if mapValueNode(personaNode, "schema_version") == nil {
		appendMapEntry(personaNode, "schema_version", intLeafNode(SchemaVersionV2))
		changed = true
	}

	return changed
}

// removeMapEntry deletes the (key, value) pair bound to key from a mapping
// node and reports whether anything was removed.
func removeMapEntry(m *yaml.Node, key string) bool {
	if m == nil || m.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// stringSeqNode builds a block-style sequence node of string scalars,
// preserving the supplied order.
func stringSeqNode(values []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: 0}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return seq
}
