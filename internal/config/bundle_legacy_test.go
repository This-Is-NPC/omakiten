package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLogsFilterSourceLegacyKeyTolerated locks in the backwards-compat
// guarantee from task #330: existing `omakiten.yaml` files in the wild
// still carry `views.logs.filter.source: [...]` — a field that pre-dates
// the new event inspector and no longer maps to a Go struct member.
//
// YAML's default unmarshal silently ignores unknown keys when the field
// is removed from the struct. This test asserts that LoadBundle accepts
// such a config without rejecting it, so users do not have to hand-edit
// their config when upgrading past the cleanup.
func TestLogsFilterSourceLegacyKeyTolerated(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() = %v", err)
	}
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	yaml := `version: 1
kit:
  id: 1
  key: default
  name: Default
config:
  output:
    json_minified: true
    omit_empty: true
  context:
    default_level: 2
    max_tokens: 12000
  mcp:
    recent_comment_limit: 5
    max_comment_chars: 0
    include_workflow_in_continue: true
    cache_prompts: true
    recent_context_limit: 3
    next_work_limit: 5
    similar_task_limit: 5
  workflow:
    active: default
  theme:
    active: omakiten
  tui:
    token_badge:
      yellow_at: 150
      red_at: 400
  template_defaults: [task, pr, comment-resume, comment-selfbranch, comment-documentation]
  priorities:
    - {id: 1, value: low, color: success}
    - {id: 2, value: normal, default: true, color: info}
    - {id: 3, value: high, color: error}
  severities:
    - {id: 1, value: info, color: info}
    - {id: 2, value: warning, default: true, color: warning}
    - {id: 3, value: error, color: error}
  views:
    board:
      sort: {field: created_at, order: desc}
      filter: {priority: []}
    table:
      sort: {field: created_at, order: desc}
      filter: {priority: [], bucket: []}
    graph:
      sort: {field: id, order: asc}
    logs:
      sort: {order: desc}
      limit: 50
      window_days: 30
      filter: {source: [cli]}
    task_activity:
      sort: {order: asc}
  sqlite: {busy_timeout_ms: 5000, cache_size_kb: 1024, mmap_size_bytes: 0}
  activity_log: {max_rows: 500, max_age_days: 7}
  solutions: {default_top_limit: 10, max_top_limit: 100}
  events: {default_recent_limit: 50, defaults: {log: true, broadcast: true, hook: true}}
  search: {stopwords: [and, are, for, from, into, the, this, that, with]}
  tag_synonyms: {golang: go, javascript: js, typescript: ts, nodejs: node, node-js: node, postgres: postgresql, psql: postgresql, mongo: mongodb, k8s: kubernetes, tf: terraform, py: python}
workflows:
  - id: 1
    key: default
    name: Default Workflow
    buckets:
      - id: 1
        key: backlog
        name: Backlog
        position: 1
`
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	if _, err := LoadBundle(configPath); err != nil {
		t.Fatalf("LoadBundle() rejected legacy filter.source key: %v", err)
	}
}
