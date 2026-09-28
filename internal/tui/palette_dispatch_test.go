package tui

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/tui/palette"
	"omakiten/internal/tui/screenhost"
)

type fixedSearchPort struct {
	hits []domain.SearchHit
	err  error
}

func (s fixedSearchPort) Search(context.Context, domain.ProjectContext, string, []string) ([]domain.SearchHit, error) {
	return s.hits, s.err
}

// stubSearchService builds an isolated SearchService against a
// per-test snapstore so dispatchPaletteSearch returns a non-nil
// tea.Cmd without sharing storage with the picker model under
// test (the picker fixture exposes the DB only via repos.Tasks,
// which is not assignable to *sqlite.Store at this seam).
func stubSearchService(t *testing.T) SearchPort {
	t.Helper()
	store := snapstore.Open(t, t.TempDir()+"/search.db")
	svc := operation.NewService(store, contract.ProjectSelector{})
	svc.SetSnapshot(store.Snapshot())
	return searchAdapter{svc: svc}
}

func TestDispatchTrickNavResolvesAndCloses(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "nav", Operand: "41", Raw: "nav:41"})
	if model.paletteOpen {
		t.Fatalf("nav dispatch should close palette on success")
	}
	if model.navigationTop() != screenhost.TopSettings || model.navigation != screenhost.SettingsGeneral {
		t.Fatalf("after nav:41 (top, sub) = (%v, %v), want (screenhost.TopSettings, screenhost.SettingsGeneral)", model.navigationTop(), model.navigation)
	}
}

func TestDispatchTrickNavUnknownCodeKeepsPaletteOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	prevTop := model.navigationTop()
	prevSub := model.navigation
	model.dispatchTrick(palette.Token{Verb: "nav", Operand: "99", Raw: "nav:99"})
	if !model.paletteOpen {
		t.Fatalf("unknown nav code should keep palette open")
	}
	if model.navigationTop() != prevTop || model.navigation != prevSub {
		t.Fatalf("unknown nav code mutated nav state: (%v, %v) → (%v, %v)", prevTop, prevSub, model.navigationTop(), model.navigation)
	}
	if model.palette.Status() == "" {
		t.Fatalf("unknown nav code should set inline status")
	}
	if !strings.Contains(model.palette.Status(), "99") {
		t.Fatalf("status %q should mention the offending code", model.palette.Status())
	}
}

func TestDispatchTrickOpRejectsNonNumeric(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "op", Operand: "abc", Raw: "op:abc"})
	if !model.paletteOpen {
		t.Fatalf("op with non-numeric operand should keep palette open")
	}
	if !strings.Contains(model.palette.Status(), "positive task id") {
		t.Fatalf("status %q should mention task id requirement", model.palette.Status())
	}
}

func TestDispatchTrickOpUnknownTaskKeepsPaletteOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "op", Operand: "999999", Raw: "op:999999"})
	if !model.paletteOpen {
		t.Fatalf("op with unknown task id should keep palette open")
	}
	if !strings.Contains(model.palette.Status(), "999999") {
		t.Fatalf("status %q should mention the unknown task id", model.palette.Status())
	}
}

func TestDispatchTrickUserDefinedVerbClosesPalette(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "hook", Operand: "1", Raw: "hook:1"})
	if model.paletteOpen {
		t.Fatalf("user-defined verb should close palette (event already emitted; hook side-effect takes over)")
	}
}

func TestJumpToRouteUnknownReturnsError(t *testing.T) {
	model, _ := newPickerModel(t)
	if err := model.jumpToRoute(palette.Route("not.a.route")); err == nil {
		t.Fatalf("jumpToRoute(unknown) error = nil, want non-nil")
	}
}

func TestJumpToRouteAllBindingsResolve(t *testing.T) {
	model, _ := newPickerModel(t)
	for _, descriptor := range paletteScreenDescriptors() {
		if err := model.jumpToRoute(descriptor.Route); err != nil {
			t.Errorf("jumpToRoute(%q) error = %v", descriptor.Route, err)
		}
	}
}

