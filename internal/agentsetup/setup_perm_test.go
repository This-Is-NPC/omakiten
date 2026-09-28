package agentsetup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSetupCreatesMcpJsonFreshParent proves the zero-error bar when WriteAtomic
// must create a missing parent directory (owner-only) and write .mcp.json.
// Explicit ConfigPath is used so this does not depend on the Claude Code
// project-root default.
func TestSetupCreatesMcpJsonFreshParent(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	configPath := filepath.Join(claudeDir, ".mcp.json")

	result, err := Setup(Options{Harness: ClaudeCodeHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup on fresh parent: %v", err)
	}
	if result.Status != "created" {
		t.Fatalf("status = %q, want created", result.Status)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf(".mcp.json not written: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(claudeDir)
		if err != nil {
			t.Fatalf("stat claude dir: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("fresh parent mode = %o, want 0700", got)
		}
	}
}

// TestSetupCreatesMcpJsonExistingParent proves the zero-error bar when the
// config parent already exists at 0o755: setup must still write .mcp.json
// without error AND must not clobber the directory mode.
func TestSetupCreatesMcpJsonExistingParent(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("pre-create parent: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(claudeDir, 0o755); err != nil {
			t.Fatalf("chmod parent: %v", err)
		}
	}
	configPath := filepath.Join(claudeDir, ".mcp.json")

	result, err := Setup(Options{Harness: ClaudeCodeHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup on existing parent: %v", err)
	}
	if result.Status != "created" {
		t.Fatalf("status = %q, want created", result.Status)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf(".mcp.json not written: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(claudeDir)
		if err != nil {
			t.Fatalf("stat claude dir: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Fatalf("existing parent mode = %o, want unchanged 0755", got)
		}
	}
}
