package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
)

func TestOverviewUsesResolvedProjectAndCompactState(t *testing.T) {
	fixture := newAgentFixture(t)

	overview, err := fixture.service.Overview(fixture.ctx, OverviewInput{})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.Project.ID != fixture.projectA.ID {
		t.Fatalf("Overview().Project.ID = %d, want %d", overview.Project.ID, fixture.projectA.ID)
	}
	if overview.PendingCount != 2 {
		t.Fatalf("Overview().PendingCount = %d, want 2", overview.PendingCount)
	}
	if len(overview.RecentContext) != 1 || overview.RecentContext[0].Body != "A context" {
		t.Fatalf("Overview().RecentContext = %#v, want only A context", overview.RecentContext)
	}
	for _, bucket := range overview.TaskBuckets {
		if bucket.Count > 2 {
			t.Fatalf("Overview().TaskBuckets = %#v, includes another project", overview.TaskBuckets)
		}
	}
}

func TestUnregisteredProjectReturnsAgentGuidance(t *testing.T) {
	ctx := context.Background()
	store := newAgentStore(t, ctx)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	service := NewService(store, ProjectSelector{CWD: outside})

	_, err := service.Overview(ctx, OverviewInput{})
	if err == nil {
		t.Fatalf("Overview() error = nil, want project_not_found")
	}
	failure := FailureFromError(err)
	if failure.Code != string(domain.ErrProjectNotFound) {
		t.Fatalf("FailureFromError().Code = %q, want project_not_found", failure.Code)
	}
	if !strings.Contains(strings.Join(failure.Guidance.Actions, " "), "okt init") {
		t.Fatalf("FailureFromError().Guidance = %#v, want okt init action", failure.Guidance)
	}
}

func TestContinueTaskRejectsCrossProjectTask(t *testing.T) {
	fixture := newAgentFixture(t)

	_, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: fixture.taskB.ID})
	assertCodedError(t, err, domain.ErrTaskNotFound)
}

func TestResumeProjectDoesNotMixProjectState(t *testing.T) {
	fixture := newAgentFixture(t)

	resume, err := fixture.service.ResumeProject(fixture.ctx, ResumeProjectInput{})
	if err != nil {
		t.Fatalf("ResumeProject() error = %v", err)
	}
	if len(resume.Dependencies) != 1 || resume.Dependencies[0].TaskID != fixture.taskA2.ID {
		t.Fatalf("ResumeProject().Dependencies = %#v, want only project A dependency", resume.Dependencies)
	}
	for _, task := range resume.LikelyNextWork {
		if task.ID == fixture.taskB.ID {
			t.Fatalf("ResumeProject().LikelyNextWork includes project B task: %#v", resume.LikelyNextWork)
		}
	}
	for _, entry := range resume.RecentContext {
		if entry.Body == "B context" {
			t.Fatalf("ResumeProject().RecentContext includes project B context: %#v", resume.RecentContext)
		}
	}
}

func TestCreateTaskIntentRequiresConfirmationForSimilarWork(t *testing.T) {
	fixture := newAgentFixture(t)

	before, err := fixture.service.ListTasks(fixture.ctx, ListTasksInput{})
	if err != nil {
		t.Fatalf("ListTasks(before) error = %v", err)
	}
	created, err := fixture.service.CreateTaskIntent(fixture.ctx, CreateTaskInput{Description: "Add MCP agent integration for AI harnesses"})
	if err != nil {
		t.Fatalf("CreateTaskIntent() error = %v", err)
	}
	if !created.Confirmation.RequiresConfirmation {
		t.Fatalf("CreateTaskIntent().Confirmation.RequiresConfirmation = false, want true")
	}
	// The Reason text is the load-bearing instruction the agent acts on when
	// the prompt has no `if returns requires_confirmation` branch. It must
	// name the next-step tools so the agent does not need to infer them.
	if !strings.Contains(created.Confirmation.Reason, "tasks.continue") || !strings.Contains(created.Confirmation.Reason, "confirmed=true") {
		t.Fatalf("Confirmation.Reason missing actionable next-step tools: %q", created.Confirmation.Reason)
	}
	if len(created.SimilarTasks) == 0 {
		t.Fatalf("CreateTaskIntent().SimilarTasks is empty, want likely match")
	}
	if created.Task != nil {
		t.Fatalf("CreateTaskIntent().Task = %#v, want nil until confirmed", created.Task)
	}
	after, err := fixture.service.ListTasks(fixture.ctx, ListTasksInput{})
	if err != nil {
		t.Fatalf("ListTasks(after) error = %v", err)
	}
	if len(after.Tasks) != len(before.Tasks) {
		t.Fatalf("task count after unconfirmed intent = %d, want %d", len(after.Tasks), len(before.Tasks))
	}
}

