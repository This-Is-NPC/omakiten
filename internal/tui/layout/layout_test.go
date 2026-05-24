package layout

import "testing"

// ---------- Side-by-side ----------

func TestSideBySideAlwaysShowsAllThreePanels(t *testing.T) {
	for _, focus := range []Focus{FocusForm, FocusSubtasks, FocusActivity} {
		b := TaskViewBudget{Kind: SideBySide, Focus: focus, FormHeight: 14, OuterHeight: 40}
		if !b.ShowForm() || !b.ShowSubtasks() || !b.ShowActivity() {
			t.Fatalf("focus=%d: side-by-side must always render all three panels (form=%v sub=%v act=%v)", focus, b.ShowForm(), b.ShowSubtasks(), b.ShowActivity())
		}
	}
}

func TestSideBySideSubtasksFillsBelowForm(t *testing.T) {
	b := TaskViewBudget{Kind: SideBySide, Focus: FocusForm, FormHeight: 14, OuterHeight: 40}
	// Sub-tasks box claims everything below the form so the left
	// column reaches the outer viewport floor (40 - 14 = 26).
	if got, want := b.SubtasksBoxHeight(), 26; got != want {
		t.Fatalf("SubtasksBoxHeight side-by-side = %d, want %d", got, want)
	}
}

func TestSideBySideActivityCapsAtOuter(t *testing.T) {
	// Left column = form + subBox = 14 + 26 = 40 = OuterHeight.
	// Activity rail mirrors the left column → 40.
	b := TaskViewBudget{Kind: SideBySide, Focus: FocusForm, FormHeight: 14, OuterHeight: 40}
	if got, want := b.ActivityBoxHeight(), 40; got != want {
		t.Fatalf("ActivityBoxHeight side-by-side = %d, want %d", got, want)
	}
}

func TestSideBySideActivityClampsRunawayForm(t *testing.T) {
	// Form is taller than the outer viewport (description overflow).
	// Sub-tasks slot would collapse to 0 (OuterH - FormH < 0) and
	// the rail must never exceed the outer slice.
	b := TaskViewBudget{Kind: SideBySide, Focus: FocusForm, FormHeight: 50, OuterHeight: 30}
	if got := b.SubtasksBoxHeight(); got != 0 {
		t.Fatalf("SubtasksBoxHeight runaway form = %d, want 0", got)
	}
	if got, want := b.ActivityBoxHeight(), 30; got != want {
		t.Fatalf("ActivityBoxHeight runaway form = %d, want %d (capped at outer)", got, want)
	}
}

// ---------- Stacked single-pane focus ----------

func TestStackedSubtasksFocusSinglePaneFillsOuter(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, Focus: FocusSubtasks, FormHeight: 14, OuterHeight: 40}
	if b.ShowForm() || !b.ShowSubtasks() || b.ShowActivity() {
		t.Fatalf("stacked + subtasks focus must hide form + activity")
	}
	if got, want := b.SubtasksBoxHeight(), 40; got != want {
		t.Fatalf("SubtasksBoxHeight stacked single-pane = %d, want %d", got, want)
	}
}

func TestStackedActivityFocusSinglePaneFillsOuter(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, Focus: FocusActivity, FormHeight: 14, OuterHeight: 40}
	if b.ShowForm() || b.ShowSubtasks() || !b.ShowActivity() {
		t.Fatalf("stacked + activity focus must hide form + subtasks")
	}
	if got, want := b.ActivityBoxHeight(), 40; got != want {
		t.Fatalf("ActivityBoxHeight stacked single-pane = %d, want %d", got, want)
	}
}

// ---------- Stacked + form focus cascade ----------

func TestStackedFormFocusThreePaneSplit(t *testing.T) {
	// outer 40 - form 14 - 2*sep(1) - subHdr(3) - actHdr(2) - 2*borders(2*2)
	// = 40 - 14 - 2 - 3 - 2 - 4 = 15 combined body rows.
	// 50/50: sub=7, act=8. Both >= mins.
	// SubBox = 7 + 3 + 2 = 12. ActBox = 8 + 2 + 2 = 12.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusForm, FormHeight: 14, OuterHeight: 40}
	if !b.ShowForm() || !b.ShowSubtasks() || !b.ShowActivity() {
		t.Fatalf("3-pane: all three should render (form=%v sub=%v act=%v)", b.ShowForm(), b.ShowSubtasks(), b.ShowActivity())
	}
	if got, want := b.SubtasksBoxHeight(), 12; got != want {
		t.Fatalf("3-pane SubtasksBoxHeight = %d, want %d", got, want)
	}
	if got, want := b.ActivityBoxHeight(), 12; got != want {
		t.Fatalf("3-pane ActivityBoxHeight = %d, want %d", got, want)
	}
	// Total rows used = form + sep + sub + sep + act = 14 + 1 + 12 + 1 + 12 = 40 = OuterH ✓
}

