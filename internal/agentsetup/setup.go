package agentsetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

const (
	ClaudeCodeHarness    = "claude-code"
	ClaudeDesktopHarness = "claude-desktop"
	OpenCodeHarness      = "opencode"
	CrushHarness         = "crush"
	GitHubCopilotHarness = "github-copilot"
	CodexHarness         = "codex"
	CursorHarness        = "cursor"
)

type Options struct {
	Harness    string
	ConfigPath string
	Command    string
	DryRun     bool
	Force      bool
}

type Result struct {
	Harness    string   `json:"harness"`
	ConfigPath string   `json:"config_path"`
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	DryRun     bool     `json:"dry_run,omitempty"`
	Changed    bool     `json:"changed"`
	Status     string   `json:"status"`
	Message    string   `json:"message"`
}

func SupportedHarnesses() []string {
	return []string{ClaudeCodeHarness, ClaudeDesktopHarness, OpenCodeHarness, CrushHarness, GitHubCopilotHarness, CodexHarness, CursorHarness}
}

func Setup(opts Options) (Result, error) {
	harness := strings.TrimSpace(opts.Harness)
	if harness == "" {
		harness = ClaudeCodeHarness
	}
	if !isSupportedHarness(harness) {
		return Result{}, domain.NewError(domain.ErrValidation, "unsupported MCP harness", map[string]any{"harness": harness, "supported": SupportedHarnesses()})
	}

	configPath := opts.ConfigPath
	if configPath == "" {
		path, err := defaultConfigPath(harness)
		if err != nil {
			return Result{}, err
		}
		configPath = path
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return Result{}, err
	}

	command := strings.TrimSpace(opts.Command)
	if command == "" {
		command = defaultCommand()
	}
	args := []string{"mcp", "serve"}
	result := Result{Harness: harness, ConfigPath: absConfigPath, Command: command, Args: args, DryRun: opts.DryRun}

	codec := codecFor(harness)
	existing, exists, err := readConfig(absConfigPath, codec)
	if err != nil {
		return Result{}, err
	}

	if entryExists(existing, harness) && !opts.Force {
		result.Changed = false
		result.Status = "already_configured"
		result.Message = "Omakiten MCP server is already configured; pass force=true or --mcp-force to replace it."
		return result, domain.NewError(domain.ErrValidation, "omakiten MCP server already configured", map[string]any{"config_path": absConfigPath, "harness": harness})
	}

	updated := mergeHarnessConfig(existing, harness, command, args)
	result.Changed = true
	if opts.DryRun {
		result.Status = "would_write"
		if exists {
			result.Message = "Dry run only; existing harness config would be updated without overwriting other entries."
		} else {
			result.Message = "Dry run only; harness config would be created."
		}
		return result, nil
	}

	data, err := codec.Marshal(updated)
	if err != nil {
		return Result{}, err
	}
	if err := config.WriteAtomic(absConfigPath, data); err != nil {
		return Result{}, err
	}
	if exists {
		result.Status = "updated"
		result.Message = "Harness config updated while preserving existing entries."
	} else {
		result.Status = "created"
		result.Message = "Harness config created."
	}
	return result, nil
}

func isSupportedHarness(harness string) bool {
	for _, h := range SupportedHarnesses() {
		if h == harness {
			return true
		}
	}
	return false
}

func defaultConfigPath(harness string) (string, error) {
	switch harness {
	case ClaudeCodeHarness:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", ".mcp.json"), nil
	case ClaudeDesktopHarness:
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, "Claude", "claude_desktop_config.json"), nil
	case OpenCodeHarness:
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, "opencode", "opencode.json"), nil
	case CrushHarness:
		return crushDefaultConfigPath()
	case GitHubCopilotHarness:
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, "Code", "User", "mcp.json"), nil
	case CodexHarness:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".codex", "config.toml"), nil
	case CursorHarness:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cursor", "mcp.json"), nil
	default:
		return "", domain.NewError(domain.ErrValidation, "no default config path for harness", map[string]any{"harness": harness})
	}
}

// crushDefaultConfigPath returns Crush's documented global config path. Crush
// uses Local AppData on Windows (not Roaming, which os.UserConfigDir returns)
// and ~/.config on macOS (not ~/Library/Application Support), so the standard
// Go helpers don't fit.
func crushDefaultConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", domain.NewError(domain.ErrValidation, "LOCALAPPDATA is not set", map[string]any{"harness": CrushHarness})
		}
		return filepath.Join(local, "crush", "crush.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "crush", "crush.json"), nil
}

func defaultCommand() string {
	path, err := os.Executable()
	if err != nil || strings.TrimSpace(path) == "" {
		return "okt"
	}
	return path
}

