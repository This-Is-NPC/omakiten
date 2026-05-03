package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIOperationalCommands(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "First")
	runCLI(t, dbPath, configPath, "add", "-t", "Second")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "Remember this")
	runCLI(t, dbPath, configPath, "depend", "add", "2", "-i", "1")
	runCLI(t, dbPath, configPath, "context", "add", "-b", "handoff note")

	workflow := runCLI(t, dbPath, configPath, "workflow", "show")
	if !strings.Contains(workflow, `"workflow"`) || !strings.Contains(workflow, `"transitions"`) {
		t.Fatalf("workflow show output = %s, want workflow with transitions", workflow)
	}

	dump := runCLI(t, dbPath, configPath, "context", "dump", "--level", "3")
	for _, want := range []string{`"context_entries"`, `"dependencies"`, `"comments"`, `"laws"`, `"token_metrics"`} {
		if !strings.Contains(dump, want) {
			t.Fatalf("context dump output missing %s: %s", want, dump)
		}
	}
}

func TestCLIConfigInvalidUsesCodedError(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte("version: 2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"config", "validate", configPath})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want coded failure")
	}

	var envelope map[string]any
	if jsonErr := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &envelope); jsonErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, output = %s", jsonErr, out.String())
	}
	if envelope["code"] != string("config_invalid") {
		t.Fatalf("code = %v, want config_invalid", envelope["code"])
	}
}

func runCLI(t *testing.T, dbPath, configPath string, args ...string) string {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	fullArgs := append([]string{"--db", dbPath, "--config", configPath}, args...)
	cmd.SetArgs(fullArgs)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v, output = %s", fullArgs, err, out.String())
	}
	trimmed := strings.TrimSpace(out.String())
	if strings.Count(trimmed, "\n") != 0 {
		t.Fatalf("Execute(%v) output is not one line: %q", fullArgs, trimmed)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(%v) error = %v, output = %s", fullArgs, err, trimmed)
	}
	if envelope["ok"] != true {
		t.Fatalf("Execute(%v) ok = %v, output = %s", fullArgs, envelope["ok"], trimmed)
	}
	return trimmed
}
