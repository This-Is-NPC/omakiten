package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadBundleAcceptsCommands round-trips a yaml that wires the new
// commands block through LoadBundle. The merged Bundle must surface every
// declared command, including the reserved `global` slot, and frontmatter
// laws on personas/templates must merge with the wiring without duplication.
func TestLoadBundleAcceptsCommands(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "omakiten.yaml")
	writeWiring(t, configPath, `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  workflow: { active: default }
  theme: { active: catppuccin }
  agent:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
    next_work_limit: 5
    similar_task_limit: 5
  tui:
    token_badge: { yellow_at: 150, red_at: 400 }
  template_defaults: [task, pr]
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
    logs: { sort: {order: desc}, limit: 50, window_days: 30 }
    task_activity: { sort: {order: asc} }
  sqlite: { busy_timeout_ms: 5000, cache_size_kb: 1024, mmap_size_bytes: 0 }
  solutions: { default_top_limit: 10, max_top_limit: 100 }
  events: { default_recent_limit: 50, defaults: { log: true, broadcast: true, hook: true } }
  search: { stopwords: [and, the] }
  tag_synonyms: { golang: go }
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
laws:
  - project-scope-only
  - template-fidelity
personas:
  - slug: backend-agent
    schema_version: 2
    skill_repertoire:
      - go
      - okt-implement
      - okt-imagine
commands:
  global:
    laws:
      - template-fidelity
  okt-implement:
    persona: backend-agent
    skills: [okt-implement]
    templates:
      - pull-request
  okt-imagine:
    persona: backend-agent
    skills: [okt-imagine]
    laws_disabled:
      - template-fidelity
`)
	writeFile(t, filepath.Join(dir, "skills", "go.md"), "---\nname: Go\nschema_version: 2\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "skills", "okt-implement.md"), "---\nname: Implement\nschema_version: 2\ncommand:\n  name: okt-implement\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "skills", "okt-imagine.md"), "---\nname: Imagine\nschema_version: 2\ncommand:\n  name: okt-imagine\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "laws", "project-scope-only.md"), "---\nseverity: error\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "laws", "template-fidelity.md"), "---\nseverity: warning\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "personas", "backend-agent.md"), "---\nname: Backend Agent\nschema_version: 2\nskill_repertoire: [go, okt-implement, okt-imagine]\nlaws:\n  - project-scope-only\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "templates", "pull-request.md"), "---\nname: Pull Request\nentity: pr\ndefault: pr\nlaws:\n  - template-fidelity\n---\n\n## Before\n## After\n")

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if len(bundle.Commands) != 3 {
		t.Fatalf("Commands len = %d, want 3 (global + 2 commands)", len(bundle.Commands))
	}
	if !containsStringInSlice(bundle.Commands["global"].Laws, "template-fidelity") {
		t.Fatalf("global laws missing template-fidelity: %+v", bundle.Commands["global"])
	}
	if bundle.Commands["okt-implement"].Persona != "backend-agent" {
		t.Fatalf("okt-implement persona = %q", bundle.Commands["okt-implement"].Persona)
	}
	if !containsStringInSlice(bundle.Commands["okt-imagine"].LawsDisabled, "template-fidelity") {
		t.Fatalf("okt-imagine laws_disabled missing template-fidelity: %+v", bundle.Commands["okt-imagine"])
	}

	// Frontmatter law on persona should appear on the merged persona record.
	persona := bundle.Personas[0]
	if !containsStringInSlice(persona.Laws, "project-scope-only") {
		t.Fatalf("persona laws missing frontmatter binding: %+v", persona.Laws)
	}

	// Frontmatter law on template should appear on the merged template record.
	tmpl := bundle.Templates[0]
	if !containsStringInSlice(tmpl.Laws, "template-fidelity") {
		t.Fatalf("template laws missing frontmatter binding: %+v", tmpl.Laws)
	}
}

// TestLoadBundleRejectsDanglingCommandRefs covers the validator: each persona,
// law, and template slug referenced inside commands must resolve to a
// loaded entity.
func TestLoadBundleRejectsDanglingCommandRefs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "omakiten.yaml")
	writeWiring(t, configPath, `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  workflow: { active: default }
  theme: { active: catppuccin }
  agent:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
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
    logs: { sort: {order: desc}, limit: 50, window_days: 30 }
    task_activity: { sort: {order: asc} }
  sqlite: { busy_timeout_ms: 5000, cache_size_kb: 1024, mmap_size_bytes: 0 }
  solutions: { default_top_limit: 10, max_top_limit: 100 }
  events: { default_recent_limit: 50, defaults: { log: true, broadcast: true, hook: true } }
  search: { stopwords: [and, the] }
  tag_synonyms: { golang: go }
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
commands:
  okt-implement:
    persona: ghost
    skills: [okt-implement]
`)
	writeFile(t, filepath.Join(dir, "skills", "okt-implement.md"), "---\nname: Implement\nschema_version: 2\ncommand:\n  name: okt-implement\n---\nbody\n")

	_, err := LoadBundle(configPath)
	if err == nil || !strings.Contains(err.Error(), "not in persona") {
		t.Fatalf("LoadBundle() error = %v, want invalid persona repertoire", err)
	}
}

// TestLoadBundleRejectsLawsDisabledOverlap covers the validator rule that
// forbids the same slug from appearing in both laws and laws_disabled on the
// same command — that combination has no defined semantics.
func TestLoadBundleRejectsLawsDisabledOverlap(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "omakiten.yaml")
	writeWiring(t, configPath, `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  workflow: { active: default }
  theme: { active: catppuccin }
  agent:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
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
    logs: { sort: {order: desc}, limit: 50, window_days: 30 }
    task_activity: { sort: {order: asc} }
  sqlite: { busy_timeout_ms: 5000, cache_size_kb: 1024, mmap_size_bytes: 0 }
  solutions: { default_top_limit: 10, max_top_limit: 100 }
  events: { default_recent_limit: 50, defaults: { log: true, broadcast: true, hook: true } }
  search: { stopwords: [and, the] }
  tag_synonyms: { golang: go }
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
laws:
  - template-fidelity
commands:
  okt-implement:
    laws:
      - template-fidelity
    laws_disabled:
      - template-fidelity
`)
	writeFile(t, filepath.Join(dir, "laws", "template-fidelity.md"), "---\nseverity: warning\n---\nbody\n")

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want overlap failure")
	}
	if !strings.Contains(err.Error(), "both laws and laws_disabled") {
		t.Fatalf("LoadBundle() error = %v, want overlap message", err)
	}
}

func containsStringInSlice(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
