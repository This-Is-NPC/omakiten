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
	validBundle := func() Bundle {
		return Bundle{
			Version: 1,
			Kit:     Kit{ID: 1, Key: "default", Name: "Default"},
			Config: Settings{
				Output:   OutputSettings{JSONMinified: true, OmitEmpty: true},
				Context:  ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
				Workflow: WorkflowSettings{Active: "default"},
				Theme:    ThemeSettings{Active: "catppuccin"},
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
