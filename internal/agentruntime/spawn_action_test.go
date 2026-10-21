package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TestSpawnHookReachesTheProjectService verifies a kit can declare a spawn
// hook and that the hook runs against the project's agent service: an
// event naming no team or task fails the action and says why.
func TestSpawnHookReachesTheProjectService(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()
	candidate, err := config.LoadBundle(rt.configPath)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	on := true
	candidate.Config.Events.Defaults.Broadcast, candidate.Config.Events.Defaults.Hook, candidate.Config.Events.Defaults.Log = &on, &on, &on
	candidate.Config.Hooks = []config.HookSpec{{On: domain.EventTypeTaskCreated, Do: config.HookActionSpawn, Args: map[string]any{"member": "lead"}}}
	path := filepath.Join(filepath.Dir(rt.configPath), "spawn-hook.yaml")
	if err := config.SaveBundle(path, candidate); err != nil {
		t.Fatalf("SaveBundle: %v", err)
	}
	if _, err := rt.Cache().Apply(ctx, rt.defaultProjectID, path, nil); err != nil {
		t.Fatalf("Apply(spawn hook) error = %v", err)
	}
	if err := rt.store.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, rt.defaultProjectID, domain.EventTypeTaskCreated, `{}`); err != nil {
		t.Fatal(err)
	}
	executed := waitForHookExecution(t, rt, `"action":"spawn"`)
	if !strings.Contains(executed, `"success":false`) || !strings.Contains(executed, "carries no team_id or task_id") {
		t.Fatalf("hook.executed = %s, want the spawn action's refusal", executed)
	}
}

// TestPlanSpawnHookReadsThePlanFromTheEvent verifies a plan_member spawn
// hook matches guard.violated by stage and takes its plan from the
// payload's plan_id, so the start reaches the project's service.
func TestPlanSpawnHookReadsThePlanFromTheEvent(t *testing.T) {
	ctx := context.Background()
	rt := openTestRuntime(t)
	defer func() { _ = rt.Close() }()
	candidate, err := config.LoadBundle(rt.configPath)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	on := true
	candidate.Config.Events.Defaults.Broadcast, candidate.Config.Events.Defaults.Hook, candidate.Config.Events.Defaults.Log = &on, &on, &on
	candidate.Config.Hooks = []config.HookSpec{{On: domain.EventTypeGuardViolated, When: map[string]string{"stage": "complete"}, Do: config.HookActionSpawn, Args: map[string]any{"plan_member": "auditor"}}}
	path := filepath.Join(filepath.Dir(rt.configPath), "plan-spawn-hook.yaml")
	if err := config.SaveBundle(path, candidate); err != nil {
		t.Fatalf("SaveBundle: %v", err)
	}
	if _, err := rt.Cache().Apply(ctx, rt.defaultProjectID, path, nil); err != nil {
		t.Fatalf("Apply(plan spawn hook) error = %v", err)
	}
	if err := rt.store.RecordEntityEvent(ctx, domain.EventEntityPlan, 999, rt.defaultProjectID, domain.EventTypeGuardViolated, `{"stage":"complete","plan_id":999}`); err != nil {
		t.Fatal(err)
	}
	executed := waitForHookExecution(t, rt, `"action":"spawn"`)
	if !strings.Contains(executed, `"success":false`) || strings.Contains(executed, "carries no") {
		t.Fatalf("hook.executed = %s, want the service, not the payload, to refuse plan 999", executed)
	}
}

// waitForHookExecution returns the payload of the first hook.executed
// event containing marker.
func waitForHookExecution(t *testing.T, rt *Runtime, marker string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		executed, err := rt.store.ListRecentEvents(context.Background(), domain.EventTypeHookExecuted, 5)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range executed {
			if strings.Contains(event.Payload, marker) {
				return event.Payload
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no hook.executed carries %s", marker)
	return ""
}
