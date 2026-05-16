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
  mcp:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
    cache_prompts: true
    recent_context_limit: 3
    next_work_limit: 5
    similar_task_limit: 5
  tui:
    token_badge: { yellow_at: 150, red_at: 400 }
  template_defaults: [task]
  priorities:
    - {id: 1, value: low}
    - {id: 2, value: normal, default: true}
    - {id: 3, value: high}
  severities:
    - {id: 1, value: info}
    - {id: 2, value: warning, default: true}
    - {id: 3, value: error}
  views:
    board: { sort: {field: created_at, order: desc}, filter: {priority: []} }
    table: { sort: {field: created_at, order: desc}, filter: {priority: [], bucket: []} }
    graph: { sort: {field: id, order: asc} }
    logs: { sort: {order: desc}, limit: 50, filter: {source: []} }
    task_activity: { sort: {order: asc} }
  sqlite: { busy_timeout_ms: 5000 }
  activity_log: { max_rows: 500, max_age_days: 7 }
  solutions: { default_top_limit: 10, max_top_limit: 100 }
  events: { default_recent_limit: 50, defaults: { log: true, broadcast: true, hook: true } }
  search:
    stopwords: [and, are, for, from, into, the, this, that, with]
  tag_synonyms:
    golang: go
    javascript: js
    typescript: ts
    nodejs: node
    node-js: node
    postgres: postgresql
    psql: postgresql
    mongo: mongodb
    k8s: kubernetes
    tf: terraform
    py: python
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

func TestLoadBundleListedSlugWithoutFileWarns(t *testing.T) {
	dir := t.TempDir()
	writeAutoloadFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, baseConfigHeader+`skills:
  - go
  - missing-skill
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v, want soft load with warning", err)
	}
	found := false
	for _, w := range bundle.Warnings {
		if strings.Contains(w.Message, "missing-skill") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("LoadBundle() warnings = %v, want mention of missing-skill", bundle.Warnings)
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
