package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func appendRuntimeTopLevelYAML(t *testing.T, path, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\n"+body)...), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestBundleCacheSubtaskKitMtimeChangeTriggersRebuild(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	appendRuntimeTopLevelYAML(t, rt.configPath, "subtask_kit: izakaya.yaml\n")
	first, err := rt.Cache().Reload(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Reload with subtask_kit: %v", err)
	}
	if _, ok := first.Snapshot.SubtaskKit(); !ok {
		t.Fatal("Reloaded snapshot missing subtask kit")
	}

	subPath := filepath.Join(filepath.Dir(rt.configPath), "izakaya.yaml")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(subPath, future, future); err != nil {
		t.Fatalf("Chtimes(%s): %v", subPath, err)
	}

	second, err := rt.Cache().Resolve(ctx, rt.defaultProjectID, "")
	if err != nil {
		t.Fatalf("Resolve after sub-kit mtime change: %v", err)
	}
	if first == second {
		t.Fatalf("Resolve did not rebuild on sub-kit mtime change: same pointer %p", first)
	}
}

func TestBundleCacheInvalidSubtaskKitReloadDoesNotReplaceEntry(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()

	first, err := rt.Cache().Resolve(ctx, rt.defaultProjectID, rt.configPath)
	if err != nil {
		t.Fatalf("Resolve seed: %v", err)
	}
	appendRuntimeTopLevelYAML(t, rt.configPath, "subtask_kit: missing.yaml\n")

	if _, err := rt.Cache().Reload(ctx, rt.defaultProjectID, rt.configPath); err == nil {
		t.Fatal("Reload with missing subtask_kit error = nil, want failure")
	}
	if got := rt.Cache().Get(rt.defaultProjectID); got != first {
		t.Fatalf("cache entry replaced after invalid subtask_kit reload: got %p want %p", got, first)
	}
}