func TestRecordProgressUsesWorkflowGuardrails(t *testing.T) {
	fixture := newAgentFixture(t)

	_, err := fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{TaskID: fixture.taskA1.ID, MoveToBucket: "done"})
	assertCodedError(t, err, domain.ErrWorkflowInvalidTransition)
}

func TestMutatingIntentsAreProjectScoped(t *testing.T) {
	fixture := newAgentFixture(t)

	_, err := fixture.service.AddComment(fixture.ctx, AddCommentInput{TaskID: fixture.taskB.ID, Body: "cross project"})
	assertCodedError(t, err, domain.ErrTaskNotFound)

	_, err = fixture.service.AddDependency(fixture.ctx, AddDependencyInput{TaskID: fixture.taskA1.ID, DependsOnTaskID: fixture.taskB.ID})
	assertCodedError(t, err, domain.ErrTaskNotFound)
}

func TestContinueTaskReturnsTaskDetails(t *testing.T) {
	fixture := newAgentFixture(t)

	_, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: 0})
	assertCodedError(t, err, domain.ErrValidation)

	cont, err := fixture.service.ContinueTask(fixture.ctx, ContinueTaskInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ContinueTask() error = %v", err)
	}
	if cont.Task.ID != fixture.taskA1.ID {
		t.Fatalf("ContinueTask().Task.ID = %d, want %d", cont.Task.ID, fixture.taskA1.ID)
	}
	if len(cont.Comments) != 1 || cont.Comments[0].Body != "A comment" {
		t.Fatalf("ContinueTask().Comments = %#v, want A comment", cont.Comments)
	}
	if cont.Workflow.Key != "default" {
		t.Fatalf("ContinueTask().Workflow.Key = %q, want default", cont.Workflow.Key)
	}
}

