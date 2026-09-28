package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigFailurePreventsWorkOperations(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
	runCLI(t, db, cfg, "task", "create", "--title", "Keep task", "--confirm")
	runCLI(t, db, cfg, "plan", "create", "keep-plan", "--name", "Keep plan")
	original := readFile(t, cfg)
	beforeTasks, beforePlans := runCLI(t, db, cfg, "list"), runCLI(t, db, cfg, "plan", "list")
	writeFile(t, cfg, "version: 999\n")
	cases := map[string][]string{
		"task create":           {"task", "create", "--title", "Must not exist", "--confirm"},
		"task edit":             {"edit", "1", "--title", "Must not change"},
		"task move":             {"move", "1", "--to", "dev"},
		"task assign":           {"assign", "1", "agent"},
		"task archive":          {"archive", "1"},
		"task unarchive":        {"unarchive", "1"},
		"task delete":           {"delete", "1", "--confirm"},
		"comment add":           {"comment", "add", "1", "--body", "Must not exist"},
		"dependency add":        {"depend", "add", "1", "--on", "2"},
		"plan create":           {"plan", "create", "new-plan", "--name", "Must not exist"},
		"plan edit":             {"plan", "edit", "keep-plan", "--name", "Must not change"},
		"plan delete":           {"plan", "delete", "keep-plan", "--confirm"},
		"wave add":              {"plan", "wave-add", "keep-plan", "Must not exist"},
		"wave rename":           {"plan", "wave-rename", "1", "Must not change"},
		"wave remove":           {"plan", "wave-remove", "1", "--confirm"},
		"wave reorder":          {"plan", "wave-reorder", "1", "2"},
		"plan assign":           {"plan", "assign", "keep-plan", "1", "1"},
		"plan unassign":         {"plan", "unassign", "1"},
		"plan claim":            {"plan", "claim", "keep-plan"},
		"law add":               {"law", "add", "--key", "new-law", "--no-edit"},
		"law edit":              {"law", "edit", "conventional-commits", "--name", "Must not change", "--no-edit"},
		"law remove":            {"law", "remove", "conventional-commits"},
		"skill add":             {"skill", "add", "--name", "Must not exist", "--no-edit"},
		"skill edit":            {"skill", "edit", "implementation", "--name", "Must not change", "--no-edit"},
		"skill remove":          {"skill", "remove", "implementation"},
		"persona add":           {"persona", "add", "--name", "Must not exist", "--no-edit"},
		"persona edit":          {"persona", "edit", "third-hokage", "--name", "Must not change", "--no-edit"},
		"persona remove":        {"persona", "remove", "third-hokage"},
		"progress record":       {"progress", "record", "--task-id", "1", "--title", "Must not change"},
		"project delete":        {"projects", "delete", "example", "--yes"},
		"task continue":         {"task", "continue", "1"},
		"plan continue":         {"plan", "continue", "keep-plan"},
		"plan show":             {"plan", "show", "keep-plan"},
		"law show":              {"law", "show", "conventional-commits"},
		"law list":              {"law", "list"},
		"skill show":            {"skill", "show", "implementation"},
		"skill list":            {"skill", "list"},
		"persona show":          {"persona", "show", "third-hokage"},
		"persona list":          {"persona", "list"},
		"project resume":        {"project", "resume"},
		"config language show":  {"config", "language", "show"},
		"config language set":   {"config", "language", "set", "--tui", "en"},
		"config language reset": {"config", "language", "reset"},
		"task list":             {"list"},
		"task search":           {"search", "Keep"},
		"event logs":            {"logs"},
		"workflow show":         {"workflow", "show"},
		"workflow orphans":      {"workflow", "orphans"},
		"tag inventory":         {"tag", "list-all"},
		"template list":         {"template", "list"},
		"template show":         {"template", "show", "task"},
		"command list":          {"command", "list"},
		"command resolve":       {"command", "resolve", "okt-task-implement"},
		"project inventory":     {"projects", "list"},
		"metrics":               {"metrics", "summary"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			envelope := runCLIExpectError(t, db, cfg, "config_invalid", args...)
			details := envelope["details"].(map[string]any)
			if details["path"] != cfg || strings.Contains(envelope["msg"].(string), "%!") {
				t.Fatalf("config failure lacks actionable path: %v", envelope)
			}
			readBackEquals(t, cfg, "version: 999\n")
		})
	}
	writeFile(t, cfg, original)
	if after := runCLI(t, db, cfg, "list"); after != beforeTasks {
		t.Fatal("config failure changed tasks")
	}
	if after := runCLI(t, db, cfg, "plan", "list"); after != beforePlans {
		t.Fatal("config failure changed plans")
	}
}
