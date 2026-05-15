package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestLoadSkillsHappyPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.md"), "---\nname: Go\ndescription: Go lang\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "sqlite.md"), "---\nname: SQLite\n---\n")

	skills, warnings, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("len(skills) = %d, want 2", len(skills))
	}
	if skills[0].Slug != "go" || skills[0].Name != "Go" {
		t.Fatalf("skills[0] = %#v", skills[0])
	}
	if skills[0].Body != "body" {
		t.Fatalf("body = %q, want body", skills[0].Body)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestLoadSkillsRejectsMissingName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.md"), "---\ndescription: no name here\n---\n")
	_, _, err := LoadSkills(dir)
	if err == nil {
		t.Fatalf("LoadSkills() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("LoadSkills() error = %v, want 'name is required'", err)
	}
}

func TestLoadSkillsRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.md"), "---\nname: X\nbogus_field: 1\n---\n")
	_, _, err := LoadSkills(dir)
	if err == nil {
		t.Fatalf("LoadSkills() error = nil, want failure")
	}
}

func TestLoadSkillsWarnsOnSlugMismatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wrong-name.md"), "---\nname: Different Name\n---\n")
	_, warnings, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("len(warnings) = %d, want 1", len(warnings))
	}
	if warnings[0].Slug != "wrong-name" {
		t.Fatalf("warning slug = %q", warnings[0].Slug)
	}
}

func TestLoadLawsRequiresSeverityAndBody(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "no-severity.md"), "---\nname: X\n---\nbody\n")
	if _, _, err := LoadLaws(dir); err == nil || !strings.Contains(err.Error(), "severity is required") {
		t.Fatalf("LoadLaws() error = %v, want severity is required", err)
	}

	dir2 := t.TempDir()
	writeFile(t, filepath.Join(dir2, "no-body.md"), "---\nseverity: error\n---\n   \n")
	if _, _, err := LoadLaws(dir2); err == nil || !strings.Contains(err.Error(), "body is required") {
		t.Fatalf("LoadLaws() error = %v, want body is required", err)
	}
}

func TestLoadBundleRejectsDanglingRef(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, `version: 1
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
  search: { stopwords: [and, the] }
  tag_synonyms: { golang: go }
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
skills:
  - go
laws:
  - missing
personas: []
`)
	writeFile(t, filepath.Join(dir, "skills", "go.md"), "---\nname: Go\n---\n")
	writeFile(t, filepath.Join(dir, "laws", "scope.md"), "---\nseverity: error\n---\nbody\n")

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v, want soft load with warning", err)
	}
	// Soft validation: dangling refs surface as bundle.Warnings instead of
	// aborting the load. Users are responsible for their own wiring.
	found := false
	for _, w := range bundle.Warnings {
		if strings.Contains(w.Message, "no matching file") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("LoadBundle() warnings = %v, want a 'no matching file' entry", bundle.Warnings)
	}
}
