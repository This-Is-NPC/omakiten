package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// fakeStores groups every port WorkflowService composes so tests can stub
// each behavior independently. Defaults (zero values) are inert: lists are
// empty, transitions are disallowed, and counts are zero.
type fakeStores struct {
	defaultBucket    string
	bucketsByKey     map[string]domain.Bucket
	allowedFromTo    map[[2]int64]bool
	guards           map[[2]int64][]domain.TransitionGuard
	finalBucketIDs   map[int64]bool
	currentBucketID  int64
	currentBucketKey string
	currentBucketErr error
	taskState        domain.TaskState

	createCalls  int
	moveCalls    int
	moveResp     domain.Task
	moveErr      error
	createResp   domain.Task
	createErr    error
	eventCalls   []recordedEvent
	blockerLists map[int64][]domain.TaskBlocker
	commentCount int
	taggedCount  map[string]int
}

type recordedEvent struct {
	projectID int64
	taskID    int64
	eventType string
	payload   string
}

// ConfigRepository
func (f *fakeStores) ImportBundle(context.Context, config.Bundle, string, string) error {
	return nil
}
func (f *fakeStores) ListActiveLaws(context.Context) ([]domain.Law, error) {
	return nil, nil
}
func (f *fakeStores) ListActiveSkills(context.Context) ([]domain.Skill, error) {
	return nil, nil
}
func (f *fakeStores) ListActivePersonas(context.Context) ([]domain.Persona, error) {
	return nil, nil
}
func (f *fakeStores) ActiveWorkflow(context.Context) (domain.Workflow, error) {
	if f.defaultBucket == "" {
		return domain.Workflow{}, nil
	}
	return domain.Workflow{Buckets: []domain.Bucket{{Key: f.defaultBucket}}}, nil
}
func (f *fakeStores) ContextSettings(context.Context) (domain.ContextSettings, error) {
	return domain.ContextSettings{}, nil
}

// WorkflowRepository
func (f *fakeStores) ResolveActiveBucket(_ context.Context, key string) (domain.Bucket, error) {
	if b, ok := f.bucketsByKey[key]; ok {
		return b, nil
	}
	return domain.Bucket{}, domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": key})
}
func (f *fakeStores) IsFinalActiveBucket(_ context.Context, bucketID int64) (bool, error) {
	return f.finalBucketIDs[bucketID], nil
}
func (f *fakeStores) TransitionAllowed(_ context.Context, from, to int64) (bool, error) {
	return f.allowedFromTo[[2]int64{from, to}], nil
}
func (f *fakeStores) LoadTransitionGuards(_ context.Context, from, to int64) ([]domain.TransitionGuard, error) {
	return f.guards[[2]int64{from, to}], nil
}
func (f *fakeStores) CurrentTaskBucket(context.Context, int64, int64) (int64, string, error) {
	return f.currentBucketID, f.currentBucketKey, f.currentBucketErr
}
func (f *fakeStores) TaskState(context.Context, int64, int64) (domain.TaskState, error) {
	if f.taskState == "" {
		return domain.TaskStateActive, nil
	}
	return f.taskState, nil
}

// GuardEvaluationRepository
func (f *fakeStores) ListTaskBlockerBuckets(_ context.Context, _, taskID int64) ([]domain.TaskBlocker, error) {
	return f.blockerLists[taskID], nil
}
func (f *fakeStores) CountTaskComments(context.Context, int64, int64) (int, error) {
	return f.commentCount, nil
}
func (f *fakeStores) CountTaskCommentsTagged(_ context.Context, _, _ int64, tag string) (int, error) {
	return f.taggedCount[tag], nil
}