func entryExists(existing map[string]any, harness string) bool {
	switch harness {
	case ClaudeCodeHarness:
		_, ok := existing["omakiten"]
		return ok
	case ClaudeDesktopHarness, CursorHarness:
		mcpServers, err := objectField(existing, "mcpServers")
		if err != nil || mcpServers == nil {
			return false
		}
		_, ok := mcpServers["omakiten"]
		return ok
	case OpenCodeHarness, CrushHarness:
		mcpSection, err := objectField(existing, "mcp")
		if err != nil || mcpSection == nil {
			return false
		}
		_, ok := mcpSection["omakiten"]
		return ok
	case GitHubCopilotHarness:
		servers, err := objectField(existing, "servers")
		if err != nil || servers == nil {
			return false
		}
		_, ok := servers["omakiten"]
		return ok
	case CodexHarness:
		section, err := objectField(existing, "mcp_servers")
		if err != nil || section == nil {
			return false
		}
		_, ok := section["omakiten"]
		return ok
	}
	return false
}

func mergeHarnessConfig(existing map[string]any, harness, command string, args []string) map[string]any {
	out := make(map[string]any, len(existing))
	for k, v := range existing {
		out[k] = v
	}
	switch harness {
	case ClaudeCodeHarness:
		out["omakiten"] = map[string]any{"command": command, "args": args}
	case ClaudeDesktopHarness, CursorHarness:
		mcpServers, _ := objectField(out, "mcpServers")
		if mcpServers == nil {
			mcpServers = map[string]any{}
		}
		mcpServers["omakiten"] = map[string]any{"command": command, "args": args}
		out["mcpServers"] = mcpServers
	case OpenCodeHarness:
		mcpSection, _ := objectField(out, "mcp")
		if mcpSection == nil {
			mcpSection = map[string]any{}
		}
		cmd := make([]string, 0, len(args)+1)
		cmd = append(cmd, command)
		cmd = append(cmd, args...)
		mcpSection["omakiten"] = map[string]any{"type": "local", "command": cmd, "enabled": true}
		out["mcp"] = mcpSection
	case CrushHarness:
		mcpSection, _ := objectField(out, "mcp")
		if mcpSection == nil {
			mcpSection = map[string]any{}
		}
		mcpSection["omakiten"] = map[string]any{"type": "stdio", "command": command, "args": args}
		out["mcp"] = mcpSection
	case GitHubCopilotHarness:
		servers, _ := objectField(out, "servers")
		if servers == nil {
			servers = map[string]any{}
		}
		servers["omakiten"] = map[string]any{"type": "stdio", "command": command, "args": args}
		out["servers"] = servers
	case CodexHarness:
		section, _ := objectField(out, "mcp_servers")
		if section == nil {
			section = map[string]any{}
		}
		section["omakiten"] = map[string]any{"command": command, "args": args}
		out["mcp_servers"] = section
	}
	return out
}

// configCodec abstracts the harness-specific serialization. JSON harnesses
// share a normalizing encoder (sorted keys for stable diffs); the Codex
// harness writes TOML via pelletier/go-toml/v2.
type configCodec interface {
	Unmarshal(data []byte) (map[string]any, error)
	Marshal(value map[string]any) ([]byte, error)
}

func codecFor(harness string) configCodec {
	if harness == CodexHarness {
		return tomlCodec{}
	}
	return jsonCodec{}
}

type jsonCodec struct{}

func (jsonCodec) Unmarshal(data []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func (jsonCodec) Marshal(value map[string]any) ([]byte, error) {
	normalized := sortMap(value)
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

type tomlCodec struct{}

func (tomlCodec) Unmarshal(data []byte) (map[string]any, error) {
	out := map[string]any{}
	if err := toml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (tomlCodec) Marshal(value map[string]any) ([]byte, error) {
	return toml.Marshal(value)
}

func readConfig(path string, codec configCodec) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, false, nil
		}
		return nil, false, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, true, nil
	}

	out, err := codec.Unmarshal(data)
	if err != nil {
		return nil, true, domain.NewError(domain.ErrValidation, "harness config is not valid", map[string]any{"path": path, "error": err.Error()})
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, true, nil
}

func objectField(parent map[string]any, key string) (map[string]any, error) {
	value, ok := parent[key]
	if !ok || value == nil {
		return nil, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, domain.NewError(domain.ErrValidation, "harness config field must be an object", map[string]any{"field": key})
	}
	return object, nil
}

func sortMap(value map[string]any) map[string]any {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(value))
	for _, key := range keys {
		switch typed := value[key].(type) {
		case map[string]any:
			out[key] = sortMap(typed)
		case []any:
			out[key] = sortSlice(typed)
		default:
			out[key] = typed
		}
	}
	return out
}

func sortSlice(value []any) []any {
	out := make([]any, len(value))
	for i, v := range value {
		switch typed := v.(type) {
		case map[string]any:
			out[i] = sortMap(typed)
		case []any:
			out[i] = sortSlice(typed)
		default:
			out[i] = typed
		}
	}
	return out
}
