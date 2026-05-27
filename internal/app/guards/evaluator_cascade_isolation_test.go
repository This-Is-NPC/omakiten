package guards

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// cascadeIsolationSnap builds a project snapshot wiring omakase as the root
// kit and izakaya as the sub-task kit. Each kit carries a `comments_tagged`
// guard on its own dev→review (root) and dev→done (sub-kit) transition with
// distinct tag names so isolation breaches are visible in the violation
// message: any cross-contamination would surface the other kit's tag.
func cascadeIsolationSnap(t *testing.T) *config.Snapshot {
	t.Helper()
	return config.BuildSnapshot(config.Bundle{
		Kit: config.Kit{ID: 100, Key: "omakase", Name: "Omakase"},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Name: "Omakase",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Dev", Position: 2},
				{ID: 3, Key: "review", Name: "Review", Position: 3},
				{ID: 4, Key: "done", Name: "Done", Position: 4},
			},
			Transitions: []config.Transition{
				{From: 1, To: 2},
				{From: 2, To: 3, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: "root-only", Count: 1}}},
				{From: 3, To: 4},
			},
		}},
		Config:     config.Settings{Workflow: config.WorkflowSettings{Active: "omakase"}},
		SubtaskKit: "izakaya.yaml",
		SubtaskBundle: &config.Bundle{
			Kit: config.Kit{ID: 200, Key: "izakaya", Name: "Izakaya"},
			Workflows: []config.Workflow{{
				ID:   2,
				Key:  "izakaya",
				Name: "Izakaya",
				Buckets: []config.Bucket{
					{ID: 10, Key: "backlog", Name: "Backlog", Position: 1},
					{ID: 20, Key: "dev", Name: "Dev", Position: 2},
					{ID: 30, Key: "done", Name: "Done", Position: 3},
				},
				Transitions: []config.Transition{
					{From: 10, To: 20},
					{From: 20, To: 30, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: "sub-only", Count: 1}}},
				},
			}},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "izakaya"}},
		},
	})
}

// TestCascade_RootGuardDoesNotApplyToSubtaskTransition proves a guard
// declared on the root kit's dev→review transition does not leak into the
// sub-kit's dev→done evaluation. The sub-kit's own guard fires (count 0 <
// required 1) but the violation message must reference only the sub-kit's
// tag — any mention of root's tag would mean the dispatcher merged guard
// sets across kits.
func TestCascade_RootGuardDoesNotApplyToSubtaskTransition(t *testing.T) {
	snap := cascadeIsolationSnap(t)
	subSnap, ok := snap.SubtaskKit()
	if !ok {
		t.Fatal("expected sub-kit snapshot to be wired")
	}
	repo := &subtasksRepo{}
	eval := NewGuardEvaluator(snap, repo, nil)

	err := eval.EvaluateTransitionFor(context.Background(), 1, 1, 20, 30, "done", subSnap)
	if err == nil {
		t.Fatal("sub-kit dev→done evaluation = nil; expected sub-only guard to fire")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("err = %v, want ErrGuardViolation", err)
	}
	if !contains(coded.Message, "sub-only") {
		t.Fatalf("violation message %q must mention sub-only tag", coded.Message)
	}
	if contains(coded.Message, "root-only") {
		t.Fatalf("violation message %q must NOT mention root-only tag (root guard leaked into sub-kit evaluation)", coded.Message)
	}
}

// TestCascade_SubGuardDoesNotApplyToRootTransition is the symmetric proof:
// a guard declared on the sub-kit's dev→done transition does not leak into
// the root kit's dev→review evaluation. Only the root kit's own guard fires.
func TestCascade_SubGuardDoesNotApplyToRootTransition(t *testing.T) {
	snap := cascadeIsolationSnap(t)
	repo := &subtasksRepo{}
	eval := NewGuardEvaluator(snap, repo, nil)

	err := eval.EvaluateTransitionFor(context.Background(), 1, 1, 2, 3, "review", snap)
	if err == nil {
		t.Fatal("root dev→review evaluation = nil; expected root-only guard to fire")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("err = %v, want ErrGuardViolation", err)
	}
	if !contains(coded.Message, "root-only") {
		t.Fatalf("violation message %q must mention root-only tag", coded.Message)
	}
	if contains(coded.Message, "sub-only") {
		t.Fatalf("violation message %q must NOT mention sub-only tag (sub-kit guard leaked into root evaluation)", coded.Message)
	}
}

