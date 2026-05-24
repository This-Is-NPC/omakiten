package layout

import "testing"

func TestSubtasksRowsStackedSubtractsFormAndSeparatorAndHeader(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, FormHeight: 10, OuterHeight: 40}
	// 40 - 10 (form) - 1 (sep) - 3 (subtasks header) = 26.
	if got := b.SubtasksRows(); got != 26 {
		t.Fatalf("SubtasksRows stacked = %d, want 26", got)
	}
}

func TestSubtasksRowsSideBySideSubtractsFormAndHeader(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 10, OuterHeight: 30}
	// 30 - 10 - 3 = 17 (no separator in JoinVertical).
	if got := b.SubtasksRows(); got != 17 {
		t.Fatalf("SubtasksRows side-by-side = %d, want 17", got)
	}
}

func TestSubtasksRowsFloorsAtZero(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, FormHeight: 50, OuterHeight: 20}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows underflow = %d, want 0 floor", got)
	}
}

func TestActivityRowsStackedAccountsForSiblings(t *testing.T) {
	// outer 40 - form 10 - sep 1 - subtasks 12 - sep 1 - activity-header 2 = 14
	b := TaskViewBudget{Kind: Stacked, FormHeight: 10, OuterHeight: 40}
	if got := b.ActivityRows(12); got != 14 {
		t.Fatalf("ActivityRows stacked = %d, want 14", got)
	}
}

func TestActivityRowsSideBySideCapsAtLeftColumnHeight(t *testing.T) {
	// Left height = form 10 + subtasks 12 = 22. Activity ≤ 22 - 2 (header) = 20.
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 10, OuterHeight: 50}
	if got := b.ActivityRows(12); got != 20 {
		t.Fatalf("ActivityRows side-by-side = %d, want 20", got)
	}
}

func TestActivityRowsFloorsAtZero(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, FormHeight: 1, OuterHeight: 50}
	if got := b.ActivityRows(0); got != 0 {
		// 1 + 0 - 2 = -1 → floor 0.
		t.Fatalf("ActivityRows underflow = %d, want 0", got)
	}
}

func TestActivityRowsStackedShrinksAsSubtasksGrows(t *testing.T) {
	// Sanity: as subtasks consumes more rows, activity gets less. The
	// bug class this kills lives in surfaces that ignored the
	// composition and let activity claim the whole column.
	b := TaskViewBudget{Kind: Stacked, FormHeight: 10, OuterHeight: 40}
	small := b.ActivityRows(5)
	big := b.ActivityRows(15)
	if big >= small {
		t.Fatalf("ActivityRows non-monotonic: subtasks=5 → %d, subtasks=15 → %d (want big < small)", small, big)
	}
}

func TestSubtasksRowsUnknownKindReturnsZero(t *testing.T) {
	b := TaskViewBudget{Kind: Kind(99), FormHeight: 10, OuterHeight: 40}
	if got := b.SubtasksRows(); got != 0 {
		t.Fatalf("SubtasksRows unknown kind = %d, want 0", got)
	}
	if got := b.ActivityRows(5); got != 0 {
		t.Fatalf("ActivityRows unknown kind = %d, want 0", got)
	}
}
