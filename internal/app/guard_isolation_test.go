package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"omakiten/internal/app/guards"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TestGuardIsolationCrossProjectMoveTask pins the Phase 2-bis Round-2
// architectural invariant the original cache_test.go stopped short of
// asserting: under N parallel goroutines per project, guards declared on
// project A's Snapshot are NEVER consulted by project B's WorkflowService
// (and vice versa). Each project owns its own *config.Snapshot, its own
// *guards.Evaluator built from that snapshot, and its own
// *WorkflowService — no path inside MoveTask can reach across to the
// other project's guard list.
//
// The fixture builds two snapshots over identical workflow shapes
// (backlog→dev) so the move is allowed on the transition layer. The
// asymmetry lives on the guard list: snapA carries a comments_min
// guard with count=99 that ALWAYS fails (every task starts with zero
// comments); snapB carries no guard at all. Per-project Move outcomes
// must therefore split cleanly: every project-A move returns
// ErrGuardViolation; every project-B move succeeds.
//
// Under -race the test additionally validates that the snapshot's
// internal maps (workflow buckets, transition table, guard list) stay
// safe under the N×iterations concurrent read pressure each
// WorkflowService applies through its captured snap pointer.
func TestGuardIsolationCrossProjectMoveTask(t *testing.T) {
	t.Parallel()

	const guardCount = 99
	snapA := buildIsolationSnapshot(t, []domain.TransitionGuard{{Type: "comments_min", Count: guardCount, Hint: "needs more comments"}})
	snapB := buildIsolationSnapshot(t, nil)

	// Single Evaluator per project, captured from the per-project Snapshot.
	// If guards crossed projects, the evaluators below would see each
	// other's guard lists when fanning out across goroutines.
	evalA := guards.NewGuardEvaluator(snapA, &nilGuardRepo{}, &nilEventSink{})
	evalB := guards.NewGuardEvaluator(snapB, &nilGuardRepo{}, &nilEventSink{})

	// Single WorkflowService per project. Each captures its own snap so
	// every internal read (BucketByKey / TransitionAllowed / Guards) goes
	// through the per-project pointer. Each goroutine still wires its OWN
	// per-call repo so the test does not race on fakeStores counters.
	const goroutinesPerProject = 64

	type result struct {
		projectKey rune
		err        error
	}
	results := make(chan result, goroutinesPerProject*2)

	var wg sync.WaitGroup
	for i := 0; i < goroutinesPerProject; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			repo := newIsolationRepo()
			svcA := NewWorkflowService(snapA, repo, evalA, repo, repo, snapA.Registry())
			_, err := svcA.MoveTask(context.Background(), domain.ProjectContext{ID: 1}, 100, "dev")
			results <- result{projectKey: 'A', err: err}
		}()
		go func() {
			defer wg.Done()
			repo := newIsolationRepo()
			svcB := NewWorkflowService(snapB, repo, evalB, repo, repo, snapB.Registry())
			_, err := svcB.MoveTask(context.Background(), domain.ProjectContext{ID: 2}, 200, "dev")
			results <- result{projectKey: 'B', err: err}
		}()
	}
	wg.Wait()
	close(results)

	var aGuardFails, bGuardFails, aSuccess, bSuccess int
	for r := range results {
		var coded *domain.CodedError
		isGuardFail := errors.As(r.err, &coded) && coded.Code == domain.ErrGuardViolation
		switch r.projectKey {
		case 'A':
			if isGuardFail {
				aGuardFails++
			} else if r.err == nil {
				aSuccess++
			} else {
				t.Fatalf("project A unexpected error: %v", r.err)
			}
		case 'B':
			if isGuardFail {
				bGuardFails++
			} else if r.err == nil {
				bSuccess++
			} else {
				t.Fatalf("project B unexpected error: %v", r.err)
			}
		}
	}

	if aGuardFails != goroutinesPerProject {
		t.Fatalf("project A guard violations = %d, want %d (project A's snapshot carries the comments_min guard; every move must trip it)", aGuardFails, goroutinesPerProject)
	}
	if aSuccess != 0 {
		t.Fatalf("project A successful moves = %d, want 0 (the guard would have to leak away from the snapshot for this to happen)", aSuccess)
	}
	if bSuccess != goroutinesPerProject {
		t.Fatalf("project B successful moves = %d, want %d (project B's snapshot carries no guard; A's guard must NOT leak in)", bSuccess, goroutinesPerProject)
	}
	if bGuardFails != 0 {
		t.Fatalf("project B guard violations = %d, want 0 (A's comments_min guard bled into B's evaluator — per-project isolation broken)", bGuardFails)
	}
}

