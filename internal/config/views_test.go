package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleParsesViewsSection(t *testing.T) {
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
      sort:
        field: priority
        order: desc
      filter:
        priority: [high]
    table:
      sort:
        field: title
        order: asc
      filter:
        bucket: [backlog]
    graph:
      sort:
        field: title
        order: desc
    logs:
      sort:
        order: asc
      limit: 25
      window_days: 14
      filter:
        source: [cli, mcp]
    task_activity:
      sort:
        order: desc
  sqlite: { busy_timeout_ms: 5000, cache_size_kb: 1024, mmap_size_bytes: 0 }
  activity_log: { max_rows: 500, max_age_days: 7 }
  solutions: { default_top_limit: 10, max_top_limit: 100 }
  events: { default_recent_limit: 50, defaults: { log: true, broadcast: true, hook: true } }
  search: { stopwords: [and, are, for, from, into, the, this, that, with] }
  tag_synonyms: { golang: go, javascript: js, typescript: ts, nodejs: node, node-js: node, postgres: postgresql, psql: postgresql, mongo: mongodb, k8s: kubernetes, tf: terraform, py: python }
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
	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() = %v", err)
	}

	views := bundle.Config.Views
	if views.Board.Sort.Field != "priority" || views.Board.Sort.Order != "desc" {
		t.Errorf("Board.Sort = %+v, want priority/desc", views.Board.Sort)
	}
	if len(views.Board.Filter.Priority) != 1 || views.Board.Filter.Priority[0] != "high" {
		t.Errorf("Board.Filter.Priority = %v, want [high]", views.Board.Filter.Priority)
	}
	if views.Logs.Limit != 25 {
		t.Errorf("Logs.Limit = %d, want 25", views.Logs.Limit)
	}
	if views.Logs.WindowDays != 14 {
		t.Errorf("Logs.WindowDays = %d, want 14", views.Logs.WindowDays)
	}
	if len(views.Logs.Filter.Source) != 2 {
		t.Errorf("Logs.Filter.Source len = %d, want 2", len(views.Logs.Filter.Source))
	}
	if views.TaskActivity.Sort.Order != "desc" {
		t.Errorf("TaskActivity.Sort.Order = %q, want desc", views.TaskActivity.Sort.Order)
	}
}

// TestLoadBundleParsesKitDefaultFile is the round-trip integrity check
// for the kit YAML: EnsureDefaultFiles materialises defaults/omakiten.
// yaml into a temp config root, LoadBundle re-reads it, and the
// validator must accept it. If this fails, the kit ships an
// unparseable / incomplete document — the most basic invariant.
func TestLoadBundleParsesKitDefaultFile(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() = %v", err)
	}
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle(default kit) = %v — kit YAML must be self-sufficient (no in-code fallback any more)", err)
	}
	// Quick assertions that the resolved bundle is structurally complete:
	// validator already covered the full set, but spot-check the most
	// load-bearing pieces so a regression here points at the kit YAML.
	if bundle.Config.MCP.RecentCommentLimit <= 0 {
		t.Error("kit MCP.RecentCommentLimit not set")
	}
	if bundle.Config.Views.Board.Sort.Field == "" {
		t.Error("kit Views.Board.Sort.Field not set")
	}
	if len(bundle.Config.Priorities) == 0 {
		t.Error("kit Priorities empty")
	}
	if len(bundle.Config.Severities) == 0 {
		t.Error("kit Severities empty")
	}
}

// fullViewSettings returns a complete, validator-passing ViewSettings
// derived from the kit so tests for individual rejection cases can mutate
// just one field. Required because the strict validator rejects any
// omitted field.
func fullViewSettings() ViewSettings {
	return ViewSettings{
		Board: BoardViewSettings{
			Sort: SortSettings{Field: "created_at", Order: "desc"},
		},
		Table: TableViewSettings{
			Sort: SortSettings{Field: "created_at", Order: "desc"},
		},
		Graph: GraphViewSettings{
			Sort: SortSettings{Field: "id", Order: "asc"},
		},
		Logs: LogsViewSettings{
			Sort:       SortSettings{Order: "desc"},
			Limit:      50,
			WindowDays: 30,
		},
		TaskActivity: TaskActivityViewSettings{
			Sort: SortSettings{Order: "asc"},
		},
	}
}

