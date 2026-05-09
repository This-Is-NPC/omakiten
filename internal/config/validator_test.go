package config

import (
	"strings"
	"testing"
)

func TestValidateTheme(t *testing.T) {
	if err := ValidateTheme(Theme{Version: 1, Key: "dark", Name: "Dark", Colors: map[string]string{"bg": "#000"}}); err != nil {
		t.Fatalf("ValidateTheme() error = %v", err)
	}

	if err := ValidateTheme(Theme{Version: 2}); err == nil {
		t.Fatal("ValidateTheme(version 2) error = nil")
	}

	if err := ValidateTheme(Theme{Version: 1, Key: "", Name: "Name", Colors: map[string]string{"bg": "#000"}}); err == nil {
		t.Fatal("ValidateTheme(empty key) error = nil")
	}

	if err := ValidateTheme(Theme{Version: 1, Key: "key", Name: "", Colors: map[string]string{"bg": "#000"}}); err == nil {
		t.Fatal("ValidateTheme(empty name) error = nil")
	}

	if err := ValidateTheme(Theme{Version: 1, Key: "key", Name: "Name"}); err == nil {
		t.Fatal("ValidateTheme(no colors) error = nil")
	}
}

func TestValidateBundleErrors(t *testing.T) {
	// validBundle returns the minimal Bundle that passes the strict
	// validator: every required canonical block (mcp, tui, views,
	// priorities, severities, template_defaults) is set to the kit's
	// canonical values. Per-test mutations target the field under
	// exercise without re-declaring everything else.
	tru := true
	validBundle := func() Bundle {
		return Bundle{
			Version: 1,
			Kit:     Kit{ID: 1, Key: "default", Name: "Default"},
			Config: Settings{
				Output:   OutputSettings{JSONMinified: true, OmitEmpty: true},
				Context:  ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
				Workflow: WorkflowSettings{Active: "default"},
				Theme:    ThemeSettings{Active: "catppuccin"},
				MCP: MCPSettings{
					RecentCommentLimit:        5,
					MaxCommentChars:           0,
					IncludeWorkflowInContinue: &tru,
					CachePrompts:              &tru,
					RecentContextLimit:        3,
					NextWorkLimit:             5,
					SimilarTaskLimit:          5,
				},
				TUI: TUISettings{TokenBadge: TokenBadgeThresholds{YellowAt: 150, RedAt: 400}},
				TemplateDefaults: []string{"task"},
				Priorities: []PriorityDefinition{
					{ID: 1, Value: "low"},
					{ID: 2, Value: "normal", Default: true},
					{ID: 3, Value: "high"},
				},
				Severities: []SeverityDefinition{
					{ID: 1, Value: "info"},
					{ID: 2, Value: "warning", Default: true},
					{ID: 3, Value: "error"},
				},
				Views: ViewSettings{
					Board:        BoardViewSettings{Sort: SortSettings{Field: "created_at", Order: "desc"}},
					Table:        TableViewSettings{Sort: SortSettings{Field: "created_at", Order: "desc"}},
					Graph:        GraphViewSettings{Sort: SortSettings{Field: "id", Order: "asc"}},
					Logs:         LogsViewSettings{Sort: SortSettings{Order: "desc"}, Limit: 50},
					TaskActivity: TaskActivityViewSettings{Sort: SortSettings{Order: "asc"}},
				},
				SQLite:      SQLiteSettings{BusyTimeoutMs: 5000},
				ActivityLog: ActivityLogSettings{MaxRows: 500, MaxAgeDays: 7},
				Solutions:   SolutionsSettings{DefaultTopLimit: 10, MaxTopLimit: 100},
				Events:      EventsSettings{DefaultRecentLimit: 50},
				Search:      SearchSettings{Stopwords: []string{"and", "the"}},
				TagSynonyms: map[string]string{"golang": "go"},
			},
			Skills: []Skill{{Slug: "go", Name: "Go"}},
			Laws:   []Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}},
			Workflows: []Workflow{{
				ID:   1,
				Key:  "default",
				Name: "Default",
				Buckets: []Bucket{
					{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				},
			}},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Bundle)
		wantErr string
	}{
		{"wrong version", func(b *Bundle) { b.Version = 2 }, "version must be 1"},
		{"invalid context level", func(b *Bundle) { b.Config.Context.DefaultLevel = 0 }, "default_level must be between 1 and 3"},
		{"negative max tokens", func(b *Bundle) { b.Config.Context.MaxTokens = -1 }, "max_tokens cannot be negative"},
		{"empty workflow active", func(b *Bundle) { b.Config.Workflow.Active = "" }, "workflow.active is required"},
		{"empty theme active", func(b *Bundle) { b.Config.Theme.Active = "" }, "theme.active is required"},
		{"missing kit id", func(b *Bundle) { b.Kit.ID = 0 }, "kit.id must be positive"},
		{"missing kit key", func(b *Bundle) { b.Kit.Key = "" }, "kit.key is required"},
		{"missing kit name", func(b *Bundle) { b.Kit.Name = "" }, "kit.name is required"},
		{"empty workflow list", func(b *Bundle) { b.Workflows = nil }, "workflows is required"},
		{"workflow missing active", func(b *Bundle) { b.Config.Workflow.Active = "missing" }, "does not match any workflow"},
		{"workflow empty buckets", func(b *Bundle) { b.Workflows[0].Buckets = nil }, "buckets is required"},
		{"workflow bucket position", func(b *Bundle) { b.Workflows[0].Buckets[0].Position = 0 }, "position must be positive"},
		{"workflow duplicate id", func(b *Bundle) {
			b.Workflows[0].Buckets = append(b.Workflows[0].Buckets, Bucket{ID: 1, Key: "dup", Name: "Dup", Position: 2})
		}, "duplicated id"},
		{"workflow duplicate key", func(b *Bundle) {
			b.Workflows[0].Buckets = append(b.Workflows[0].Buckets, Bucket{ID: 2, Key: "backlog", Name: "Dup", Position: 2})
		}, "duplicated key"},
		{"workflow transition missing bucket", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 999, To: 1}}
		}, "from missing bucket id"},
		{"workflow duplicate transition", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1}, {From: 1, To: 1}}
		}, "duplicated transition"},
		{"invalid law severity", func(b *Bundle) { b.Laws[0].Severity = "invalid" }, "invalid severity"},
		{"project empty slug", func(b *Bundle) {
			b.Projects = []Project{{Slug: "", Name: "Test"}}
		}, "slug is required"},
		{"project empty name", func(b *Bundle) {
			b.Projects = []Project{{Slug: "test", Name: " "}}
		}, "name is required"},
		{"guard unknown type", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "unknown_type"}}}}
		}, "unknown guard type"},
		{"guard blockers_in empty buckets", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "blockers_in"}}}}
		}, "buckets is required"},
		{"guard blockers_in invalid bucket key", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "blockers_in", Buckets: []string{"nonexistent"}}}}}
		}, "bucket key"},
		{"guard comments_min zero count", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "comments_min", Count: 0}}}}
		}, "count must be"},
		{"guard comments_min negative count", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "comments_min", Count: -1}}}}
		}, "count must be"},
		{"guard comments_tagged missing tag", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "comments_tagged", Count: 1}}}}
		}, "tag is required"},
		{"guard comments_tagged zero count", func(b *Bundle) {
			b.Workflows[0].Transitions = []Transition{{From: 1, To: 1, Guards: []TransitionGuard{{Type: "comments_tagged", Tag: "resume", Count: 0}}}}
		}, "count must be"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := validBundle()
			tc.mutate(&b)
			err := ValidateBundle(b, b.Skills, b.Laws, b.Personas, b.Templates)
			if err == nil {
				t.Fatalf("ValidateBundle() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateBundle() error = %q, want containing %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestValidatePrioritiesEnforcesAscendingIDs is the regression for
// review item #10: the id is the SQL sort weight (ORDER BY priority
// reads priority_id) and the TUI cycle follows slice order, so a
// declaration like [{id:3,value:high},{id:1,value:low}] would silently
// invert both. Validator now rejects descending or jumbled order.
func TestValidatePrioritiesEnforcesAscendingIDs(t *testing.T) {
	cases := []struct {
		name    string
		input   []PriorityDefinition
		wantErr string
	}{
		{
			name:    "strictly ascending passes",
			input:   []PriorityDefinition{{ID: 1, Value: "low"}, {ID: 2, Value: "normal"}, {ID: 3, Value: "high"}},
			wantErr: "",
		},
		{
			name:    "sparse ascending passes",
			input:   []PriorityDefinition{{ID: 1, Value: "low"}, {ID: 5, Value: "normal"}, {ID: 9, Value: "high"}},
			wantErr: "",
		},
		{
			name:    "descending rejected",
			input:   []PriorityDefinition{{ID: 3, Value: "high"}, {ID: 1, Value: "low"}},
			wantErr: "ascending order",
		},
		{
			name:    "jumbled rejected",
			input:   []PriorityDefinition{{ID: 1, Value: "low"}, {ID: 5, Value: "high"}, {ID: 3, Value: "normal"}},
			wantErr: "ascending order",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePriorities(tc.input)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q missing %q", err.Error(), tc.wantErr)
				}
			}
		})
	}
}

