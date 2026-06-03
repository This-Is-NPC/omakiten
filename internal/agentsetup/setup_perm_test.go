package agentsetup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSetupCreatesMcpJsonFreshParent proves the zero-error bar for the
// claude-code harness when ~/.claude/ does not yet exist: WriteAtomic creates
// the parent (owner-only) and writes .mcp.json without error.
func TestSetupCreatesMcpJsonFreshParent(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	configPath := filepath.Join(claudeDir, ".mcp.json")

	result, err := Setup(Options{Harness: ClaudeCodeHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup on fresh ~/.claude/: %v", err)
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
			t.Fatalf("fresh ~/.claude/ mode = %o, want 0700", got)
		}
	}
}

// TestSetupCreatesMcpJsonExistingParent proves the zero-error bar when
// ~/.claude/ already exists at the typical Claude-Code 0o755: setup must still
// write .mcp.json without error AND must not clobber the directory mode.
func TestSetupCreatesMcpJsonExistingParent(t *testing.T) {
	claudeDir := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("pre-create ~/.claude/: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(claudeDir, 0o755); err != nil {
			t.Fatalf("chmod ~/.claude/: %v", err)
		}
	}
	configPath := filepath.Join(claudeDir, ".mcp.json")

	result, err := Setup(Options{Harness: ClaudeCodeHarness, ConfigPath: configPath, Command: "okt"})
	if err != nil {
		t.Fatalf("Setup on existing ~/.claude/: %v", err)
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
			t.Fatalf("existing ~/.claude/ mode = %o, want unchanged 0755", got)
		}
	}
}
