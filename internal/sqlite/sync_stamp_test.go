package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func assertFreshUIDs(t *testing.T, ctx context.Context, store *storeFixture, table string, want int) {
	t.Helper()
	rows, err := store.db.QueryContext(ctx, "SELECT uid FROM "+table+" WHERE uid IS NOT NULL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	seen := map[string]bool{}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if _, err := domain.ParseUID(uid); err != nil {
			t.Fatalf("%s uid %q: %v", table, uid, err)
		}
		if seen[uid] {
			t.Fatalf("%s uid %q repeated", table, uid)
		}
		seen[uid] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) < want {
		t.Fatalf("%s has %d uids, want at least %d", table, len(seen), want)
	}
	var missing int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE uid IS NULL OR uid = ''").Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("%s rows without uid = %d, %v", table, missing, err)
	}
}

func TestInsertsStampUIDs(t *testing.T) {
	ctx, store, project := setupPlans(t)
	for i := range 2 {
		if _, err := store.CreateTask(ctx, project.ID, "task", "", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
			t.Fatal(err)
		}
		record, err := store.RecordError(ctx, project.ID, "boom", "ctx", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AddSolution(ctx, record.ID, "fix", "", nil); err != nil {
			t.Fatal(err)
		}
		plan, err := store.CreatePlan(ctx, project.ID, "plan-"+string(rune('a'+i)), "Plan", "goal")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave", 1); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"tasks", "errors", "solutions", "plans", "plan_waves"} {
		assertFreshUIDs(t, ctx, store, table, 2)
	}
}

func TestEventsCarryIncreasingHLCEnvelope(t *testing.T) {
	ctx, store, project := setupPlans(t)
	device, actor, err := store.SyncIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, project.ID, "task", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTaskEvent(ctx, project.ID, task.ID, "task.note", "body", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "comment", "human", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, project.ID, "system.note", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source: domain.ActivitySourceTUI, Entrypoint: "tasks.create_intent", Operation: "op",
		ProjectID: project.ID, ProjectSlug: "project", ArgumentsJSON: `{}`, Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	count := assertEnvelopes(t, ctx, store, device, actor)
	if count < 5 {
		t.Fatalf("events = %d, want at least 5", count)
	}
	assertFreshUIDs(t, ctx, store, "events", 5)
}

func assertEnvelopes(t *testing.T, ctx context.Context, store *storeFixture, device, actor domain.UID) int {
	t.Helper()
	rows, err := store.db.QueryContext(ctx, `SELECT hlc, device_id, actor_id FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var last domain.HLC
	count := 0
	for rows.Next() {
		var hlc, dev, act string
		if err := rows.Scan(&hlc, &dev, &act); err != nil {
			t.Fatal(err)
		}
		stamp, err := domain.ParseHLC(hlc)
		if err != nil {
			t.Fatal(err)
		}
		if count > 0 && stamp.Compare(last) <= 0 {
			t.Fatalf("event %d hlc %s does not follow %s", count, stamp, last)
		}
		if dev != string(device) || act != string(actor) {
			t.Fatalf("event %d device/actor = %s/%s, want %s/%s", count, dev, act, device, actor)
		}
		last = stamp
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return count
}
