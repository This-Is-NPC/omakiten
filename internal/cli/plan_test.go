package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPlanLifecycle(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "T1")
	runCLI(t, dbPath, configPath, "add", "-t", "T2")

	created := runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship", "--goal-body", "Goal")
	if !strings.Contains(created, `"slug":"ship"`) || !strings.Contains(created, `"goal_body":"Goal"`) {
		t.Fatalf("plan create output = %s", created)
	}

	listed := runCLI(t, dbPath, configPath, "plan", "list")
	if !strings.Contains(listed, `"slug":"ship"`) {
		t.Fatalf("plan list output = %s", listed)
	}

	wave := runCLI(t, dbPath, configPath, "plan", "wave-add", "ship", "alpha")
	if !strings.Contains(wave, `"position":1`) || !strings.Contains(wave, `"name":"alpha"`) {
		t.Fatalf("wave-add output = %s", wave)
	}

	// Re-parse wave id from the JSON; cheaper to grep the printed payload
	// than to re-run a fresh JSON decoder fixture here.
	idIdx := strings.Index(wave, `"id":`)
	if idIdx < 0 {
		t.Fatalf("wave-add lacks id field: %s", wave)
	}
	commaIdx := strings.Index(wave[idIdx:], ",")
	if commaIdx < 0 {
		t.Fatalf("wave-add id field missing terminator: %s", wave)
	}
	waveID := strings.TrimPrefix(wave[idIdx:idIdx+commaIdx], `"id":`)

	runCLI(t, dbPath, configPath, "plan", "assign", "ship", waveID, "1")

	shown := runCLI(t, dbPath, configPath, "plan", "show", "ship")
	if !strings.Contains(shown, `"total_count":1`) {
		t.Fatalf("plan show should report 1 task assigned: %s", shown)
	}
}

func TestCLIAssignSetsAndClearsAssignee(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "T1")

	// Set assignee via `okt assign 1 alice` → emits task.assigned.
	setOut := runCLI(t, dbPath, configPath, "assign", "1", "alice")
	if !strings.Contains(setOut, `"event_type":"task.assigned"`) {
		t.Fatalf("set output should carry task.assigned: %s", setOut)
	}

	// Clear via `okt assign 1` (no WHO) → emits task.unassigned.
	clearOut := runCLI(t, dbPath, configPath, "assign", "1")
	if !strings.Contains(clearOut, `"event_type":"task.unassigned"`) {
		t.Fatalf("clear output should carry task.unassigned: %s", clearOut)
	}
}

func TestCLIPlanClaimRequiresAgentModel(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship")
	runCLI(t, dbPath, configPath, "plan", "wave-add", "ship", "alpha")

	// claim with empty OMAKITEN_AGENT_MODEL must fail validation.
	t.Setenv("OMAKITEN_AGENT_MODEL", "")
	runCLIExpectError(t, dbPath, configPath, "validation_error", "plan", "claim", "ship")
}

func TestCLIPlanEditNameSlugStatus(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship", "--goal-body", "old")

	edited := runCLI(t, dbPath, configPath, "plan", "edit", "ship",
		"--name", "Shipped", "--slug", "shipped", "--status", "done")
	if !strings.Contains(edited, `"slug":"shipped"`) || !strings.Contains(edited, `"name":"Shipped"`) {
		t.Fatalf("plan edit output = %s", edited)
	}
	if !strings.Contains(edited, `"status":"done"`) {
		t.Fatalf("plan edit status not applied: %s", edited)
	}

	// Goal-body-only edit on the new slug.
	goal := runCLI(t, dbPath, configPath, "plan", "edit", "shipped", "--goal-body", "fresh")
	if !strings.Contains(goal, `"goal_body":"fresh"`) {
		t.Fatalf("plan edit goal output = %s", goal)
	}
}

func TestCLIPlanEditRequiresAtLeastOneFlag(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship")
	runCLIExpectError(t, dbPath, configPath, "validation_error", "plan", "edit", "ship")
}

