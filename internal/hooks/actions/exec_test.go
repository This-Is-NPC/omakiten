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
