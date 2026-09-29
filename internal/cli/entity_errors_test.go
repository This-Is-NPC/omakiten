package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIEntityErrorsPreserveTheExistingDefinitions(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
	for _, entity := range []string{"law", "skill", "persona"} {
		t.Run(entity, func(t *testing.T) {
			checkMissingEntityOperations(t, db, cfg, entity)
		})
	}
	runCLIExpectError(t, db, cfg, "validation_error", "law", "add", "--key", "invalid-scope", "--scope", "other", "--no-edit")
	runCLIExpectError(t, db, cfg, "validation_error", "law", "edit", "conventional-commits", "--severity", "fatal", "--no-edit")
	runCLIExpectError(t, db, cfg, "skill_not_found", "persona", "add", "--name", "Missing skill", "--skill-slug", "absent", "--no-edit")

	for name, editor := range map[string]string{"missing binary": filepath.Join(root, "missing-editor"), "failed process": filepath.Join(root, "failed-editor")} {
		t.Run(name, func(t *testing.T) {
			checkFailedEditor(t, db, cfg, name, editor)
		})
	}
}

func checkMissingEntityOperations(t *testing.T, db, cfg, entity string) {
	t.Helper()
	before := runCLI(t, db, cfg, entity, "list")
	for _, command := range []string{"show", "edit", "remove"} {
		args := []string{entity, command, "absent"}
		if command == "edit" {
			args = append(args, "--name", "Changed", "--no-edit")
		}
		runCLIExpectError(t, db, cfg, entity+"_not_found", args...)
	}
	if after := runCLI(t, db, cfg, entity, "list"); after != before {
		t.Fatalf("failed operation modified %s definitions", entity)
	}
}

func checkFailedEditor(t *testing.T, db, cfg, name, editor string) {
	t.Helper()
	if name == "failed process" {
		if _, err := os.Stat("/bin/sh"); err != nil {
			t.Skip("POSIX editor executable")
		}
		writeFile(t, editor, "#!/bin/sh\nexit 7\n")
		if err := os.Chmod(editor, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("EDITOR", editor)
	t.Setenv("VISUAL", "")
	before := runCLI(t, db, cfg, "skill", "show", "implementation")
	runCLIExpectError(t, db, cfg, "editor_failed", "skill", "edit", "implementation")
	out := runCLI(t, db, cfg, "skill", "show", "implementation")
	if out != before {
		t.Fatal("editor failure lost the entity")
	}
}
