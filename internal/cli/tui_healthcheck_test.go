package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
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

// TestSummariseValidationErrors_HandlesBothPayloadShapes pins the
// `[]map[string]any` ↔ `[]any` fallback for
// `emitTUIHealthCheckFailedFromOpenError`. The in-process wrap
// builds the slice as `[]map[string]any`; a JSON roundtrip
// (activity-store, hook payload) lands it as `[]any`. The TUI helper
// must handle both — pre-fix the JSON shape silently fell through
// to count=1 with no kind.
func TestSummariseValidationErrors_HandlesBothPayloadShapes(t *testing.T) {
	cases := []struct {
		name      string
		raw       any
		wantCount int
		wantKind  string
	}{
		{
			name: "in-process []map[string]any",
			raw: []map[string]any{
				{"kind": "theme_not_found", "message": "x"},
				{"kind": "invalid_value", "message": "y"},
			},
			wantCount: 2,
			wantKind:  "theme_not_found",
		},
		{
			name: "JSON-roundtripped []any of map[string]any",
			raw: []any{
				map[string]any{"kind": "missing_required_key", "message": "x"},
				map[string]any{"kind": "invalid_value", "message": "y"},
			},
			wantCount: 2,
			wantKind:  "missing_required_key",
		},
		{
			name:      "JSON-roundtripped []any without map elements",
			raw:       []any{"not-a-map"},
			wantCount: 1,
			wantKind:  "",
		},
		{
			name:      "nil falls through to count=1",
			raw:       nil,
			wantCount: 1,
			wantKind:  "",
		},
		{
			name:      "wrong type falls through to count=1",
			raw:       "garbage",
			wantCount: 1,
			wantKind:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotCount, gotKind := summariseValidationErrors(tc.raw)
			if gotCount != tc.wantCount {
				t.Errorf("count = %d want %d", gotCount, tc.wantCount)
			}
			if gotKind != tc.wantKind {
				t.Errorf("kind = %q want %q", gotKind, tc.wantKind)
			}
		})
	}
}

// TestRunTUI_BrokenConfigEmitsTUIHealthCheckFailed pins #369 AC 4: when
// `okt tui` aborts on a config-invalid boot, the tui.healthcheck.failed
// event lands in the activity log so the operator can audit the
// failure later (`okt logs list --kind tui.healthcheck.failed`).
func TestRunTUI_BrokenConfigEmitsTUIHealthCheckFailed(t *testing.T) {
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
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected coded error on broken config, out=%s", out.String())
	}

	store, err := sqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer store.Close()
	rows, err := store.ListRecentEvents(context.Background(), domain.EventTypeTUIHealthCheckFailed, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("tui.healthcheck.failed rows = %d want 1", len(rows))
	}
	if !strings.Contains(rows[0].Payload, "validator_first_error_kind") {
		t.Errorf("payload missing validator_first_error_kind: %s", rows[0].Payload)
	}
}