// TaskRepository
func (f *fakeStores) CreateTask(_ context.Context, _ int64, _, _ string, _ domain.Priority, _ string) (domain.Task, error) {
	f.createCalls++
	return f.createResp, f.createErr
}
func (f *fakeStores) ListTasks(context.Context, int64, domain.TaskFilter) ([]domain.Task, error) {
	return nil, nil
}
func (f *fakeStores) MoveTask(context.Context, int64, int64, string) (domain.Task, error) {
	f.moveCalls++
	return f.moveResp, f.moveErr
}
func (f *fakeStores) UpdateTask(context.Context, int64, int64, domain.TaskUpdate) (domain.Task, error) {
	return domain.Task{}, nil
}
func (f *fakeStores) TaskCount(context.Context, int64) (int64, error) {
	return 0, nil
}
func (f *fakeStores) HardDeleteTask(context.Context, int64, int64) (domain.Event, error) {
	return domain.Event{}, nil
}
func (f *fakeStores) SetTaskState(context.Context, int64, int64, domain.TaskState, string) (domain.Task, domain.Event, error) {
	return domain.Task{}, domain.Event{}, nil
}
func (f *fakeStores) EmitTaskEditedEvent(context.Context, int64, int64, domain.Task, domain.Task) (domain.Event, error) {
	return domain.Event{}, nil
}

// EventRepository
func (f *fakeStores) RecordTaskEvent(_ context.Context, projectID, taskID int64, eventType, _, payload string) (domain.Event, error) {
	f.eventCalls = append(f.eventCalls, recordedEvent{projectID, taskID, eventType, payload})
	return domain.Event{}, nil
}
func (f *fakeStores) RecordEntityEvent(_ context.Context, _ string, entityID, projectID int64, eventType, payload string) error {
	f.eventCalls = append(f.eventCalls, recordedEvent{projectID, entityID, eventType, payload})
	return nil
}
func (f *fakeStores) ListTaskActivity(context.Context, int64, int64, string) ([]domain.Event, error) {
	return nil, nil
}

func newWorkflowServiceForTest(f *fakeStores) *WorkflowService {
	return NewWorkflowService(f, f, f, f, f, nil)
}

func TestWorkflowResolveDefaultBucket(t *testing.T) {
	f := &fakeStores{defaultBucket: "todo"}
	svc := newWorkflowServiceForTest(f)
	got, err := svc.ResolveDefaultBucket(context.Background())
	if err != nil {
		t.Fatalf("ResolveDefaultBucket = %v", err)
	}
	if got != "todo" {
		t.Fatalf("ResolveDefaultBucket = %q, want %q", got, "todo")
	}
}

func TestWorkflowResolveDefaultBucketEmptyWorkflow(t *testing.T) {
	f := &fakeStores{} // no buckets
	svc := newWorkflowServiceForTest(f)
	_, err := svc.ResolveDefaultBucket(context.Background())
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrConfigInvalid {
		t.Fatalf("ResolveDefaultBucket err = %v, want config_invalid", err)
	}
}

func TestWorkflowCreateTaskUsesDefaultWhenBucketEmpty(t *testing.T) {
	f := &fakeStores{
		defaultBucket: "todo",
		createResp:    domain.Task{ID: 7, BucketKey: "todo"},
	}
	svc := newWorkflowServiceForTest(f)

	if _, err := svc.CreateTask(context.Background(), 1, "title", "", domain.Priority(2), ""); err != nil {
		t.Fatalf("CreateTask = %v", err)
	}
	if f.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", f.createCalls)
	}
}

func TestWorkflowMoveTaskValidatesArgs(t *testing.T) {
	svc := newWorkflowServiceForTest(&fakeStores{})
	cases := []struct {
		name   string
		taskID int64
		bucket string
	}{
		{"zero task id", 0, "dev"},
		{"empty bucket", 5, ""},
		{"whitespace bucket", 5, "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, tc.taskID, tc.bucket)
			var coded *domain.CodedError
			if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
				t.Fatalf("err = %v, want validation_error", err)
			}
		})
	}
}

func TestWorkflowMoveTaskBlocksUnknownTransition(t *testing.T) {
	f := &fakeStores{
		bucketsByKey:    map[string]domain.Bucket{"dev": {ID: 2, Key: "dev"}},
		currentBucketID: 1,
	}
	svc := newWorkflowServiceForTest(f)
	_, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 5, "dev")
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrWorkflowInvalidTransition {
		t.Fatalf("err = %v, want workflow_invalid_transition", err)
	}
	if f.moveCalls != 0 {
		t.Fatalf("MoveTask should not have been called, got %d", f.moveCalls)
	}
}

