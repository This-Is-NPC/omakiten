package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigration023PlansSchema(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "okt.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	for _, table := range []string{"plans", "plan_waves"} {
		var name string
		if err := store.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name); err != nil {
			t.Fatalf("table %q not present: %v", table, err)
		}
	}

	for _, col := range []string{"plan_id", "wave_id", "assigned_to"} {
		var cnt int
		if err := store.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM pragma_table_info('tasks') WHERE name = ?`, col,
		).Scan(&cnt); err != nil {
			t.Fatalf("pragma_table_info tasks: %v", err)
		}
		if cnt != 1 {
			t.Fatalf("tasks column %q missing", col)
		}
	}

	var idx string
	if err := store.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tasks_plan_wave'`,
	).Scan(&idx); err != nil {
		t.Fatalf("idx_tasks_plan_wave missing: %v", err)
	}
}

func TestMigration023CascadeAndSetNullOnPlanDelete(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "okt.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed a project so the plans.project_id FK is satisfied.
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO projects(name, slug, root_path) VALUES ('P', 'p', '/p')`,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	planResult, err := store.db.ExecContext(ctx,
		`INSERT INTO plans(project_id, slug, name) VALUES (1, 'plan-a', 'Plan A')`,
	)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	planID, _ := planResult.LastInsertId()

	waveResult, err := store.db.ExecContext(ctx,
		`INSERT INTO plan_waves(plan_id, name, position) VALUES (?, 'w1', 1)`, planID,
	)
	if err != nil {
		t.Fatalf("insert wave: %v", err)
	}
	waveID, _ := waveResult.LastInsertId()

	// Insert a task that references the plan + wave.
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, priority_id, plan_id, wave_id)
VALUES (1, 1, 'T1', 1, ?, ?)`, planID, waveID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, planID); err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	var waveCount int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM plan_waves WHERE id = ?`, waveID,
	).Scan(&waveCount); err != nil {
		t.Fatalf("count waves: %v", err)
	}
	if waveCount != 0 {
		t.Fatalf("plan_waves not cascaded; row count = %d", waveCount)
	}

	var planLink, waveLink any
	if err := store.db.QueryRowContext(ctx,
		`SELECT plan_id, wave_id FROM tasks WHERE id = 1`,
	).Scan(&planLink, &waveLink); err != nil {
		t.Fatalf("select task: %v", err)
	}
	if planLink != nil {
		t.Fatalf("tasks.plan_id = %v, want NULL after plan delete", planLink)
	}
	if waveLink != nil {
		t.Fatalf("tasks.wave_id = %v, want NULL after plan delete", waveLink)
	}
}
