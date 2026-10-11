package cli

import (
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/paths"
)

func TestCLIProjectRelocateAfterDirectoryMove(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(paths.HomeEnv, filepath.Join(tmp, "home"))
	db := filepath.Join(tmp, "db")
	old := filepath.Join(tmp, "old")
	root := filepath.Join(tmp, "new")
	if err := os.Mkdir(old, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(old)
	runCLI(t, db, "", "init", "--name", "Project", "--slug", "project")
	runCLI(t, db, "", "task", "create", "--title", "Preserved task")
	if err := os.Rename(old, root); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	runCLIExpectError(t, db, "", "validation_error", "project", "relocate", "--root", root)
	result := decodeEnvelope(t, runCLI(t, db, "", "--project", "project", "project", "relocate", "--root", root))["data"].(map[string]any)
	if result["previous_root_path"] != old {
		t.Fatalf("previous root: %+v", result)
	}
	project := result["project"].(map[string]any)
	if project["root_path"] != root || project["slug"] != "project" {
		t.Fatalf("project: %+v", project)
	}
	overview := decodeEnvelope(t, runCLI(t, db, "", "project", "overview"))["data"].(map[string]any)
	if overview["pending_count"] != float64(1) {
		t.Fatalf("lost task: %+v", overview)
	}
}