func TestWorkflowMoveTaskAllowsSameBucketWithoutGuards(t *testing.T) {
	f := &fakeStores{
		bucketsByKey:    map[string]domain.Bucket{"backlog": {ID: 1, Key: "backlog"}},
		currentBucketID: 1, // same as target
		moveResp:        domain.Task{ID: 5, BucketKey: "backlog"},
	}
	svc := newWorkflowServiceForTest(f)
	_, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 5, "backlog")
	if err != nil {
		t.Fatalf("MoveTask same-bucket = %v", err)
	}
	if f.moveCalls != 1 {
		t.Fatalf("move calls = %d, want 1 (same-bucket still persists)", f.moveCalls)
	}
	if len(f.eventCalls) != 0 {
		t.Fatalf("same-bucket move emitted events: %+v", f.eventCalls)
	}
}

func TestWorkflowMoveTaskEmitsCompletedOnFinalBucket(t *testing.T) {
	f := &fakeStores{
		bucketsByKey:    map[string]domain.Bucket{"done": {ID: 3, Key: "done"}},
		allowedFromTo:   map[[2]int64]bool{{2, 3}: true},
		finalBucketIDs:  map[int64]bool{3: true},
		currentBucketID: 2,
		moveResp:        domain.Task{ID: 5, BucketKey: "done"},
	}
	svc := newWorkflowServiceForTest(f)
	if _, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 5, "done"); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}
	if len(f.eventCalls) != 1 || f.eventCalls[0].eventType != domain.EventTypeTaskCompleted {
		t.Fatalf("event calls = %+v, want one task.completed", f.eventCalls)
	}
}

func TestWorkflowMoveTaskBlockersInGuard(t *testing.T) {
	f := &fakeStores{
		bucketsByKey:  map[string]domain.Bucket{"dev": {ID: 2, Key: "dev"}},
		allowedFromTo: map[[2]int64]bool{{1, 2}: true},
		guards: map[[2]int64][]domain.TransitionGuard{
			{1, 2}: {{Type: "blockers_in", Buckets: []string{"done"}}},
		},
		currentBucketID: 1,
		blockerLists: map[int64][]domain.TaskBlocker{
			5: {{TaskID: 9, Title: "blocker", BucketKey: "backlog"}},
		},
	}
	svc := newWorkflowServiceForTest(f)
	_, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 5, "dev")
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("err = %v, want guard_violation", err)
	}
	if !hasEvent(f.eventCalls, domain.EventTypeGuardViolated) {
		t.Fatalf("guard.violated event not emitted; got %#v", f.eventCalls)
	}
}

func hasEvent(calls []recordedEvent, eventType string) bool {
	for _, c := range calls {
		if c.eventType == eventType {
			return true
		}
	}
	return false
}

func TestWorkflowMoveTaskCommentsMinGuard(t *testing.T) {
	f := &fakeStores{
		bucketsByKey:  map[string]domain.Bucket{"dev": {ID: 2, Key: "dev"}},
		allowedFromTo: map[[2]int64]bool{{1, 2}: true},
		guards: map[[2]int64][]domain.TransitionGuard{
			{1, 2}: {{Type: "comments_min", Count: 2}},
		},
		currentBucketID: 1,
		commentCount:    1,
	}
	svc := newWorkflowServiceForTest(f)
	_, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 5, "dev")
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("err = %v, want guard_violation", err)
	}
}

func TestWorkflowMoveTaskCommentsTaggedGuard(t *testing.T) {
	f := &fakeStores{
		bucketsByKey:  map[string]domain.Bucket{"dev": {ID: 2, Key: "dev"}},
		allowedFromTo: map[[2]int64]bool{{1, 2}: true},
		guards: map[[2]int64][]domain.TransitionGuard{
			{1, 2}: {{Type: "comments_tagged", Tag: "review", Count: 1}},
		},
		currentBucketID: 1,
		taggedCount:     map[string]int{"review": 0},
	}
	svc := newWorkflowServiceForTest(f)
	_, err := svc.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 5, "dev")
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("err = %v, want guard_violation", err)
	}
}
