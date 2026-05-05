package config

import (
	"path/filepath"
	"strings"
	"testing"
)

const baseConfigHeader = `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  context: { default_level: 2, max_tokens: 12000 }
  workflow: { active: default }
  theme: { active: catppuccin }
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
`

func writeAutoloadFixture(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "skills", "go.md"), "---\nname: Go\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "skills", "sqlite.md"), "---\nname: SQLite\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "laws", "scope.md"), "---\nseverity: error\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "laws", "extra.md"), "---\nseverity: warning\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "personas", "agent.md"), "---\nname: Agent\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "personas", "ghost.md"), "---\nname: Ghost\n---\nbody\n")
}

func TestLoadBundleAutoLoadsAllEntitiesWhenSlotsOmitted(t *testing.T) {
	dir := t.TempDir()
	writeAutoloadFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, baseConfigHeader)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}

	if len(bundle.Skills) != 2 {
		t.Fatalf("Skills len = %d, want 2 (auto-load)", len(bundle.Skills))
	}
	if len(bundle.Laws) != 2 {
		t.Fatalf("Laws len = %d, want 2 (auto-load)", len(bundle.Laws))
	}
	for _, l := range bundle.Laws {
		if l.Scope != "global" {
			t.Fatalf("auto-loaded law %q scope = %q, want global", l.Slug, l.Scope)
		}
	}
	if len(bundle.Personas) != 2 {
		t.Fatalf("Personas len = %d, want 2 (auto-load)", len(bundle.Personas))
	}
	for _, p := range bundle.Personas {
		if len(p.Skills) != 0 || len(p.Laws) != 0 {
			t.Fatalf("auto-loaded persona %q wiring = skills=%v laws=%v, want empty", p.Slug, p.Skills, p.Laws)
		}
	}
}

func TestLoadBundleExplicitAllowlistFiltersFolder(t *testing.T) {
	dir := t.TempDir()
	writeAutoloadFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, baseConfigHeader+`skills:
  - go
laws:
  - scope
personas:
  - slug: agent
    skills: [go]
    laws: [scope]
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}

	if len(bundle.Skills) != 1 || bundle.Skills[0].Slug != "go" {
		t.Fatalf("Skills = %+v, want [go] only (allowlist)", bundle.Skills)
	}
	if len(bundle.Laws) != 1 || bundle.Laws[0].Slug != "scope" {
		t.Fatalf("Laws = %+v, want [scope] only (allowlist)", bundle.Laws)
	}
	if len(bundle.Personas) != 1 || bundle.Personas[0].Slug != "agent" {
		t.Fatalf("Personas = %+v, want [agent] only (allowlist)", bundle.Personas)
	}
	if len(bundle.Personas[0].Skills) != 1 || bundle.Personas[0].Skills[0] != "go" {
		t.Fatalf("Personas[0].Skills = %v, want [go]", bundle.Personas[0].Skills)
	}
}

func TestLoadBundleExtraFilesAreNotErrorsWhenAllowlistPartial(t *testing.T) {
	dir := t.TempDir()
	writeAutoloadFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	// Skills allowlist is partial; the unlisted `sqlite` file must be silently
	// ignored (not an error) per AC #4.
	writeFile(t, configPath, baseConfigHeader+`skills:
  - go
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v, want success despite extra skill file", err)
	}
	if len(bundle.Skills) != 1 || bundle.Skills[0].Slug != "go" {
		t.Fatalf("Skills = %+v, want [go] (sqlite must be filtered out)", bundle.Skills)
	}
}

func TestLoadBundleListedSlugWithoutFileIsError(t *testing.T) {
	dir := t.TempDir()
	writeAutoloadFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, baseConfigHeader+`skills:
  - go
  - missing-skill
`)

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want dangling-ref failure")
	}
	if !strings.Contains(err.Error(), "missing-skill") {
		t.Fatalf("LoadBundle() error = %v, want mention of missing-skill", err)
	}
}

func TestLoadBundlePersonaWithLawWiringStillScopesCorrectly(t *testing.T) {
	dir := t.TempDir()
	writeAutoloadFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	// Top-level laws omitted; `extra` is wired into the persona, so it must end
	// up scope=persona. The remaining `scope` law is unreferenced and should be
	// auto-promoted to global.
	writeFile(t, configPath, baseConfigHeader+`personas:
  - slug: agent
    laws: [extra]
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}

	scopeBySlug := map[string]string{}
	for _, l := range bundle.Laws {
		scopeBySlug[l.Slug] = l.Scope
	}
	if scopeBySlug["extra"] != "persona" {
		t.Fatalf("law extra scope = %q, want persona (explicit wiring beats auto-load)", scopeBySlug["extra"])
	}
	if scopeBySlug["scope"] != "global" {
		t.Fatalf("law scope scope = %q, want global (auto-promoted because unreferenced)", scopeBySlug["scope"])
	}
}
