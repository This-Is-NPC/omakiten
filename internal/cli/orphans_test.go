package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIWorkflowOrphans_NoOpWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "First")

	out := runCLI(t, dbPath, configPath, "workflow", "orphans")
	if !strings.Contains(out, `"applied":false`) {
		t.Fatalf("expected applied=false on no-op, got %s", out)
	}
	if !strings.Contains(out, `"total":0`) {
		t.Fatalf("expected total=0 on no-op, got %s", out)
	}
	if strings.Contains(out, `"requires_confirmation":true`) {
		t.Fatalf("empty orphan report must not require confirmation: %s", out)
	}
}

func TestCLIWorkflowShow_ThroughOperation(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	out := runCLI(t, dbPath, configPath, "workflow", "show")
	if !strings.Contains(out, `"workflow"`) || !strings.Contains(out, `"transitions"`) {
		t.Fatalf("workflow show output = %s, want workflow with transitions", out)
	}
	if !strings.Contains(out, `"project"`) {
		t.Fatalf("workflow show via operation.ShowWorkflow should include project summary: %s", out)
	}
	if !strings.Contains(out, `"buckets"`) {
		t.Fatalf("workflow show missing buckets: %s", out)
	}
}
