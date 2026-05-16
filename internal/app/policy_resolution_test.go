package app_test

import (
	"context"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
)

// Each scenario file under testdata/policy_*.yaml exercises one slice of
// the bucket→defaults→implicit-true resolution chain. A single test
// drives them all by loading the fixture, importing it, then asking
// WorkflowService.ResolveBucketPermissions for every (bucket × entity ×
// op) combination the fixture is expected to cover. Adding a new
// scenario means one YAML + one row in this table — no helper plumbing.
func TestWorkflowServicePolicyResolutionFromYAML(t *testing.T) {
	type ask struct {
		bucket    string
		entity    string
		operation string
		want      bool
	}
	cases := []struct {
		fixture string
		asks    []ask
	}{
		{
			// Comment fields nil at the workflow.defaults layer fall back to
			// the task fields at the same layer — comment edit/delete inherit
			// task's strict false when the bucket has no override.
			fixture: "policy_comment_inherits_task.yaml",
			asks: []ask{
				{"backlog", app.EntityTask, app.PermissionEdit, false},
				{"backlog", app.EntityTask, app.PermissionDelete, false},
				{"backlog", app.EntityComment, app.PermissionEdit, false},
				{"backlog", app.EntityComment, app.PermissionDelete, false},
				{"dev", app.EntityComment, app.PermissionEdit, false},
				{"dev", app.EntityComment, app.PermissionDelete, false},
			},
		},
		{
			// Bucket override on comment.delete only — every other field
			// flows through to workflow.defaults.
			fixture: "policy_comment_partial_override.yaml",
			asks: []ask{
				{"backlog", app.EntityTask, app.PermissionEdit, true},
				{"backlog", app.EntityTask, app.PermissionDelete, false},
				{"backlog", app.EntityComment, app.PermissionEdit, true},
				{"backlog", app.EntityComment, app.PermissionDelete, false},
				{"dev", app.EntityTask, app.PermissionEdit, true},
				{"dev", app.EntityTask, app.PermissionDelete, false},
				{"dev", app.EntityComment, app.PermissionEdit, true},
				{"dev", app.EntityComment, app.PermissionDelete, true},
			},
		},
		{
			// No defaults block, no bucket overrides — every field falls
			// through to the implicit `true` at the bottom of the chain.
			fixture: "policy_no_defaults_block.yaml",
			asks: []ask{
				{"backlog", app.EntityTask, app.PermissionEdit, true},
				{"backlog", app.EntityTask, app.PermissionDelete, true},
				{"backlog", app.EntityComment, app.PermissionEdit, true},
				{"backlog", app.EntityComment, app.PermissionDelete, true},
				{"dev", app.EntityTask, app.PermissionEdit, true},
				{"dev", app.EntityComment, app.PermissionDelete, true},
			},
		},
		{
			// Strict defaults + per-bucket flips. The bucket layer's
			// comment←task inheritance fires when the bucket declares
			// task but not comment — see policy_bucket_overrides.yaml's
			// header for the full breakdown.
			fixture: "policy_bucket_overrides.yaml",
			asks: []ask{
				{"backlog", app.EntityTask, app.PermissionEdit, true},
				{"backlog", app.EntityTask, app.PermissionDelete, false},
				// backlog declares task.edit=true and no comment block, so
				// comment.edit inherits the bucket-level task.edit (true).
				{"backlog", app.EntityComment, app.PermissionEdit, true},
				// task.delete is nil at bucket layer, falls through to
				// workflow.defaults.task.delete (false) and to
				// defaults.comment.delete (false).
				{"backlog", app.EntityComment, app.PermissionDelete, false},
				{"dev", app.EntityTask, app.PermissionEdit, false},
				{"dev", app.EntityTask, app.PermissionDelete, false},
				{"dev", app.EntityComment, app.PermissionDelete, true},
				{"dev", app.EntityComment, app.PermissionEdit, false},
				{"done", app.EntityTask, app.PermissionEdit, false},
				{"done", app.EntityTask, app.PermissionDelete, false},
				{"done", app.EntityComment, app.PermissionEdit, false},
				{"done", app.EntityComment, app.PermissionDelete, false},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(ctx, t.TempDir()+"/policy.db")
			if err != nil {
				t.Fatalf("sqlite.Open() = %v", err)
			}
			t.Cleanup(func() { _ = store.Close() })
			bundle, _ := testfixtures.LoadBundle(t, c.fixture)
			if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
				t.Fatalf("ImportBundle() = %v", err)
			}
			project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
			if err != nil {
				t.Fatalf("UpsertProject() = %v", err)
			}
			workflow := app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

			// Each ask creates a fresh task in the named bucket so the
			// resolver evaluates the policy for that exact location. We
			// delete the task between asks to keep the workflow state clean
			// — otherwise transition guards would interfere on later asks.
			for _, a := range c.asks {
				task, err := store.CreateTask(ctx, project.ID, "probe", "", domain.Priority(2), a.bucket, store.Snapshot())
				if err != nil {
					t.Fatalf("CreateTask(%s) = %v", a.bucket, err)
				}
				allowed, hint, err := workflow.ResolveBucketPermissions(ctx, project.Context(), task.ID, a.entity, a.operation)
				if err != nil {
					t.Fatalf("ResolveBucketPermissions(%s, %s, %s) = %v", a.bucket, a.entity, a.operation, err)
				}
				if allowed != a.want {
					t.Errorf("ResolveBucketPermissions(%s, %s, %s) = %v (hint %q), want %v", a.bucket, a.entity, a.operation, allowed, hint, a.want)
				}
				if _, err := store.HardDeleteTask(ctx, project.ID, task.ID, store.Snapshot()); err != nil {
					t.Fatalf("HardDeleteTask(%d) = %v", task.ID, err)
				}
			}
		})
	}
}
