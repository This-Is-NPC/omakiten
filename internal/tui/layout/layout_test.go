package layout

import "testing"

func TestSubtasksRowsStackedSinglePaneTakesFullOuter(t *testing.T) {
	// Stacked is single-pane: the focused panel owns the outer
	// viewport. Sub-tasks budget = OuterH - SubtasksHeader (3) -
	// PanelBorders (2) = 40 - 5 = 35.
	b := TaskViewBudget{Kind: Stacked, FormHeight: 10, OuterHeight: 40}
	if got, want := b.SubtasksRows(), 35; got != want {
		t.Fatalf("SubtasksRows stacked = %d, want %d", got, want)
	}
}

func TestSubtasksRowsStackedIgnoresFormHeight(t *testing.T) {
	// Single-pane: form isn't rendered alongside sub-tasks, so its
	// height does not eat into the sub-tasks budget. Two budgets
	// with different FormHeights must produce the same SubtasksRows.
	a := TaskViewBudget{Kind: Stacked, FormHeight: 1, OuterHeight: 40}
	b := TaskViewBudget{Kind: Stacked, FormHeight: 30, OuterHeight: 40}
	if a.SubtasksRows() != b.SubtasksRows() {
		t.Fatalf("stacked SubtasksRows depends on FormHeight: 1→%d vs 30→%d", a.SubtasksRows(), b.SubtasksRows())
	}
}

func TestSubtasksRowsSideBySideSubtractsFormHeaderAndBorders(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 10, OuterHeight: 30}
	// 30 - 10 (form) - 3 (subtasks header) - 2 (panel borders) = 15.
	if got := b.SubtasksRows(); got != 15 {
		t.Fatalf("SubtasksRows side-by-side = %d, want 15", got)
	}
}

func TestSubtasksRowsFloorsAtZero(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 50, OuterHeight: 20}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows underflow = %d, want 0 floor", got)
	}
}

func TestActivityRowsStackedSinglePaneTakesFullOuter(t *testing.T) {
	// Stacked single-pane: ActivityRows = OuterH - ActivityHeader -
	// PanelBorders = 40 - 4 = 36. The subtasksHeight arg is
	// irrelevant in stacked.
	b := TaskViewBudget{Kind: Stacked, FormHeight: 10, OuterHeight: 40}
	if got, want := b.ActivityRows(99), 36; got != want {
		t.Fatalf("ActivityRows stacked = %d, want %d", got, want)
	}
}

func TestActivityRowsStackedIgnoresSubtasksHeightArg(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, FormHeight: 10, OuterHeight: 40}
	if b.ActivityRows(5) != b.ActivityRows(15) {
		t.Fatalf("stacked ActivityRows depends on subtasksHeight: 5→%d vs 15→%d", b.ActivityRows(5), b.ActivityRows(15))
	}
}

func TestActivityRowsSideBySideCapsAtLeftColumnHeight(t *testing.T) {
	// Left height = form 10 + subtasks 12 = 22. Outer 50 is generous,
	// so the cap is the left column. Activity ≤ 22 - 2 (header) - 2
	// (borders) = 18.
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 10, OuterHeight: 50}
	if got := b.ActivityRows(12); got != 18 {
		t.Fatalf("ActivityRows side-by-side = %d, want 18", got)
	}
}

func TestActivityRowsSideBySideCapsAtOuterViewportToo(t *testing.T) {
	// Left height = form 18 + subtasks 5 = 23 > outer 19. The left
	// column would exceed the outer slice, so the activity rail must
	// shrink to fit the outer viewport. Activity ≤ min(23, 19) - 4 = 15.
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 18, OuterHeight: 19}
	if got := b.ActivityRows(5); got != 15 {
		t.Fatalf("ActivityRows side-by-side outer-capped = %d, want 15", got)
	}
}

func TestActivityRowsSideBySideFloorsAtZero(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 1, OuterHeight: 50}
	if got := b.ActivityRows(0); got != 0 {
		t.Fatalf("ActivityRows underflow = %d, want 0", got)
	}
}

func TestStackedTinyTerminalReturnsZero(t *testing.T) {
	// OuterHeight smaller than the per-panel chrome → both budgets
	// floor to 0. Real renderers fall back to "render whatever fits
	// and let outer slicing chop" in that pathological case.
	b := TaskViewBudget{Kind: Stacked, FormHeight: 0, OuterHeight: 3}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows tiny = %d, want 0", got)
	}
	if got := b.ActivityRows(0); got != 0 {
		t.Fatalf("ActivityRows tiny = %d, want 0", got)
	}
}

func TestUnknownKindReturnsZero(t *testing.T) {
	b := TaskViewBudget{Kind: Kind(99), FormHeight: 10, OuterHeight: 40}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows unknown kind = %d, want 0", got)
	}
	if got := b.ActivityRows(5); got != 0 {
		t.Fatalf("ActivityRows unknown kind = %d, want 0", got)
	}
}