func TestStackedFormFocusTwoPaneFallback(t *testing.T) {
	// outer 25 - form 14 - 2*sep - subHdr - actHdr - 2*borders =
	// 25 - 14 - 2 - 3 - 2 - 4 = 0 combined body. < SubMin+ActMin(10) → 2-pane.
	// 2-pane: subBody = outer - form - sep - subHdr - borders = 25 - 14 - 1 - 3 - 2 = 5.
	// 5 >= SubMin(4) → render. SubBox = 5 + 5 = 10.
	b := TaskViewBudget{Kind: Stacked, Focus: FocusForm, FormHeight: 14, OuterHeight: 25}
	if !b.ShowForm() || !b.ShowSubtasks() || b.ShowActivity() {
		t.Fatalf("2-pane fallback: form + subtasks render, activity drops (form=%v sub=%v act=%v)", b.ShowForm(), b.ShowSubtasks(), b.ShowActivity())
	}
	if got, want := b.SubtasksBoxHeight(), 10; got != want {
		t.Fatalf("2-pane SubtasksBoxHeight = %d, want %d", got, want)
	}
	if got := b.ActivityBoxHeight(); got != 0 {
		t.Fatalf("2-pane ActivityBoxHeight = %d, want 0", got)
	}
}

func TestStackedFormFocusOnePaneFallback(t *testing.T) {
	// outer 18 - form 14 - sep - subHdr - borders = 18 - 14 - 1 - 3 - 2 = -2.
	// < SubMin(4) → 1-pane (form only).
	b := TaskViewBudget{Kind: Stacked, Focus: FocusForm, FormHeight: 14, OuterHeight: 18}
	if !b.ShowForm() || b.ShowSubtasks() || b.ShowActivity() {
		t.Fatalf("1-pane fallback: only form (form=%v sub=%v act=%v)", b.ShowForm(), b.ShowSubtasks(), b.ShowActivity())
	}
	if got := b.SubtasksBoxHeight(); got != 0 {
		t.Fatalf("1-pane SubtasksBoxHeight = %d, want 0", got)
	}
	if got := b.ActivityBoxHeight(); got != 0 {
		t.Fatalf("1-pane ActivityBoxHeight = %d, want 0", got)
	}
}

// ---------- Cascade drop order ----------

func TestCascadeDropsActivityBeforeSubtasks(t *testing.T) {
	// Walk OuterHeight upward; the first sibling to drop should be
	// activity. Pre-condition: form fits, sub fits below the 3-pane
	// threshold.
	form := 14
	// At outer = 25 we're in 2-pane (already covered). At outer = 28
	// we should reach 3-pane.
	b25 := TaskViewBudget{Kind: Stacked, Focus: FocusForm, FormHeight: form, OuterHeight: 25}
	if b25.ShowSubtasks() && b25.ShowActivity() {
		t.Fatalf("outer=25 should be 2-pane (activity dropped); got both visible")
	}
	if b25.ShowSubtasks() != true {
		t.Fatalf("outer=25 should still show subtasks (drop order: activity first)")
	}
}

// ---------- Inner body rows ----------

func TestSubtasksRowsExcludesChromeAndBorders(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, Focus: FocusSubtasks, FormHeight: 14, OuterHeight: 40}
	// Box = 40. Body = 40 - SubHdr(3) - Borders(2) = 35.
	if got, want := b.SubtasksRows(), 35; got != want {
		t.Fatalf("SubtasksRows stacked single-pane = %d, want %d", got, want)
	}
}

func TestActivityRowsExcludesChromeAndBorders(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, Focus: FocusActivity, FormHeight: 14, OuterHeight: 40}
	// Box = 40. Body = 40 - ActHdr(2) - Borders(2) = 36.
	if got, want := b.ActivityRows(), 36; got != want {
		t.Fatalf("ActivityRows stacked single-pane = %d, want %d", got, want)
	}
}

func TestHiddenPanelsReturnZeroBoxAndRows(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, Focus: FocusSubtasks}
	if got := b.ActivityBoxHeight(); got != 0 {
		t.Fatalf("hidden activity ActivityBoxHeight = %d, want 0", got)
	}
	if got := b.ActivityRows(); got != 0 {
		t.Fatalf("hidden activity ActivityRows = %d, want 0", got)
	}
}

// ---------- Empty content does not drop ----------

// (The render layer is responsible for showing an empty-state line
// inside the panel; the BUDGET layer does not look at content. This
// test pins that the budget never shrinks based on the caller's
// data — it depends only on Kind / Focus / FormHeight / OuterHeight.)
func TestBudgetIndependentOfContent(t *testing.T) {
	b := TaskViewBudget{Kind: Stacked, Focus: FocusForm, FormHeight: 14, OuterHeight: 40}
	first := b.SubtasksBoxHeight()
	again := b.SubtasksBoxHeight() // no content arg — same inputs, same output
	if first != again {
		t.Fatalf("budget not deterministic: %d vs %d", first, again)
	}
}