func TestDispatchPaletteSearchReturnsCmdWhenRepoPresent(t *testing.T) {
	model, _ := newPickerModel(t)
	model.repos.Search = stubSearchService(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	cmd := model.dispatchPaletteSearch("anything")
	if cmd == nil {
		t.Fatalf("dispatchPaletteSearch returned nil cmd; expected async tea.Cmd")
	}
	if model.palette.Status() == "" {
		t.Fatalf("synchronous pre-status not set; user should see immediate feedback")
	}
	if !strings.Contains(model.palette.Status(), "searching") {
		t.Fatalf("pre-status = %q, want \"searching\" prefix", model.palette.Status())
	}
	result, ok := cmd().(paletteSearchResultMsg)
	if !ok {
		t.Fatalf("search command returned %T, want paletteSearchResultMsg", result)
	}
	if result.projectID != model.project.ID || result.projectGeneration != model.projectGeneration {
		t.Fatalf("search result project scope = (%d, %d), want (%d, %d)", result.projectID, result.projectGeneration, model.project.ID, model.projectGeneration)
	}
	if result.paletteOpenGeneration != model.paletteOpenGeneration || result.paletteSearchGeneration != 1 {
		t.Fatalf("search result generations = (%d, %d), want (%d, 1)", result.paletteOpenGeneration, result.paletteSearchGeneration, model.paletteOpenGeneration)
	}
}

func TestDispatchPaletteSearchNilRepoStaysSyncStatus(t *testing.T) {
	model, _ := newPickerModel(t)
	model.repos.Search = nil
	if rt := model.repos.Cache.View(model.repos.ProjectID); rt != nil {
		rt.Service = nil
	}
	model.paletteOpen = true
	model.palette = palette.NewModel()
	cmd := model.dispatchPaletteSearch("anything")
	if cmd != nil {
		t.Fatalf("nil-repo path should not return a cmd; got %v", cmd)
	}
	if !strings.Contains(model.palette.Status(), "not wired") {
		t.Fatalf("status = %q, want \"not wired\" mention", model.palette.Status())
	}
}

func TestPaletteSearchResultMsgStatusPathSetsInlineHint(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	next, _ := model.Update(paletteSearchResultMsg{
		query:                   "x",
		status:                  "no results for \"x\"",
		projectID:               model.project.ID,
		projectGeneration:       model.projectGeneration,
		paletteOpenGeneration:   model.paletteOpenGeneration,
		paletteSearchGeneration: model.paletteSearchGeneration,
	})
	got := next.(Model)
	if !strings.Contains(got.palette.Status(), "no results") {
		t.Fatalf("status = %q, want no-results hint", got.palette.Status())
	}
	if got.palette.HasResults() {
		t.Fatalf("HasResults true on status-only path")
	}
}

func TestPaletteSearchResultMsgHitsPathPopulatesList(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	hits := []domain.SearchHit{
		{EntityType: domain.SearchEntityTask, ID: 42, Snippet: "foo", ProjectID: model.project.ID},
		{EntityType: domain.SearchEntityComment, ID: 99, Snippet: "bar", ProjectID: model.project.ID},
	}
	next, _ := model.Update(paletteSearchResultMsg{
		query:                   "x",
		hits:                    hits,
		projectID:               model.project.ID,
		projectGeneration:       model.projectGeneration,
		paletteOpenGeneration:   model.paletteOpenGeneration,
		paletteSearchGeneration: model.paletteSearchGeneration,
	})
	got := next.(Model)
	if !got.palette.HasResults() {
		t.Fatalf("HasResults false after hits path")
	}
	if len(got.palette.Results()) != 2 {
		t.Fatalf("Results len = %d, want 2", len(got.palette.Results()))
	}
}

func TestPaletteSearchResultMsgFiltersToActiveProject(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	activeID := model.project.ID
	foreignID := activeID + 1

	next, _ := model.Update(paletteSearchResultMsg{
		query: "mixed",
		hits: []domain.SearchHit{
			{EntityType: domain.SearchEntityTask, ID: 42, Snippet: "active snippet", ProjectID: activeID},
			{EntityType: domain.SearchEntityPlan, ID: 99, Snippet: "foreign snippet", ProjectID: foreignID},
		},
		status:                  "foreign status",
		projectID:               activeID,
		projectGeneration:       model.projectGeneration,
		paletteOpenGeneration:   model.paletteOpenGeneration,
		paletteSearchGeneration: model.paletteSearchGeneration,
	})
	got := next.(Model)
	results := got.palette.Results()
	if len(results) != 1 || results[0].ProjectID != activeID || results[0].Snippet != "active snippet" {
		t.Fatalf("filtered results = %+v, want only active-project hit", results)
	}
	if results[0].EntityType != domain.SearchEntityTask {
		t.Fatalf("filtered entity type = %q, want task", results[0].EntityType)
	}
	if got.palette.Status() != "" {
		t.Fatalf("status = %q, want SetResults to clear the response status", got.palette.Status())
	}
}

func TestPaletteSearchResultMsgAllWrongProjectIsNoOp(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	activeID := model.project.ID
	model.palette.SetResults([]domain.SearchHit{
		{EntityType: domain.SearchEntityTask, ID: 7, Snippet: "current snippet", ProjectID: activeID},
		{EntityType: domain.SearchEntityComment, ID: 8, Snippet: "selected snippet", ProjectID: activeID},
	})
	model.palette.SetStatus("current status")
	beforeResults := model.palette.Results()
	beforeCursor := model.palette.ResultsCursor()

	next, _ := model.Update(paletteSearchResultMsg{
		query: "wrong",
		hits: []domain.SearchHit{
			{EntityType: domain.SearchEntityPlan, ID: 101, Snippet: "foreign plan", ProjectID: activeID + 1},
			{EntityType: domain.SearchEntityError, ID: 102, Snippet: "foreign error", ProjectID: activeID + 2},
		},
		status:                  "foreign status",
		projectID:               activeID,
		projectGeneration:       model.projectGeneration,
		paletteOpenGeneration:   model.paletteOpenGeneration,
		paletteSearchGeneration: model.paletteSearchGeneration,
	})
	got := next.(Model)
	if got.palette.Status() != "current status" {
		t.Fatalf("status = %q, want current status", got.palette.Status())
	}
	if got.palette.ResultsCursor() != beforeCursor {
		t.Fatalf("results cursor = %d, want unchanged %d", got.palette.ResultsCursor(), beforeCursor)
	}
	if results := got.palette.Results(); len(results) != len(beforeResults) || results[0] != beforeResults[0] || results[1] != beforeResults[1] {
		t.Fatalf("results = %+v, want unchanged %+v", results, beforeResults)
	}
}

func TestPaletteSearchResultMsgIgnoredAfterRuntimeRotation(t *testing.T) {
	for name, tc := range map[string]struct {
		err error
	}{
		"success": {},
		"failure": {err: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			model, _ := newPickerModel(t)
			model.paletteOpen = true
			model.palette = palette.NewModel()
			model.studioRuntimeGeneration = 7
			model.repos.Search = fixedSearchPort{
				hits: []domain.SearchHit{{EntityType: domain.SearchEntityPlan, ID: 1, Snippet: "old runtime", ProjectID: model.project.ID}},
				err:  tc.err,
			}
			cmd := model.dispatchPaletteSearch("old-runtime")
			result := cmd().(paletteSearchResultMsg)
			if result.runtimeGeneration != 7 {
				t.Fatalf("captured runtime generation = %d, want 7", result.runtimeGeneration)
			}
			model.studioRuntimeGeneration = 8
			model.palette.SetStatus("current runtime")

			next, _ := model.Update(result)
			got := next.(Model)
			if got.palette.Status() != "current runtime" || got.palette.HasResults() {
				t.Fatalf("stale runtime palette result leaked: status=%q results=%+v", got.palette.Status(), got.palette.Results())
			}
		})
	}
}

