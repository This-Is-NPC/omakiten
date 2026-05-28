package tui

import (
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/tui/palette"
)

// stubSearchService builds an isolated SearchService against a
// per-test snapstore so dispatchPaletteSearch returns a non-nil
// tea.Cmd without sharing storage with the picker model under
// test (the picker fixture exposes the DB only via repos.Tasks,
// which is not assignable to *sqlite.Store at this seam).
func stubSearchService(t *testing.T) *app.SearchService {
	t.Helper()
	store := snapstore.Open(t, t.TempDir()+"/search.db")
	return app.NewSearchService(store.Store, store.Store)
}

func TestDispatchTrickNavResolvesAndCloses(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	model.dispatchTrick(palette.Token{Verb: "nav", Operand: "31", Raw: "nav:31"})
	if model.paletteOpen {
		t.Fatalf("nav dispatch should close palette on success")
	}
	if model.top != topSettings || model.sub != subSettingsGeneral {
		t.Fatalf("after nav:31 (top, sub) = (%v, %v), want (topSettings, subSettingsGeneral)", model.top, model.sub)
	}
}

func TestDispatchTrickNavUnknownCodeKeepsPaletteOpen(t *testing.T) {
	model, _ := newPickerModel(t)
	model.paletteOpen = true
	model.palette = palette.NewModel()
	prevTop := model.top
	prevSub := model.sub
	model.dispatchTrick(palette.Token{Verb: "nav", Operand: "99", Raw: "nav:99"})
	if !model.paletteOpen {
		t.Fatalf("unknown nav code should keep palette open")
	}
	if model.top != prevTop || model.sub != prevSub {
		t.Fatalf("unknown nav code mutated nav state: (%v, %v) → (%v, %v)", prevTop, prevSub, model.top, model.sub)
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
	for _, descriptor := range palette.DefaultScreens() {
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
}

func TestDispatchPaletteSearchNilRepoStaysSyncStatus(t *testing.T) {
	model, _ := newPickerModel(t)
	model.repos.Search = nil
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
	next, _ := model.Update(paletteSearchResultMsg{query: "x", status: "no results for \"x\""})
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
		{EntityType: domain.SearchEntityTask, ID: 42, Snippet: "foo"},
		{EntityType: domain.SearchEntityComment, ID: 99, Snippet: "bar"},
	}
	next, _ := model.Update(paletteSearchResultMsg{query: "x", hits: hits})
	got := next.(Model)
	if !got.palette.HasResults() {
		t.Fatalf("HasResults false after hits path")
	}
	if len(got.palette.Results()) != 2 {
		t.Fatalf("Results len = %d, want 2", len(got.palette.Results()))
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
	if model.taskID != task.ID {
		t.Fatalf("taskID = %d after open, want %d", model.taskID, task.ID)
	}
	if model.taskScreen == taskScreenClosed {
		t.Fatalf("taskScreen still closed after open-task dispatch")
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
		domain.SearchEntityContext,
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
	if got != palette.RouteTasksBoard {
		t.Fatalf("Resolve(11) = %q, want tasks.board", got)
	}
}