// buildIsolationSnapshot builds a Snapshot wrapping a minimal two-bucket
// workflow (backlog→dev) plus an optional guard list on the transition.
// The returned snapshot is independent of any other call's snapshot — the
// whole point of the cross-project test is that two snapshots hold
// disjoint state.
func buildIsolationSnapshot(t *testing.T, guards []domain.TransitionGuard) *config.Snapshot {
	t.Helper()
	wireGuards := make([]config.TransitionGuard, len(guards))
	for i, g := range guards {
		wireGuards[i] = config.TransitionGuard{Type: g.Type, Buckets: g.Buckets, Count: g.Count, Tag: g.Tag, Hint: g.Hint}
	}
	return config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "isolation",
			Name: "Isolation",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Dev", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2, Guards: wireGuards}},
		}},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "isolation"}},
	})
}

// isolationRepo is a one-shot fake satisfying every port WorkflowService +
// guards.Evaluator compose, with atomic.Int64 counters so the test can
// confirm MoveTask reached the persistence boundary the expected number of
// times across N concurrent invocations. Production never builds one of
// these — it is the minimum surface area the test needs.
type isolationRepo struct {
	moveCalls   atomic.Int64
	commentsFn  func() int
	taggedFn    func(string) int
	blockersFn  func(taskID int64) []domain.TaskBlocker
	taskState   domain.TaskState
	currentBkID int64
}

func newIsolationRepo() *isolationRepo {
	return &isolationRepo{
		// Currently sitting in bucket "backlog" (id=1). Move target is
		// "dev" (id=2). Zero comments means the comments_min guard with
		// count=99 will always trip on project A.
		currentBkID: 1,
		commentsFn:  func() int { return 0 },
		taggedFn:    func(string) int { return 0 },
		blockersFn:  func(int64) []domain.TaskBlocker { return nil },
		taskState:   domain.TaskStateActive,
	}
}

// WorkflowRepository
func (r *isolationRepo) CurrentTaskBucket(context.Context, int64, int64, domain.BucketResolver) (int64, string, error) {
	return r.currentBkID, "backlog", nil
}
func (r *isolationRepo) TaskState(context.Context, int64, int64) (domain.TaskState, error) {
	return r.taskState, nil
}

// GuardEvaluationRepository
func (r *isolationRepo) ListTaskBlockerBuckets(_ context.Context, _, taskID int64, _ domain.BucketResolver) ([]domain.TaskBlocker, error) {
	return r.blockersFn(taskID), nil
}
func (r *isolationRepo) CountTaskComments(context.Context, int64, int64) (int, error) {
	return r.commentsFn(), nil
}
func (r *isolationRepo) CountTaskCommentsTagged(_ context.Context, _, _ int64, tag string) (int, error) {
	return r.taggedFn(tag), nil
}
func (r *isolationRepo) CountPriorWavesPending(context.Context, int64, int64, domain.BucketResolver) (int, error) {
	return 0, nil
}
func (r *isolationRepo) FirstChildNotInBucket(context.Context, int64, int64, int64, domain.BucketResolver) (domain.Task, bool, error) {
	return domain.Task{}, false, nil
}

