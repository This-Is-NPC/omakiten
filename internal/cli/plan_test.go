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
