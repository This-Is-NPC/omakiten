package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLIErrorAndSolutionCommands(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	record := runCLI(t, dbPath, configPath, "error", "record",
		"--description", "nil pointer in renderer",
		"--context", "stack: boom",
		"--tag", "tui",
		"--tag", "panic",
	)
	if !strings.Contains(record, `"id"`) {
		t.Fatalf("error record missing id: %s", record)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(record), &env); err != nil {
		t.Fatalf("unmarshal record: %v (%s)", err, record)
	}
	data, _ := env["data"].(map[string]any)
	errorObj, _ := data["error"].(map[string]any)
	errorID, _ := errorObj["id"].(float64)
	if errorID == 0 {
		// some envelopes may flatten
		if id, ok := data["id"].(float64); ok {
			errorID = id
		}
	}
	if errorID == 0 {
		t.Fatalf("cannot find error id in %s", record)
	}

	add := runCLI(t, dbPath, configPath, "solution", "add",
		"--error-id", strconv.FormatInt(int64(errorID), 10),
		"--description", "guard nil before paint",
		"--steps", "if v == nil { return }",
	)
	var addEnv map[string]any
	if err := json.Unmarshal([]byte(add), &addEnv); err != nil {
		t.Fatalf("unmarshal add: %v (%s)", err, add)
	}
	addData, _ := addEnv["data"].(map[string]any)
	sol, _ := addData["solution"].(map[string]any)
	solID, _ := sol["id"].(float64)
	if solID == 0 {
		if id, ok := addData["id"].(float64); ok {
			solID = id
		}
	}
	if solID == 0 {
		t.Fatalf("cannot find solution id in %s", add)
	}

	confirm := runCLI(t, dbPath, configPath, "solution", "confirm",
		"--solution-id", strconv.FormatInt(int64(solID), 10),
		"--success=true",
	)
	if !strings.Contains(confirm, `"id"`) {
		t.Fatalf("confirm missing id: %s", confirm)
	}

	top := runCLI(t, dbPath, configPath, "solution", "list-top", "--limit", "5")
	if !strings.Contains(top, "guard nil") && !strings.Contains(top, `"solutions"`) && !strings.Contains(top, `"items"`) {
		t.Fatalf("list-top unexpected: %s", top)
	}
}
