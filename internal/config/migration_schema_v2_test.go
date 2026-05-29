package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// v1Fixture is a schema-v1 profile fragment: personas carry direct skills,
// mcp_commands carry per-command skill subsets, and the deprecated
// shared_skill_pool + preset_variants keys are present at the top level.
const v1Fixture = `# fixture header comment
version: 1
personas:
  - slug: builder
    skills:
      - go
      - sqlite
  - slug: reviewer
    skills:
      - code-smells
shared_skill_pool:
  - markdown
preset_variants:
  fast:
    persona: builder
mcp_commands:
  global:
    laws:
      - template-fidelity
  okt-implement:
    persona: builder
    skills:
      - go
      - markdown
  okt-continue:
    persona: builder
    skills:
      - sqlite
      - go
  okt-review:
    persona: reviewer
    skills:
      - code-smells
`

func writeProfile(t *testing.T, body string) (rootDir, yamlPath string) {
	t.Helper()
	rootDir = t.TempDir()
	configDir := filepath.Join(rootDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	yamlPath = filepath.Join(configDir, "omakase.yaml")
	if err := os.WriteFile(yamlPath, []byte(body), 0o600); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	return rootDir, yamlPath
}

// TestMigrateSchemaV2InfersRepertoireAndDropsDeprecated is the core v1→v2
// transform: skill_repertoire is the union of per-command skills referencing
// each persona, schema_version: 2 is stamped, and shared_skill_pool +
// preset_variants are removed.
func TestMigrateSchemaV2InfersRepertoireAndDropsDeprecated(t *testing.T) {
	rootDir, yamlPath := writeProfile(t, v1Fixture)

	if err := migrateSchemaV2(rootDir); err != nil {
		t.Fatalf("migrateSchemaV2: %v", err)
	}

	got, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after migrate: %v", err)
	}

	// Parse the result structurally so order-insensitive assertions hold.
	var doc struct {
		Personas []struct {
			Slug            string   `yaml:"slug"`
			SchemaVersion   int      `yaml:"schema_version"`
			Skills          []string `yaml:"skills"`
			SkillRepertoire []string `yaml:"skill_repertoire"`
		} `yaml:"personas"`
		SharedSkillPool []string       `yaml:"shared_skill_pool"`
		PresetVariants  map[string]any `yaml:"preset_variants"`
	}
	if err := yaml.Unmarshal(got, &doc); err != nil {
		t.Fatalf("parse migrated yaml: %v\n%s", err, got)
	}

	if doc.SharedSkillPool != nil {
		t.Fatalf("shared_skill_pool not dropped: %v", doc.SharedSkillPool)
	}
	if doc.PresetVariants != nil {
		t.Fatalf("preset_variants not dropped: %v", doc.PresetVariants)
	}

	byslug := map[string]int{}
	for i, p := range doc.Personas {
		byslug[p.Slug] = i
	}
	eng := doc.Personas[byslug["builder"]]
	if eng.SchemaVersion != SchemaVersionV2 {
		t.Fatalf("builder schema_version = %d, want %d", eng.SchemaVersion, SchemaVersionV2)
	}
	// union of okt-implement {go, markdown} then okt-continue {sqlite, go}
	// in document order, first-seen wins: {go, markdown, sqlite}.
	wantEng := []string{"go", "markdown", "sqlite"}
	if !equalStringSlice(eng.SkillRepertoire, wantEng) {
		t.Fatalf("builder skill_repertoire = %v, want %v", eng.SkillRepertoire, wantEng)
	}

	rev := doc.Personas[byslug["reviewer"]]
	if rev.SchemaVersion != SchemaVersionV2 {
		t.Fatalf("reviewer schema_version = %d, want %d", rev.SchemaVersion, SchemaVersionV2)
	}
	if !equalStringSlice(rev.SkillRepertoire, []string{"code-smells"}) {
		t.Fatalf("reviewer skill_repertoire = %v, want [code-smells]", rev.SkillRepertoire)
	}

	// The fixture's header comment must survive the node round-trip.
	if !strings.Contains(string(got), "# fixture header comment") {
		t.Fatalf("header comment lost:\n%s", got)
	}
}

// TestMigrateSchemaV2Idempotent pins the no-op guarantee: running the migrator
// on an already-v2 profile produces byte-identical output, and a second run on
// top of the first migration leaves the bytes unchanged.
func TestMigrateSchemaV2Idempotent(t *testing.T) {
	// First, migrate a v1 fixture to v2.
	rootDir, yamlPath := writeProfile(t, v1Fixture)
	if err := migrateSchemaV2(rootDir); err != nil {
		t.Fatalf("first migrateSchemaV2: %v", err)
	}
	afterFirst, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}

	// Second run must be a byte-identical no-op.
	if err := migrateSchemaV2(rootDir); err != nil {
		t.Fatalf("second migrateSchemaV2: %v", err)
	}
	afterSecond, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if !bytes.Equal(afterFirst, afterSecond) {
		t.Fatalf("idempotency broken:\nrun1: %q\nrun2: %q", afterFirst, afterSecond)
	}
}

// TestMigrateSchemaV2AlreadyV2IsNoOp pins that a hand-authored v2 profile (no
// deprecated keys, personas already at schema_version: 2 with explicit
// skill_repertoire) is left byte-identical — the migrator must not re-stamp or
// recompute over a profile the author already upgraded.
func TestMigrateSchemaV2AlreadyV2IsNoOp(t *testing.T) {
	v2 := `version: 1
personas:
  - slug: builder
    schema_version: 2
    skill_repertoire:
      - go
      - markdown
mcp_commands:
  okt-implement:
    persona: builder
    skills:
      - go
`
	rootDir, yamlPath := writeProfile(t, v2)
	before, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if err := migrateSchemaV2(rootDir); err != nil {
		t.Fatalf("migrateSchemaV2: %v", err)
	}
	after, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("v2 input was rewritten; expected byte-identical no-op\nbefore: %q\nafter:  %q", before, after)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
