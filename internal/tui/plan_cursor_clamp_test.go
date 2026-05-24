package tui

import (
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/cursorwindow"
)

// TestPlanCursorClampsAfterDelete pins the W11-extended migration:
// deleting the cursored plan must land the cursor on the new last
// row, not strand it past the end. clampPlanCursor routes through
// cursorwindow.WithItemCount, whose internal resync clamps the
// cursor to n-1 when shrinking past it.
//
// Pre-cursorwindow shape: callers wrote `m.planCursor = len(plans)-1`
// inline, which a fraction of them forgot — the cursor would stay
// at idx 4 even after refresh trimmed the slice to 4 entries, and
// the next render would index out of bounds (or render the wrong
// row). The test would have caught the inline-clamp regression by
// asserting cursor == 3 after the shrink. With the cursorwindow
// migration that bug class is unrepresentable — WithItemCount
// always re-clamps.
func TestPlanCursorClampsAfterDelete(t *testing.T) {
	var m Model
	m.plansCursor = cursorwindow.New(20)

	// Seed five plans, cursor on the last one (idx 4).
	m.plans = []app.PlanRollup{
		{Plan: domain.Plan{Slug: "p0"}},
		{Plan: domain.Plan{Slug: "p1"}},
		{Plan: domain.Plan{Slug: "p2"}},
		{Plan: domain.Plan{Slug: "p3"}},
		{Plan: domain.Plan{Slug: "p4"}},
	}
	m.plansCursor = m.plansCursor.WithItemCount(len(m.plans)).SetCursor(4)
	if got := m.plansCursor.Cursor(); got != 4 {
		t.Fatalf("setup: cursor = %d, want 4", got)
	}

	// Delete the active (last) plan — drop the slice to 4 entries
	// and re-run clampPlanCursor (the same helper refresh() chains
	// after PlanService.ListRollups returns the new slice).
	m.plans = m.plans[:4]
	m.clampPlanCursor()

	if got := m.plansCursor.Cursor(); got != 3 {
		t.Fatalf("after delete: cursor = %d, want 3 (new last)", got)
	}

	// Drop to zero entries — cursor + scroll both pin at 0.
	m.plans = nil
	m.clampPlanCursor()
	if got := m.plansCursor.Cursor(); got != 0 {
		t.Fatalf("after empty: cursor = %d, want 0", got)
	}
	if got := m.plansCursor.Scroll(); got != 0 {
		t.Fatalf("after empty: scroll = %d, want 0", got)
	}
}
