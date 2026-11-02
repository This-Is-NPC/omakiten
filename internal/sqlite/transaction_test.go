package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"omakiten/internal/domain"
)

func TestWorkTransactionPublishesOnlyDurableEvents(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	sink := attachBusSink(t, store)
	ctx := context.Background()
	project, err := store.UpsertProject(ctx, "Example", "example", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before := len(sink.snapshot())
	abort := errors.New("abort work")
	err = store.WithinTransaction(ctx, func(ctx context.Context) error {
		plan, err := store.CreatePlan(ctx, project.ID, "atomic", "Atomic", "")
		if err != nil {
			return err
		}
		if _, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Foundation", 1); err != nil {
			return err
		}
		if len(sink.snapshot()) != before {
			t.Fatal("uncommitted events were published")
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("rollback error: %v", err)
	}
	plans, err := store.ListPlans(ctx, project.ID)
	if err != nil || len(plans) != 0 || len(sink.snapshot()) != before {
		t.Fatalf("rollback leaked work: %v, %v", plans, err)
	}
	err = store.WithinTransaction(ctx, func(ctx context.Context) error {
		_, err := store.CreatePlan(ctx, project.ID, "atomic", "Atomic", "")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := sink.snapshot()
	if len(got) != before+1 || got[len(got)-1].EventType != domain.EventTypePlanCreated {
		t.Fatalf("committed events: %v", got)
	}
}

func TestReadTransactionDoesNotBlockWriter(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	project, err := store.UpsertProject(ctx, "Example", "example", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = store.WithinReadTransaction(ctx, func(ctx context.Context) error {
		var n int
		if err := store.query(ctx).QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&n); err != nil {
			return err
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := store.db.ExecContext(writeCtx, `UPDATE projects SET description = 'written' WHERE id = ?`, project.ID); err != nil {
			t.Errorf("writer blocked by read transaction: %v", err)
		}
		if _, err := store.query(ctx).ExecContext(ctx, `UPDATE projects SET description = 'x' WHERE id = ?`, project.ID); err == nil {
			t.Error("read transaction accepted a write")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
