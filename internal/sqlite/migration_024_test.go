package sqlite

import (
	"testing"

	"omakiten/internal/domain"
)

func TestMigration024PlanFTSBackfillAndTriggers(t *testing.T) {
	ctx, store, project := setupPlans(t)

	// Insert a plan AFTER the migrations run; the AI trigger must add it.
	plan, err := store.CreatePlan(ctx, project.ID, "search-me", "Search Me", "alpha bravo charlie")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	hits, err := store.Search(ctx, "bravo", project.ID, []domain.SearchEntityType{domain.SearchEntityPlan})
	if err != nil {
		t.Fatalf("Search(bravo): %v", err)
	}
	if len(hits) != 1 || hits[0].ID != plan.ID || hits[0].EntityType != domain.SearchEntityPlan {
		t.Fatalf("Search(bravo) = %+v, want one plan hit on %d", hits, plan.ID)
	}

	// Delete and confirm the AD trigger removes the index row.
	if _, err := store.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, plan.ID); err != nil {
		t.Fatalf("DELETE plan: %v", err)
	}
	hits, err = store.Search(ctx, "bravo", project.ID, []domain.SearchEntityType{domain.SearchEntityPlan})
	if err != nil {
		t.Fatalf("Search(bravo) post-delete: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("Search(bravo) post-delete = %+v, want empty", hits)
	}
}

func TestMigration024PlanFTSUpdateRebuildsIndex(t *testing.T) {
	ctx, store, project := setupPlans(t)

	plan, err := store.CreatePlan(ctx, project.ID, "evolve", "Evolve", "first body")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Mutate name + goal_body directly so the AU trigger runs.
	if _, err := store.db.ExecContext(ctx,
		`UPDATE plans SET name = ?, goal_body = ? WHERE id = ?`,
		"Evolved", "second body delta", plan.ID,
	); err != nil {
		t.Fatalf("UPDATE plan: %v", err)
	}

	// Old body should no longer hit; new body must.
	hitsOld, err := store.Search(ctx, "first", project.ID, []domain.SearchEntityType{domain.SearchEntityPlan})
	if err != nil {
		t.Fatalf("Search(first): %v", err)
	}
	if len(hitsOld) != 0 {
		t.Fatalf("Search(first) post-update = %+v, want empty (AU trigger should drop stale row)", hitsOld)
	}
	hitsNew, err := store.Search(ctx, "delta", project.ID, []domain.SearchEntityType{domain.SearchEntityPlan})
	if err != nil {
		t.Fatalf("Search(delta): %v", err)
	}
	if len(hitsNew) != 1 || hitsNew[0].ID != plan.ID {
		t.Fatalf("Search(delta) = %+v, want one plan hit on %d", hitsNew, plan.ID)
	}
}
