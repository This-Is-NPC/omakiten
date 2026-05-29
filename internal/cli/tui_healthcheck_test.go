package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestRunTUI_BrokenConfigReturnsStructuredEnvelope pins #365 AC 5: a
// broken bundle on `okt tui` must surface the same structured failure
// the `okt config validate --migrate` path emits, so the user sees
// the per-error kind + suggested_command instead of a bare validator
// string. The TUI must abort before tea.NewProgram is constructed —
// the assertion on cmd.Execute() returning err proves bubbletea was
// never reached.
func TestRunTUI_BrokenConfigReturnsStructuredEnvelope(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "db.sqlite")
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "omakase.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 2\n"), 0o644); err != nil {
		t.Fatalf("write broken yaml: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--db", dbPath, "--config", cfgPath, "tui"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected coded error on broken config, out=%s", out.String())
	}

	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type = %T want *domain.CodedError, out=%s", err, out.String())
	}
	if coded.Code != domain.ErrConfigInvalid {
		t.Fatalf("code = %v want ErrConfigInvalid", coded.Code)
	}

	errs, ok := coded.Details["errors"].([]map[string]any)
	if !ok {
		t.Fatalf("details.errors missing or wrong type: %v", coded.Details["errors"])
	}
	if len(errs) != 1 {
		t.Fatalf("details.errors len = %d want 1, entries=%v", len(errs), errs)
	}
	entry := errs[0]
	if k, _ := entry["kind"].(string); k == "" {
		t.Fatalf("entry.kind empty: %v", entry)
	}
	cmdStr, _ := entry["suggested_command"].(string)
	if strings.TrimSpace(cmdStr) == "" {
		t.Fatalf("entry.suggested_command empty: %v", entry)
	}
	if msg, _ := entry["message"].(string); !strings.Contains(strings.ToLower(msg), "version") {
		t.Fatalf("entry.message = %q want substring 'version'", msg)
	}
}