func TestPaletteSearchDelayedProjectSwitchResultsIgnored(t *testing.T) {
	for name, tc := range map[string]struct {
		hits []domain.SearchHit
		err  error
	}{
		"success": {
			hits: []domain.SearchHit{{EntityType: domain.SearchEntityPlan, ID: 1, Snippet: "project A"}},
		},
		"failure": {err: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			model, _ := newPickerModel(t)
			model.paletteOpen = true
			model.palette = palette.NewModel()
			projectA := model.project.ID
			hits := append([]domain.SearchHit(nil), tc.hits...)
			for i := range hits {
				hits[i].ProjectID = projectA
			}
			model.repos.Search = fixedSearchPort{hits: hits, err: tc.err}
			cmd := model.dispatchPaletteSearch("project-a")
			if cmd == nil {
				t.Fatal("dispatchPaletteSearch returned nil command")
			}
			result := cmd().(paletteSearchResultMsg)
			model.project.ID++
			model.projectGeneration++
			model.palette.SetStatus("project B current")

			next, _ := model.Update(result)
			got := next.(Model)
			if got.palette.Status() != "project B current" || got.palette.HasResults() {
				t.Fatalf("delayed project-A result leaked into B: status=%q results=%+v", got.palette.Status(), got.palette.Results())
			}
		})
	}
}

