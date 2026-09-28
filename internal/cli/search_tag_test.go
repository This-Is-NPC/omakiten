package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLISearch(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	addOut := runCLI(t, dbPath, configPath, "task", "create", "--confirm", "--title", "Searchable alpha task")
	if !strings.Contains(addOut, `"id"`) {
		t.Fatalf("add missing id: %s", addOut)
	}
	runCLI(t, dbPath, configPath, "db", "reindex", "--confirm")

	out := runCLI(t, dbPath, configPath, "search", "alpha", "--entity-type", "task")
	if !strings.Contains(out, `"hits"`) {
		t.Fatalf("search missing hits: %s", out)
	}
	if !strings.Contains(out, "alpha") && !strings.Contains(out, "Searchable") {
		t.Fatalf("search missed task content: %s", out)
	}
}

func TestCLITagCommands(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	addOut := runCLI(t, dbPath, configPath, "task", "create", "--confirm", "--title", "Tagged task")
	var addEnv map[string]any
	if err := json.Unmarshal([]byte(addOut), &addEnv); err != nil {
		t.Fatalf("Unmarshal add: %v (%s)", err, addOut)
	}
	idStr := taskIDFromAddOutput(t, addOut, addEnv)

	addTag := runCLI(t, dbPath, configPath, "tag", "add", "--entity-type", "task", "--entity-id", idStr, "--name", "Wave Two")
	if !strings.Contains(addTag, `"name":"wave-two"`) && !strings.Contains(addTag, `"name": "wave-two"`) {
		t.Fatalf("tag add missing normalized name: %s", addTag)
	}
	var tagEnv map[string]any
	if err := json.Unmarshal([]byte(addTag), &tagEnv); err != nil {
		t.Fatalf("Unmarshal tag add: %v", err)
	}
	tagData, _ := tagEnv["data"].(map[string]any)
	tag, _ := tagData["tag"].(map[string]any)
	tagID, _ := tag["id"].(float64)
	if tagID == 0 {
		t.Fatalf("tag id missing: %s", addTag)
	}
	tagIDStr := jsonNumber(tagID)

	listOut := runCLI(t, dbPath, configPath, "tag", "list", "--entity-type", "task", "--entity-id", idStr)
	if !strings.Contains(listOut, "wave-two") {
		t.Fatalf("tag list missing wave-two: %s", listOut)
	}

	allOut := runCLI(t, dbPath, configPath, "tag", "list-all")
	if !strings.Contains(allOut, "wave-two") {
		t.Fatalf("tag list-all missing wave-two: %s", allOut)
	}

	preview := runCLI(t, dbPath, configPath, "tag", "remove", "--entity-type", "task", "--entity-id", idStr, "--tag-id", tagIDStr)
	if !strings.Contains(preview, `"requires_confirmation":true`) && !strings.Contains(preview, `"requires_confirmation": true`) {
		t.Fatalf("tag remove without confirm should require confirmation: %s", preview)
	}

	removed := runCLI(t, dbPath, configPath, "tag", "remove", "--entity-type", "task", "--entity-id", idStr, "--tag-id", tagIDStr, "--confirm")
	if !strings.Contains(removed, `"removed":true`) && !strings.Contains(removed, `"removed": true`) {
		t.Fatalf("tag remove --confirm missing removed=true: %s", removed)
	}
}

func taskIDFromAddOutput(t *testing.T, raw string, envelope map[string]any) string {
	data, _ := envelope["data"].(map[string]any)
	task, _ := data["task"].(map[string]any)
	taskID, _ := task["id"].(float64)
	if taskID != 0 {
		return jsonNumber(taskID)
	}
	if !strings.Contains(raw, `"id":`) {
		t.Fatalf("cannot find task id in add output: %s", raw)
	}
	return extractFirstID(raw)
}

func jsonNumber(v float64) string {
	if v == float64(int64(v)) {
		return strings.TrimSpace(strings.ReplaceAll(strconv.FormatFloat(v, 'f', 0, 64), " ", ""))
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func extractFirstID(raw string) string {
	const key = `"id":`
	i := strings.Index(raw, key)
	if i < 0 {
		return ""
	}
	rest := raw[i+len(key):]
	rest = strings.TrimLeft(rest, " ")
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	return rest[:end]
}
