package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPlaybookResolution(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "state.db")
	profile := filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, profile, "init", "--name", "Example", "--slug", "example", "--root", root)
	projects := decodeEnvelope(t, runCLI(t, db, profile, "projects", "list"))
	registered := projects["data"].(map[string]any)["projects"].([]any)
	if len(registered) != 1 || registered[0].(map[string]any)["slug"] != "example" {
		t.Fatalf("project discovery: %v", registered)
	}
	list := decodeEnvelope(t, runCLI(t, db, profile, "command", "list"))
	data := list["data"].(map[string]any)
	if len(data["commands"].([]any)) == 0 {
		t.Fatal("no playbooks discovered")
	}
	resolved := decodeEnvelope(t, runCLI(t, db, profile, "command", "resolve", "okt-task-implement", "--arguments", `{"task_id":42}`))
	body := resolved["data"].(map[string]any)
	markdown := body["markdown"].(string)
	if !strings.Contains(markdown, "## Skills") || !strings.Contains(markdown, "`task_id`: 42") {
		t.Fatalf("missing context: %s", markdown)
	}
	runCLIExpectError(t, db, profile, "validation_error", "command", "resolve", "okt-task-implement", "--arguments", "[]")
	runCLIExpectError(t, db, profile, "validation_error", "command", "resolve", "unknown")
}

func TestCLIInitPublishesLocalSkill(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "state.db")
	profile := filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, profile, "init", "--name", "Example", "--slug", "example", "--root", root, "--skill", "--claude-code")
	for _, directory := range []string{".agents", ".claude"} {
		path := filepath.Join(root, directory, "skills", "omakiten", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "name: omakiten") {
			t.Fatalf("skill missing at %s: %v", path, err)
		}
	}
}
