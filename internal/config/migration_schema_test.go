package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateSchemaDefaultsInjectsMissingSqliteKnobs is the W7 #246
// fixture: a legacy bundle authored before W7 #225 added the
// cache_size_kb + mmap_size_bytes keys must survive the next launch
// because the migration helper backfills them from the kit canonical
// before the validator runs. Without the backfill, the user-facing
// failure would be a cryptic "config.sqlite.cache_size_kb: must be
// > 0" on a config they never edited.
func TestMigrateSchemaDefaultsInjectsMissingSqliteKnobs(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	yamlPath := filepath.Join(configDir, "omakase.yaml")
	legacy := []byte("# legacy header\nconfig:\n  sqlite:\n    busy_timeout_ms: 5000\n")
	if err := os.WriteFile(yamlPath, legacy, 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	if err := migrateSchemaDefaults(rootDir); err != nil {
		t.Fatalf("migrateSchemaDefaults: %v", err)
	}

	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after migrate: %v", err)
	}
	gotStr := string(got)
	if !strings.Contains(gotStr, "cache_size_kb:") {
		t.Fatalf("cache_size_kb not injected: %s", gotStr)
	}
	if !strings.Contains(gotStr, "mmap_size_bytes:") {
		t.Fatalf("mmap_size_bytes not injected: %s", gotStr)
	}
	// busy_timeout_ms must survive — the helper only adds missing
	// keys, never rewrites the rest of the sqlite block.
	if !strings.Contains(gotStr, "busy_timeout_ms: 5000") {
		t.Fatalf("busy_timeout_ms lost: %s", gotStr)
	}
}

// TestMigrateSchemaDefaultsIsIdempotent pins the no-op guarantee: a
// bundle that already carries both keys reads through to a byte-
// identical write that the helper skips (no os.Rename, no mtime
// change). Re-running on a freshly-migrated file produces the same
// bytes.
func TestMigrateSchemaDefaultsIsIdempotent(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	yamlPath := filepath.Join(configDir, "omakase.yaml")
	complete := []byte(strings.Join([]string{
		"config:",
		"  sqlite:",
		"    busy_timeout_ms: 5000",
		"    cache_size_kb: 2048",
		"    mmap_size_bytes: 268435456",
		"",
	}, "\n"))
	if err := os.WriteFile(yamlPath, complete, 0o600); err != nil {
		t.Fatalf("seed complete: %v", err)
	}

	if err := migrateSchemaDefaults(rootDir); err != nil {
		t.Fatalf("first migrateSchemaDefaults: %v", err)
	}
	afterFirst, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	// Byte-identical to the seed: no key was missing → no rewrite.
	if !bytes.Equal(afterFirst, complete) {
		t.Fatalf("byte-identical no-op expected; got rewrite\nwant: %q\n got: %q", complete, afterFirst)
	}

	if err := migrateSchemaDefaults(rootDir); err != nil {
		t.Fatalf("second migrateSchemaDefaults: %v", err)
	}
	afterSecond, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if !bytes.Equal(afterFirst, afterSecond) {
		t.Fatalf("idempotency broken between run 1 and run 2:\nrun1: %q\nrun2: %q", afterFirst, afterSecond)
	}
}

// TestMigrateSchemaDefaultsPartialInjectsOnlyMissing pins the surgical
// behaviour: when one of the two keys is present and the other is
// not, only the missing key is injected; the present key + its value
// + its inline comments survive the round-trip.
func TestMigrateSchemaDefaultsPartialInjectsOnlyMissing(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	yamlPath := filepath.Join(configDir, "omakase.yaml")
	partial := []byte(strings.Join([]string{
		"config:",
		"  sqlite:",
		"    busy_timeout_ms: 5000",
		"    cache_size_kb: 2048",
		"",
	}, "\n"))
	if err := os.WriteFile(yamlPath, partial, 0o600); err != nil {
		t.Fatalf("seed partial: %v", err)
	}

	if err := migrateSchemaDefaults(rootDir); err != nil {
		t.Fatalf("migrateSchemaDefaults: %v", err)
	}

	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after migrate: %v", err)
	}
	gotStr := string(got)
	// Present key preserved at its user-chosen value.
	if !strings.Contains(gotStr, "cache_size_kb: 2048") {
		t.Fatalf("user value lost; got: %s", gotStr)
	}
	// Missing key injected.
	if !strings.Contains(gotStr, "mmap_size_bytes:") {
		t.Fatalf("mmap_size_bytes not injected: %s", gotStr)
	}
}

// TestMigrateSchemaDefaultsCreatesSqliteBlock covers the deepest
// legacy: a bundle that has no `config.sqlite` block at all (the
// keys are required NOW but the block itself did not exist before
// W7 #225 — early bundles only carried busy_timeout_ms inline elsewhere
// or under a different shape). The helper creates the block and
// injects both keys with kit-canonical values.
func TestMigrateSchemaDefaultsCreatesSqliteBlock(t *testing.T) {
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	yamlPath := filepath.Join(configDir, "omakase.yaml")
	noSqlite := []byte("config:\n  output:\n    json_minified: true\n")
	if err := os.WriteFile(yamlPath, noSqlite, 0o600); err != nil {
		t.Fatalf("seed no-sqlite: %v", err)
	}

	if err := migrateSchemaDefaults(rootDir); err != nil {
		t.Fatalf("migrateSchemaDefaults: %v", err)
	}

	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after migrate: %v", err)
	}
	gotStr := string(got)
	if !strings.Contains(gotStr, "sqlite:") {
		t.Fatalf("sqlite block not created: %s", gotStr)
	}
	if !strings.Contains(gotStr, "cache_size_kb:") {
		t.Fatalf("cache_size_kb missing after sqlite-block creation: %s", gotStr)
	}
	if !strings.Contains(gotStr, "mmap_size_bytes:") {
		t.Fatalf("mmap_size_bytes missing after sqlite-block creation: %s", gotStr)
	}
}