// TestCLIPlanEditNoOpNameDoesNotPersistGoalBody pins the partial-write
// guard: `plan edit --goal-body NEW --name <unchanged>` must reject the
// no-op name diff WITHOUT having persisted the goal-body edit. Before
// the ordering fix the goal write committed + emitted first, then
// UpdatePlan rejected "changed nothing", leaving the new goal on disk
// behind an error response. Running UpdatePlan first means the no-op
// rejection fires before the goal write, so the goal body stays "old".
func TestCLIPlanEditNoOpNameDoesNotPersistGoalBody(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship", "--goal-body", "old")

	// --goal-body changes, but --name repeats the current name → the
	// name diff is a no-op and UpdatePlan rejects "changed nothing".
	runCLIExpectError(t, dbPath, configPath, "validation_error",
		"plan", "edit", "ship", "--goal-body", "fresh", "--name", "Ship")

	// The rejected no-op must NOT have leaked the goal-body write.
	shown := runCLI(t, dbPath, configPath, "plan", "show", "ship")
	if !strings.Contains(shown, `"goal_body":"old"`) {
		t.Fatalf("goal_body should still be \"old\" after rejected edit, got: %s", shown)
	}
	if strings.Contains(shown, `"goal_body":"fresh"`) {
		t.Fatalf("partial write: goal_body was persisted despite the rejected name diff: %s", shown)
	}
}

func TestCLIPlanDeleteRequiresConfirm(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship")

	// Without --confirm → validation error, plan survives.
	runCLIExpectError(t, dbPath, configPath, "validation_error", "plan", "delete", "ship")
	if listed := runCLI(t, dbPath, configPath, "plan", "list"); !strings.Contains(listed, `"slug":"ship"`) {
		t.Fatalf("plan should survive unconfirmed delete: %s", listed)
	}

	// With --confirm → removed.
	deleted := runCLI(t, dbPath, configPath, "plan", "delete", "ship", "--confirm")
	if !strings.Contains(deleted, `"deleted":"ship"`) {
		t.Fatalf("plan delete output = %s", deleted)
	}
	if listed := runCLI(t, dbPath, configPath, "plan", "list"); strings.Contains(listed, `"slug":"ship"`) {
		t.Fatalf("plan should be gone after confirmed delete: %s", listed)
	}
}

func TestCLIPlanWaveMutationsAndUnassign(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "T1")
	runCLI(t, dbPath, configPath, "plan", "create", "ship", "--name", "Ship")

	w1 := runCLI(t, dbPath, configPath, "plan", "wave-add", "ship", "alpha", "--position", "1")
	wave1ID := waveIDFromJSON(t, w1)
	runCLI(t, dbPath, configPath, "plan", "wave-add", "ship", "beta", "--position", "2")

	// wave-rename.
	renamed := runCLI(t, dbPath, configPath, "plan", "wave-rename", wave1ID, "alpha-prime")
	if !strings.Contains(renamed, `"name":"alpha-prime"`) {
		t.Fatalf("wave-rename output = %s", renamed)
	}

	// wave-reorder: move wave 1 to position 2 (collision → swap).
	reordered := runCLI(t, dbPath, configPath, "plan", "wave-reorder", wave1ID, "2")
	if !strings.Contains(reordered, `"position":2`) {
		t.Fatalf("wave-reorder output = %s", reordered)
	}

	// assign task 1 to wave 1, then unassign.
	runCLI(t, dbPath, configPath, "plan", "assign", "ship", wave1ID, "1")
	unassigned := runCLI(t, dbPath, configPath, "plan", "unassign", "1")
	if !strings.Contains(unassigned, `"detached":true`) {
		t.Fatalf("unassign output = %s", unassigned)
	}

	// wave-remove requires --confirm.
	runCLIExpectError(t, dbPath, configPath, "validation_error", "plan", "wave-remove", wave1ID)
	removed := runCLI(t, dbPath, configPath, "plan", "wave-remove", wave1ID, "--confirm")
	if !strings.Contains(removed, `"removed_wave"`) {
		t.Fatalf("wave-remove output = %s", removed)
	}
}

// waveIDFromJSON extracts the wave id from a wave-add JSON payload.
func waveIDFromJSON(t *testing.T, out string) string {
	t.Helper()
	idIdx := strings.Index(out, `"id":`)
	if idIdx < 0 {
		t.Fatalf("payload lacks id field: %s", out)
	}
	commaIdx := strings.Index(out[idIdx:], ",")
	if commaIdx < 0 {
		t.Fatalf("id field missing terminator: %s", out)
	}
	return strings.TrimPrefix(out[idIdx:idIdx+commaIdx], `"id":`)
}
