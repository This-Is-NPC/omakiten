package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func studioBundle(guards []config.TransitionGuard) config.Bundle {
	return config.Bundle{
		Kit:    config.Kit{Key: "omakase"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "flow"}, Theme: config.ThemeSettings{Active: "omacon"}},
		Workflows: []config.Workflow{{ID: 1, Key: "flow", Name: "Flow",
			Buckets:     []config.Bucket{{ID: 2, Key: "dev", Name: "Dev", Position: 2}, {ID: 1, Key: "backlog", Name: "Backlog", Position: 1}, {ID: 3, Key: "done", Name: "Done", Position: 3}},
			Transitions: []config.Transition{{From: 1, To: 2, Guards: guards}, {From: 2, To: 3}}}},
		Commands: map[string]config.CommandSpec{"review": {Persona: "reviewer", Skills: []string{"diff"}}, "build": {Persona: "builder"}},
		Personas: []config.Persona{{Slug: "reviewer", Name: "Reviewer", SkillRepertoire: []string{"diff"}}},
		Laws:     []config.Law{{Slug: "tests", Name: "Tests", Severity: "error", Body: "Run them."}},
	}
}

func TestStudioProjectsTheProjectConfiguration(t *testing.T) {
	root := studioBundle([]config.TransitionGuard{{Type: "comments_tagged", Tag: "ready"}})
	sub := studioBundle(nil)
	root.SubtaskBundle = &sub
	server := newStudioServer(t, fakeRuntimes{projects: map[string]int64{"alpha": 7}, snapshot: config.BuildSnapshot(root)})

	rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/studio", "", nil)
	env := decode(t, rec)
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("studio = %d %s", rec.Code, rec.Body.String())
	}
	var got StudioResponse
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	wantBuckets := []StudioBucket{{Key: "backlog", Name: "Backlog", Position: 1}, {Key: "dev", Name: "Dev", Position: 2}, {Key: "done", Name: "Done", Position: 3, Final: true}}
	if diff := cmp.Diff(wantBuckets, got.Workflow.Buckets); diff != "" {
		t.Fatalf("buckets (-want +got):\n%s", diff)
	}
	wantTransitions := []StudioTransition{
		{From: "backlog", To: "dev", Guards: []domain.TransitionGuard{{Type: "comments_tagged", Tag: "ready"}}},
		{From: "dev", To: "done", Guards: []domain.TransitionGuard{}},
	}
	if diff := cmp.Diff(wantTransitions, got.Workflow.Transitions); diff != "" {
		t.Fatalf("transitions (-want +got):\n%s", diff)
	}
	if got.Subtask == nil || len(got.Subtask.Transitions) != 2 || len(got.Subtask.Transitions[0].Guards) != 0 {
		t.Fatalf("subtask workflow = %+v, want the unguarded sub-task matrix", got.Subtask)
	}
	if len(got.Commands) != 2 || got.Commands[0].Key != "build" || got.Commands[1].Persona != "reviewer" {
		t.Fatalf("commands = %+v, want them by key", got.Commands)
	}
	if len(got.Personas) != 1 || got.Personas[0].Skills[0] != "diff" || len(got.Laws) != 1 || got.Laws[0].Body != "Run them." {
		t.Fatalf("personas %+v laws %+v", got.Personas, got.Laws)
	}
	if len(got.Settings) == 0 {
		t.Fatal("settings are empty, want the effective sections")
	}
}

func TestStudioOmitsASubtaskWorkflowWithTheSameGuards(t *testing.T) {
	root := studioBundle(nil)
	sub := studioBundle(nil)
	root.SubtaskBundle = &sub
	got := studioOf(config.BuildSnapshot(root))
	if got.Subtask != nil {
		t.Fatalf("subtask = %+v, want none when the guards match", got.Subtask)
	}
}

func TestKnowledgeReturnsTheProjectSnapshot(t *testing.T) {
	snapshot := domain.KnowledgeSnapshot{
		Resources: []domain.KnowledgeResource{{ID: "markdown:alpha:docs/a.md", Project: "alpha", Kind: "markdown", Title: "A", Path: "docs/a.md"}},
		Relations: []domain.KnowledgeRelation{{From: "markdown:alpha:docs/a.md", To: "markdown:alpha:docs/b.md", Kind: "links"}},
	}
	server := newStudioServer(t, fakeRuntimes{projects: map[string]int64{"alpha": 7}, knowledge: snapshot})
	rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/knowledge", "", nil)
	env := decode(t, rec)
	var got domain.KnowledgeSnapshot
	if err := json.Unmarshal(env.Data, &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("knowledge = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	if diff := cmp.Diff(snapshot, got); diff != "" {
		t.Fatalf("knowledge (-want +got):\n%s", diff)
	}
}

func newStudioServer(t *testing.T, runtimes fakeRuntimes) *Server {
	t.Helper()
	runtimes.ops = &fakeOps{}
	runtimes.catalog = config.NewCatalog(nil, &config.Language{Code: "en", Keys: map[string]string{}})
	server, err := New(Options{Token: testToken, Version: "test", Runtimes: runtimes, Hub: NewHub(), Log: fakeLog{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server
}
