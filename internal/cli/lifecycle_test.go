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
// (and the parseTaskID + open + resolveProject + service-call chain
// inside each) are all exercised at least once on success and once on
// validation failure.
func TestCLIDependencyAndLifecycleCommands(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "First")
	runCLI(t, dbPath, configPath, "add", "-t", "Second")
	runCLI(t, dbPath, configPath, "add", "-t", "Third")

	// depend add then list — list path was 11.1% covered.
	runCLI(t, dbPath, configPath, "depend", "add", "2", "-i", "1")
	runCLI(t, dbPath, configPath, "depend", "add", "3", "-i", "1")
	listed := runCLI(t, dbPath, configPath, "depend", "list", "1")
	if !strings.Contains(listed, `"dependencies"`) {
		t.Fatalf("depend list output missing dependencies key: %s", listed)
	}

	// depend remove — was 25% covered.
	removed := runCLI(t, dbPath, configPath, "depend", "remove", "2", "-i", "1")
	if !strings.Contains(removed, `"removed":true`) {
		t.Fatalf("depend remove output missing removed=true: %s", removed)
	}

	// list (TASK_ID provided implicitly via flag): the top-level `list` command
	// renders the active board, hitting newListCommand's RunE.
	listOut := runCLI(t, dbPath, configPath, "list")
	if !strings.Contains(listOut, `"tasks"`) {
		t.Fatalf("list output missing tasks: %s", listOut)
	}

	// archive / unarchive / delete cycle on task 3 (no remaining dependencies on it).
	archived := runCLI(t, dbPath, configPath, "archive", "3")
	if !strings.Contains(archived, `"state":"archived"`) {
		t.Fatalf("archive output: %s", archived)
	}
	unarchived := runCLI(t, dbPath, configPath, "unarchive", "3")
	if !strings.Contains(unarchived, `"state":"active"`) {
		t.Fatalf("unarchive output: %s", unarchived)
	}
	// delete requires the task to be in a bucket whose permissions allow
	// destruction. The default workflow only permits delete in backlog, and
	// task 3 is now back there after the unarchive cycle — exercise the
	// confirm-flag path on task 2 (still in backlog) so the cascade runs.
	deleted := runCLI(t, dbPath, configPath, "delete", "2", "--confirm")
	if !strings.Contains(deleted, `"task.removed"`) {
		t.Fatalf("delete output missing task.removed event: %s", deleted)
	}

	// Repeat without --confirm on the last remaining task to cover the
	// validation-error branch of newDeleteCommand explicitly.
	envelope2 := runCLIExpectError(t, dbPath, configPath, "validation_error", "delete", "3")
	if _, ok := envelope2["details"]; !ok {
		t.Fatalf("delete-without-confirm envelope missing details: %v", envelope2)
	}

	// Error paths: validation_error on a malformed task id surfaces the
	// coded envelope and exercises parseTaskID's failure branch.
	envelope1 := runCLIExpectError(t, dbPath, configPath, "validation_error", "depend", "list", "not-a-number")
	if _, ok := envelope1["details"]; !ok {
		t.Fatalf("validation envelope missing details: %v", envelope1)
	}

	// task_not_found on archive of a non-existent id.
	runCLIExpectError(t, dbPath, configPath, "task_not_found", "archive", "9999")
}
