package tui

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// scopedFeedModel builds a TUI model backed by a real sqlite store seeded
// with one task, one task-scoped comment, one project-scoped comment, and
// one universal comment. Returns the model and the project so callers can
// assert the scope-flexible fetch/render path against concrete ids.
func scopedFeedModel(t *testing.T) (Model, domain.Project, domain.Task) {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}

	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeTask, ProjectID: project.ID, TaskID: task.ID,
		Body: "task body", AuthorType: "human",
	}); err != nil {
		t.Fatalf("AddScopedComment(task): %v", err)
	}
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "project handoff body", AuthorType: "agent",
	}); err != nil {
		t.Fatalf("AddScopedComment(project): %v", err)
	}
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeProject, ProjectID: project.ID,
		Body: "pinned cover sheet", AuthorType: "agent", Pinned: true,
	}); err != nil {
		t.Fatalf("AddScopedComment(project pinned): %v", err)
	}
	if _, err := store.AddScopedComment(ctx, domain.CommentWrite{
		Scope: domain.CommentScopeUniversal,
		Body:  "universal body", AuthorType: "human",
	}); err != nil {
		t.Fatalf("AddScopedComment(universal): %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160
	return model, project, task
}

// TestCommentsForProjectScopeFetchesProjectAndUniversal proves the scoped
// fetch helper returns project- and universal-scoped comment events for the
// current project, with the correct entity_type/entity_id, and excludes the
// task-scoped comment that belongs to the task feed.
func TestCommentsForProjectScopeFetchesProjectAndUniversal(t *testing.T) {
	model, project, _ := scopedFeedModel(t)

	events, err := model.commentsForProjectScope(domain.CommentFilter{})
	if err != nil {
		t.Fatalf("commentsForProjectScope() = %v", err)
	}

	gotScopes := map[string]int{}
	for _, ev := range events {
		if ev.EventType != domain.EventTypeComment {
			t.Fatalf("non-comment event in scoped feed: %+v", ev)
		}
		gotScopes[ev.EntityType]++
		if ev.EntityType == domain.EventEntityTask {
			t.Fatalf("task-scoped comment leaked into project feed: %+v", ev)
		}
		if ev.EntityType == domain.EventEntityProject && ev.EntityID != project.ID {
			t.Fatalf("project event entity_id = %d, want project id %d", ev.EntityID, project.ID)
		}
		if ev.EntityType == domain.EventEntityUniversal && ev.EntityID != 0 {
			t.Fatalf("universal event entity_id = %d, want 0", ev.EntityID)
		}
	}
	if gotScopes[domain.EventEntityProject] != 2 {
		t.Fatalf("project-scoped events = %d, want 2", gotScopes[domain.EventEntityProject])
	}
	if gotScopes[domain.EventEntityUniversal] != 1 {
		t.Fatalf("universal-scoped events = %d, want 1", gotScopes[domain.EventEntityUniversal])
	}

	// Pinned-first: the pinned project comment must lead the slice.
	if len(events) == 0 || !strings.Contains(events[0].Body, "pinned cover sheet") {
		t.Fatalf("pinned comment not first; head = %+v", events)
	}

	// Render smoke: the same comment renderer must produce a card for a
	// project-scoped event without assuming a task owner.
	card := model.renderCommentCardSelected(eventToComment(events[0]), true)
	if !strings.Contains(card, "pinned cover sheet") {
		t.Fatalf("project comment card missing body:\n%s", card)
	}
}

// TestCommentsForProjectScopeFilterByKind proves the optional filter seam
// passes through to QueryComments (here narrowing by scope) so #390 can wire
// kind/tag/FTS/pinned without touching this helper.
func TestCommentsForProjectScopeFilterByScope(t *testing.T) {
	model, _, _ := scopedFeedModel(t)

	events, err := model.commentsForProjectScope(domain.CommentFilter{Scope: domain.CommentScopeUniversal})
	if err != nil {
		t.Fatalf("commentsForProjectScope(universal) = %v", err)
	}
	if len(events) != 1 || events[0].EntityType != domain.EventEntityUniversal {
		t.Fatalf("scope=universal filter returned %d events: %+v", len(events), events)
	}
}

// TestActivityCacheKeyDisambiguatesScope pins the cache-collision fix: a
// task-scoped feed and a project-scoped feed that share cursor + widths must
// fingerprint to different keys, so they never share a cached card slice.
func TestActivityCacheKeyDisambiguatesScope(t *testing.T) {
	model, _, _ := scopedFeedModel(t)

	taskFeed := []domain.Event{{
		ID: 1, EntityType: domain.EventEntityTask, EntityID: 99,
		EventType: domain.EventTypeComment, Body: "shared", AuthorType: "human",
	}}
	projectFeed := []domain.Event{{
		ID: 1, EntityType: domain.EventEntityProject, EntityID: 99,
		EventType: domain.EventTypeComment, Body: "shared", AuthorType: "human",
	}}

	if model.activityRowsForRenderKey(taskFeed) == model.activityRowsForRenderKey(projectFeed) {
		t.Fatal("task and project feeds with same ids/cursor/width hashed identical — cache would collide")
	}

	// Warm the cache with the task feed, then prove the project feed does not
	// hit that warmed slice (it must rebuild against its own scope key).
	taskCards := model.cachedActivityRowsForRender(taskFeed)
	projectCards := model.cachedActivityRowsForRender(projectFeed)
	if sliceHeader(taskCards) == sliceHeader(projectCards) {
		t.Fatal("project feed reused the task feed's cached slice — scope not disambiguated")
	}
}
