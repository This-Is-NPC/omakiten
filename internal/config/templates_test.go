package config

import (
	"path/filepath"
	"strings"
	"testing"
)

const templateBase = `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  workflow: { active: default }
  theme: { active: catppuccin }
  mcp:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
    cache_prompts: true
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
`

func writeTemplateFixture(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "templates", "task-default.md"),
		"---\nname: Default Task Template\ndescription: Standard scaffold\nentity: task\n---\n**User Story**\n\nComo X.\n")
	writeFile(t, filepath.Join(dir, "templates", "task-bug.md"),
		"---\nname: Bug Report\nentity: task\n---\n**Steps**\n\n1.\n")
}

func TestLoadTemplatesAutoLoadsAllFiles(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if len(bundle.Templates) != 2 {
		t.Fatalf("Templates len = %d, want 2 (auto-load)", len(bundle.Templates))
	}

	bySlug := map[string]TaskTemplate{}
	for _, tpl := range bundle.Templates {
		bySlug[tpl.Slug] = tpl
	}
	def, ok := bySlug["task-default"]
	if !ok {
		t.Fatal("task-default missing from auto-load")
	}
	if def.Name != "Default Task Template" {
		t.Fatalf("task-default name = %q, want Default Task Template", def.Name)
	}
	if !strings.Contains(def.Body, "User Story") {
		t.Fatalf("task-default body missing scaffold content: %q", def.Body)
	}
	if def.Entity != "task" {
		t.Fatalf("task-default entity = %q, want task", def.Entity)
	}
}

func TestLoadTemplatesAllowlistFilters(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase+`templates:
  - task-default
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if len(bundle.Templates) != 1 || bundle.Templates[0].Slug != "task-default" {
		t.Fatalf("Templates = %+v, want [task-default] only", bundle.Templates)
	}
}

func TestLoadTemplatesListedSlugWithoutFileWarns(t *testing.T) {
	dir := t.TempDir()
	writeTemplateFixture(t, dir)
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase+`templates:
  - task-default
  - missing-template
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v, want soft load with warning", err)
	}
	found := false
	for _, w := range bundle.Warnings {
		if strings.Contains(w.Message, "missing-template") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("LoadBundle() warnings = %v, want missing-template mention", bundle.Warnings)
	}
}

func TestTemplateDefaultFrontmatterResolves(t *testing.T) {
	dir := t.TempDir()
	// Two templates; the bug one declares default: task in its frontmatter.
	writeFile(t, filepath.Join(dir, "templates", "task-default.md"),
		"---\nname: Default\nentity: task\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "templates", "task-bug.md"),
		"---\nname: Bug Report\nentity: task\ndefault: task\n---\nbody\n")
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	got := bundle.TemplateByDefault("task", "")
	if got == nil {
		t.Fatalf("TemplateByDefault(task) = nil, want task-bug")
	}
	if got.Slug != "task-bug" {
		t.Fatalf("TemplateByDefault(task).Slug = %q, want task-bug", got.Slug)
	}
}

func TestTemplateDefaultUniquenessIsEnforced(t *testing.T) {
	dir := t.TempDir()
	// Two templates BOTH claim default: task — must fail validation.
	writeFile(t, filepath.Join(dir, "templates", "a.md"),
		"---\nname: A\ndefault: task\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "templates", "b.md"),
		"---\nname: B\ndefault: task\n---\nbody\n")
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase)

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want default-collision failure")
	}
	if !strings.Contains(err.Error(), "default=\"task\"") {
		t.Fatalf("LoadBundle() error = %v, want default=task mention", err)
	}
}

func TestTemplateDefaultRejectsKindOutsideTemplateDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "templates", "weird.md"),
		"---\nname: Weird\ndefault: not-a-known-kind\n---\nbody\n")
	configPath := filepath.Join(dir, "omakiten.yaml")
	// templateBase doesn't customize template_defaults, so we get the
	// canonical [task, pr, comment-resume, comment-selfbranch] list which
	// rejects "not-a-known-kind".
	writeFile(t, configPath, templateBase)

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want kind-not-allowed failure")
	}
	if !strings.Contains(err.Error(), "template_defaults") {
		t.Fatalf("LoadBundle() error = %v, want template_defaults mention", err)
	}
}

func TestLoadBundleWithoutTemplatesFolderIsNotError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v, want success when templates/ is absent", err)
	}
	if len(bundle.Templates) != 0 {
		t.Fatalf("Templates = %+v, want empty", bundle.Templates)
	}
}

func TestLoadTemplatesRequiresName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "templates", "broken.md"),
		"---\ndescription: missing name\n---\nbody\n")
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, templateBase)

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want template name validation failure")
	}
	if !strings.Contains(err.Error(), "template name is required") {
		t.Fatalf("LoadBundle() error = %v, want template name required", err)
	}
}