func TestPaletteSearchDelayedRoundTripResultsIgnored(t *testing.T) {
	for name, tc := range map[string]struct {
		hits []domain.SearchHit
		err  error
	}{
		"success": {
			hits: []domain.SearchHit{{EntityType: domain.SearchEntityComment, ID: 2, Snippet: "old A"}},
		},
		"failure": {err: context.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			model, _ := newPickerModel(t)
			model.paletteOpen = true
			model.palette = palette.NewModel()
			projectA := model.project.ID
			hits := append([]domain.SearchHit(nil), tc.hits...)
			for i := range hits {
				hits[i].ProjectID = projectA
			}
			model.repos.Search = fixedSearchPort{hits: hits, err: tc.err}
			cmd := model.dispatchPaletteSearch("old-a")
			if cmd == nil {
				t.Fatal("dispatchPaletteSearch returned nil command")
			}
			result := cmd().(paletteSearchResultMsg)
			model.project.ID = projectA + 1
			model.projectGeneration++
			model.project.ID = projectA
			model.projectGeneration++
			model.palette.SetStatus("new A current")

			next, _ := model.Update(result)
			got := next.(Model)
			if got.palette.Status() != "new A current" || got.palette.HasResults() {
				t.Fatalf("delayed first-visit result leaked after A-B-A: status=%q results=%+v", got.palette.Status(), got.palette.Results())
			}
		})
	}
}

func TestPaletteSearchResultMsgIgnoredWhenClosed(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = false
	model.palette = palette.NewModel()
	model.palette.SetStatus("stale")
	next, _ := model.Update(paletteSearchResultMsg{query: "x", status: "fresh result"})
	got := next.(Model)
	if got.palette.Status() != "stale" {
		t.Fatalf("closed-overlay path mutated status to %q; should leave prior value intact", got.palette.Status())
	}
}

func TestPaletteSearchResultMsgIgnoredAfterProjectSwitch(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.project.ID = 2
	model.projectGeneration = 3
	model.palette.SetStatus("current project")

	next, _ := model.Update(paletteSearchResultMsg{
		projectID:         1,
		projectGeneration: 2,
		status:            "stale project result",
	})
	got := next.(Model)
	if got.palette.Status() != "current project" {
		t.Fatalf("stale project search mutated status to %q", got.palette.Status())
	}
}

func TestPaletteSearchResultMsgIgnoredOutOfOrderWithinProject(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.paletteOpenGeneration = 4
	model.paletteSearchGeneration = 2
	model.palette.SetStatus("newer query")

	next, _ := model.Update(paletteSearchResultMsg{
		query:                   "old",
		status:                  "old query result",
		projectID:               model.project.ID,
		projectGeneration:       model.projectGeneration,
		paletteOpenGeneration:   4,
		paletteSearchGeneration: 1,
	})
	got := next.(Model)
	if got.palette.Status() != "newer query" {
		t.Fatalf("out-of-order same-project search mutated status to %q", got.palette.Status())
	}
}

func TestPaletteSearchResultMsgIgnoredAfterCloseAndReopen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.paletteOpenGeneration = 1
	model.paletteSearchGeneration = 1
	model.palette.SetStatus("current session")

	closed, _ := model.Update(palette.DismissMsg{})
	opened, _ := closed.(Model).Update(ctrlK())
	current := opened.(Model)
	current.palette.SetStatus("reopened session")

	next, _ := current.Update(paletteSearchResultMsg{
		query:                   "old",
		status:                  "closed session result",
		projectID:               current.project.ID,
		projectGeneration:       current.projectGeneration,
		paletteOpenGeneration:   1,
		paletteSearchGeneration: 1,
	})
	got := next.(Model)
	if got.palette.Status() != "reopened session" {
		t.Fatalf("close/reopen search mutated status to %q", got.palette.Status())
	}
}

