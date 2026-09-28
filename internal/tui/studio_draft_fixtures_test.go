package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/tui/screens/studio"
)

// The Studio draft fakes stay here because the screen-driving tests in
// internal/tui still build a draft to bind into the Studio screen. The engine
// tests that used to sit alongside them moved with the engine, to
// internal/config/bundledraft.

func studioDraftBundle() config.Bundle {
	tru := true
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "omakase", Name: "Omakase"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Workflow: config.WorkflowSettings{Active: "omakase"},
			Theme:    config.ThemeSettings{Active: "default"},
			Agent: config.AgentSettings{
				RecentCommentLimit:        5,
				IncludeWorkflowInContinue: &tru,
				NextWorkLimit:             5,
				SimilarTaskLimit:          5,
			},
			TUI:              config.TUISettings{TokenBadge: config.TokenBadgeThresholds{YellowAt: 150, RedAt: 400}},
			TemplateDefaults: []string{"task"},
			Priorities: []config.PriorityDefinition{
				{ID: 1, Value: "low"},
				{ID: 2, Value: "normal", Default: true},
				{ID: 3, Value: "high"},
			},
			Severities: []config.SeverityDefinition{
				{ID: 1, Value: "info"},
				{ID: 2, Value: "warning", Default: true},
				{ID: 3, Value: "error"},
			},
			Views: config.ViewSettings{
				Board:        config.BoardViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Table:        config.TableViewSettings{Sort: config.SortSettings{Field: "created_at", Order: "desc"}},
				Graph:        config.GraphViewSettings{Sort: config.SortSettings{Field: "id", Order: "asc"}},
				Logs:         config.LogsViewSettings{Sort: config.SortSettings{Order: "desc"}, Limit: 50, WindowDays: 30},
				TaskActivity: config.TaskActivityViewSettings{Sort: config.SortSettings{Order: "asc"}},
			},
			SQLite:    config.SQLiteSettings{BusyTimeoutMs: 5000, CacheSizeKB: 1024},
			Solutions: config.SolutionsSettings{DefaultTopLimit: 10, MaxTopLimit: 100},
			Backup:    config.BackupSettings{RetentionCount: 5},
			Events: config.EventsSettings{
				DefaultRecentLimit: 50,
				Defaults:           config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
			},
			Search:      config.SearchSettings{Stopwords: []string{"and", "the"}},
			TagSynonyms: map[string]string{"golang": "go"},
		},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Name: "Omakase",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "done", Name: "Done", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
		Skills:      []config.Skill{{Slug: "code"}, {Slug: "deploy"}},
		AllSkills:   []config.Skill{{Slug: "code"}, {Slug: "deploy"}},
		Personas:    []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}},
		AllPersonas: []config.Persona{{Slug: "builder", SkillRepertoire: []string{"code"}}},
		Commands:    map[string]config.CommandSpec{"task": {Persona: "builder", Skills: []string{"code"}}},
		Surfaces:    config.CanonicalSurfaceTable(),
	}
}

type studioDraftStore struct {
	bundle config.Bundle
	saves  int
}

func (s *studioDraftStore) LoadBundle(string) (config.Bundle, error) {
	return studio.CloneBundle(s.bundle), nil
}

func (s *studioDraftStore) LoadBundlePlan(path string) (config.Bundle, map[string]string, error) {
	bundle, err := s.LoadBundle(path)
	if err != nil {
		return config.Bundle{}, nil, err
	}
	hash, err := s.HashFile(path)
	if err != nil {
		return config.Bundle{}, nil, err
	}
	return bundle, map[string]string{path: hash}, nil
}

func (s *studioDraftStore) SaveBundle(_ string, bundle config.Bundle) error {
	s.saves++
	s.bundle = studio.CloneBundle(bundle)
	return nil
}

func (s *studioDraftStore) HashFile(string) (string, error) {
	raw, err := json.Marshal(s.bundle)
	return string(raw), err
}

func (s *studioDraftStore) ValidatePath(root, path string) error {
	return config.ValidatePath(root, path)
}

func (s *studioDraftStore) WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *studioDraftStore) RemoveFile(path string) error {
	return os.Remove(path)
}

func (s *studioDraftStore) EnsureDefaultFiles(string) error { return nil }

func (s *studioDraftStore) ConfigRootFromYAMLPath(path string) string { return filepath.Dir(path) }

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%q not found in %#v", want, values)
}