// TestCascade_SubtaskRespectsSubKitGuardSetExclusively builds two kits that
// declare guards on the SAME (from, to) bucket pair. The pair is intentional
// — without per-kit isolation the evaluator would otherwise need to pick a
// winner or merge the guard slices. Each kit's evaluation must consult only
// its own snapshot's guard list.
func TestCascade_SubtaskRespectsSubKitGuardSetExclusively(t *testing.T) {
	snap := config.BuildSnapshot(config.Bundle{
		Kit: config.Kit{ID: 100, Key: "omakase", Name: "Omakase"},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Buckets: []config.Bucket{
				{ID: 2, Key: "dev", Position: 1},
				{ID: 3, Key: "review", Position: 2},
			},
			Transitions: []config.Transition{
				{From: 2, To: 3, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: "root-only", Count: 1}}},
			},
		}},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "omakase"}},
		SubtaskBundle: &config.Bundle{
			Kit: config.Kit{ID: 200, Key: "izakaya", Name: "Izakaya"},
			Workflows: []config.Workflow{{
				ID:   2,
				Key:  "izakaya",
				Buckets: []config.Bucket{
					{ID: 2, Key: "dev", Position: 1},
					{ID: 3, Key: "review", Position: 2},
				},
				Transitions: []config.Transition{
					{From: 2, To: 3, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: "sub-only", Count: 1}}},
				},
			}},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "izakaya"}},
		},
	})
	subSnap, ok := snap.SubtaskKit()
	if !ok {
		t.Fatal("expected sub-kit snapshot to be wired")
	}
	repo := &subtasksRepo{}
	eval := NewGuardEvaluator(snap, repo, nil)

	// Root snapshot path: only the root's guard fires.
	err := eval.EvaluateTransitionFor(context.Background(), 1, 1, 2, 3, "review", snap)
	var coded *domain.CodedError
	if !errors.As(err, &coded) || !contains(coded.Message, "root-only") {
		t.Fatalf("root snap evaluation must fire root-only; got err = %v", err)
	}
	if contains(coded.Message, "sub-only") {
		t.Fatalf("root snap evaluation leaked sub-only into message %q", coded.Message)
	}

	// Sub-kit snapshot path: only the sub-kit's guard fires.
	err = eval.EvaluateTransitionFor(context.Background(), 1, 1, 2, 3, "review", subSnap)
	if !errors.As(err, &coded) || !contains(coded.Message, "sub-only") {
		t.Fatalf("sub-kit snap evaluation must fire sub-only; got err = %v", err)
	}
	if contains(coded.Message, "root-only") {
		t.Fatalf("sub-kit snap evaluation leaked root-only into message %q", coded.Message)
	}
}

// TestCascade_NoSubtaskKit_AllTasksShareRootGuards is the regression check
// for projects without `subtask_kit:` configured. `Snapshot.For(child)` must
// collapse to the root snapshot so every task — root or sub — evaluates the
// same root-kit guard set. Pre-#281 behavior preserved.
func TestCascade_NoSubtaskKit_AllTasksShareRootGuards(t *testing.T) {
	snap := config.BuildSnapshot(config.Bundle{
		Kit: config.Kit{ID: 100, Key: "omakase", Name: "Omakase"},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "omakase",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Position: 1},
				{ID: 2, Key: "dev", Position: 2},
				{ID: 3, Key: "review", Position: 3},
			},
			Transitions: []config.Transition{
				{From: 2, To: 3, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: "shipped", Count: 1}}},
			},
		}},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "omakase"}},
	})
	if _, ok := snap.SubtaskKit(); ok {
		t.Fatal("no SubtaskBundle configured; SubtaskKit() must report false")
	}
	parent := int64(99)
	resolved := snap.For(domain.Task{ParentID: &parent})
	if resolved != snap {
		t.Fatalf("For(sub-task) without cascade must return root snap; got %p vs root %p", resolved, snap)
	}

	repo := &subtasksRepo{}
	eval := NewGuardEvaluator(snap, repo, nil)
	err := eval.EvaluateTransitionFor(context.Background(), 1, 1, 2, 3, "review", resolved)
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("no-cascade sub-task evaluation err = %v, want ErrGuardViolation", err)
	}
	if !contains(coded.Message, "shipped") {
		t.Fatalf("violation message %q must mention shipped tag (root guard for all depths)", coded.Message)
	}
}