func TestPaletteSearchRejectsHomeWithoutActiveProject(t *testing.T) {
	model, _ := newPickerModel(t)
	model.project = domain.ProjectContext{}
	model.paletteOpen = true
	model.palette = palette.NewModel()

	if cmd := model.dispatchPaletteSearch("shared"); cmd != nil {
		t.Fatal("Home search returned a command without an active project")
	}
	if model.palette.Status() != "search requires an active project" {
		t.Fatalf("Home search status = %q, want active-project rejection", model.palette.Status())
	}

	next, _ := model.Update(paletteSearchResultMsg{
		query:                   "shared",
		status:                  "cross-project result",
		projectID:               0,
		projectGeneration:       0,
		paletteOpenGeneration:   model.paletteOpenGeneration,
		paletteSearchGeneration: model.paletteSearchGeneration,
	})
	if got := next.(Model).palette.Status(); got != "search requires an active project" {
		t.Fatalf("Home result status = %q, want rejection preserved", got)
	}
}

func TestDispatchOpenHitTaskOpensViewAndClosesPalette(t *testing.T) {
	model, _ := newPickerModel(t)
	// Seed a task in the same project the picker model holds so
	// GetTaskByID can find it. The picker fixture wires repos.Tasks
	// to *snapstore.Store, so a type assertion gives access to the
	// CreateTask + Snapshot pair the test fixture exposes.
	store, ok := model.repos.Tasks.(*snapstore.Store)
	if !ok {
		t.Fatalf("repos.Tasks is %T, expected *snapstore.Store", model.repos.Tasks)
	}
	if err := store.ImportBundle(model.ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("seed bundle: %v", err)
	}
	task, err := store.CreateTask(model.ctx, model.project.ID, "Search target", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchOpenHit(domain.SearchHit{EntityType: domain.SearchEntityTask, ID: task.ID})
	if model.paletteOpen {
		t.Fatalf("open-task dispatch did not close the palette")
	}
	if model.taskDetailScreen.Payload().Task.ID != task.ID {
		t.Fatalf("taskID = %d after open, want %d", model.taskDetailScreen.Payload().Task.ID, task.ID)
	}
	if !model.inTaskDetail() {
		t.Fatal("task detail still closed after open-task dispatch")
	}
}

func TestDispatchOpenHitUnknownTaskKeepsPaletteOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchOpenHit(domain.SearchHit{EntityType: domain.SearchEntityTask, ID: 999999})
	if !model.paletteOpen {
		t.Fatalf("unknown-task should keep palette open")
	}
	if !strings.Contains(model.palette.Status(), "not found") {
		t.Fatalf("status = %q, want \"not found\" mention", model.palette.Status())
	}
}

func TestDispatchOpenHitUnsupportedTypeStaysInline(t *testing.T) {
	for _, entityType := range []domain.SearchEntityType{
		domain.SearchEntityError,
		domain.SearchEntitySolution,
		domain.SearchEntityPlan,
	} {
		t.Run(string(entityType), func(t *testing.T) {
			model, _ := newPickerModel(t)
			model.paletteOpen = true
			model.palette = palette.NewModel()
			model.dispatchOpenHit(domain.SearchHit{EntityType: entityType, ID: 1})
			if !model.paletteOpen {
				t.Errorf("entity type %s closed the palette; should be inline-only", entityType)
			}
			if !strings.Contains(model.palette.Status(), "no TUI view") {
				t.Errorf("entity type %s status = %q, want \"no TUI view\" hint", entityType, model.palette.Status())
			}
		})
	}
}

func TestBuildPaletteRegistryHonorsConfigOverrides(t *testing.T) {
	model, _ := newPickerModel(t)
	if model.paletteRegistry == nil {
		t.Fatalf("paletteRegistry should be built at NewModel")
	}
	// Verify positional default lookup works without overrides.
	got, ok := model.paletteRegistry.Resolve("11")
	if !ok {
		t.Fatalf("Resolve(11) miss on default registry")
	}
	if got != palette.Route(screenhost.TasksBoard) {
		t.Fatalf("Resolve(11) = %q, want tasks.board", got)
	}
}
