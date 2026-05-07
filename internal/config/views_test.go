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
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
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
  workflow:
    active: default
  theme:
    active: omakiten
  template_defaults: [task, pr, comment-resume, comment-selfbranch, comment-documentation]
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
      filter:
        source: [cli, mcp]
    task_activity:
      sort:
        order: desc
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
	if len(views.Logs.Filter.Source) != 2 {
		t.Errorf("Logs.Filter.Source len = %d, want 2", len(views.Logs.Filter.Source))
	}
	if views.TaskActivity.Sort.Order != "desc" {
		t.Errorf("TaskActivity.Sort.Order = %q, want desc", views.TaskActivity.Sort.Order)
	}
}

func TestLoadBundleAcceptsMissingViewsSection(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() = %v", err)
	}
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle(default) = %v", err)
	}
	// Default kit yaml does not declare views; the loader should not invent
	// fields, but EffectiveViews must still surface canonical defaults.
	v := bundle.Config.EffectiveViews()
	if v.Board.Sort.Field == "" || v.Board.Sort.Order == "" {
		t.Errorf("EffectiveViews on default kit = %+v, expected populated defaults", v.Board.Sort)
	}
}

func TestEffectiveViewsFillsCanonicalDefaults(t *testing.T) {
	v := Settings{}.EffectiveViews()

	if v.Board.Sort.Field != DefaultBoardSortField {
		t.Errorf("Board.Sort.Field = %q, want %q", v.Board.Sort.Field, DefaultBoardSortField)
	}
	if v.Board.Sort.Order != DefaultBoardSortOrder {
		t.Errorf("Board.Sort.Order = %q, want %q", v.Board.Sort.Order, DefaultBoardSortOrder)
	}
	if v.Table.Sort.Field != DefaultTableSortField {
		t.Errorf("Table.Sort.Field = %q, want %q", v.Table.Sort.Field, DefaultTableSortField)
	}
	if v.Graph.Sort.Field != DefaultGraphSortField {
		t.Errorf("Graph.Sort.Field = %q, want %q", v.Graph.Sort.Field, DefaultGraphSortField)
	}
	if v.Graph.Sort.Order != DefaultGraphSortOrder {
		t.Errorf("Graph.Sort.Order = %q, want %q", v.Graph.Sort.Order, DefaultGraphSortOrder)
	}
	if v.Logs.Sort.Order != DefaultLogsSortOrder {
		t.Errorf("Logs.Sort.Order = %q, want %q", v.Logs.Sort.Order, DefaultLogsSortOrder)
	}
	if v.Logs.Limit != DefaultLogsLimit {
		t.Errorf("Logs.Limit = %d, want %d", v.Logs.Limit, DefaultLogsLimit)
	}
	if v.TaskActivity.Sort.Order != DefaultTaskActivitySortOrder {
		t.Errorf("TaskActivity.Sort.Order = %q, want %q", v.TaskActivity.Sort.Order, DefaultTaskActivitySortOrder)
	}
}

