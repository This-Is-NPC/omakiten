package agentsetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	toml "github.com/pelletier/go-toml/v2"

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
	configPath := filepath.Join(tmp, ".mcp.json")
	existing := []byte(`{"other":{"command":"other"}}`)
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
	if _, ok := written["other"]; !ok {
		t.Fatalf("other missing after setup: %#v", written)
	}
	if _, ok := written["omakiten"]; !ok {
		t.Fatalf("omakiten missing after setup: %#v", written)
	}

	_, err = Setup(Options{ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"omakiten":{"command":"old"}}`), 0o644); err != nil {
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
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	omakiten := written["omakiten"].(map[string]any)
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
	want := []string{ClaudeCodeHarness, ClaudeDesktopHarness, OpenCodeHarness, CrushHarness, GitHubCopilotHarness, CodexHarness, CursorHarness}
	got := SupportedHarnesses()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("SupportedHarnesses() mismatch (-want +got):\n%s", diff)
	}
}

func TestSetupDefaultHarnessAndCommand(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	result, err := Setup(Options{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Harness != ClaudeCodeHarness {
		t.Fatalf("Harness = %q, want %q", result.Harness, ClaudeCodeHarness)
	}
	if result.Command == "" {
		t.Fatal("Command is empty")
	}
	if len(result.Args) != 2 || result.Args[0] != "mcp" || result.Args[1] != "serve" {
		t.Fatalf("Args = %v, want [mcp serve]", result.Args)
	}
}

func TestReadConfigEdgeCases(t *testing.T) {
	codec := jsonCodec{}
	// Empty file
	emptyPath := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(emptyPath, []byte("   "), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, exists, err := readConfig(emptyPath, codec)
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
	_, _, err = readConfig(invalidPath, codec)
	if err == nil {
		t.Fatal("readConfig(invalid) error = nil")
	}

	// Null JSON
	nullPath := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(nullPath, []byte("null"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, exists, err = readConfig(nullPath, codec)
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

// OpenCode harness tests

func TestSetupOpenCodeDryRunDoesNotWriteConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "opencode.json")

	result, err := Setup(Options{Harness: OpenCodeHarness, ConfigPath: configPath, Command: "okt", DryRun: true})
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

func TestSetupOpenCodePreservesExistingConfigAndRefusesSilentOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	existing := []byte(`{"autoupdate":false,"mcp":{"other":{"type":"local","command":["other"]}}}`)
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: OpenCodeHarness, ConfigPath: configPath, Command: "okt"})
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
	if written["autoupdate"] != false {
		t.Fatalf("autoupdate = %v, want preserved false", written["autoupdate"])
	}
	mcpSection := written["mcp"].(map[string]any)
	if _, ok := mcpSection["other"]; !ok {
		t.Fatalf("mcp.other missing after setup: %#v", mcpSection)
	}
	if _, ok := mcpSection["omakiten"]; !ok {
		t.Fatalf("mcp.omakiten missing after setup: %#v", mcpSection)
	}

	_, err = Setup(Options{Harness: OpenCodeHarness, ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupOpenCodeForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"mcp":{"omakiten":{"type":"local","command":["old"]}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: OpenCodeHarness, ConfigPath: configPath, Command: "new-okt", Force: true})
	if err != nil {
		t.Fatalf("Setup(force) error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup(force) = %#v, want updated changed", result)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	mcpSection := written["mcp"].(map[string]any)
	omakiten := mcpSection["omakiten"].(map[string]any)
	cmd := omakiten["command"].([]any)
	if len(cmd) != 3 || cmd[0] != "new-okt" {
		t.Fatalf("command = %v, want [new-okt mcp serve]", cmd)
	}
}

func TestSetupOpenCodeCreatedStatus(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "opencode.json")

	result, err := Setup(Options{Harness: OpenCodeHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Status != "created" || !result.Changed {
		t.Fatalf("Setup() = %#v, want created changed", result)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	mcpSection := written["mcp"].(map[string]any)
	omakiten := mcpSection["omakiten"].(map[string]any)
	if omakiten["type"] != "local" {
		t.Fatalf("type = %v, want local", omakiten["type"])
	}
	if omakiten["enabled"] != true {
		t.Fatalf("enabled = %v, want true", omakiten["enabled"])
	}
}

func TestSetupClaudeCodeDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Setup(Options{Harness: ClaudeCodeHarness, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Harness != ClaudeCodeHarness {
		t.Fatalf("Harness = %q, want %q", result.Harness, ClaudeCodeHarness)
	}
	expected := filepath.Join(home, ".claude", ".mcp.json")
	if result.ConfigPath != expected {
		t.Fatalf("ConfigPath = %q, want %q", result.ConfigPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("default config file missing: %v", err)
	}

	data, _ := os.ReadFile(expected)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := written["omakiten"]; !ok {
		t.Fatalf("omakiten missing: %#v", written)
	}
}

func TestSetupOpenCodeDefaultConfigPath(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HOME", configDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	configPath := filepath.Join(configDir, "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	result, err := Setup(Options{Harness: OpenCodeHarness, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Harness != OpenCodeHarness {
		t.Fatalf("Harness = %q, want %q", result.Harness, OpenCodeHarness)
	}
	if result.Status != "created" || !result.Changed {
		t.Fatalf("Setup() = %#v, want created changed", result)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("default config file missing: %v", err)
	}
}

// Crush harness tests

func TestSetupCrushPreservesExistingConfigAndRefusesSilentOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "crush", "crush.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	existing := []byte(`{"providers":{"openai":{"apiKey":"sk-x"}},"mcp":{"other":{"type":"stdio","command":"other"}}}`)
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: CrushHarness, ConfigPath: configPath, Command: "okt"})
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
	providers, ok := written["providers"].(map[string]any)
	if !ok || providers["openai"] == nil {
		t.Fatalf("providers.openai missing after setup: %#v", written)
	}
	mcpSection := written["mcp"].(map[string]any)
	if _, ok := mcpSection["other"]; !ok {
		t.Fatalf("mcp.other missing after setup: %#v", mcpSection)
	}
	omakiten, ok := mcpSection["omakiten"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.omakiten missing after setup: %#v", mcpSection)
	}
	if omakiten["type"] != "stdio" {
		t.Fatalf("type = %v, want stdio", omakiten["type"])
	}
	if omakiten["command"] != "okt" {
		t.Fatalf("command = %v, want okt", omakiten["command"])
	}
	args, ok := omakiten["args"].([]any)
	if !ok || len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Fatalf("args = %v, want [mcp serve]", omakiten["args"])
	}

	_, err = Setup(Options{Harness: CrushHarness, ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupCrushForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "crush.json")
	if err := os.WriteFile(configPath, []byte(`{"mcp":{"omakiten":{"type":"stdio","command":"old","args":["mcp","serve"]}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: CrushHarness, ConfigPath: configPath, Command: "new-okt", Force: true})
	if err != nil {
		t.Fatalf("Setup(force) error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup(force) = %#v, want updated changed", result)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	omakiten := written["mcp"].(map[string]any)["omakiten"].(map[string]any)
	if omakiten["command"] != "new-okt" {
		t.Fatalf("command = %v, want new-okt", omakiten["command"])
	}
}

func TestSetupCrushDefaultConfigPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("crush default path on Windows uses LOCALAPPDATA which the test rig does not stub")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Setup(Options{Harness: CrushHarness, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	expected := filepath.Join(home, ".config", "crush", "crush.json")
	if result.ConfigPath != expected {
		t.Fatalf("ConfigPath = %q, want %q", result.ConfigPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("default config file missing: %v", err)
	}
}

// GitHub Copilot harness tests

func TestSetupGitHubCopilotPreservesExistingConfigAndRefusesSilentOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "Code", "User", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	existing := []byte(`{"inputs":[{"id":"foo"}],"servers":{"other":{"type":"stdio","command":"other"}}}`)
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: GitHubCopilotHarness, ConfigPath: configPath, Command: "okt"})
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
	inputs, ok := written["inputs"].([]any)
	if !ok || len(inputs) != 1 {
		t.Fatalf("inputs missing or mutated: %#v", written["inputs"])
	}
	servers := written["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("servers.other missing after setup: %#v", servers)
	}
	omakiten, ok := servers["omakiten"].(map[string]any)
	if !ok {
		t.Fatalf("servers.omakiten missing after setup: %#v", servers)
	}
	if omakiten["type"] != "stdio" {
		t.Fatalf("type = %v, want stdio", omakiten["type"])
	}

	_, err = Setup(Options{Harness: GitHubCopilotHarness, ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupGitHubCopilotForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"servers":{"omakiten":{"type":"stdio","command":"old","args":["mcp","serve"]}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := Setup(Options{Harness: GitHubCopilotHarness, ConfigPath: configPath, Command: "new-okt", Force: true}); err != nil {
		t.Fatalf("Setup(force) error = %v", err)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	omakiten := written["servers"].(map[string]any)["omakiten"].(map[string]any)
	if omakiten["command"] != "new-okt" {
		t.Fatalf("command = %v, want new-okt", omakiten["command"])
	}
	if _, ok := written["mcpServers"]; ok {
		t.Fatalf("Copilot config must not introduce mcpServers key: %#v", written)
	}
}

func TestSetupGitHubCopilotDefaultConfigPath(t *testing.T) {
	configDir := t.TempDir()
	switch runtime.GOOS {
	case "linux":
		t.Setenv("XDG_CONFIG_HOME", configDir)
	case "darwin":
		t.Setenv("HOME", configDir)
		configDir = filepath.Join(configDir, "Library", "Application Support")
	case "windows":
		t.Setenv("AppData", configDir)
	default:
		t.Skipf("os.UserConfigDir behavior not stubbed for GOOS=%s", runtime.GOOS)
	}

	result, err := Setup(Options{Harness: GitHubCopilotHarness, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	expected := filepath.Join(configDir, "Code", "User", "mcp.json")
	if result.ConfigPath != expected {
		t.Fatalf("ConfigPath = %q, want %q", result.ConfigPath, expected)
	}
}

// Codex harness tests (TOML)

func TestSetupCodexCreatesTOMLConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")

	result, err := Setup(Options{Harness: CodexHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Status != "created" || !result.Changed {
		t.Fatalf("Setup() = %#v, want created changed", result)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var written map[string]any
	if err := toml.Unmarshal(data, &written); err != nil {
		t.Fatalf("toml.Unmarshal() error = %v\nfile:\n%s", err, string(data))
	}
	servers, ok := written["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers missing or wrong type: %#v", written)
	}
	omakiten, ok := servers["omakiten"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers.omakiten missing: %#v", servers)
	}
	if omakiten["command"] != "okt" {
		t.Fatalf("command = %v, want okt", omakiten["command"])
	}
	args, ok := omakiten["args"].([]any)
	if !ok || len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Fatalf("args = %v, want [mcp serve]", omakiten["args"])
	}
}

func TestSetupCodexPreservesExistingTOMLAndRefusesSilentOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	existing := []byte(`model = "gpt-5"
approval_policy = "manual"

[mcp_servers.other]
command = "uvx"
args = ["other-tool"]
`)
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: CodexHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup() = %#v, want updated changed", result)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := toml.Unmarshal(data, &written); err != nil {
		t.Fatalf("toml.Unmarshal() error = %v\nfile:\n%s", err, string(data))
	}
	if written["model"] != "gpt-5" {
		t.Fatalf("model = %v, want preserved gpt-5", written["model"])
	}
	if written["approval_policy"] != "manual" {
		t.Fatalf("approval_policy = %v, want preserved manual", written["approval_policy"])
	}
	servers := written["mcp_servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("mcp_servers.other missing after setup: %#v", servers)
	}
	if _, ok := servers["omakiten"]; !ok {
		t.Fatalf("mcp_servers.omakiten missing after setup: %#v", servers)
	}

	_, err = Setup(Options{Harness: CodexHarness, ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupCodexForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(configPath, []byte("[mcp_servers.omakiten]\ncommand = \"old\"\nargs = [\"mcp\", \"serve\"]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := Setup(Options{Harness: CodexHarness, ConfigPath: configPath, Command: "new-okt", Force: true}); err != nil {
		t.Fatalf("Setup(force) error = %v", err)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := toml.Unmarshal(data, &written); err != nil {
		t.Fatalf("toml.Unmarshal() error = %v\nfile:\n%s", err, string(data))
	}
	omakiten := written["mcp_servers"].(map[string]any)["omakiten"].(map[string]any)
	if omakiten["command"] != "new-okt" {
		t.Fatalf("command = %v, want new-okt", omakiten["command"])
	}
}

func TestSetupCodexDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Setup(Options{Harness: CodexHarness, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	expected := filepath.Join(home, ".codex", "config.toml")
	if result.ConfigPath != expected {
		t.Fatalf("ConfigPath = %q, want %q", result.ConfigPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("default config file missing: %v", err)
	}
}

func TestSetupCodexInvalidTOMLReturnsValidationError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("this = is = not valid toml"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Setup(Options{Harness: CodexHarness, ConfigPath: configPath, Command: "okt"})
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

// Cursor harness tests

func TestSetupCursorDefaultConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	result, err := Setup(Options{Harness: CursorHarness, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	expected := filepath.Join(home, ".cursor", "mcp.json")
	if result.ConfigPath != expected {
		t.Fatalf("ConfigPath = %q, want %q", result.ConfigPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("default config file missing: %v", err)
	}

	data, _ := os.ReadFile(expected)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	servers, ok := written["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or wrong type: %#v", written)
	}
	if _, ok := servers["omakiten"]; !ok {
		t.Fatalf("mcpServers.omakiten missing: %#v", servers)
	}
}

func TestSetupCursorPreservesExistingConfigAndRefusesSilentOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "mcp.json")
	existing := []byte(`{"mcpServers":{"other":{"command":"other","args":[]}}}`)
	if err := os.WriteFile(configPath, existing, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: CursorHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup() = %#v, want updated changed", result)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	servers := written["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("mcpServers.other missing after setup: %#v", servers)
	}
	if _, ok := servers["omakiten"]; !ok {
		t.Fatalf("mcpServers.omakiten missing after setup: %#v", servers)
	}

	_, err = Setup(Options{Harness: CursorHarness, ConfigPath: configPath, Command: "okt"})
	assertSetupCode(t, err, domain.ErrValidation)
}

func TestSetupCursorForceOverwrite(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{"omakiten":{"command":"old","args":["mcp","serve"]}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Setup(Options{Harness: CursorHarness, ConfigPath: configPath, Command: "new-okt", Force: true})
	if err != nil {
		t.Fatalf("Setup(force) error = %v", err)
	}
	if result.Status != "updated" || !result.Changed {
		t.Fatalf("Setup(force) = %#v, want updated changed", result)
	}

	data, _ := os.ReadFile(configPath)
	var written map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	omakiten := written["mcpServers"].(map[string]any)["omakiten"].(map[string]any)
	if omakiten["command"] != "new-okt" {
		t.Fatalf("command = %v, want new-okt", omakiten["command"])
	}
}

func TestSetupCursorCreatedStatus(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")

	result, err := Setup(Options{Harness: CursorHarness, ConfigPath: configPath, Command: "okt"})
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
