package config

import (
	"strings"
	"testing"
)

func TestValidateBundle(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Bundle)
		wantErr string
	}{
		{name: "valid bundle"},
		{
			name: "persona references missing skill",
			mutate: func(bundle *Bundle) {
				bundle.Personas[0].Skills = []string{"missing"}
			},
			wantErr: "no matching skill",
		},
		{
			name: "invalid law severity",
			mutate: func(bundle *Bundle) {
				bundle.Laws[0].Severity = "fatal"
			},
			wantErr: "invalid severity",
		},
		{
			name: "transition references missing bucket",
			mutate: func(bundle *Bundle) {
				bundle.Workflows[0].Transitions = append(bundle.Workflows[0].Transitions, Transition{From: 1, To: 99})
			},
			wantErr: "missing bucket",
		},
		{
			name: "active workflow is required",
			mutate: func(bundle *Bundle) {
				bundle.Config.Workflow.Active = "missing"
			},
			wantErr: "does not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := validBundle()
			if tt.mutate != nil {
				tt.mutate(&bundle)
			}

			err := ValidateBundle(bundle, bundle.Skills, bundle.Laws, bundle.Personas)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ValidateBundle() error = %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ValidateBundle() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ValidateBundle() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func validBundle() Bundle {
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
		Personas: []Persona{{
			Slug:   "agent",
			Name:   "Agent",
			Skills: []string{"go"},
		}},
		Laws: []Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}},
		Workflows: []Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
			},
			Transitions: []Transition{{From: 1, To: 2}},
		}},
	}
}