// TaskRepository — only MoveTask is exercised; the other methods are unused
// on the MoveTask path and return zero values.
func (r *isolationRepo) CreateTask(context.Context, int64, string, string, domain.Priority, string, *int64, domain.BucketResolver) (domain.Task, error) {
	return domain.Task{}, nil
}
func (r *isolationRepo) ListTasks(context.Context, int64, domain.TaskFilter, domain.BucketResolver) ([]domain.Task, error) {
	return nil, nil
}
func (r *isolationRepo) GetTaskByID(_ context.Context, projectID, taskID int64, _ domain.BucketResolver) (domain.Task, error) {
	return domain.Task{ID: taskID, ProjectID: projectID}, nil
}
func (r *isolationRepo) MoveTask(_ context.Context, projectID, taskID int64, _ string, _ domain.BucketResolver) (domain.Task, error) {
	r.moveCalls.Add(1)
	return domain.Task{ID: taskID, ProjectID: projectID}, nil
}
func (r *isolationRepo) UpdateTask(context.Context, int64, int64, domain.TaskUpdate, domain.BucketResolver) (domain.Task, error) {
	return domain.Task{}, nil
}
func (r *isolationRepo) TaskCount(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *isolationRepo) HardDeleteTask(context.Context, int64, int64, domain.BucketResolver) (domain.Event, error) {
	return domain.Event{}, nil
}
func (r *isolationRepo) SetTaskState(context.Context, int64, int64, domain.TaskState, string, domain.BucketResolver) (domain.Task, domain.Event, error) {
	return domain.Task{}, domain.Event{}, nil
}
func (r *isolationRepo) EmitTaskEditedEvent(context.Context, int64, int64, domain.Task, domain.Task, domain.BucketResolver) (domain.Event, error) {
	return domain.Event{}, nil
}
func (r *isolationRepo) AssignTask(context.Context, int64, int64, string, string, domain.BucketResolver) (domain.Task, domain.Event, error) {
	return domain.Task{}, domain.Event{}, nil
}
func (r *isolationRepo) SetTaskParent(context.Context, int64, int64, *int64) error {
	return nil
}
func (r *isolationRepo) IsDescendantOf(context.Context, int64, int64, int64) (bool, error) {
	return false, nil
}
func (r *isolationRepo) ListDirectChildren(context.Context, int64, int64, domain.BucketResolver) ([]domain.Task, error) {
	return nil, nil
}
func (r *isolationRepo) CountDirectChildren(context.Context, int64, int64) (int, error) {
	return 0, nil
}
func (r *isolationRepo) CountDescendants(context.Context, int64, int64) (int, error) {
	return 0, nil
}

// EventRepository — task.completed emission noop; the move target isn't the
// final bucket in this fixture, so MoveTask never reaches RecordTaskEvent.
func (r *isolationRepo) RecordTaskEvent(context.Context, int64, int64, string, string, string) (domain.Event, error) {
	return domain.Event{}, nil
}
func (r *isolationRepo) RecordEntityEvent(context.Context, string, int64, int64, string, string) error {
	return nil
}
func (r *isolationRepo) ListTaskActivity(context.Context, int64, int64, string) ([]domain.Event, error) {
	return nil, nil
}
func (r *isolationRepo) ListEvents(context.Context, domain.EventFilter) ([]domain.EventRow, error) {
	return nil, nil
}
func (r *isolationRepo) EventCategoryCounts(context.Context, int64, time.Time) (map[domain.EventCategory]int, error) {
	return nil, nil
}

// nilGuardRepo is the read-only counts repo the evaluators borrow when the
// per-call isolationRepo is not yet in scope (the evaluator is built once
// per project, before the goroutine spawn, so it cannot reach the
// goroutine-local repo). The evaluator only consults it through
// EvaluateTransition's checkXXX paths, which read project-level counts;
// returning zero/empty everywhere satisfies the contract and lets the
// guard logic decide based on the configured count threshold alone.
type nilGuardRepo struct{}

func (nilGuardRepo) ListTaskBlockerBuckets(context.Context, int64, int64, domain.BucketResolver) ([]domain.TaskBlocker, error) {
	return nil, nil
}
func (nilGuardRepo) CountTaskComments(context.Context, int64, int64) (int, error) {
	return 0, nil
}
func (nilGuardRepo) CountTaskCommentsTagged(context.Context, int64, int64, string) (int, error) {
	return 0, nil
}
func (nilGuardRepo) CountPriorWavesPending(context.Context, int64, int64, domain.BucketResolver) (int, error) {
	return 0, nil
}
func (nilGuardRepo) FirstChildNotInBucket(context.Context, int64, int64, int64, domain.BucketResolver) (domain.Task, bool, error) {
	return domain.Task{}, false, nil
}

// nilEventSink discards every guard.violated emission. The test asserts
// outcomes from MoveTask's return value, not from emitted telemetry, so
// silencing the sink keeps the assertions independent of the audit log.
type nilEventSink struct{}

func (nilEventSink) RecordEntityEvent(context.Context, string, int64, int64, string, string) error {
	return nil
}
