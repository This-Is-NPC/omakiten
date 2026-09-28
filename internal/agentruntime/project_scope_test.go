package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TestBundleCacheImportedEventStaysWithImportingRuntime proves that a
// candidate imported for project B cannot reach project A's simultaneously
// registered hook engine. It also covers candidate quarantine: a rejected B
// candidate emits neither bundle.imported nor hook.executed, and the same
// candidate can be retried successfully.
func TestBundleCacheImportedEventStaysWithImportingRuntime(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	tmp := t.TempDir()
	markerA := filepath.Join(tmp, "project-a-hook-fired")
	markerB := filepath.Join(tmp, "project-b-hook-fired")
	configDir := filepath.Dir(rt.configPath)
	pathA := filepath.Join(configDir, "project-a.yaml")
	pathB := filepath.Join(configDir, "project-b.yaml")
	writeHookCandidate(t, rt.configPath, pathA, markerA)
	writeHookCandidate(t, rt.configPath, pathB, markerB)

	seedScopedHookRuntimes(t, ctx, rt, pathA, pathB, markerA, markerB)
	rejectCandidate(t, ctx, rt, pathB, markerA, markerB)
	importEvent := retryCandidate(t, ctx, rt, pathB, markerA, markerB)
	assertImportedHookProject(t, ctx, rt, importEvent.ProjectID)
}

func seedScopedHookRuntimes(t *testing.T, ctx context.Context, rt *Runtime, pathA, pathB, markerA, markerB string) {
	t.Helper()
	if _, err := rt.Cache().Resolve(ctx, 101, pathA); err != nil {
		t.Fatalf("Resolve project A: %v", err)
	}
	waitForCandidateHook(t, markerA)
	removeMarker(t, markerA)
	if _, err := rt.Cache().Resolve(ctx, 202, pathB); err != nil {
		t.Fatalf("Resolve project B: %v", err)
	}
	waitForCandidateHook(t, markerB)
	// Let any incorrectly admitted A action from B's initial import finish
	// before the marker is reset for the candidate assertion below.
	time.Sleep(50 * time.Millisecond)
	removeMarker(t, markerA)
	removeMarker(t, markerB)
}

func rejectCandidate(t *testing.T, ctx context.Context, rt *Runtime, pathB, markerA, markerB string) {
	t.Helper()
	before, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeBundleImported, 100)
	if err != nil {
		t.Fatalf("ListRecentEvents before retry: %v", err)
	}
	beforeBImports := countEventsForPath(before, pathB)
	rebindErr := errors.New("consumer rebind failed")
	if _, err := rt.Cache().ApplyWithCommit(ctx, 202, pathB, func(*ProjectRuntime) (func() error, error) {
		return nil, rebindErr
	}); !errors.Is(err, rebindErr) {
		t.Fatalf("rejected B candidate error = %v, want %v", err, rebindErr)
	}
	if got := rt.Cache().Get(202); got == nil || got.SourcePath != pathB {
		t.Fatalf("rejected B candidate replaced cached runtime: %+v", got)
	}
	assertMarkerAbsent(t, markerA, "project A hook fired for quarantined B candidate")
	assertMarkerAbsent(t, markerB, "project B hook fired before candidate acceptance")
	afterReject, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeBundleImported, 100)
	if err != nil {
		t.Fatalf("ListRecentEvents after rejection: %v", err)
	}
	if got := countEventsForPath(afterReject, pathB); got != beforeBImports {
		t.Fatalf("rejected B candidate imported events = %d, want %d", got, beforeBImports)
	}
}

func retryCandidate(t *testing.T, ctx context.Context, rt *Runtime, pathB, markerA, markerB string) domain.Event {
	t.Helper()
	retried, err := rt.Cache().Apply(ctx, 202, pathB, nil)
	if err != nil {
		t.Fatalf("retry B candidate: %v", err)
	}
	if retried == nil || retried.SourcePath != pathB {
		t.Fatalf("retry returned invalid B runtime: %+v", retried)
	}
	waitForCandidateHook(t, markerB)
	time.Sleep(50 * time.Millisecond)
	assertMarkerAbsent(t, markerA, "project A hook fired for retried B import")
	afterRetry, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeBundleImported, 100)
	if err != nil {
		t.Fatalf("ListRecentEvents after retry: %v", err)
	}
	importEvent, ok := latestEventForPath(afterRetry, pathB)
	if !ok {
		t.Fatalf("no bundle.imported event for retried B path %q: %+v", pathB, afterRetry)
	}
	if importEvent.ProjectID != 202 {
		t.Fatalf("retried B bundle.imported ProjectID = %d, want 202", importEvent.ProjectID)
	}
	return importEvent
}

func assertImportedHookProject(t *testing.T, ctx context.Context, rt *Runtime, projectID int64) {
	t.Helper()
	hookEvents, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeHookExecuted, 100)
	if err != nil {
		t.Fatalf("ListRecentEvents hook.executed: %v", err)
	}
	for _, event := range hookEvents {
		var payload struct {
			EventType string `json:"event_type"`
		}
		if json.Unmarshal([]byte(event.Payload), &payload) == nil && payload.EventType == domain.EventTypeBundleImported && event.ProjectID == projectID {
			return
		}
	}
	t.Fatalf("no hook.executed event is attributed to B import ProjectID %d", projectID)
}

func assertMarkerAbsent(t *testing.T, path, message string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s: %v", message, err)
	}
}

func writeHookCandidate(t *testing.T, sourcePath, targetPath, marker string) {
	t.Helper()
	candidate, err := config.LoadBundle(sourcePath)
	if err != nil {
		t.Fatalf("LoadBundle(%s): %v", sourcePath, err)
	}
	trueValue := true
	candidate.Config.Events.Defaults.Broadcast = &trueValue
	candidate.Config.Events.Defaults.Hook = &trueValue
	candidate.Config.Events.Defaults.Log = &trueValue
	candidate.Config.Hooks = []config.HookSpec{{
		On:   domain.EventTypeBundleImported,
		Do:   "exec",
		Args: map[string]interface{}{"argv": []string{"touch", marker}},
	}}
	if err := config.SaveBundle(targetPath, candidate); err != nil {
		t.Fatalf("SaveBundle(%s): %v", targetPath, err)
	}
}

func removeMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove marker %s: %v", path, err)
	}
}

func countEventsForPath(events []domain.Event, path string) int {
	count := 0
	for _, event := range events {
		if strings.Contains(event.Payload, `"path":"`+path+`"`) {
			count++
		}
	}
	return count
}

func latestEventForPath(events []domain.Event, path string) (domain.Event, bool) {
	for _, event := range events {
		if strings.Contains(event.Payload, `"path":"`+path+`"`) {
			return event, true
		}
	}
	return domain.Event{}, false
}
