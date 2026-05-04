package agentsetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/domain"
)

func TestSetupDryRunDoesNotWriteConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "claude_desktop_config.json")

	result, err := Setup(Options{ConfigPath: configPath, Command: "okt", DryRun: true})
	if err != nil {
		t.Fatalf("Setup(dry-run) error = %v", err)
	}
	if !result.DryRun || result.Status != "would_write" || !result.Changed {
		t.Fatalf("Setup(dry-run) = %#v, want would_write changed dry run", result)
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config file exists after dry run or unexpected error: %v", err)
	}
}

func TestSetupPreservesExistingConfigAndRefusesSilentOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	existing := []byte(`{"theme":"dark","mcpServers":{"other":{"command":"other"}}}`)
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup() = %#v, want updated changed", result)
	}

	var written map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if written["theme"] != "dark" {
		t.Fatalf("theme = %v, want preserved dark", written["theme"])
	}
	servers := written["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("mcpServers.other missing after setup: %#v", servers)
	}
	if _, ok := servers["omakiten"]; !ok {
		t.Fatalf("mcpServers.omakiten missing after setup: %#v", servers)
	}

	_, err = Setup(Options{ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"omakiten":{"command":"old"}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{ConfigPath: configPath, Command: "new-okt", Force: true})
	if err != nil {
		t.Fatalf("Setup(force) error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup(force) = %#v, want updated changed", result)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	json.Unmarshal(data, &written)
	servers := written["mcpServers"].(map[string]any)
	omakiten := servers["omakiten"].(map[string]any)
	if omakiten["command"] != "new-okt" {
		t.Fatalf("command = %v, want new-okt", omakiten["command"])
	}
}

func TestSetupCreatedStatus(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "new_config.json")

	result, err := Setup(Options{ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Status != "created" || !result.Changed {
		t.Fatalf("Setup() = %#v, want created changed", result)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
}

func TestSupportedHarnesses(t *testing.T) {
	harnesses := SupportedHarnesses()
	if len(harnesses) != 1 || harnesses[0] != ClaudeDesktopHarness {
		t.Fatalf("SupportedHarnesses() = %v, want [claude-desktop]", harnesses)
	}
}

func TestSetupDefaultHarnessAndCommand(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	result, err := Setup(Options{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Harness != ClaudeDesktopHarness {
		t.Fatalf("Harness = %q, want %q", result.Harness, ClaudeDesktopHarness)
	}
	if result.Command == "" {
		t.Fatal("Command is empty")
	}
	if len(result.Args) != 2 || result.Args[0] != "mcp" || result.Args[1] != "serve" {
		t.Fatalf("Args = %v, want [mcp serve]", result.Args)
	}
}

func TestReadConfigEdgeCases(t *testing.T) {
	// Empty file
	emptyPath := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(emptyPath, []byte("   "), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, exists, err := readConfig(emptyPath)
	if err != nil {
		t.Fatalf("readConfig(empty) error = %v", err)
	}
	if !exists || len(cfg) != 0 {
		t.Fatalf("readConfig(empty) = (%v, %v), want (map, true)", cfg, exists)
	}

	// Invalid JSON
	invalidPath := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, _, err = readConfig(invalidPath)
	if err == nil {
		t.Fatal("readConfig(invalid) error = nil")
	}

	// Null JSON
	nullPath := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(nullPath, []byte("null"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, exists, err = readConfig(nullPath)
	if err != nil {
		t.Fatalf("readConfig(null) error = %v", err)
	}
	if !exists || len(cfg) != 0 {
		t.Fatalf("readConfig(null) = (%v, %v), want (map, true)", cfg, exists)
	}
}

func TestObjectFieldNotObject(t *testing.T) {
	_, err := objectField(map[string]any{"mcpServers": "string"}, "mcpServers")
	if err == nil {
		t.Fatal("objectField(string) error = nil")
	}
}

func TestSetupRejectsUnsupportedHarness(t *testing.T) {
	_, err := Setup(Options{Harness: "unknown", ConfigPath: filepath.Join(t.TempDir(), "config.json"), Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func assertSetupCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", code)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T %v, want CodedError", err, err)
	}
	if coded.Code != code {
		t.Fatalf("code = %q, want %q", coded.Code, code)
	}
}
