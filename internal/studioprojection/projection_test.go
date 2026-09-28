package studioprojection

import (
	"context"
	"reflect"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestWorkflowProjectionWarnsOnUnreachableFinalAndFindsMissingEdge(t *testing.T) {
	workflow := config.Workflow{
		Buckets: []config.Bucket{
			{ID: 1, Key: "backlog", Position: 1},
			{ID: 2, Key: "dev", Position: 2},
			{ID: 3, Key: "done", Position: 3},
		},
		Transitions: []config.Transition{{From: 1, To: 2}},
	}
	warnings := FlowWarnings(workflow, nil)
	if len(warnings) != 3 {
		t.Fatalf("warnings = %v, want outbound, no-reopen, and no-path", warnings)
	}
	from, to, ok := FirstMissingTransition(workflow, workflow.Buckets)
	if !ok || from != 1 || to != 3 {
		t.Fatalf("missing transition = %d -> %d, %t", from, to, ok)
	}
}

func TestBuildProjectsCatalogRelationshipsAndHookHistoryWithoutQueries(t *testing.T) {
	persona := config.Persona{Slug: "naruto-uzumaki", Description: "Builder — ships", SkillRepertoire: []string{"testing"}, Laws: []string{"safe"}}
	bundle := config.Bundle{
		Personas:    []config.Persona{persona},
		AllPersonas: []config.Persona{persona},
		Skills:      []config.Skill{{Slug: "testing"}}, AllSkills: []config.Skill{{Slug: "testing"}},
		Laws: []config.Law{{Slug: "safe", Severity: "warning"}}, AllLaws: []config.Law{{Slug: "safe", Severity: "warning"}},
		Commands: map[string]config.CommandSpec{
			"okt-task-continue": {Persona: persona.Slug, Skills: []string{"testing"}},
			"okt-made-up":       {Persona: "missing"},
		},
	}
	p := Build(Input{Bundle: bundle, CommandNames: []string{"okt-task-continue"}, HookHistory: map[int][]HookExecuted{2: {{Success: true}}}})
	if PersonaRole(persona) != "builder" {
		t.Fatalf("role = %q", PersonaRole(persona))
	}
	if got := p.Personas[0].Commands[0].Key; got != "okt-task-continue" {
		t.Fatalf("command relationship = %q", got)
	}
	related := PersonaRelatedRows(p.Personas[0], nil, p.LawSeverities)
	if len(related) != 3 || related[1].Detail != "warning" || len(p.HookHistory[2]) != 1 {
		t.Fatalf("related/history projection = %+v / %+v", related, p.HookHistory)
	}
	if len(p.CommandWarnings) != 2 {
		t.Fatalf("command warnings = %v", p.CommandWarnings)
	}
}

type historyEvents struct{ rows []domain.EventRow }

func (h historyEvents) ListEvents(context.Context, domain.EventFilter) ([]domain.EventRow, error) {
	return h.rows, nil
}

func TestLoadHookHistoryGroupsAndIgnoresMalformedEvents(t *testing.T) {
	rows, err := LoadHookHistory(context.Background(), historyEvents{rows: []domain.EventRow{
		{EventType: domain.EventTypeHookExecuted, CreatedAt: "now", Payload: `{"hook_index":4,"success":true,"duration_ms":5}`},
		{EventType: domain.EventTypeHookExecuted, Payload: "not-json"},
		{EventType: "task.created", Payload: `{"hook_index":4}`},
	}}, 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int][]HookExecuted{4: {{CreatedAt: "now", Success: true, DurationMs: 5}}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("history = %+v, want %+v", rows, want)
	}
}
