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
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "First")
	runCLI(t, dbPath, configPath, "add", "-t", "Second")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "Remember this")
	runCLI(t, dbPath, configPath, "comment", "add", "1", "-b", "feat/test", "--tag", "self-branch")
	runCLI(t, dbPath, configPath, "depend", "add", "2", "-i", "1")
	runCLI(t, dbPath, configPath, "context", "add", "-b", "handoff note")

	movedShort := runCLI(t, dbPath, configPath, "move", "1", "-t", "dev")
	if !strings.Contains(movedShort, `"bucket_key":"dev"`) {
		t.Fatalf("move short flag output = %s, want dev bucket", movedShort)
	}
	movedLong := runCLI(t, dbPath, configPath, "move", "1", "--to", "dev")
	if !strings.Contains(movedLong, `"bucket_key":"dev"`) {
		t.Fatalf("move long flag output = %s, want dev bucket", movedLong)
	}

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

	shortDump := runCLI(t, dbPath, configPath, "context", "dump", "-l", "3")
	for _, want := range []string{`"context_entries"`, `"dependencies"`, `"comments"`, `"laws"`, `"token_metrics"`} {
		if !strings.Contains(shortDump, want) {
			t.Fatalf("context dump short flag output missing %s: %s", want, shortDump)
		}
	}
}

func TestCLIInitCanPreviewMCPSetup(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	mcpConfig := filepath.Join(tmp, "claude_desktop_config.json")
	output := runCLI(t, dbPath, configPath,
		"init",
		"--name", "Project",
		"--slug", "project",
		"--enable-mcp",
		"--mcp-dry-run",
		"--mcp-config", mcpConfig,
		"--mcp-command", "okt",
	)
	if !strings.Contains(output, `"agent_setup"`) || !strings.Contains(output, `"status":"would_write"`) {
		t.Fatalf("init --enable-mcp dry-run output = %s, want agent setup preview", output)
	}
	if _, err := os.Stat(mcpConfig); !os.IsNotExist(err) {
		t.Fatalf("MCP config exists after dry run or unexpected error: %v", err)
	}
}

func TestCLICodedErrorsForAgentRecovery(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "-t", "First")
	runCLI(t, dbPath, configPath, "add", "-t", "Second")

	t.Run("workflow blocked transition returns coded error", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "workflow_invalid_transition",
			"move", "1", "-t", "done")
	})

	t.Run("dependency self loop returns coded error", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "dependency_invalid",
			"depend", "add", "1", "-i", "1")
	})

	t.Run("dependency cycle returns coded error", func(t *testing.T) {
		runCLI(t, dbPath, configPath, "depend", "add", "2", "-i", "1")
		runCLIExpectError(t, dbPath, configPath, "dependency_invalid",
			"depend", "add", "1", "-i", "2")
	})

	t.Run("missing task returns coded error", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "task_not_found",
			"comment", "add", "9999", "-b", "ghost")
	})

	t.Run("missing project returns coded error", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "project_not_found",
			"--project", "ghost-slug",
			"add", "-t", "x")
	})

	t.Run("validation rejects non-numeric task id", func(t *testing.T) {
		runCLIExpectError(t, dbPath, configPath, "validation_error",
			"comment", "add", "not-a-number", "-b", "x")
	})
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

func runCLIExpectError(t *testing.T, dbPath, configPath, wantCode string, args ...string) map[string]any {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	fullArgs := append([]string{"--db", dbPath, "--config", configPath}, args...)
	cmd.SetArgs(fullArgs)
	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute(%v) error = nil, want failure (%s); output = %s", fullArgs, wantCode, out.String())
	}
	trimmed := strings.TrimSpace(out.String())
	if strings.Count(trimmed, "\n") != 0 {
		t.Fatalf("Execute(%v) output is not one line: %q", fullArgs, trimmed)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(%v) error = %v, output = %s", fullArgs, err, trimmed)
	}
	if envelope["ok"] != false {
		t.Fatalf("Execute(%v) ok = %v, want false; output = %s", fullArgs, envelope["ok"], trimmed)
	}
	if envelope["code"] != wantCode {
		t.Fatalf("Execute(%v) code = %v, want %s; output = %s", fullArgs, envelope["code"], wantCode, trimmed)
	}
	return envelope
}
