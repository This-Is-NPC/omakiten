package agentsetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

const ClaudeDesktopHarness = "claude-desktop"

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
	return []string{ClaudeDesktopHarness}
}

func Setup(opts Options) (Result, error) {
	harness := strings.TrimSpace(opts.Harness)
	if harness == "" {
		harness = ClaudeDesktopHarness
	}
	if harness != ClaudeDesktopHarness {
		return Result{}, domain.NewError(domain.ErrValidation, "unsupported MCP harness", map[string]any{"harness": harness, "supported": SupportedHarnesses()})
	}

	configPath := opts.ConfigPath
	if configPath == "" {
		path, err := defaultClaudeDesktopConfigPath()
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
	result := Result{Harness: harness, ConfigPath: absConfigPath, Command: command, Args: []string{"mcp", "serve"}, DryRun: opts.DryRun}

	existing, exists, err := readConfig(absConfigPath)
	if err != nil {
		return Result{}, err
	}

	mcpServers, err := objectField(existing, "mcpServers")
	if err != nil {
		return Result{}, err
	}
	if mcpServers == nil {
		mcpServers = map[string]any{}
	}

	if _, ok := mcpServers["omakiten"]; ok && !opts.Force {
		result.Changed = false
		result.Status = "already_configured"
		result.Message = "Omakiten MCP server is already configured; pass force=true or --mcp-force to replace it."
		return result, domain.NewError(domain.ErrValidation, "omakiten MCP server already configured", map[string]any{"config_path": absConfigPath})
	}

	mcpServers["omakiten"] = map[string]any{"command": command, "args": result.Args}
	existing["mcpServers"] = mcpServers
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

	data, err := marshalStable(existing)
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

func defaultClaudeDesktopConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "Claude", "claude_desktop_config.json"), nil
}

func defaultCommand() string {
	path, err := os.Executable()
	if err != nil || strings.TrimSpace(path) == "" {
		return "okt"
	}
	return path
}

func readConfig(path string) (map[string]any, bool, error) {
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

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, true, domain.NewError(domain.ErrValidation, "harness config is not valid JSON", map[string]any{"path": path, "error": err.Error()})
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

func marshalStable(value map[string]any) ([]byte, error) {
	normalized := sortMap(value)
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
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
		default:
			out[key] = typed
		}
	}
	return out
}
