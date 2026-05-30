package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// commentTestEnv spins up an initialized project with one task, returning the
// db/config paths. Mirrors the plan_test harness.
func commentTestEnv(t *testing.T) (dbPath, configPath string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath = filepath.Join(tmp, "omakiten.db")
	configPath = filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "T1")
	return dbPath, configPath
}

// commentIDFromJSON decodes the comment.id field from an add/edit JSON payload.
// Decoding the nested object explicitly avoids scraping the first "id":, which
// would silently pick up the wrong field if the envelope shape ever changes.
func commentIDFromJSON(t *testing.T, out string) string {
	t.Helper()
	var payload struct {
		Data struct {
			Comment struct {
				ID int64 `json:"id"`
			} `json:"comment"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode comment payload: %v\n%s", err, out)
	}
	if payload.Data.Comment.ID == 0 {
		t.Fatalf("payload lacks data.comment.id: %s", out)
	}
	return strconv.FormatInt(payload.Data.Comment.ID, 10)
}

func TestCLICommentAddScopes(t *testing.T) {
	dbPath, configPath := commentTestEnv(t)

	// Project scope with no TASK_ID succeeds.
	proj := runCLI(t, dbPath, configPath, "comment", "add", "--scope", "project", "-b", "project note")
	if !strings.Contains(proj, `"scope":"project"`) || !strings.Contains(proj, `"body":"project note"`) {
		t.Fatalf("project-scope add output = %s", proj)
	}

	// Universal scope with no TASK_ID succeeds.
	uni := runCLI(t, dbPath, configPath, "comment", "add", "--scope", "universal", "-b", "universal note")
	if !strings.Contains(uni, `"scope":"universal"`) || !strings.Contains(uni, `"body":"universal note"`) {
		t.Fatalf("universal-scope add output = %s", uni)
	}

	// Task scope (default) with TASK_ID succeeds and carries title/kind/pinned.
	task := runCLI(t, dbPath, configPath, "comment", "add", "1",
		"-b", "task note", "--title", "Heading", "--kind", "handoff", "--pinned")
	if !strings.Contains(task, `"title":"Heading"`) || !strings.Contains(task, `"kind":"handoff"`) || !strings.Contains(task, `"pinned":true`) {
		t.Fatalf("task-scope add output = %s", task)
	}

	// Project scope WITH a TASK_ID errors.
	runCLIExpectError(t, dbPath, configPath, "validation_error",
		"comment", "add", "1", "--scope", "project", "-b", "bad")

	// Task scope WITHOUT a TASK_ID errors.
	runCLIExpectError(t, dbPath, configPath, "validation_error",
		"comment", "add", "--scope", "task", "-b", "bad")
}

func TestCLICommentListFilters(t *testing.T) {
	dbPath, configPath := commentTestEnv(t)

	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "plain task note")
	pinned := runCLI(t, dbPath, configPath, "comment", "add", "--scope", "project",
		"-b", "pinned recap", "--kind", "recap", "--pinned")
	pinnedID := commentIDFromJSON(t, pinned)
	runCLI(t, dbPath, configPath, "comment", "add", "--scope", "project", "-b", "unpinned note")

	// Bare task list (back-compat) returns the task comment via legacy List.
	bare := runCLI(t, dbPath, configPath, "comment", "list", "1")
	if !strings.Contains(bare, `"body":"plain task note"`) {
		t.Fatalf("bare list output = %s", bare)
	}
	if strings.Contains(bare, `"body":"pinned recap"`) {
		t.Fatalf("bare task list leaked project comment: %s", bare)
	}

	// Filter by scope=project returns both project comments, not the task one.
	byScope := runCLI(t, dbPath, configPath, "comment", "list", "--scope", "project")
	if !strings.Contains(byScope, `"body":"pinned recap"`) || !strings.Contains(byScope, `"body":"unpinned note"`) {
		t.Fatalf("scope=project list output = %s", byScope)
	}
	if strings.Contains(byScope, `"body":"plain task note"`) {
		t.Fatalf("scope=project list leaked task comment: %s", byScope)
	}

	// Filter by kind=recap returns only the recap.
	byKind := runCLI(t, dbPath, configPath, "comment", "list", "--kind", "recap")
	if !strings.Contains(byKind, `"body":"pinned recap"`) || strings.Contains(byKind, `"body":"unpinned note"`) {
		t.Fatalf("kind=recap list output = %s", byKind)
	}

	// Filter by --pinned returns only the pinned comment.
	byPinned := runCLI(t, dbPath, configPath, "comment", "list", "--pinned")
	if !strings.Contains(byPinned, `"body":"pinned recap"`) || strings.Contains(byPinned, `"body":"unpinned note"`) {
		t.Fatalf("--pinned list output = %s", byPinned)
	}

	// Filter by --comment-id returns exactly that row.
	byID := runCLI(t, dbPath, configPath, "comment", "list", "--comment-id", pinnedID)
	if !strings.Contains(byID, `"body":"pinned recap"`) || strings.Contains(byID, `"body":"unpinned note"`) {
		t.Fatalf("--comment-id list output = %s", byID)
	}
}

// TestCLICommentEditTriState pins the #385 fix through the CLI: a body-only
// edit preserves pinned/title/kind; an explicit --pinned=false unpins; and a
// metadata-only edit (no --body) is accepted and leaves the body intact.
func TestCLICommentEditTriState(t *testing.T) {
	dbPath, configPath := commentTestEnv(t)

	created := runCLI(t, dbPath, configPath, "comment", "add", "--scope", "project",
		"-b", "before", "--title", "Heading", "--kind", "handoff", "--pinned")
	id := commentIDFromJSON(t, created)

	// Body-only edit must preserve pinned/title/kind.
	bodyOnly := runCLI(t, dbPath, configPath, "comment", "edit", id, "-b", "after")
	if !strings.Contains(bodyOnly, `"body":"after"`) {
		t.Fatalf("body-only edit did not change body: %s", bodyOnly)
	}
	if !strings.Contains(bodyOnly, `"pinned":true`) ||
		!strings.Contains(bodyOnly, `"title":"Heading"`) ||
		!strings.Contains(bodyOnly, `"kind":"handoff"`) {
		t.Fatalf("body-only edit wiped metadata: %s", bodyOnly)
	}

	// Metadata-only edit (no --body) must work and leave the body intact.
	metaOnly := runCLI(t, dbPath, configPath, "comment", "edit", id, "--title", "Updated")
	if !strings.Contains(metaOnly, `"title":"Updated"`) {
		t.Fatalf("metadata-only edit did not set title: %s", metaOnly)
	}
	if !strings.Contains(metaOnly, `"body":"after"`) {
		t.Fatalf("metadata-only edit changed the body: %s", metaOnly)
	}
	if !strings.Contains(metaOnly, `"pinned":true`) {
		t.Fatalf("metadata-only edit wiped pinned: %s", metaOnly)
	}

	// Explicit --pinned=false unpins (pinned is omitempty → absent when false).
	unpin := runCLI(t, dbPath, configPath, "comment", "edit", id, "--pinned=false")
	if strings.Contains(unpin, `"pinned":true`) {
		t.Fatalf("--pinned=false did not unpin: %s", unpin)
	}
	if !strings.Contains(unpin, `"title":"Updated"`) || !strings.Contains(unpin, `"kind":"handoff"`) {
		t.Fatalf("--pinned=false wiped title/kind: %s", unpin)
	}

	// Edit with no fields at all is rejected.
	runCLIExpectError(t, dbPath, configPath, "validation_error", "comment", "edit", id)
}

// TestCLICommentEditPreservesTagsOnBodyOnly pins the tag tri-state through the
// CLI: `comment edit ID --body x` with no --tag must NOT wipe the comment's
// tags. Fails on the pre-fix path that always forwarded a (nil) tag slice and
// unconditionally replaced the tag set in the store.
func TestCLICommentEditPreservesTagsOnBodyOnly(t *testing.T) {
	dbPath, configPath := commentTestEnv(t)

	created := runCLI(t, dbPath, configPath, "comment", "add", "--scope", "project",
		"-b", "before", "--tag", "keepme")
	id := commentIDFromJSON(t, created)

	// Body-only edit (no --tag) preserves keepme.
	bodyOnly := runCLI(t, dbPath, configPath, "comment", "edit", id, "-b", "after")
	if !strings.Contains(bodyOnly, `"body":"after"`) {
		t.Fatalf("body-only edit did not change body: %s", bodyOnly)
	}
	if !strings.Contains(bodyOnly, `"name":"keepme"`) {
		t.Fatalf("body-only edit wiped tags: %s", bodyOnly)
	}

	// Explicit --tag replaces.
	replaced := runCLI(t, dbPath, configPath, "comment", "edit", id, "--tag", "other")
	if !strings.Contains(replaced, `"name":"other"`) || strings.Contains(replaced, `"name":"keepme"`) {
		t.Fatalf("--tag edit did not replace tags: %s", replaced)
	}
}

// TestCLICommentGuardDenied proves the comment edit/delete guards are
// enforced THROUGH the `okt comment` CLI (not just at the agent/MCP layer):
// the CLI routes edit/delete via commentServiceWithWorkflow(...).Edit/Remove
// → enforceCommentPermission, the same guard path MCP uses. A denial must
// surface the coded `guard_violation` envelope.
//
// Two scopes are exercised against the seeded default omakase workflow:
//
//   - TASK scope: the default config allows comment edit/delete in `backlog`
//     and `dev`, but `done` pins permissions.comment.delete=false and inherits
//     edit=false from defaults.comment. Moving a task to `done` and then
//     editing/deleting its comment must be blocked — the bucket-resolved
//     task-scope path. A backlog comment edit/delete is the ALLOWED control.
//
//   - PROJECT scope: the seeded default has no defaults.comment.project
//     sub-block (project comments resolve to the implicit allow). We overwrite
//     the seeded omakase.yaml with a defaults.comment.project deny sub-block so
//     a project-scoped edit is blocked task-lessly via
//     ResolveCommentScopePermission. EnsureDefaultFiles never overwrites an
//     existing file, so the next CLI invocation LoadBundle's our deny policy.
func TestCLICommentGuardDenied(t *testing.T) {
	dbPath, configPath := commentTestEnv(t)

	// --- TASK scope, ALLOWED control: a comment on a backlog task edits and
	// deletes fine under the default policy. Task 1 lands in `backlog`.
	ctrl := runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "backlog note")
	ctrlID := commentIDFromJSON(t, ctrl)
	edited := runCLI(t, dbPath, configPath, "comment", "edit", ctrlID, "-b", "backlog note edited")
	if !strings.Contains(edited, `"body":"backlog note edited"`) {
		t.Fatalf("backlog (allowed) edit did not apply: %s", edited)
	}
	if del := runCLI(t, dbPath, configPath, "comment", "delete", ctrlID, "--confirm"); !strings.Contains(del, `"snapshot"`) {
		t.Fatalf("backlog (allowed) delete did not succeed: %s", del)
	}

	// --- TASK scope, DENIED: add a comment, move the task to `done`
	// (comment edit inherited-false, delete pinned-false), then edit/delete.
	denied := runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "doomed note")
	deniedID := commentIDFromJSON(t, denied)
	// Walk the task to `done` through the default omakase transition guards,
	// satisfying each forward gate with the tagged comment it requires:
	// backlog→dev (self-branch), dev→review (resume + tests-passing),
	// review→done (documentation).
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "feat/x", "--tag", "self-branch")
	runCLI(t, dbPath, configPath, "move", "1", "-t", "dev")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "resume", "--tag", "resume")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "tests", "--tag", "tests-passing")
	runCLI(t, dbPath, configPath, "move", "1", "-t", "review")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "docs", "--tag", "documentation")
	runCLI(t, dbPath, configPath, "move", "1", "-t", "done")

	// Now in `done`: comment edit is denied (defaults.comment.edit=false,
	// no bucket override) and delete is denied (done pins delete=false).
	runCLIExpectError(t, dbPath, configPath, "guard_violation",
		"comment", "edit", deniedID, "-b", "nope")
	runCLIExpectError(t, dbPath, configPath, "guard_violation",
		"comment", "delete", deniedID, "--confirm")

	// --- PROJECT scope, DENIED via injected deny-policy. Add a project
	// comment FIRST (project edits are allowed by default), then overwrite the
	// seeded config with a defaults.comment.project.edit=false sub-block so the
	// subsequent edit is blocked task-lessly.
	proj := runCLI(t, dbPath, configPath, "comment", "add", "--scope", "project", "-b", "project note")
	projID := commentIDFromJSON(t, proj)
	injectProjectCommentDeny(t, configPath)
	runCLIExpectError(t, dbPath, configPath, "guard_violation",
		"comment", "edit", projID, "-b", "blocked")
}

// injectProjectCommentDeny rewrites the seeded omakase.yaml so the active
// workflow declares defaults.comment.project.{edit,delete}=false. It targets
// the verbatim default block shipped in defaults/config/omakase.yaml:
//
//	      comment:
//	        edit: false
//	        delete: false
//
// replacing it with the same flat fields PLUS a project deny sub-block. The
// flat fields are kept so the task-scope chain is unchanged; only the
// previously-absent project sub-block is added. EnsureDefaultFiles skips files
// that already exist, so this on-disk edit survives subsequent CLI boots.
func injectProjectCommentDeny(t *testing.T, configPath string) {
	t.Helper()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read seeded config %s: %v", configPath, err)
	}
	const oldBlock = "      comment:\n        edit: false\n        delete: false\n"
	const newBlock = "      comment:\n        edit: false\n        delete: false\n" +
		"        project:\n          edit: false\n          delete: false\n"
	if !strings.Contains(string(raw), oldBlock) {
		t.Fatalf("seeded config lacks the expected defaults.comment block; "+
			"the default omakase.yaml layout changed. content:\n%s", raw)
	}
	patched := strings.Replace(string(raw), oldBlock, newBlock, 1)
	if err := os.WriteFile(configPath, []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched config %s: %v", configPath, err)
	}
}
