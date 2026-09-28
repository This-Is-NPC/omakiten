package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLIProgressTaskAndPlanContinue(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	addOut := runCLI(t, dbPath, configPath, "add", "--title", "Alpha renderer bug")
	taskID := extractFirstID(addOut)
	if taskID == "" {
		t.Fatalf("cannot find task id in add output: %s", addOut)
	}

	progress := runCLI(t, dbPath, configPath, "progress", "record",
		"--task-id", taskID,
		"--comment", "reproduced on main",
		"--author-type", "agent",
	)
	if !strings.Contains(progress, `"comment"`) {
		t.Fatalf("progress record missing comment: %s", progress)
	}

	cont := runCLI(t, dbPath, configPath, "task", "continue", taskID)
	if !strings.Contains(cont, `"next_step_prompt"`) {
		t.Fatalf("task continue missing next_step_prompt: %s", cont)
	}
	if !strings.Contains(cont, "Alpha renderer bug") {
		t.Fatalf("task continue missing title: %s", cont)
	}

	activity := runCLI(t, dbPath, configPath, "task", "activity", taskID)
	if !strings.Contains(activity, `"events"`) {
		t.Fatalf("task activity missing events: %s", activity)
	}

	// create-intent similarity gate: similar description without --confirm
	gate := runCLI(t, dbPath, configPath, "task", "create-intent",
		"--description", "Alpha renderer bug again",
	)
	if !strings.Contains(gate, `"requires_confirmation":true`) && !strings.Contains(gate, `"requires_confirmation": true`) {
		t.Fatalf("create-intent without confirm should gate on similar tasks: %s", gate)
	}

	created := runCLI(t, dbPath, configPath, "task", "create-intent",
		"--description", "Alpha renderer bug again",
		"--confirm",
	)
	if !strings.Contains(created, `"task"`) {
		t.Fatalf("create-intent --confirm missing task: %s", created)
	}

	runCLI(t, dbPath, configPath, "plan", "create", "wave-one", "--name", "Wave One")
	planCont := runCLI(t, dbPath, configPath, "plan", "continue", "wave-one")
	if !strings.Contains(planCont, `"percent"`) {
		t.Fatalf("plan continue missing percent: %s", planCont)
	}
	if !strings.Contains(planCont, `"plan"`) {
		t.Fatalf("plan continue missing plan: %s", planCont)
	}

	// sanity: JSON envelope still parses
	var env map[string]any
	if err := json.Unmarshal([]byte(cont), &env); err != nil {
		t.Fatalf("continue envelope not JSON: %v", err)
	}
	_ = strconv.Itoa(1)
}
