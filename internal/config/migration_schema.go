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

// migrateSchemaDefaults backfills the sqlite knobs introduced in
// W7 #225 (config.sqlite.cache_size_kb + config.sqlite.mmap_size_bytes,
// validator-required since they were added). User bundles authored
// before that commit lack the keys; the load path then rejects the
// bundle with "config.sqlite.cache_size_kb: must be > 0" on the very
// first `okt` launch post-upgrade.
//
// The helper walks every <rootDir>/config/*.yaml (mirrors
// migrateLegacyTemplateBinding) and rewrites the file in place when
// either key is missing, preserving every other line and every
// existing comment because the rewrite goes through the yaml.Node
// API rather than the struct-typed encoder. The canonical injected
// values are read from the embedded kit YAML so the migration cannot
// drift away from the shipped defaults when those defaults change.
//
// Idempotent: a bundle that already carries both keys reads through
// to a byte-identical write that is skipped (no os.Rename, no mtime
// bump). A bundle missing one of the two keys gets only the missing
// key injected; the present key + any inline comment around it
// survive the round-trip.
//
// Errors:
//   - ErrConfigTooLarge when a profile exceeds MaxWiringFileBytes
//     (shares the cap with SaveBundle's preservation hook).
//   - Other read / parse / write failures propagate as wrapped
//     fmt.Errorf chains so the operator can identify the offending
//     profile path.
func migrateSchemaDefaults(rootDir string) error {
	configDir := filepath.Join(rootDir, "config")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", configDir, err)
	}

	kit, kitErr := LoadKitConfig()
	if kitErr != nil {
		return fmt.Errorf("load kit canonical for schema migration: %w", kitErr)
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
		if err := migrateSchemaDefaultsInFile(yamlPath, kit); err != nil {
			return err
		}
	}
	return nil
}

// migrateSchemaDefaultsInFile applies the backfill to a single yaml
// profile. Exported via the package-private name so tests can drive
// it directly without staging an entire config root.
func migrateSchemaDefaultsInFile(yamlPath string, kit Settings) error {
	raw, err := readFileBounded(yamlPath, MaxWiringFileBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		// A profile that does not parse as YAML is the operator's
		// problem to fix; do not block the upgrade path on it. The
		// load path will surface the parse error with full context.
		return nil
	}

	doc := documentMap(&root)
	if doc == nil {
		return nil
	}

	configNode := mapValueNode(doc, "config")
	if configNode == nil {
		// No `config:` block — bundle is incomplete, validator will
		// reject it for unrelated reasons. Schema migration cannot
		// inject into a structure that does not exist.
		return nil
	}
	if configNode.Kind != yaml.MappingNode {
		return nil
	}

	sqliteNode := mapValueNode(configNode, "sqlite")
	if sqliteNode == nil {
		sqliteNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendMapEntry(configNode, "sqlite", sqliteNode)
	} else if sqliteNode.Kind != yaml.MappingNode {
		return nil
	}

	changed := false
	if mapValueNode(sqliteNode, "cache_size_kb") == nil {
		appendMapEntry(sqliteNode, "cache_size_kb", intLeafNode(int64(kit.SQLite.CacheSizeKB)))
		changed = true
	}
	if mapValueNode(sqliteNode, "mmap_size_bytes") == nil {
		appendMapEntry(sqliteNode, "mmap_size_bytes", intLeafNode(int64(kit.SQLite.MmapSizeBytes)))
		changed = true
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

// documentMap descends from a freshly-unmarshalled root to the first
// mapping node — that node owns the top-level keys.
func documentMap(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

// mapValueNode returns the value node bound to key inside a mapping
// node, or nil when the key is absent or m is not a MappingNode.
// Walks the Content slice in pairs (key, value) per yaml.Node's
// mapping representation.
func mapValueNode(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// appendMapEntry adds a (key, value) pair to a mapping node, taking
// care of constructing the key node with the scalar tag yaml.v3
// expects.
func appendMapEntry(m *yaml.Node, key string, value *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	m.Content = append(m.Content, keyNode, value)
}

// intLeafNode builds a scalar yaml.Node carrying an int value with
// the canonical !!int tag so the encoded output matches the kit
// YAML's representation byte-for-byte for the same value.
func intLeafNode(value int64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", value)}
}
