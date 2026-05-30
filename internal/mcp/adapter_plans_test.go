package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// callPlanTool dispatches a plans.* tool via CallTool and returns the decoded
// payload. Fails on transport or (unless wantErr) tool error.
func callPlanTool(t *testing.T, ctx context.Context, adapter *Adapter, tool string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	result, err := adapter.CallTool(ctx, tool, withModel(args))
	if err != nil {
		t.Fatalf("CallTool(%s) transport error = %v", tool, err)
	}
	var payload map[string]any
	if uerr := json.Unmarshal([]byte(result.Content[0].Text), &payload); uerr != nil {
		t.Fatalf("%s payload not JSON: %v (%s)", tool, uerr, result.Content[0].Text)
	}
	return payload, result.IsError
}

// showPlan returns the decoded plans.show payload for slug.
func showPlan(t *testing.T, ctx context.Context, adapter *Adapter, slug string) map[string]any {
	t.Helper()
	payload, isErr := callPlanTool(t, ctx, adapter, "plans.show", map[string]any{"slug": slug})
	if isErr {
		t.Fatalf("plans.show(%s) IsError: %v", slug, payload)
	}
	return payload
}

// createPlan creates a plan via CallTool and asserts success.
func createPlan(t *testing.T, ctx context.Context, adapter *Adapter, slug, name string) {
	t.Helper()
	payload, isErr := callPlanTool(t, ctx, adapter, "plans.create", map[string]any{"slug": slug, "name": name})
	if isErr {
		t.Fatalf("plans.create(%s) IsError: %v", slug, payload)
	}
}

// TestAdapterPlansEditThroughCallTool drives plans.edit behaviorally: a
// name+status edit is reflected in show, and a colliding new_slug is rejected.
func TestAdapterPlansEditThroughCallTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	createPlan(t, ctx, adapter, "edit-me", "Edit Me")
	createPlan(t, ctx, adapter, "other", "Other") // collision target for new_slug

	// Edit name + status; expect the change reflected in show.
	editPayload, isErr := callPlanTool(t, ctx, adapter, "plans.edit", map[string]any{
		"slug": "edit-me", "name": "Renamed", "status": "done",
	})
	if isErr {
		t.Fatalf("plans.edit IsError: %v", editPayload)
	}
	plan, _ := editPayload["plan"].(map[string]any)
	if plan == nil || plan["name"] != "Renamed" || plan["status"] != "done" {
		t.Fatalf("plans.edit returned plan = %#v, want name=Renamed status=done", plan)
	}

	shown := showPlan(t, ctx, adapter, "edit-me")
	shownPlan, _ := shown["plan"].(map[string]any)
	if shownPlan["name"] != "Renamed" || shownPlan["status"] != "done" {
		t.Fatalf("plans.show after edit = %#v, want name=Renamed status=done", shownPlan)
	}

	// Colliding new_slug → error.
	collidePayload, isErr := callPlanTool(t, ctx, adapter, "plans.edit", map[string]any{
		"slug": "edit-me", "new_slug": "other",
	})
	if !isErr {
		t.Fatalf("plans.edit(new_slug=other) should collide, got success: %v", collidePayload)
	}
}

