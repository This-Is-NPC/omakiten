package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

func TestModelSwitchesViews(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got := updated.(Model)
	if got.view != 1 {
		t.Fatalf("view = %d, want 1", got.view)
	}
}

func tuiTestBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []config.Skill{{ID: 1, Key: "go", Name: "Go"}},
		Personas: []config.Persona{{ID: 1, Key: "agent", Name: "Agent", SkillIDs: []int{1}}},
		Laws:     []config.Law{{ID: 1, Key: "scope", Severity: "error", Body: "Stay in scope."}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}

func tuiTestTheme() config.Theme {
	return config.Theme{
		Version: 1,
		Key:     "catppuccin",
		Name:    "Catppuccin",
		Colors: map[string]string{
			"background": "#24273A",
			"foreground": "#CAD3F5",
			"primary":    "#8AADF4",
			"secondary":  "#C6A0F6",
			"border":     "#494D64",
			"highlight":  "#363A4F",
			"error":      "#ED8796",
		},
	}
}