func TestValidateViewSettingsRejectsBadValues(t *testing.T) {
	baseWorkflow := []Workflow{{
		ID: 1, Key: "default", Name: "Default",
		Buckets: []Bucket{
			{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
			{ID: 2, Key: "dev", Name: "Dev", Position: 2},
		},
	}}

	tests := []struct {
		name    string
		mutate  func(*ViewSettings)
		wantErr string
	}{
		{
			name:    "board sort field invalid",
			mutate:  func(v *ViewSettings) { v.Board.Sort.Field = "color" },
			wantErr: "config.views.board.sort.field",
		},
		{
			name:    "board sort order invalid",
			mutate:  func(v *ViewSettings) { v.Board.Sort.Order = "alpha" },
			wantErr: "config.views.board.sort.order",
		},
		{
			name:    "board filter priority invalid",
			mutate:  func(v *ViewSettings) { v.Board.Filter.Priority = []string{"medium"} },
			wantErr: "config.views.board.filter.priority",
		},
		{
			name:    "table filter bucket unknown",
			mutate:  func(v *ViewSettings) { v.Table.Filter.Bucket = []string{"archive"} },
			wantErr: "config.views.table.filter.bucket",
		},
		{
			name:    "graph sort field invalid",
			mutate:  func(v *ViewSettings) { v.Graph.Sort.Field = "priority" },
			wantErr: "config.views.graph.sort.field",
		},
		{
			name:    "logs sort field forbidden",
			mutate:  func(v *ViewSettings) { v.Logs.Sort.Field = "id" },
			wantErr: "config.views.logs.sort.field is not configurable",
		},
		{
			name:    "logs filter source invalid",
			mutate:  func(v *ViewSettings) { v.Logs.Filter.Source = []string{"slack"} },
			wantErr: "config.views.logs.filter.source",
		},
		{
			name:    "logs zero limit",
			mutate:  func(v *ViewSettings) { v.Logs.Limit = 0 },
			wantErr: "config.views.logs.limit",
		},
		{
			name:    "logs zero window_days",
			mutate:  func(v *ViewSettings) { v.Logs.WindowDays = 0 },
			wantErr: "config.views.logs.window_days",
		},
		{
			name:    "logs negative window_days",
			mutate:  func(v *ViewSettings) { v.Logs.WindowDays = -1 },
			wantErr: "config.views.logs.window_days",
		},
		{
			name:    "task_activity field forbidden",
			mutate:  func(v *ViewSettings) { v.TaskActivity.Sort.Field = "created_at" },
			wantErr: "config.views.task_activity.sort.field is not configurable",
		},
		{
			name:    "board missing sort field",
			mutate:  func(v *ViewSettings) { v.Board.Sort.Field = "" },
			wantErr: "config.views.board.sort.field",
		},
		{
			name:    "board missing sort order",
			mutate:  func(v *ViewSettings) { v.Board.Sort.Order = "" },
			wantErr: "config.views.board.sort.order",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			views := fullViewSettings()
			tc.mutate(&views)
			err := validateViewSettings(views, baseWorkflow, "default", []string{"low", "normal", "high"})
			if err == nil {
				t.Fatalf("validateViewSettings() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateViewSettings() = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateViewSettingsAcceptsValid(t *testing.T) {
	baseWorkflow := []Workflow{{
		ID: 1, Key: "default", Name: "Default",
		Buckets: []Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
	}}

	if err := validateViewSettings(fullViewSettings(), baseWorkflow, "default", []string{"low", "normal", "high"}); err != nil {
		t.Fatalf("kit-shape ViewSettings should pass: %v", err)
	}

	// User-customised values pass too.
	custom := fullViewSettings()
	custom.Board.Sort = SortSettings{Field: "priority", Order: "desc"}
	custom.Board.Filter.Priority = []string{"high", "normal"}
	custom.Table.Filter.Bucket = []string{"backlog"}
	custom.Logs.Limit = 200
	custom.Logs.Filter.Source = []string{"cli", "tui"}
	if err := validateViewSettings(custom, baseWorkflow, "default", []string{"low", "normal", "high"}); err != nil {
		t.Fatalf("custom valid ViewSettings should pass: %v", err)
	}
}

// TestValidateViewSettingsHonorsCustomPriorities was the regression test
// for the post-config-driven review: prior to threading the priorities
// list through, an "urgent" entry in config.priorities was rejected by
// the view filter validator because it still consulted the hardcoded
// ["low","normal","high"] allowlist. The fix derives the allowed list
// from bundle.Config.EffectivePriorities() and threads it into
// validateViewSettings as a parameter, so a board filter mentioning
// "urgent" passes when the priority is declared.
func TestValidateViewSettingsHonorsCustomPriorities(t *testing.T) {
	baseWorkflow := []Workflow{{
		ID: 1, Key: "default", Name: "Default",
		Buckets: []Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
	}}
	customLabels := []string{"low", "normal", "high", "urgent"}
	views := fullViewSettings()
	views.Board.Filter.Priority = []string{"urgent"}
	if err := validateViewSettings(views, baseWorkflow, "default", customLabels); err != nil {
		t.Fatalf("custom priority %q rejected by validator: %v", "urgent", err)
	}

	// Sanity: a value NOT in the configured set is still rejected.
	bad := fullViewSettings()
	bad.Board.Filter.Priority = []string{"critical"}
	if err := validateViewSettings(bad, baseWorkflow, "default", customLabels); err == nil {
		t.Fatal("expected unknown priority \"critical\" to be rejected, got nil")
	}
}
