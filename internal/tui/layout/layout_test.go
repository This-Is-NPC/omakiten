package layout

import "testing"

func TestSubtasksRowsSideBySideSubtractsFormHeaderAndBorders(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 10, OuterHeight: 30}
	// 30 - 10 (form) - 3 (subtasks header) - 2 (panel borders) = 15.
	if got := b.SubtasksRows(); got != 15 {
		t.Fatalf("SubtasksRows side-by-side = %d, want 15", got)
	}
}

func TestSubtasksRowsSideBySideFloorsAtZero(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 50, OuterHeight: 20}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows underflow = %d, want 0 floor", got)
	}
}

func TestStackedSplitNeutralFocusIsRoughlyEven(t *testing.T) {
	// leftover = 40 - 10 (form) - 2 (seps) - 3 (sub-hdr) - 2 (act-hdr) - 4 (2 panel borders) = 19.
	// FocusNone → subPct=50 → sub=9, act=10.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusNone, FormHeight: 10, OuterHeight: 40}
	if got, want := b.SubtasksRows(), 9; got != want {
		t.Fatalf("SubtasksRows neutral focus = %d, want %d", got, want)
	}
	if got, want := b.ActivityRows(0), 10; got != want {
		t.Fatalf("ActivityRows neutral focus = %d, want %d", got, want)
	}
}

func TestStackedSplitSubtasksFocusGivesSubtasksMajority(t *testing.T) {
	// leftover = 19, subPct=65 → sub=12, act=7.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusSubtasks, FormHeight: 10, OuterHeight: 40}
	if got, want := b.SubtasksRows(), 12; got != want {
		t.Fatalf("SubtasksRows subtasks-focus = %d, want %d", got, want)
	}
	if got, want := b.ActivityRows(0), 7; got != want {
		t.Fatalf("ActivityRows subtasks-focus = %d, want %d", got, want)
	}
}

func TestStackedSplitActivityFocusGivesActivityMajority(t *testing.T) {
	// leftover = 19, subPct=35 → sub=6, act=13. Both above their mins → no
	// reallocation kicks in.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusActivity, FormHeight: 10, OuterHeight: 40}
	if got, want := b.SubtasksRows(), 6; got != want {
		t.Fatalf("SubtasksRows activity-focus = %d, want %d", got, want)
	}
	if got, want := b.ActivityRows(0), 13; got != want {
		t.Fatalf("ActivityRows activity-focus = %d, want %d", got, want)
	}
}

func TestStackedSplitMinsEnforced(t *testing.T) {
	// Tight leftover where the activity-focus 35/65 split would
	// underflow sub-tasks below SubtasksMinRows=4. The reallocator
	// has to top sub-tasks up at activity's expense.
	// leftover = 22 - 10 - 2 - 3 - 2 - 4 = 1. Even 50/50 underflows
	// both sides; both get topped up to their mins and the donor
	// runs negative → floored to 0.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusActivity, FormHeight: 10, OuterHeight: 22}
	sub := b.SubtasksRows()
	act := b.ActivityRows(0)
	if sub < 0 || act < 0 {
		t.Fatalf("negative budget: sub=%d act=%d", sub, act)
	}
	if sub == 0 && act == 0 {
		t.Fatalf("both shares hit zero — min reallocator should not double-floor when leftover>0 (leftover=1)")
	}
}

func TestStackedSplitTinyTerminalReturnsZero(t *testing.T) {
	// leftover negative → both shares 0.
	b := TaskViewBudget{Kind: Stacked, FormHeight: 50, OuterHeight: 20}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows underflow = %d, want 0", got)
	}
	if got := b.ActivityRows(0); got != 0 {
		t.Fatalf("ActivityRows underflow = %d, want 0", got)
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
	// shrink to fit the outer viewport, not the left column —
	// otherwise applyTaskViewScroll chops the bottom of the rail and
	// the focused card disappears (the resize-regression case).
	// Activity ≤ min(23, 19) - 4 = 15.
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

func TestStackedSplitIgnoresSubtasksHeightArg(t *testing.T) {
	// The pre-W12 design used the measured subtasks panel height to
	// compute activity's share. The new joint split is independent
	// of that argument — pass two different values and assert the
	// activity budget does not move. The argument is kept in the
	// signature only so the side-by-side branch can still cap by
	// the left column's height.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusNone, FormHeight: 10, OuterHeight: 40}
	if got := b.ActivityRows(5); got != b.ActivityRows(15) {
		t.Fatalf("stacked ActivityRows depends on subtasksHeight: 5→%d vs 15→%d", b.ActivityRows(5), b.ActivityRows(15))
	}
}

func TestStackedSplitFocusedPanelGetsMore(t *testing.T) {
	// The focus signal must move rows from one panel to the other —
	// not just leave both at 50/50. Compare subtasks-focused vs
	// activity-focused budgets on the same leftover.
	bSub := TaskViewBudget{Kind: Stacked, Focus: FocusSubtasks, FormHeight: 10, OuterHeight: 40}
	bAct := TaskViewBudget{Kind: Stacked, Focus: FocusActivity, FormHeight: 10, OuterHeight: 40}
	if bSub.SubtasksRows() <= bAct.SubtasksRows() {
		t.Fatalf("subtasks-focus did not give sub-tasks more rows: sub-focus=%d act-focus=%d", bSub.SubtasksRows(), bAct.SubtasksRows())
	}
	if bAct.ActivityRows(0) <= bSub.ActivityRows(0) {
		t.Fatalf("activity-focus did not give activity more rows: act-focus=%d sub-focus=%d", bAct.ActivityRows(0), bSub.ActivityRows(0))
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
