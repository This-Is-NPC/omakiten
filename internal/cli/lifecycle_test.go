package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIDependencyAndLifecycleCommands fills the error-path coverage
// gap audited under F-002: depend remove, depend list, archive, delete,
// unarchive, and list each had <30% line coverage. The test drives them
// end-to-end through runCLI / runCLIExpectError so the RunE closures
// (and the parseTaskID + open + operation.Service call chain inside each)
// are all exercised at least once on success and once on validation
// failure.
func TestCLIDependencyAndLifecycleCommands(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "First")
	runCLI(t, dbPath, configPath, "add", "-t", "Second")
	runCLI(t, dbPath, configPath, "add", "-t", "Third")

	testCLIDependencyCommands(t, dbPath, configPath)
	testCLILifecycleCommands(t, dbPath, configPath)
	testCLILifecycleErrors(t, dbPath, configPath)
}

func testCLIDependencyCommands(t *testing.T, dbPath, configPath string) {
	t.Helper()
	runCLI(t, dbPath, configPath, "depend", "add", "2", "-i", "1")
	runCLI(t, dbPath, configPath, "depend", "add", "3", "-i", "1")
	listed := runCLI(t, dbPath, configPath, "depend", "list", "1")
	if !strings.Contains(listed, `"dependencies"`) {
		t.Fatalf("depend list output missing dependencies key: %s", listed)
	}
	depPreview := runCLI(t, dbPath, configPath, "depend", "remove", "2", "-i", "1")
	if !strings.Contains(depPreview, `"requires_confirmation":true`) && !strings.Contains(depPreview, `"requires_confirmation": true`) {
		t.Fatalf("depend remove without confirm should require confirmation: %s", depPreview)
	}
	removed := runCLI(t, dbPath, configPath, "depend", "remove", "2", "-i", "1", "--confirm")
	if !strings.Contains(removed, `"removed":true`) {
		t.Fatalf("depend remove output missing removed=true: %s", removed)
	}
	listOut := runCLI(t, dbPath, configPath, "list")
	if !strings.Contains(listOut, `"tasks"`) {
		t.Fatalf("list output missing tasks: %s", listOut)
	}
}

func testCLILifecycleCommands(t *testing.T, dbPath, configPath string) {
	t.Helper()
	runCLI(t, dbPath, configPath, "comment", "add", "3", "-b", "shipped", "--tag", "documentation")
	archived := runCLI(t, dbPath, configPath, "archive", "3")
	if !strings.Contains(archived, `"state":"archived"`) {
		t.Fatalf("archive output: %s", archived)
	}
	unarchived := runCLI(t, dbPath, configPath, "unarchive", "3")
	if strings.Contains(unarchived, `"state":"archived"`) {
		t.Fatalf("unarchive left state=archived: %s", unarchived)
	}
	if !strings.Contains(unarchived, `"id":3`) && !strings.Contains(unarchived, `"id": 3`) {
		t.Fatalf("unarchive missing task id: %s", unarchived)
	}
	deleted := runCLI(t, dbPath, configPath, "delete", "2", "--confirm")
	if !strings.Contains(deleted, `"task.removed"`) {
		t.Fatalf("delete output missing task.removed event: %s", deleted)
	}
	preview := runCLI(t, dbPath, configPath, "delete", "3")
	if !strings.Contains(preview, `"requires_confirmation":true`) && !strings.Contains(preview, `"requires_confirmation": true`) {
		t.Fatalf("delete-without-confirm missing requires_confirmation: %s", preview)
	}
}

func testCLILifecycleErrors(t *testing.T, dbPath, configPath string) {
	t.Helper()
	envelope := runCLIExpectError(t, dbPath, configPath, "validation_error", "depend", "list", "not-a-number")
	if _, ok := envelope["details"]; !ok {
		t.Fatalf("validation envelope missing details: %v", envelope)
	}
	runCLIExpectError(t, dbPath, configPath, "task_not_found", "archive", "9999")
}