// TestAdapterPlansDeleteThroughCallTool drives plans.delete: an unconfirmed
// call is a confirmation no-op (plan survives), a confirmed call removes the
// plan while its member task survives detached.
func TestAdapterPlansDeleteThroughCallTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	createPlan(t, ctx, adapter, "doomed", "Doomed")
	// Attach the seeded task #1 to a wave so we can assert it survives detached.
	wavePayload, isErr := callPlanTool(t, ctx, adapter, "plans.add_wave", map[string]any{"slug": "doomed", "name": "W1"})
	if isErr {
		t.Fatalf("plans.add_wave IsError: %v", wavePayload)
	}
	wave, _ := wavePayload["wave"].(map[string]any)
	waveID := int64(wave["id"].(float64))
	if assignPayload, isErr := callPlanTool(t, ctx, adapter, "plans.assign_task", map[string]any{
		"slug": "doomed", "task_id": int64(1), "wave_id": waveID,
	}); isErr {
		t.Fatalf("plans.assign_task IsError: %v", assignPayload)
	}

	// Unconfirmed delete → confirmation block, plan NOT removed.
	noConfirm, isErr := callPlanTool(t, ctx, adapter, "plans.delete", map[string]any{"slug": "doomed"})
	if isErr {
		t.Fatalf("plans.delete (unconfirmed) IsError: %v", noConfirm)
	}
	confirmation, _ := noConfirm["confirmation"].(map[string]any)
	if confirmation == nil || confirmation["requires_confirmation"] != true {
		t.Fatalf("plans.delete (unconfirmed) = %#v, want requires_confirmation block", noConfirm)
	}
	// Plan still present.
	if shown := showPlan(t, ctx, adapter, "doomed"); shown["plan"] == nil {
		t.Fatalf("plan vanished after unconfirmed delete: %v", shown)
	}

	// Confirmed delete → plan gone.
	confirmed, isErr := callPlanTool(t, ctx, adapter, "plans.delete", map[string]any{"slug": "doomed", "confirmed": true})
	if isErr {
		t.Fatalf("plans.delete (confirmed) IsError: %v", confirmed)
	}
	gone, isErr := callPlanTool(t, ctx, adapter, "plans.show", map[string]any{"slug": "doomed"})
	if !isErr {
		t.Fatalf("plans.show after confirmed delete should error (plan gone), got: %v", gone)
	}

	// Member task survives detached: tasks.list returns it (no plan filter here),
	// and it is no longer in any plan. We assert the task still exists.
	listRes, err := adapter.CallTool(ctx, "tasks.list", withModel(map[string]any{}))
	if err != nil || listRes.IsError {
		t.Fatalf("tasks.list after plan delete failed: %v / %s", err, listRes.Content[0].Text)
	}
	var listPayload struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if uerr := json.Unmarshal([]byte(listRes.Content[0].Text), &listPayload); uerr != nil {
		t.Fatalf("tasks.list payload not JSON: %v", uerr)
	}
	found := false
	for _, task := range listPayload.Tasks {
		if int64(task["id"].(float64)) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("member task #1 did not survive plan delete: %#v", listPayload.Tasks)
	}
}

