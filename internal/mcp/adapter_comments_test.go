package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"omakiten/internal/agent"
	"omakiten/internal/config"
)

// addComment is a shared helper: dispatch comments.add via CallTool and return
// the decoded comment object. Fails the test on transport or tool error.
func addComment(t *testing.T, ctx context.Context, adapter *Adapter, args map[string]any) map[string]any {
	t.Helper()
	result, err := adapter.CallTool(ctx, "comments.add", withModel(args))
	if err != nil {
		t.Fatalf("CallTool(comments.add) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("comments.add IsError = true, content = %s", result.Content[0].Text)
	}
	var payload struct {
		Comment map[string]any `json:"comment"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("comments.add content not JSON: %v", err)
	}
	if payload.Comment == nil {
		t.Fatalf("comments.add payload missing comment: %s", result.Content[0].Text)
	}
	return payload.Comment
}

// listComments dispatches comments.list via CallTool and returns the decoded
// rows. Fails on transport or tool error.
func listComments(t *testing.T, ctx context.Context, adapter *Adapter, args map[string]any) []map[string]any {
	t.Helper()
	result, err := adapter.CallTool(ctx, "comments.list", withModel(args))
	if err != nil {
		t.Fatalf("CallTool(comments.list) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("comments.list IsError = true, content = %s", result.Content[0].Text)
	}
	var payload struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("comments.list content not JSON: %v", err)
	}
	return payload.Comments
}

// TestAdapterCommentsEditTriStateThroughCallTool guards the #385 data-loss fix
// at the transport layer: a body-only comments.edit must preserve a comment's
// pinned/title/kind, and an explicit pinned=false must unpin. The raw arg map
// round-trips through decodeArgs (re-marshal → unmarshal) exactly as a real
// JSON-RPC client's body would, so an omitted `pinned` decodes to a nil *bool
// (untouched) while an explicit `false` decodes to a non-nil *bool (overwrite).
func TestAdapterCommentsEditTriStateThroughCallTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	created := addComment(t, ctx, adapter, map[string]any{
		"scope":       "project",
		"body":        "before",
		"author_type": "agent",
		"pinned":      true,
		"title":       "X",
		"kind":        "handoff",
	})
	commentID := int64(created["id"].(float64))
	if created["pinned"] != true || created["title"] != "X" || created["kind"] != "handoff" {
		t.Fatalf("seed comment = %#v, want pinned/title/kind set", created)
	}

	// Body-only edit: pinned/title/kind omitted → must be preserved.
	editRes, err := adapter.CallTool(ctx, "comments.edit", withModel(map[string]any{
		"comment_id": commentID,
		"body":       "after",
	}))
	if err != nil {
		t.Fatalf("CallTool(comments.edit body-only) error = %v", err)
	}
	if editRes.IsError {
		t.Fatalf("comments.edit body-only IsError = true: %s", editRes.Content[0].Text)
	}
	var edited struct {
		Comment map[string]any `json:"comment"`
	}
	if err := json.Unmarshal([]byte(editRes.Content[0].Text), &edited); err != nil {
		t.Fatalf("comments.edit payload not JSON: %v", err)
	}
	if edited.Comment["body"] != "after" {
		t.Fatalf("comments.edit body = %v, want after", edited.Comment["body"])
	}
	if edited.Comment["pinned"] != true || edited.Comment["title"] != "X" || edited.Comment["kind"] != "handoff" {
		t.Fatalf("body-only comments.edit wiped note fields = %#v", edited.Comment)
	}

	// Confirm the listed row also reflects the preserved fields (read-back).
	rows := listComments(t, ctx, adapter, map[string]any{"comment_id": commentID})
	if len(rows) != 1 || rows[0]["pinned"] != true || rows[0]["title"] != "X" || rows[0]["kind"] != "handoff" {
		t.Fatalf("listed comment after body-only edit = %#v, want note fields intact", rows)
	}

	// Explicit pinned=false → unpins; title/kind still preserved.
	unpinRes, err := adapter.CallTool(ctx, "comments.edit", withModel(map[string]any{
		"comment_id": commentID,
		"body":       "after2",
		"pinned":     false,
	}))
	if err != nil {
		t.Fatalf("CallTool(comments.edit unpin) error = %v", err)
	}
	if unpinRes.IsError {
		t.Fatalf("comments.edit unpin IsError = true: %s", unpinRes.Content[0].Text)
	}
	var unpinned struct {
		Comment map[string]any `json:"comment"`
	}
	if err := json.Unmarshal([]byte(unpinRes.Content[0].Text), &unpinned); err != nil {
		t.Fatalf("comments.edit unpin payload not JSON: %v", err)
	}
	// pinned omitted from JSON when false (omitempty) — assert it is NOT true.
	if unpinned.Comment["pinned"] == true {
		t.Fatalf("explicit pinned=false did not unpin = %#v", unpinned.Comment)
	}
	if unpinned.Comment["title"] != "X" || unpinned.Comment["kind"] != "handoff" {
		t.Fatalf("explicit pinned=false wiped title/kind = %#v", unpinned.Comment)
	}
	// Read-back: the listed row must also be unpinned.
	rows = listComments(t, ctx, adapter, map[string]any{"comment_id": commentID})
	if len(rows) != 1 || rows[0]["pinned"] == true {
		t.Fatalf("listed comment after unpin = %#v, want pinned cleared", rows)
	}
}

// commentGuardBundle returns the mcp default bundle with a scope-aware comment
// policy installed on workflow[0].defaults + the backlog bucket. Mirrors
// internal/app/testdata/policy_comment_scopes.yaml, built inline so the
// adapter test owns its fixture:
//
//   - defaults.comment.task.edit      = false  (task edit denied)
//   - defaults.comment.project.edit   = true   (project edit allowed)
//   - defaults.comment.project.delete = true   (project delete allowed)
//   - defaults.comment.universal.delete = false (universal delete denied)
//   - bucket "backlog" comment.edit   = false  (task edit denied at bucket too)
//
// The seeded task lives in backlog, so a task-scoped comment edit is blocked at
// both layers; project/universal scopes resolve purely through the defaults
// chain without a task lookup.
func commentGuardBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle := mcpTestBundle(t)
	f := false
	tr := true
	bundle.Workflows[0].Defaults = &config.WorkflowDefaults{
		Comment: &config.EntityPermission{
			Task:      &config.EntityPermission{Edit: &f, Delete: &f},
			Project:   &config.EntityPermission{Edit: &tr, Delete: &tr},
			Universal: &config.EntityPermission{Edit: &f, Delete: &f},
		},
	}
	bundle.Workflows[0].Buckets[0].Permissions = &config.BucketPermissions{
		Comment: &config.EntityPermission{Edit: &f},
	}
	return bundle
}

// TestAdapterCommentsEditDeleteGuardDenialViaMCP drives the comment policy chain
// end-to-end through CallTool: edit/delete on a denied scope returns IsError
// with code=guard_violation (matching the tasks.move guard assertion shape),
// while an allowed scope succeeds.
func TestAdapterCommentsEditDeleteGuardDenialViaMCP(t *testing.T) {
	ctx := context.Background()
	store, project, task := newMCPProjectWithBundle(t, ctx, "guarded", commentGuardBundle(t))
	service := agent.NewService(store, agent.ProjectSelector{ProjectID: project.ID})
	service.SetSnapshot(store.Snapshot())
	adapter := NewAdapter(service)

	// Seed one comment per scope.
	taskC := addComment(t, ctx, adapter, map[string]any{
		"project": "guarded", "task_id": task.ID, "body": "task note", "author_type": "agent",
	})
	projC := addComment(t, ctx, adapter, map[string]any{
		"project": "guarded", "scope": "project", "body": "project note", "author_type": "agent",
	})
	uniC := addComment(t, ctx, adapter, map[string]any{
		"project": "guarded", "scope": "universal", "body": "universal note", "author_type": "agent",
	})

	assertGuardViolation := func(label, tool string, args map[string]any) {
		t.Helper()
		args["project"] = "guarded"
		result, err := adapter.CallTool(ctx, tool, withModel(args))
		if err != nil {
			t.Fatalf("%s: CallTool(%s) transport error = %v", label, tool, err)
		}
		if !result.IsError {
			t.Fatalf("%s: %s on denied scope should be IsError, got: %s", label, tool, result.Content[0].Text)
		}
		var failure map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].Text), &failure); err != nil {
			t.Fatalf("%s: %s failure payload not JSON: %v", label, tool, err)
		}
		if failure["code"] != "guard_violation" {
			t.Fatalf("%s: %s failure code = %v, want guard_violation; payload=%v", label, tool, failure["code"], failure)
		}
	}

	assertSuccess := func(label, tool string, args map[string]any) {
		t.Helper()
		args["project"] = "guarded"
		result, err := adapter.CallTool(ctx, tool, withModel(args))
		if err != nil {
			t.Fatalf("%s: CallTool(%s) transport error = %v", label, tool, err)
		}
		if result.IsError {
			t.Fatalf("%s: %s on allowed scope should succeed, got: %s", label, tool, result.Content[0].Text)
		}
	}

	// edit: task scope denied (defaults.comment.task.edit=false + bucket).
	assertGuardViolation("task edit", "comments.edit", map[string]any{
		"comment_id": int64(taskC["id"].(float64)), "body": "x",
	})
	// edit: project scope allowed (defaults.comment.project.edit=true).
	assertSuccess("project edit", "comments.edit", map[string]any{
		"comment_id": int64(projC["id"].(float64)), "body": "x",
	})
	// delete: universal scope denied (defaults.comment.universal.delete=false).
	assertGuardViolation("universal delete", "comments.delete", map[string]any{
		"comment_id": int64(uniC["id"].(float64)), "confirmed": true,
	})
	// delete: project scope allowed (defaults.comment.project.delete=true).
	assertSuccess("project delete", "comments.delete", map[string]any{
		"comment_id": int64(projC["id"].(float64)), "confirmed": true,
	})
}

// TestAdapterCommentsDeleteThroughCallTool deletes a project and a universal
// comment via CallTool and asserts a subsequent list omits each.
func TestAdapterCommentsDeleteThroughCallTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	projC := addComment(t, ctx, adapter, map[string]any{
		"scope": "project", "body": "project note", "author_type": "agent",
	})
	uniC := addComment(t, ctx, adapter, map[string]any{
		"scope": "universal", "body": "universal note", "author_type": "agent",
	})
	projID := int64(projC["id"].(float64))
	uniID := int64(uniC["id"].(float64))

	del := func(id int64) {
		t.Helper()
		result, err := adapter.CallTool(ctx, "comments.delete", withModel(map[string]any{
			"comment_id": id, "confirmed": true,
		}))
		if err != nil {
			t.Fatalf("CallTool(comments.delete %d) error = %v", id, err)
		}
		if result.IsError {
			t.Fatalf("comments.delete %d IsError = true: %s", id, result.Content[0].Text)
		}
	}
	del(projID)
	del(uniID)

	if rows := listComments(t, ctx, adapter, map[string]any{"comment_id": projID}); len(rows) != 0 {
		t.Fatalf("project comment still present after delete: %#v", rows)
	}
	if rows := listComments(t, ctx, adapter, map[string]any{"comment_id": uniID}); len(rows) != 0 {
		t.Fatalf("universal comment still present after delete: %#v", rows)
	}
}

// TestAdapterCommentsListFiltersThroughCallTool exercises the comments.list
// filter surface beyond `kind`: tag, pinned, query (FTS body + universal
// title-only hit), since (duration shorthand window), and comment_id (cross
// scope get-by-id). Each runs through CallTool against a single seeded set.
func TestAdapterCommentsListFiltersThroughCallTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// project recap: pinned, tagged deploy, body has "alpha".
	pinnedC := addComment(t, ctx, adapter, map[string]any{
		"scope": "project", "body": "deploy plan alpha", "author_type": "agent",
		"kind": "recap", "pinned": true, "tags": []any{"deploy"},
	})
	// project standup: unpinned, untagged, body has "beta".
	addComment(t, ctx, adapter, map[string]any{
		"scope": "project", "body": "standup beta", "author_type": "agent", "kind": "standup",
	})
	// universal note matched by a title-only word ("zephyr"); body lacks it.
	uniC := addComment(t, ctx, adapter, map[string]any{
		"scope": "universal", "body": "global body text", "title": "zephyr heading", "author_type": "agent",
	})

	// tag=deploy → only the deploy-tagged project recap.
	if rows := listComments(t, ctx, adapter, map[string]any{"tag": "deploy"}); len(rows) != 1 || rows[0]["body"] != "deploy plan alpha" {
		t.Fatalf("comments.list(tag=deploy) = %#v, want one deploy row", rows)
	}

	// pinned=true → only the single pinned project recap.
	pinnedRows := listComments(t, ctx, adapter, map[string]any{"pinned": true})
	if len(pinnedRows) != 1 || pinnedRows[0]["pinned"] != true {
		t.Fatalf("comments.list(pinned=true) = %#v, want one pinned row", pinnedRows)
	}

	// query=alpha → FTS body hit on the project recap.
	if rows := listComments(t, ctx, adapter, map[string]any{"query": "alpha"}); len(rows) != 1 || rows[0]["body"] != "deploy plan alpha" {
		t.Fatalf("comments.list(query=alpha) = %#v, want one body-match row", rows)
	}

	// query=zephyr → FTS title-only hit on the universal note (scope=universal
	// so the project-scoped projection does not exclude the project_id=NULL row).
	zephyrRows := listComments(t, ctx, adapter, map[string]any{"scope": "universal", "query": "zephyr"})
	if len(zephyrRows) != 1 || int64(zephyrRows[0]["id"].(float64)) != int64(uniC["id"].(float64)) {
		t.Fatalf("comments.list(query=zephyr) = %#v, want the universal title-only hit", zephyrRows)
	}

	// since=24h → the just-added project rows are within the window (the
	// resolved floor is now-24h, formatted to the SQLite datetime shape the
	// datetime()-normalized column comparison consumes).
	recent := listComments(t, ctx, adapter, map[string]any{"scope": "project", "since": "24h"})
	if len(recent) != 2 {
		t.Fatalf("comments.list(scope=project, since=24h) = %d rows, want 2 recent", len(recent))
	}

	// comment_id → exactly that row, reachable across scopes (universal here).
	idRows := listComments(t, ctx, adapter, map[string]any{"comment_id": int64(pinnedC["id"].(float64))})
	if len(idRows) != 1 || int64(idRows[0]["id"].(float64)) != int64(pinnedC["id"].(float64)) {
		t.Fatalf("comments.list(comment_id) = %#v, want exactly the named row", idRows)
	}
	uniIDRows := listComments(t, ctx, adapter, map[string]any{"comment_id": int64(uniC["id"].(float64))})
	if len(uniIDRows) != 1 || int64(uniIDRows[0]["id"].(float64)) != int64(uniC["id"].(float64)) {
		t.Fatalf("comments.list(comment_id universal) = %#v, want the universal row across scopes", uniIDRows)
	}
}