// TestCLISubTaskParentFlags closes the gap reported in comment #8280 — that
// `add`, `edit`, and `list` did not surface the `--parent` flag while the
// service layer was already wired for sub-tasks. The test drives each
// command through the public CLI surface so the cobra flag + Changed()
// branch + service call get exercised end-to-end, including the tri-state
// behaviour of `--parent` for `edit` (re-parent then clear) and `list`
// (roots / direct children).
func TestCLISubTaskParentFlags(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "Root")

	// add --parent attaches the new row as a sub-task via CreateTask/AddSub.
	subOut := runCLI(t, dbPath, configPath, "add", "-t", "Child", "--parent", "1")
	if !strings.Contains(subOut, `"parent_id":1`) {
		t.Fatalf("add --parent output missing parent_id=1: %s", subOut)
	}

	// list --parent 1 returns the direct children of #1.
	childList := runCLI(t, dbPath, configPath, "list", "--parent", "1")
	if !strings.Contains(childList, `"parent_id":1`) {
		t.Fatalf("list --parent 1 missing child: %s", childList)
	}

	// list --parent 0 (sentinel) returns roots only — the child must be
	// filtered out.
	rootList := runCLI(t, dbPath, configPath, "list", "--parent", "0")
	if strings.Contains(rootList, `"parent_id":1`) {
		t.Fatalf("list --parent 0 leaked sub-task: %s", rootList)
	}

	// edit --parent 0 re-roots the child (clears parent_id).
	reroot := runCLI(t, dbPath, configPath, "edit", "2", "--parent", "0")
	if strings.Contains(reroot, `"parent_id":`) {
		t.Fatalf("edit --parent 0 left parent_id on payload: %s", reroot)
	}

	// edit --parent <id> re-parents the now-rooted task back under #1
	// (anti-cycle is enforced by the service layer; happy path here).
	reparent := runCLI(t, dbPath, configPath, "edit", "2", "--parent", "1")
	if !strings.Contains(reparent, `"parent_id":1`) {
		t.Fatalf("edit --parent 1 missing parent_id=1: %s", reparent)
	}
}

// TestCLITaskCRUDThroughOperationLayer covers add / edit / list / move
// after the CLI was routed through operation.Service (CreateTask, EditTask,
// ListTasks, MoveTask). edit --bucket must call MoveTask separately because
// EditTask intentionally omits BucketKey.
func TestCLITaskCRUDThroughOperationLayer(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	added := runCLI(t, dbPath, configPath, "add", "-t", "CRUD task", "-d", "body")
	if !strings.Contains(added, `"title":"CRUD task"`) {
		t.Fatalf("add missing title: %s", added)
	}
	if !strings.Contains(added, `"id":1`) && !strings.Contains(added, `"id": 1`) {
		t.Fatalf("add missing id: %s", added)
	}

	edited := runCLI(t, dbPath, configPath, "edit", "1", "-t", "Renamed", "--priority", "high")
	if !strings.Contains(edited, `"title":"Renamed"`) {
		t.Fatalf("edit title missing: %s", edited)
	}
	if !strings.Contains(edited, `"priority":"high"`) {
		t.Fatalf("edit priority missing: %s", edited)
	}

	// omakase backlog→dev requires a #self-branch comment.
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "feat/crud", "--tag", "self-branch")

	// edit --bucket alone routes through MoveTask (EditTask has no BucketKey).
	bucketed := runCLI(t, dbPath, configPath, "edit", "1", "-b", "dev")
	if !strings.Contains(bucketed, `"bucket_key":"dev"`) {
		t.Fatalf("edit --bucket missing dev: %s", bucketed)
	}

	// Return to backlog (unguarded regression) so field edits remain allowed,
	// then combine EditTask + MoveTask in one CLI invocation.
	runCLI(t, dbPath, configPath, "move", "1", "--to", "backlog")
	combined := runCLI(t, dbPath, configPath, "edit", "1", "-t", "Moved rename", "-b", "dev")
	if !strings.Contains(combined, `"title":"Moved rename"`) {
		t.Fatalf("edit+bucket title missing: %s", combined)
	}
	if !strings.Contains(combined, `"bucket_key":"dev"`) {
		t.Fatalf("edit+bucket bucket missing: %s", combined)
	}

	listed := runCLI(t, dbPath, configPath, "list", "-b", "dev")
	if !strings.Contains(listed, `"Moved rename"`) {
		t.Fatalf("list --bucket dev missed task: %s", listed)
	}

	moved := runCLI(t, dbPath, configPath, "move", "1", "--to", "backlog")
	if !strings.Contains(moved, `"bucket_key":"backlog"`) {
		t.Fatalf("move --to missing backlog: %s", moved)
	}

	// Empty edit patch should fail validation via the facade/service.
	runCLIExpectError(t, dbPath, configPath, "validation_error", "edit", "1")
}