// TestAdapterPlansWaveOpsThroughCallTool exercises the wave lifecycle via
// CallTool: add_wave, rename_wave, reorder_wave (position swap), remove_wave
// (gone + member task wave_id nulled), and unassign (task detached). Each
// effect is asserted via plans.show.
func TestAdapterPlansWaveOpsThroughCallTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	createPlan(t, ctx, adapter, "waves", "Waves")

	addWave := func(name string) (int64, int) {
		t.Helper()
		payload, isErr := callPlanTool(t, ctx, adapter, "plans.add_wave", map[string]any{"slug": "waves", "name": name})
		if isErr {
			t.Fatalf("plans.add_wave(%s) IsError: %v", name, payload)
		}
		wave, _ := payload["wave"].(map[string]any)
		return int64(wave["id"].(float64)), int(wave["position"].(float64))
	}

	w1ID, w1Pos := addWave("Alpha")
	w2ID, w2Pos := addWave("Bravo")
	if w1Pos == w2Pos {
		t.Fatalf("waves got identical positions: %d, %d", w1Pos, w2Pos)
	}

	// rename_wave: Alpha → Renamed; reflected in show.
	if renamePayload, isErr := callPlanTool(t, ctx, adapter, "plans.rename_wave", map[string]any{
		"wave_id": w1ID, "name": "Renamed",
	}); isErr {
		t.Fatalf("plans.rename_wave IsError: %v", renamePayload)
	}
	if name := waveName(t, showPlan(t, ctx, adapter, "waves"), w1ID); name != "Renamed" {
		t.Fatalf("rename_wave: wave %d name = %q, want Renamed", w1ID, name)
	}

	// reorder_wave: move w1 to w2's slot → positions swap.
	if reorderPayload, isErr := callPlanTool(t, ctx, adapter, "plans.reorder_wave", map[string]any{
		"wave_id": w1ID, "position": w2Pos,
	}); isErr {
		t.Fatalf("plans.reorder_wave IsError: %v", reorderPayload)
	}
	shown := showPlan(t, ctx, adapter, "waves")
	if gotW1 := wavePosition(t, shown, w1ID); gotW1 != w2Pos {
		t.Fatalf("reorder_wave: wave %d position = %d, want %d", w1ID, gotW1, w2Pos)
	}
	if gotW2 := wavePosition(t, shown, w2ID); gotW2 != w1Pos {
		t.Fatalf("reorder_wave: wave %d position = %d, want %d (swap)", w2ID, gotW2, w1Pos)
	}

	// Assign the seeded task #1 to w2, then remove_wave w2 → wave gone, task
	// survives with its wave membership cleared (no longer appears under w2).
	if assignPayload, isErr := callPlanTool(t, ctx, adapter, "plans.assign_task", map[string]any{
		"slug": "waves", "task_id": int64(1), "wave_id": w2ID,
	}); isErr {
		t.Fatalf("plans.assign_task IsError: %v", assignPayload)
	}
	if !waveHasTask(t, showPlan(t, ctx, adapter, "waves"), w2ID, 1) {
		t.Fatalf("task #1 not under wave %d after assign", w2ID)
	}
	if removePayload, isErr := callPlanTool(t, ctx, adapter, "plans.remove_wave", map[string]any{
		"wave_id": w2ID, "confirmed": true,
	}); isErr {
		t.Fatalf("plans.remove_wave IsError: %v", removePayload)
	}
	afterRemove := showPlan(t, ctx, adapter, "waves")
	if waveExists(t, afterRemove, w2ID) {
		t.Fatalf("wave %d still present after remove_wave: %v", w2ID, afterRemove["waves"])
	}
	// Member task wave_id nulled: it must not appear under any remaining wave.
	if anyWaveHasTask(t, afterRemove, 1) {
		t.Fatalf("task #1 still attached to a wave after its wave was removed: %v", afterRemove["waves"])
	}

	// unassign: re-attach task #1 to the surviving wave (w1, now at w2Pos),
	// then unassign → detached=true and task no longer under any wave.
	if assignPayload, isErr := callPlanTool(t, ctx, adapter, "plans.assign_task", map[string]any{
		"slug": "waves", "task_id": int64(1), "wave_id": w1ID,
	}); isErr {
		t.Fatalf("plans.assign_task (re-attach) IsError: %v", assignPayload)
	}
	unassignPayload, isErr := callPlanTool(t, ctx, adapter, "plans.unassign", map[string]any{"task_id": int64(1)})
	if isErr {
		t.Fatalf("plans.unassign IsError: %v", unassignPayload)
	}
	if unassignPayload["detached"] != true {
		t.Fatalf("plans.unassign detached = %v, want true", unassignPayload["detached"])
	}
	if anyWaveHasTask(t, showPlan(t, ctx, adapter, "waves"), 1) {
		t.Fatalf("task #1 still under a wave after unassign")
	}
}

// --- plans.show wave inspection helpers ---

func planWaves(t *testing.T, shown map[string]any) []map[string]any {
	t.Helper()
	raw, _ := shown["waves"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, w := range raw {
		if m, ok := w.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func waveByID(t *testing.T, shown map[string]any, waveID int64) map[string]any {
	t.Helper()
	for _, w := range planWaves(t, shown) {
		if int64(w["id"].(float64)) == waveID {
			return w
		}
	}
	return nil
}

func waveExists(t *testing.T, shown map[string]any, waveID int64) bool {
	t.Helper()
	return waveByID(t, shown, waveID) != nil
}

func waveName(t *testing.T, shown map[string]any, waveID int64) string {
	t.Helper()
	w := waveByID(t, shown, waveID)
	if w == nil {
		t.Fatalf("wave %d not found in show", waveID)
	}
	name, _ := w["name"].(string)
	return name
}

func wavePosition(t *testing.T, shown map[string]any, waveID int64) int {
	t.Helper()
	w := waveByID(t, shown, waveID)
	if w == nil {
		t.Fatalf("wave %d not found in show", waveID)
	}
	return int(w["position"].(float64))
}

func waveHasTask(t *testing.T, shown map[string]any, waveID, taskID int64) bool {
	t.Helper()
	w := waveByID(t, shown, waveID)
	if w == nil {
		return false
	}
	tasks, _ := w["tasks"].([]any)
	for _, raw := range tasks {
		if tm, ok := raw.(map[string]any); ok {
			if int64(tm["task_id"].(float64)) == taskID {
				return true
			}
		}
	}
	return false
}

func anyWaveHasTask(t *testing.T, shown map[string]any, taskID int64) bool {
	t.Helper()
	for _, w := range planWaves(t, shown) {
		if waveHasTask(t, shown, int64(w["id"].(float64)), taskID) {
			return true
		}
	}
	return false
}