func TestCreateTaskDirectly(t *testing.T) {
	fixture := newAgentFixture(t)

	// CreateTask bypasses similarity check
	created, err := fixture.service.CreateTask(fixture.ctx, CreateTaskInput{Title: "Add MCP agent integration", Description: "Expose Omakiten state"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if created.Task == nil {
		t.Fatal("CreateTask().Task = nil, want task")
	}

	// Empty title/description
	_, err = fixture.service.CreateTaskIntent(fixture.ctx, CreateTaskInput{Title: "", Description: ""})
	assertCodedError(t, err, domain.ErrValidation)

	// SkipSimilarityCheck path
	intent, err := fixture.service.CreateTaskIntent(fixture.ctx, CreateTaskInput{Title: "Totally unique title", SkipSimilarityCheck: true})
	if err != nil {
		t.Fatalf("CreateTaskIntent() error = %v", err)
	}
	if intent.Task == nil {
		t.Fatal("CreateTaskIntent().Task = nil, want task")
	}

	// Confirmed=true override
	confirmed, err := fixture.service.CreateTaskIntent(fixture.ctx, CreateTaskInput{Title: "Add MCP agent integration", Confirmed: true})
	if err != nil {
		t.Fatalf("CreateTaskIntent() confirmed error = %v", err)
	}
	if confirmed.Task == nil {
		t.Fatal("CreateTaskIntent().Task = nil, want task")
	}
}

func TestMoveTaskHappyPath(t *testing.T) {
	fixture := newAgentFixture(t)

	moved, err := fixture.service.MoveTask(fixture.ctx, MoveTaskInput{TaskID: fixture.taskA1.ID, BucketKey: "dev"})
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Task.BucketKey != "dev" {
		t.Fatalf("MoveTask().BucketKey = %q, want dev", moved.Task.BucketKey)
	}
}

func TestListCommentsHappyPath(t *testing.T) {
	fixture := newAgentFixture(t)

	comments, err := fixture.service.ListComments(fixture.ctx, ListCommentsInput{TaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments.Comments) != 1 || comments.Comments[0].Body != "A comment" {
		t.Fatalf("ListComments() = %#v, want 1 A comment", comments.Comments)
	}
}

func TestListDependenciesHappyPath(t *testing.T) {
	fixture := newAgentFixture(t)

	deps, err := fixture.service.ListDependencies(fixture.ctx, ListDependenciesInput{TaskID: fixture.taskA2.ID})
	if err != nil {
		t.Fatalf("ListDependencies() error = %v", err)
	}
	if len(deps.Dependencies) != 1 || deps.Dependencies[0].TaskID != fixture.taskA2.ID {
		t.Fatalf("ListDependencies() = %#v, want 1 dependency", deps.Dependencies)
	}
}

func TestAddContextHappyPath(t *testing.T) {
	fixture := newAgentFixture(t)

	resp, err := fixture.service.AddContext(fixture.ctx, AddContextInput{Body: "New context"})
	if err != nil {
		t.Fatalf("AddContext() error = %v", err)
	}
	if resp.Entry.Body != "New context" {
		t.Fatalf("AddContext().Entry.Body = %q, want %q", resp.Entry.Body, "New context")
	}
}

func TestDumpContextDefaultLevel(t *testing.T) {
	fixture := newAgentFixture(t)

	// level == 0 should resolve to default (2)
	dump, err := fixture.service.DumpContext(fixture.ctx, DumpContextInput{})
	if err != nil {
		t.Fatalf("DumpContext() error = %v", err)
	}
	if dump.Level != 2 {
		t.Fatalf("DumpContext().Level = %d, want 2", dump.Level)
	}
	if len(dump.Tasks) != 2 {
		t.Fatalf("DumpContext().Tasks len = %d, want 2", len(dump.Tasks))
	}

	// custom level
	dump1, err := fixture.service.DumpContext(fixture.ctx, DumpContextInput{Level: 1})
	if err != nil {
		t.Fatalf("DumpContext(level 1) error = %v", err)
	}
	if dump1.Level != 1 {
		t.Fatalf("DumpContext().Level = %d, want 1", dump1.Level)
	}
	if len(dump1.Tasks) != 0 {
		t.Fatalf("DumpContext(level 1).Tasks len = %d, want 0", len(dump1.Tasks))
	}
}

func TestShowWorkflowHappyPath(t *testing.T) {
	fixture := newAgentFixture(t)

	resp, err := fixture.service.ShowWorkflow(fixture.ctx, WorkflowInput{})
	if err != nil {
		t.Fatalf("ShowWorkflow() error = %v", err)
	}
	if resp.Workflow.Key != "default" {
		t.Fatalf("ShowWorkflow().Key = %q, want default", resp.Workflow.Key)
	}
}

func TestRemoveDependencyConfirmation(t *testing.T) {
	fixture := newAgentFixture(t)

	// Unconfirmed -> requires confirmation
	resp, err := fixture.service.RemoveDependency(fixture.ctx, RemoveDependencyInput{TaskID: fixture.taskA2.ID, DependsOnTaskID: fixture.taskA1.ID})
	if err != nil {
		t.Fatalf("RemoveDependency() error = %v", err)
	}
	if !resp.Confirmation.RequiresConfirmation {
		t.Fatal("RemoveDependency().RequiresConfirmation = false, want true")
	}

	// Confirmed -> removes
	removed, err := fixture.service.RemoveDependency(fixture.ctx, RemoveDependencyInput{TaskID: fixture.taskA2.ID, DependsOnTaskID: fixture.taskA1.ID, Confirmed: true})
	if err != nil {
		t.Fatalf("RemoveDependency() confirmed error = %v", err)
	}
	if !removed.Removed {
		t.Fatal("RemoveDependency().Removed = false, want true")
	}
}

func TestRecordProgressScenarios(t *testing.T) {
	fixture := newAgentFixture(t)

	// TaskID <= 0 + edits -> error
	_, err := fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{Title: strPtr("new title")})
	assertCodedError(t, err, domain.ErrValidation)

	// TaskID <= 0 + no context -> error
	_, err = fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{})
	assertCodedError(t, err, domain.ErrValidation)

	// Edit only
	newTitle := "Updated title"
	editOnly, err := fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{TaskID: fixture.taskA1.ID, Title: &newTitle})
	if err != nil {
		t.Fatalf("RecordProgress(edit) error = %v", err)
	}
	if editOnly.Task == nil || editOnly.Task.Title != "Updated title" {
		t.Fatalf("RecordProgress(edit).Task = %#v, want Updated title", editOnly.Task)
	}

	// Comment only
	commentOnly, err := fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{TaskID: fixture.taskA1.ID, Comment: "progress note"})
	if err != nil {
		t.Fatalf("RecordProgress(comment) error = %v", err)
	}
	if commentOnly.Comment == nil || commentOnly.Comment.Body != "progress note" {
		t.Fatalf("RecordProgress(comment).Comment = %#v, want progress note", commentOnly.Comment)
	}

	// Context only
	ctxOnly, err := fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{Context: "handoff"})
	if err != nil {
		t.Fatalf("RecordProgress(context) error = %v", err)
	}
	if ctxOnly.ContextEntry == nil || ctxOnly.ContextEntry.Body != "handoff" {
		t.Fatalf("RecordProgress(context).ContextEntry = %#v, want handoff", ctxOnly.ContextEntry)
	}

	// Combined
	newDesc := "updated desc"
	combined, err := fixture.service.RecordProgress(fixture.ctx, RecordProgressInput{
		TaskID:      fixture.taskA1.ID,
		Description: &newDesc,
		Comment:     "combined note",
		Context:     "combined context",
	})
	if err != nil {
		t.Fatalf("RecordProgress(combined) error = %v", err)
	}
	if combined.Task == nil || combined.Comment == nil || combined.ContextEntry == nil {
		t.Fatalf("RecordProgress(combined) = %#v, want all three", combined)
	}
}

