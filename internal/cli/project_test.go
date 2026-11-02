package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIProjectOverviewResumeEdit(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "task", "create", "--confirm", "--title", "Next task")

	overview := runCLI(t, dbPath, configPath, "project", "overview")
	if !strings.Contains(overview, `"pending_count"`) {
		t.Fatalf("overview missing pending_count: %s", overview)
	}
	if !strings.Contains(overview, `"next_step_prompt"`) {
		t.Fatalf("overview missing next_step_prompt: %s", overview)
	}

	resume := runCLI(t, dbPath, configPath, "project", "resume")
	if !strings.Contains(resume, `"likely_next_work"`) && !strings.Contains(resume, "likely_next_work") {
		t.Fatalf("resume missing likely_next_work: %s", resume)
	}

	edit := runCLI(t, dbPath, configPath, "project", "edit", "--description", "Facade-owned description")
	var env map[string]any
	if err := json.Unmarshal([]byte(edit), &env); err != nil {
		t.Fatalf("unmarshal edit: %v (%s)", err, edit)
	}
	data, _ := env["data"].(map[string]any)
	if data["description"] != "Facade-owned description" {
		t.Fatalf("edit description = %#v, want Facade-owned description (%s)", data["description"], edit)
	}

	overview2 := runCLI(t, dbPath, configPath, "project", "overview")
	// overview DTO may not echo description; edit response already asserted.
	if !strings.Contains(overview2, `"project"`) {
		t.Fatalf("overview after edit missing project: %s", overview2)
	}
}

func TestCLIProjectEditSyncScope(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	if overview := runCLI(t, dbPath, configPath, "project", "overview"); !strings.Contains(overview, `"sync_scope":"local"`) {
		t.Fatalf("new project is not local: %s", overview)
	}

	runCLI(t, dbPath, configPath, "project", "edit", "--sync-scope", "synced")
	if overview := runCLI(t, dbPath, configPath, "project", "overview"); !strings.Contains(overview, `"sync_scope":"synced"`) {
		t.Fatalf("overview does not show synced: %s", overview)
	}

	edit := runCLI(t, dbPath, configPath, "project", "edit", "--description", "only text")
	if !strings.Contains(edit, `"sync_scope":"synced"`) || !strings.Contains(edit, `"description":"only text"`) {
		t.Fatalf("description-only edit changed scope or lost text: %s", edit)
	}

	runCLIExpectError(t, dbPath, configPath, "validation_error", "project", "edit", "--sync-scope", "everywhere")
	runCLIExpectError(t, dbPath, configPath, "validation_error", "project", "edit")
}
