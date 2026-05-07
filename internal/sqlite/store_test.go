package sqlite

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestStoreProjectScopedTasks(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	projectA, err := store.UpsertProject(ctx, "Project A", "a", "/work/a")
	if err != nil {
		t.Fatalf("UpsertProject(A) error = %v", err)
	}
	projectB, err := store.UpsertProject(ctx, "Project B", "b", "/work/b")
	if err != nil {
		t.Fatalf("UpsertProject(B) error = %v", err)
	}

	if _, err := store.CreateTask(ctx, projectA.ID, "A task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask(A) error = %v", err)
	}
	if _, err := store.CreateTask(ctx, projectB.ID, "B task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}

	tasks, err := store.ListTasks(ctx, projectA.ID, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListTasks() len = %d, want 1", len(tasks))
	}
	if tasks[0].Title != "A task" {
		t.Fatalf("ListTasks()[0].Title = %q, want A task", tasks[0].Title)
	}
}

// TestMoveTaskEnforcesWorkflow exercises the move-policy that the workflow
// service enforces on top of the store. The store no longer makes the
// transition decision, so this test threads through app.WorkflowService to
// keep integration coverage of the end-to-end path.
func TestMoveTaskEnforcesWorkflow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	workflow := app.NewWorkflowServiceFromStore(store)

	tests := []struct {
		name    string
		to      string
		wantErr bool
	}{
		{name: "allowed backlog to dev", to: "dev"},
		{name: "blocked dev to done", to: "done", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workflow.MoveTask(ctx, project.Context(), task.ID, tt.to)
			if tt.wantErr && err == nil {
				t.Fatalf("MoveTask() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("MoveTask() error = %v", err)
			}
		})
	}
}

func TestStoreOperationalDataIsProjectScoped(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	projectA, err := store.UpsertProject(ctx, "Project A", "a", "/work/a")
	if err != nil {
		t.Fatalf("UpsertProject(A) error = %v", err)
	}
	projectB, err := store.UpsertProject(ctx, "Project B", "b", "/work/b")
	if err != nil {
		t.Fatalf("UpsertProject(B) error = %v", err)
	}

	taskA1, err := store.CreateTask(ctx, projectA.ID, "A first", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A1) error = %v", err)
	}
	taskA2, err := store.CreateTask(ctx, projectA.ID, "A second", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A2) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, projectB.ID, "B first", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}

	if _, err := store.AddComment(ctx, projectA.ID, taskA1.ID, "A note", "human", nil); err != nil {
		t.Fatalf("AddComment(A) error = %v", err)
	}
	if _, err := store.AddComment(ctx, projectB.ID, taskB.ID, "B note", "human", nil); err != nil {
		t.Fatalf("AddComment(B) error = %v", err)
	}
	comments, err := store.ListComments(ctx, projectA.ID, 0)
	if err != nil {
		t.Fatalf("ListComments(A) error = %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "A note" {
		t.Fatalf("ListComments(A) = %#v, want only A note", comments)
	}

	if _, err := store.AddTaskDependency(ctx, projectA.ID, taskA2.ID, taskA1.ID); err != nil {
		t.Fatalf("AddTaskDependency(A) error = %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, projectA.ID, taskA2.ID, taskB.ID); err == nil {
		t.Fatalf("AddTaskDependency(cross project) error = nil, want error")
	}
	dependencies, err := store.ListTaskDependencies(ctx, projectA.ID, 0)
	if err != nil {
		t.Fatalf("ListTaskDependencies(A) error = %v", err)
	}
	if len(dependencies) != 1 || dependencies[0].DependsOnTaskID != taskA1.ID {
		t.Fatalf("ListTaskDependencies(A) = %#v, want only A dependency", dependencies)
	}

	if _, err := store.AddContextEntry(ctx, projectA.ID, "A context", 2); err != nil {
		t.Fatalf("AddContextEntry(A) error = %v", err)
	}
	if _, err := store.AddContextEntry(ctx, projectB.ID, "B context", 2); err != nil {
		t.Fatalf("AddContextEntry(B) error = %v", err)
	}
	entries, err := store.ListContextEntries(ctx, projectA.ID)
	if err != nil {
		t.Fatalf("ListContextEntries(A) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Body != "A context" {
		t.Fatalf("ListContextEntries(A) = %#v, want only A context", entries)
	}
}

func TestDependencyServiceRejectsCycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	taskA, err := store.CreateTask(ctx, project.ID, "A", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, project.ID, "B", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}
	service := app.NewDependencyService(store)
	if _, err := service.Add(ctx, project.Context(), taskA.ID, taskB.ID); err != nil {
		t.Fatalf("Add(A->B) error = %v", err)
	}
	if _, err := service.Add(ctx, project.Context(), taskB.ID, taskA.ID); err == nil {
		t.Fatalf("Add(B->A) error = nil, want cycle error")
	}
}

func TestStoreActiveWorkflow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	workflow, err := store.ActiveWorkflow(ctx)
	if err != nil {
		t.Fatalf("ActiveWorkflow() error = %v", err)
	}
	if workflow.Key != "default" {
		t.Fatalf("ActiveWorkflow().Key = %q, want default", workflow.Key)
	}
	if len(workflow.Buckets) != 3 {
		t.Fatalf("ActiveWorkflow().Buckets len = %d, want 3", len(workflow.Buckets))
	}
	if len(workflow.Transitions) != 1 {
		t.Fatalf("ActiveWorkflow().Transitions len = %d, want 1", len(workflow.Transitions))
	}
}

func sqliteTestBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []config.Skill{{Slug: "go", Name: "Go"}},
		Personas: []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}},
		Laws:     []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
				{ID: 3, Key: "done", Name: "Done", Position: 3},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}

func TestStoreFindProject(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	found, err := store.FindProjectByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("FindProjectByID() error = %v", err)
	}
	if found.ID != project.ID {
		t.Fatalf("FindProjectByID().ID = %d, want %d", found.ID, project.ID)
	}

	_, err = store.FindProjectByID(ctx, 9999)
	if err == nil {
		t.Fatal("FindProjectByID() error = nil, want not found")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("FindProjectByID() error = %T %v, want project_not_found", err, err)
	}
	if coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("FindProjectByID() code = %q, want %q", coded.Code, domain.ErrProjectNotFound)
	}
	if coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("FindProjectByID() code = %q, want %q", coded.Code, domain.ErrProjectNotFound)
	}

	foundSlug, err := store.FindProjectBySlug(ctx, "project")
	if err != nil {
		t.Fatalf("FindProjectBySlug() error = %v", err)
	}
	if foundSlug.ID != project.ID {
		t.Fatalf("FindProjectBySlug().ID = %d, want %d", foundSlug.ID, project.ID)
	}

	_, err = store.FindProjectBySlug(ctx, "missing")
	if err == nil {
		t.Fatal("FindProjectBySlug() error = nil, want not found")
	}

	paths, err := store.FindProjectsContainingPath(ctx, "/work/project/src")
	if err != nil {
		t.Fatalf("FindProjectsContainingPath() error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("FindProjectsContainingPath() len = %d, want 1", len(paths))
	}

	paths, err = store.FindProjectsContainingPath(ctx, "/other/path")
	if err != nil {
		t.Fatalf("FindProjectsContainingPath() error = %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("FindProjectsContainingPath() len = %d, want 0", len(paths))
	}
}

func TestStoreListActiveEntities(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	// No bundle imported yet
	skills, err := store.ListActiveSkills(ctx)
	if err != nil {
		t.Fatalf("ListActiveSkills() error = %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("ListActiveSkills() len = %d, want 0", len(skills))
	}

	personas, err := store.ListActivePersonas(ctx)
	if err != nil {
		t.Fatalf("ListActivePersonas() error = %v", err)
	}
	if len(personas) != 0 {
		t.Fatalf("ListActivePersonas() len = %d, want 0", len(personas))
	}

	laws, err := store.ListActiveLaws(ctx)
	if err != nil {
		t.Fatalf("ListActiveLaws() error = %v", err)
	}
	if len(laws) != 0 {
		t.Fatalf("ListActiveLaws() len = %d, want 0", len(laws))
	}

	// After import
	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	skills, err = store.ListActiveSkills(ctx)
	if err != nil {
		t.Fatalf("ListActiveSkills() error = %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("ListActiveSkills() len = %d, want 1", len(skills))
	}
}

func TestStoreUpdateTask(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	_, err = store.UpdateTask(ctx, project.ID, 9999, domain.TaskUpdate{})
	if err == nil {
		t.Fatal("UpdateTask() error = nil, want not found")
	}

	task, err := store.CreateTask(ctx, project.ID, "Task", "Desc", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	newTitle := "Updated"
	updated, err := store.UpdateTask(ctx, project.ID, task.ID, domain.TaskUpdate{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if updated.Title != "Updated" {
		t.Fatalf("UpdateTask().Title = %q, want %q", updated.Title, "Updated")
	}
}

func TestStoreTaskCount(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	projectA, _ := store.UpsertProject(ctx, "A", "a", "/work/a")
	projectB, _ := store.UpsertProject(ctx, "B", "b", "/work/b")

	if _, err := store.CreateTask(ctx, projectA.ID, "A1", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, projectB.ID, "B1", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	countA, err := store.TaskCount(ctx, projectA.ID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if countA != 1 {
		t.Fatalf("TaskCount(A) = %d, want 1", countA)
	}

	countB, err := store.TaskCount(ctx, projectB.ID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if countB != 1 {
		t.Fatalf("TaskCount(B) = %d, want 1", countB)
	}
}

func TestStoreRemoveTaskDependency(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	taskA, _ := store.CreateTask(ctx, project.ID, "A", "", "", "backlog")
	taskB, _ := store.CreateTask(ctx, project.ID, "B", "", "", "backlog")

	if _, err := store.AddTaskDependency(ctx, project.ID, taskB.ID, taskA.ID); err != nil {
		t.Fatalf("AddTaskDependency() error = %v", err)
	}

	if err := store.RemoveTaskDependency(ctx, project.ID, taskB.ID, taskA.ID); err != nil {
		t.Fatalf("RemoveTaskDependency() error = %v", err)
	}

	// Removing non-existent is a no-op
	if err := store.RemoveTaskDependency(ctx, project.ID, taskB.ID, taskA.ID); err != nil {
		t.Fatalf("RemoveTaskDependency() second call error = %v", err)
	}
}

func TestStoreMoveTaskErrors(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog")

	_, err = store.MoveTask(ctx, project.ID, 9999, "dev")
	if err == nil {
		t.Fatal("MoveTask() error = nil, want not found")
	}

	_, err = store.MoveTask(ctx, project.ID, task.ID, "missing")
	if err == nil {
		t.Fatal("MoveTask() error = nil, want bucket not found")
	}

	// Move to same bucket should succeed without transition check
	moved, err := store.MoveTask(ctx, project.ID, task.ID, "backlog")
	if err != nil {
		t.Fatalf("MoveTask(same bucket) error = %v", err)
	}
	if moved.BucketKey != "backlog" {
		t.Fatalf("MoveTask().BucketKey = %q, want backlog", moved.BucketKey)
	}
}

func bundleWithBlockersInGuard() config.Bundle {
	b := sqliteTestBundle()
	b.Workflows[0].Transitions = []config.Transition{{
		From: 1, To: 2,
		Guards: []config.TransitionGuard{{Type: "blockers_in", Buckets: []string{"done"}}},
	}}
	return b
}

func bundleWithCommentsMinGuard(minCount int) config.Bundle {
	b := sqliteTestBundle()
	b.Workflows[0].Transitions = []config.Transition{{
		From: 1, To: 2,
		Guards: []config.TransitionGuard{{Type: "comments_min", Count: minCount}},
	}}
	return b
}

func bundleWithCommentsTaggedGuard(tag string, minCount int) config.Bundle {
	b := sqliteTestBundle()
	b.Workflows[0].Transitions = []config.Transition{{
		From: 1, To: 2,
		Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: tag, Count: minCount}},
	}}
	return b
}

func TestMoveTaskGuardBlockersIn(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, bundleWithBlockersInGuard(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	workflow := app.NewWorkflowServiceFromStore(store)

	blocker, _ := store.CreateTask(ctx, project.ID, "Blocker", "", "", "backlog")
	main, _ := store.CreateTask(ctx, project.ID, "Main", "", "", "backlog")
	if _, err := store.AddTaskDependency(ctx, project.ID, main.ID, blocker.ID); err != nil {
		t.Fatalf("AddTaskDependency() error = %v", err)
	}

	// Guard fails: blocker is in backlog, not done
	_, err = workflow.MoveTask(ctx, project.Context(), main.ID, "dev")
	if err == nil {
		t.Fatal("MoveTask() error = nil, want guard_violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("MoveTask() error = %v, want guard_violation", err)
	}

	// Move blocker to done bucket (create directly there since no dev→done transition)
	doneBlocker, _ := store.CreateTask(ctx, project.ID, "Done Blocker", "", "", "done")
	main2, _ := store.CreateTask(ctx, project.ID, "Main2", "", "", "backlog")
	if _, err := store.AddTaskDependency(ctx, project.ID, main2.ID, doneBlocker.ID); err != nil {
		t.Fatalf("AddTaskDependency() error = %v", err)
	}

	// Guard passes: blocker is in done
	moved, err := workflow.MoveTask(ctx, project.Context(), main2.ID, "dev")
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.BucketKey != "dev" {
		t.Fatalf("MoveTask().BucketKey = %q, want dev", moved.BucketKey)
	}
}

func TestMoveTaskGuardBlockersInNoBlockers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, bundleWithBlockersInGuard(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog")
	workflow := app.NewWorkflowServiceFromStore(store)

	// No blockers — guard passes
	moved, err := workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.BucketKey != "dev" {
		t.Fatalf("MoveTask().BucketKey = %q, want dev", moved.BucketKey)
	}
}

func TestMoveTaskGuardCommentsMin(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, bundleWithCommentsMinGuard(1), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog")
	workflow := app.NewWorkflowServiceFromStore(store)

	// Guard fails: 0 comments
	_, err = workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err == nil {
		t.Fatal("MoveTask() error = nil, want guard_violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("MoveTask() error = %v, want guard_violation", err)
	}

	// Add a comment
	if _, err := store.AddComment(ctx, project.ID, task.ID, "done", "human", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	// Guard passes: 1 comment >= 1
	moved, err := workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.BucketKey != "dev" {
		t.Fatalf("MoveTask().BucketKey = %q, want dev", moved.BucketKey)
	}
}

func TestMoveTaskNoGuardUnaffected(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, sqliteTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	blocker, _ := store.CreateTask(ctx, project.ID, "Blocker", "", "", "backlog")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog")
	if _, err := store.AddTaskDependency(ctx, project.ID, task.ID, blocker.ID); err != nil {
		t.Fatalf("AddTaskDependency() error = %v", err)
	}
	workflow := app.NewWorkflowServiceFromStore(store)

	// Transition has no guard — move succeeds even with pending blocker
	moved, err := workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.BucketKey != "dev" {
		t.Fatalf("MoveTask().BucketKey = %q, want dev", moved.BucketKey)
	}
}

func TestMoveTaskGuardCommentsTagged(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, bundleWithCommentsTaggedGuard("resume", 1), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	project, _ := store.UpsertProject(ctx, "Project", "project", "/work/project")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog")
	workflow := app.NewWorkflowServiceFromStore(store)

	// Guard fails: no comments at all
	_, err = workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err == nil {
		t.Fatal("MoveTask() error = nil, want guard_violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("MoveTask() error = %v, want guard_violation", err)
	}

	// Add comment without the required tag
	if _, err := store.AddComment(ctx, project.ID, task.ID, "untagged note", "human", nil); err != nil {
		t.Fatalf("AddComment(untagged) error = %v", err)
	}

	// Guard still fails: comment exists but lacks the tag
	_, err = workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err == nil {
		t.Fatal("MoveTask() untagged error = nil, want guard_violation")
	}
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("MoveTask() untagged error = %v, want guard_violation", err)
	}

	// Add comment with the required tag
	if _, err := store.AddComment(ctx, project.ID, task.ID, "resume note", "agent", []domain.Tag{{Name: "resume", Label: "resume"}}); err != nil {
		t.Fatalf("AddComment(tagged) error = %v", err)
	}

	// Guard passes
	moved, err := workflow.MoveTask(ctx, project.Context(), task.ID, "dev")
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.BucketKey != "dev" {
		t.Fatalf("MoveTask().BucketKey = %q, want dev", moved.BucketKey)
	}
}

func TestPathWithinRoot(t *testing.T) {
	tests := []struct {
		path     string
		root     string
		expected bool
	}{
		{"/work/project", "/work/project", true},
		{"/work/project/src", "/work/project", true},
		{"/work/other", "/work/project", false},
		{"/work", "/work/project", false},
		{"/work/project", "/work/project/src", false},
	}

	for _, tc := range tests {
		actual := pathWithinRoot(tc.path, tc.root)
		if actual != tc.expected {
			t.Errorf("pathWithinRoot(%q, %q) = %v, want %v", tc.path, tc.root, actual, tc.expected)
		}
	}
}