func TestFailureFromErrorNonDomain(t *testing.T) {
	failure := FailureFromError(errors.New("plain error"))
	if failure.Code != "internal_error" {
		t.Fatalf("FailureFromError().Code = %q, want internal_error", failure.Code)
	}
}

func TestGuidanceForCodes(t *testing.T) {
	codes := []domain.ErrorCode{
		domain.ErrProjectNotFound,
		domain.ErrProjectAmbiguous,
		domain.ErrTaskNotFound,
		domain.ErrWorkflowInvalidTransition,
		domain.ErrBucketNotFound,
		domain.ErrDependencyInvalid,
		domain.ErrValidation,
		domain.ErrConfigInvalid,
		"unknown_code",
	}
	for _, code := range codes {
		g := guidanceForCode(code)
		if g.Message == "" {
			t.Errorf("guidanceForCode(%q).Message is empty", code)
		}
		if len(g.Actions) == 0 {
			t.Errorf("guidanceForCode(%q).Actions is empty", code)
		}
	}
}

func strPtr(s string) *string { return &s }

type agentFixture struct {
	ctx      context.Context
	store    *sqlite.Store
	service  *Service
	projectA domain.Project
	projectB domain.Project
	taskA1   domain.Task
	taskA2   domain.Task
	taskB    domain.Task
}

func newAgentFixture(t *testing.T) agentFixture {
	t.Helper()
	ctx := context.Background()
	store := newAgentStore(t, ctx)
	tmp := t.TempDir()
	rootA := filepath.Join(tmp, "project-a")
	rootB := filepath.Join(tmp, "project-b")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootA) error = %v", err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootB) error = %v", err)
	}

	projectA, err := store.UpsertProject(ctx, "Project A", "project-a", rootA)
	if err != nil {
		t.Fatalf("UpsertProject(A) error = %v", err)
	}
	projectB, err := store.UpsertProject(ctx, "Project B", "project-b", rootB)
	if err != nil {
		t.Fatalf("UpsertProject(B) error = %v", err)
	}
	taskA1, err := store.CreateTask(ctx, projectA.ID, "Add MCP agent integration", "Expose Omakiten state to AI harnesses", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A1) error = %v", err)
	}
	taskA2, err := store.CreateTask(ctx, projectA.ID, "Write agent tests", "Cover project isolation", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask(A2) error = %v", err)
	}
	taskB, err := store.CreateTask(ctx, projectB.ID, "Other project task", "Must never leak", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask(B) error = %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, projectA.ID, taskA2.ID, taskA1.ID); err != nil {
		t.Fatalf("AddTaskDependency(A) error = %v", err)
	}
	if _, err := store.AddComment(ctx, projectA.ID, taskA1.ID, "A comment", "agent", nil); err != nil {
		t.Fatalf("AddComment(A) error = %v", err)
	}
	if _, err := store.AddComment(ctx, projectB.ID, taskB.ID, "B comment", "agent", nil); err != nil {
		t.Fatalf("AddComment(B) error = %v", err)
	}
	if _, err := store.AddContextEntry(ctx, projectA.ID, "A context", 2); err != nil {
		t.Fatalf("AddContextEntry(A) error = %v", err)
	}
	if _, err := store.AddContextEntry(ctx, projectB.ID, "B context", 2); err != nil {
		t.Fatalf("AddContextEntry(B) error = %v", err)
	}

	// Production wires settings from bundle.Config.MCP via the
	// composition root; tests construct the service directly so seed
	// kit-shape settings here. The validator at the config layer
	// guarantees these values in production; mirroring them keeps
	// behavioural parity in tests.
	svc := NewService(store, ProjectSelector{CWD: rootA})
	svc.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		MaxCommentChars:    0,
		IncludeWorkflow:    true,
		CachePrompts:       true,
		RecentContextLimit: 3,
		NextWorkLimit:      5,
		SimilarTaskLimit:   5,
	})
	return agentFixture{ctx: ctx, store: store, service: svc, projectA: projectA, projectB: projectB, taskA1: taskA1, taskA2: taskA2, taskB: taskB}
}

func newAgentStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "omakiten.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ImportBundle(ctx, agentTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	return store
}

func assertCodedError(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T %v, want CodedError", err, err)
	}
	if coded.Code != code {
		t.Fatalf("CodedError.Code = %q, want %q", coded.Code, code)
	}
}

func agentTestBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle := testfixtures.LoadBundle(t, "default.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}}
	return bundle
}
