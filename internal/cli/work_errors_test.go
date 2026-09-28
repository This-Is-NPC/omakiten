package cli

import (
	"path/filepath"
	"testing"
)

func TestCLIWorkFileValidationPreservesTasksAndPlans(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
	file, plain := filepath.Join(root, "task.md"), filepath.Join(root, "plain.md")
	writeFile(t, file, "---\ntype: Omakiten Task\ntitle: Imported\nomakiten: {version: 1}\n---\nTask body\n")
	writeFile(t, plain, "Task body\n")
	beforeTasks, beforePlans := runCLI(t, db, cfg, "list"), runCLI(t, db, cfg, "plan", "list")
	cases := map[string][]string{
		"plan slug with file":              {"plan", "create", "extra-slug", "--file", file},
		"plan missing name":                {"plan", "create", "example"},
		"plan dry-run without file":        {"plan", "create", "example", "--name", "Example", "--dry-run"},
		"task dry-run without OKF":         {"task", "create", "--file", plain, "--dry-run"},
		"task title overrides OKF":         {"task", "create", "--file", file, "--title", "Override"},
		"task export nonnumeric id":        {"task", "export", "not-an-id"},
		"wave remove nonnumeric id":        {"plan", "wave-remove", "not-an-id", "--confirm"},
		"wave rename nonnumeric id":        {"plan", "wave-rename", "not-an-id", "New name"},
		"wave reorder nonnumeric id":       {"plan", "wave-reorder", "not-an-id", "1"},
		"wave reorder nonnumeric position": {"plan", "wave-reorder", "1", "not-a-position"},
		"unassign nonnumeric task":         {"plan", "unassign", "not-an-id"},
		"assign nonnumeric wave":           {"plan", "assign", "example", "not-an-id", "1"},
		"assign nonnumeric task":           {"plan", "assign", "example", "1", "not-an-id"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) { runCLIExpectError(t, db, cfg, "validation_error", args...) })
	}
	writeFile(t, file, "---\nbroken: [\n---\n")
	runCLIExpectError(t, db, cfg, "validation_error", "plan", "import", "--file", file)
	runCLIExpectError(t, db, cfg, "validation_error", "task", "create", "--file", file)
	writeFile(t, plain, string([]byte{0xff}))
	runCLIExpectError(t, db, cfg, "validation_error", "task", "create", "--file", plain)
	if got := runCLI(t, db, cfg, "list"); got != beforeTasks {
		t.Fatal("rejected input changed tasks")
	}
	if got := runCLI(t, db, cfg, "plan", "list"); got != beforePlans {
		t.Fatal("rejected input changed plans")
	}
}
