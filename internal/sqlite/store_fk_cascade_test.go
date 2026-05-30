package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"omakiten/internal/domain"
)

// TestForeignKeysEnforcedOnEveryConnection probes the FK contract on a
// SECOND pooled connection. modernc.org/sqlite defaults foreign_keys
// OFF on every new connection, so a single `PRAGMA foreign_keys = ON`
// issued once at Open only protects whichever connection happened to
// run it. This test pins one connection busy so the probe is forced
// onto a freshly opened second connection, then asserts that one
// reports foreign_keys = 1. Fails on the pre-fix single-conn pragma.
func TestForeignKeysEnforcedOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/fk.db")

	// Pin connection #1 busy with an open transaction so the next
	// db.Conn() is forced to materialise a brand-new connection.
	busyConn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin first connection: %v", err)
	}
	defer func() { _ = busyConn.Close() }()
	busyTx, err := busyConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holding tx: %v", err)
	}
	defer func() { _ = busyTx.Rollback() }()

	// Second connection — must independently report FK enforcement ON.
	coldConn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("open second connection: %v", err)
	}
	defer func() { _ = coldConn.Close() }()

	var fk int64
	if err := coldConn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys on second connection: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys on second connection = %d, want 1 "+
			"(FK enforcement must apply to EVERY pooled connection)", fk)
	}
}

// TestDeletePlanCascadesOnColdConnection is the regression that fails
// on the single-conn pragma: it forces DeletePlan's mutation onto a
// cold/second pooled connection (by pinning conn #1 busy) and asserts
// the FK cascade actually fires — plan_waves rows are deleted and the
// task's plan_id/wave_id are nulled. With FK OFF on the second conn,
// the cascade silently no-ops and the waves/links survive.
func TestDeletePlanCascadesOnColdConnection(t *testing.T) {
	ctx, store, project := setupPlans(t)

	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan: %v", err)
	}

	// Pin connection #1 busy so DeletePlan's transaction is forced onto
	// a second, freshly opened connection.
	busyConn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin first connection: %v", err)
	}
	defer func() { _ = busyConn.Close() }()
	busyTx, err := busyConn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holding tx: %v", err)
	}
	defer func() { _ = busyTx.Rollback() }()

	if _, err := store.DeletePlan(ctx, project.ID, plan.ID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	// Release the pinned connection so the assertion reads see latest.
	_ = busyTx.Rollback()
	_ = busyConn.Close()

	var waveCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_waves WHERE plan_id = ?`, plan.ID).Scan(&waveCount); err != nil {
		t.Fatalf("count plan_waves: %v", err)
	}
	if waveCount != 0 {
		t.Fatalf("plan_waves after DeletePlan = %d, want 0 (FK cascade must delete waves)", waveCount)
	}

	var planID, waveID sql.NullInt64
	if err := store.db.QueryRowContext(ctx, `SELECT plan_id, wave_id FROM tasks WHERE id = ?`, task.ID).Scan(&planID, &waveID); err != nil {
		t.Fatalf("scan task FKs: %v", err)
	}
	if planID.Valid || waveID.Valid {
		t.Fatalf("task plan_id=%v wave_id=%v after DeletePlan, want both NULL (FK SET NULL)", planID, waveID)
	}
}
