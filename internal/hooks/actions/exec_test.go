package actions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestExecRunsCommandAndPipesEventJSON(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "stdin.json")
	err := Exec{}.Execute(context.Background(),
		domain.Event{EventType: domain.EventTypeTaskCreated, EntityID: 42, Payload: `{"bucket":"backlog"}`},
		map[string]any{
			"argv":       []any{"sh", "-c", "cat > " + out},
			"timeout_ms": 2000,
		})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read stdin file: %v", err)
	}
	var ev map[string]any
	if jsonErr := json.Unmarshal(body, &ev); jsonErr != nil {
		t.Fatalf("stdin not JSON: %v (%s)", jsonErr, body)
	}
	if ev["event_type"] != domain.EventTypeTaskCreated {
		t.Fatalf("event_type = %v, want task.created", ev["event_type"])
	}
}

func TestExecMissingArgvFails(t *testing.T) {
	if err := (Exec{}).Execute(context.Background(), domain.Event{}, map[string]any{}); err == nil {
		t.Fatal("Execute without argv should error")
	}
}

func TestExecEmptyArgvFails(t *testing.T) {
	if err := (Exec{}).Execute(context.Background(), domain.Event{}, map[string]any{"argv": []any{}}); err == nil {
		t.Fatal("Execute with empty argv should error")
	}
}

func TestExecTimesOut(t *testing.T) {
	err := Exec{}.Execute(context.Background(),
		domain.Event{},
		map[string]any{
			"argv":       []any{"sleep", "5"},
			"timeout_ms": 50,
		})
	if err == nil {
		t.Fatal("Execute did not return on timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

// TestResolveExecBinary pins the security guard from task #219: bare
// names must round-trip through exec.LookPath into an absolute path so
// the engine no longer resolves against the inherited PATH at fork
// time. Absolute paths pass through. Relative paths with embedded
// separators (./foo, ../bin/foo) are rejected outright because the
// hook YAML cannot reason about the engine's CWD.
func TestResolveExecBinary(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantErr   bool
		wantExact string // when wantErr is false; "" = don't check (LookPath-resolved)
	}{
		{name: "absolute path passes through", input: "/usr/bin/env", wantErr: false, wantExact: "/usr/bin/env"},
		{name: "absolute path cleaned", input: "/usr/bin/./env", wantErr: false, wantExact: "/usr/bin/env"},
		{name: "bare command resolves via PATH", input: "sh", wantErr: false},
		{name: "empty argv0 rejected", input: "", wantErr: true},
		{name: "blank argv0 rejected", input: "   ", wantErr: true},
		{name: "relative ./script rejected", input: "./script.sh", wantErr: true},
		{name: "relative ../bin/foo rejected", input: "../bin/foo", wantErr: true},
		{name: "sub/dir/foo rejected", input: "sub/dir/foo", wantErr: true},
		{name: "missing binary on PATH errors", input: "definitely-not-a-real-cmd-xyz-omakiten", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveExecBinary(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("resolveExecBinary returned non-absolute path %q", got)
			}
			if tc.wantExact != "" && got != tc.wantExact {
				t.Fatalf("resolveExecBinary(%q) = %q, want %q", tc.input, got, tc.wantExact)
			}
		})
	}
}

func TestExecNonZeroExitCodeReturnsError(t *testing.T) {
	err := Exec{}.Execute(context.Background(),
		domain.Event{},
		map[string]any{
			"argv":       []any{"sh", "-c", "echo bad >&2; exit 7"},
			"timeout_ms": 2000,
		})
	if err == nil {
		t.Fatal("non-zero exit should error")
	}
	if !strings.Contains(err.Error(), "stderr") {
		t.Fatalf("error should include captured stderr, got %v", err)
	}
}