func TestEffectiveViewsPreservesUserValues(t *testing.T) {
	s := Settings{
		Views: ViewSettings{
			Board: BoardViewSettings{
				Sort: SortSettings{Field: "title", Order: "asc"},
				Filter: BoardFilterSettings{Priority: []string{"high"}},
			},
			Logs: LogsViewSettings{
				Limit: 200,
			},
		},
	}
	v := s.EffectiveViews()

	if v.Board.Sort.Field != "title" || v.Board.Sort.Order != "asc" {
		t.Errorf("Board sort = %+v, want title/asc", v.Board.Sort)
	}
	if len(v.Board.Filter.Priority) != 1 || v.Board.Filter.Priority[0] != "high" {
		t.Errorf("Board filter priority = %v, want [high]", v.Board.Filter.Priority)
	}
	if v.Logs.Limit != 200 {
		t.Errorf("Logs.Limit = %d, want 200 (user override should not be replaced)", v.Logs.Limit)
	}
	// Order omitted by user — default still kicks in for that single field.
	if v.Logs.Sort.Order != DefaultLogsSortOrder {
		t.Errorf("Logs.Sort.Order = %q, want default %q", v.Logs.Sort.Order, DefaultLogsSortOrder)
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
		views   ViewSettings
		wantErr string
	}{
		{
			name:    "board sort field invalid",
			views:   ViewSettings{Board: BoardViewSettings{Sort: SortSettings{Field: "color"}}},
			wantErr: "config.views.board.sort.field",
		},
		{
			name:    "board sort order invalid",
			views:   ViewSettings{Board: BoardViewSettings{Sort: SortSettings{Order: "alpha"}}},
			wantErr: "config.views.board.sort.order",
		},
		{
			name:    "board filter priority invalid",
			views:   ViewSettings{Board: BoardViewSettings{Filter: BoardFilterSettings{Priority: []string{"medium"}}}},
			wantErr: "config.views.board.filter.priority",
		},
		{
			name:    "table filter bucket unknown",
			views:   ViewSettings{Table: TableViewSettings{Filter: TableFilterSettings{Bucket: []string{"archive"}}}},
			wantErr: "config.views.table.filter.bucket",
		},
		{
			name:    "graph sort field invalid",
			views:   ViewSettings{Graph: GraphViewSettings{Sort: SortSettings{Field: "priority"}}},
			wantErr: "config.views.graph.sort.field",
		},
		{
			name:    "logs sort field forbidden",
			views:   ViewSettings{Logs: LogsViewSettings{Sort: SortSettings{Field: "id"}}},
			wantErr: "config.views.logs.sort.field is not configurable",
		},
		{
			name:    "logs filter source invalid",
			views:   ViewSettings{Logs: LogsViewSettings{Filter: LogsFilterSettings{Source: []string{"slack"}}}},
			wantErr: "config.views.logs.filter.source",
		},
		{
			name:    "logs negative limit",
			views:   ViewSettings{Logs: LogsViewSettings{Limit: -1}},
			wantErr: "config.views.logs.limit cannot be negative",
		},
		{
			name:    "task_activity field forbidden",
			views:   ViewSettings{TaskActivity: TaskActivityViewSettings{Sort: SortSettings{Field: "created_at"}}},
			wantErr: "config.views.task_activity.sort.field is not configurable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateViewSettings(tc.views, baseWorkflow, "default")
			if err == nil {
				t.Fatalf("validateViewSettings() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateViewSettings() = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateViewSettingsAcceptsEmptyAndValid(t *testing.T) {
	baseWorkflow := []Workflow{{
		ID: 1, Key: "default", Name: "Default",
		Buckets: []Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
	}}

	if err := validateViewSettings(ViewSettings{}, baseWorkflow, "default"); err != nil {
		t.Fatalf("empty ViewSettings should pass: %v", err)
	}

	full := ViewSettings{
		Board: BoardViewSettings{
			Sort:   SortSettings{Field: "priority", Order: "desc"},
			Filter: BoardFilterSettings{Priority: []string{"high", "normal"}},
		},
		Table: TableViewSettings{
			Sort:   SortSettings{Field: "title", Order: "asc"},
			Filter: TableFilterSettings{Priority: []string{"low"}, Bucket: []string{"backlog"}},
		},
		Graph: GraphViewSettings{Sort: SortSettings{Field: "title", Order: "desc"}},
		Logs: LogsViewSettings{
			Sort:   SortSettings{Order: "asc"},
			Limit:  100,
			Filter: LogsFilterSettings{Source: []string{"cli", "tui"}},
		},
		TaskActivity: TaskActivityViewSettings{Sort: SortSettings{Order: "desc"}},
	}
	if err := validateViewSettings(full, baseWorkflow, "default"); err != nil {
		t.Fatalf("full valid ViewSettings should pass: %v", err)
	}
}
