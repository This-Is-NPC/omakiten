package config

import (
	"path/filepath"
	"testing"
)

// TestLoadSkillsParsesSchemaV2Fields verifies the entity loader accepts and
// surfaces the schema-v2 skill fields: schema_version + role_affinity.
func TestLoadSkillsParsesSchemaV2Fields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.md"),
		"---\nname: Go\ndescription: Go lang\nschema_version: 2\nrole_affinity:\n  - builder\n  - verifier\n---\nbody\n")

	skills, _, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(skills))
	}
	got := skills[0]
	if got.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", got.SchemaVersion)
	}
	want := []string{"builder", "verifier"}
	if !equalStringSlice(got.RoleAffinity, want) {
		t.Fatalf("role_affinity = %v, want %v", got.RoleAffinity, want)
	}
}

// TestLoadPersonasParsesSchemaV2Fields verifies the entity loader accepts and
// surfaces the schema-v2 persona fields: schema_version + skill_repertoire.
func TestLoadPersonasParsesSchemaV2Fields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "builder.md"),
		"---\nname: Builder\nschema_version: 2\nskill_repertoire:\n  - go\n  - sqlite\n  - markdown\n---\nbody\n")

	personas, _, err := LoadPersonas(dir)
	if err != nil {
		t.Fatalf("LoadPersonas() error = %v", err)
	}
	if len(personas) != 1 {
		t.Fatalf("len(personas) = %d, want 1", len(personas))
	}
	got := personas[0]
	if got.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", got.SchemaVersion)
	}
	want := []string{"go", "sqlite", "markdown"}
	if !equalStringSlice(got.SkillRepertoire, want) {
		t.Fatalf("skill_repertoire = %v, want %v", got.SkillRepertoire, want)
	}
}
