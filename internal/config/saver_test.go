package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillFileBytes(t *testing.T) {
	bytes, err := SkillFileBytes(Skill{Slug: "go", Name: "Go", Description: "Go lang", Body: "Body text"})
	if err != nil {
		t.Fatalf("SkillFileBytes() error = %v", err)
	}
	if !strings.Contains(string(bytes), "---") {
		t.Fatal("SkillFileBytes() missing frontmatter delimiter")
	}
	if !strings.Contains(string(bytes), "Body text") {
		t.Fatal("SkillFileBytes() missing body")
	}
}

func TestLawFileBytes(t *testing.T) {
	bytes, err := LawFileBytes(Law{Slug: "scope", Name: "Scope", Severity: "error", Body: "Stay scoped."})
	if err != nil {
		t.Fatalf("LawFileBytes() error = %v", err)
	}
	if !strings.Contains(string(bytes), "Stay scoped.") {
		t.Fatal("LawFileBytes() missing body")
	}

	// Empty body should become " "
	bytes, err = LawFileBytes(Law{Slug: "empty", Name: "Empty", Severity: "info", Body: ""})
	if err != nil {
		t.Fatalf("LawFileBytes() error = %v", err)
	}
	if !strings.Contains(string(bytes), " ") {
		t.Fatal("LawFileBytes() empty body not replaced with space")
	}
}

func TestPersonaFileBytes(t *testing.T) {
	bytes, err := PersonaFileBytes(Persona{Slug: "agent", Name: "Agent", Description: "AI agent", Body: "Instructions."})
	if err != nil {
		t.Fatalf("PersonaFileBytes() error = %v", err)
	}
	if !strings.Contains(string(bytes), "Instructions.") {
		t.Fatal("PersonaFileBytes() missing body")
	}
}

func TestEntityFilePath(t *testing.T) {
	path := EntityFilePath("/config", EntityKindSkill, "go")
	if !strings.HasSuffix(path, "skills/go.md") {
		t.Fatalf("EntityFilePath() = %q, want suffix skills/go.md", path)
	}
}

func TestSaveBundleAndLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "omakiten.yaml")

	tru := true
	bundle := Bundle{
		Version: 1,
		Kit:     Kit{ID: 1, Key: "default", Name: "Default"},
		Config: Settings{
			Output:   OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: WorkflowSettings{Active: "default"},
			Theme:    ThemeSettings{Active: "catppuccin"},
			MCP: MCPSettings{
				RecentCommentLimit:        5,
				IncludeWorkflowInContinue: &tru,
				CachePrompts:              &tru,
				RecentContextLimit:        3,
				NextWorkLimit:             5,
				SimilarTaskLimit:          5,
			},
			TUI:              TUISettings{TokenBadge: TokenBadgeThresholds{YellowAt: 150, RedAt: 400}},
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
		Skills:   []Skill{{Slug: "go", Name: "Go"}},
		Personas: []Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}},
		Laws:     []Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}},
		Workflows: []Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
			},
		}},
	}

	if err := SaveBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveBundle() error = %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}

	// Create entity files so LoadBundle can resolve refs
	for _, skill := range bundle.Skills {
		data, _ := SkillFileBytes(skill)
		_ = WriteAtomic(EntityFilePath(tmp, EntityKindSkill, skill.Slug), data)
	}
	for _, law := range bundle.Laws {
		data, _ := LawFileBytes(law)
		_ = WriteAtomic(EntityFilePath(tmp, EntityKindLaw, law.Slug), data)
	}
	for _, persona := range bundle.Personas {
		data, _ := PersonaFileBytes(persona)
		_ = WriteAtomic(EntityFilePath(tmp, EntityKindPersona, persona.Slug), data)
	}

	loaded, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if loaded.Kit.Key != "default" {
		t.Fatalf("LoadBundle().Kit.Key = %q, want default", loaded.Kit.Key)
	}
}

func TestSaveFullBundle(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "omakiten.yaml")

	bundle := Bundle{
		Version: 1,
		Kit:     Kit{ID: 1, Key: "default", Name: "Default"},
		Config: Settings{
			Output:   OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: WorkflowSettings{Active: "default"},
			Theme:    ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []Skill{{Slug: "go", Name: "Go", Body: "Go body"}},
		Personas: []Persona{{Slug: "agent", Name: "Agent", Body: "Agent body"}},
		Laws:     []Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}},
		Workflows: []Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
		Buckets: []Bucket{
			{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
		},
		}},
	}

	if err := SaveFullBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveFullBundle() error = %v", err)
	}

	// Verify entity files were created
	for _, path := range []string{
		filepath.Join(tmp, "skills", "go.md"),
		filepath.Join(tmp, "personas", "agent.md"),
		filepath.Join(tmp, "laws", "scope.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("entity file missing: %s %v", path, err)
		}
	}
}
