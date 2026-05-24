// Package layout centralises per-section viewport budgets for
// composite TUI screens. The bug class it closes: the task detail
// view stacks three panels (form / sub-tasks / activity), but each
// panel used to compute its own row budget from m.height as if it
// owned the whole vertical column. The result was the activity
// panel asking for m.height-9 rows in a stacked layout where the
// form already consumed ~10 rows above it — total content
// overflowed the outer viewport.
//
// The fix lives in this package: a single TaskViewBudget value
// computed once per render that hands each section its correct row
// allocation. The panels stop guessing; the layout struct is the
// source of truth.
//
// Lives outside `tui` for the same reason scrollwindow does: it has
// to be importable by viewport sub-components (cardlist /
// linelist consumers) without dragging the parent package and its
// dependency graph along.
package layout

// Kind enumerates the packing shapes the task detail view can take.
// Mirrors the existing taskViewLayoutKind in render_task.go — kept
// as its own type here so the layout package owns the contract.
type Kind int

const (
	// Stacked stacks form + sub-tasks + activity vertically as
	// full-width boxes. Used when the terminal is too narrow to
	// carve out a right rail for activity.
	Stacked Kind = iota
	// SideBySide packs form + sub-tasks stacked in a left column
	// with activity in a right rail.
	SideBySide
)

// SubtasksHeader is the row cost of the sub-tasks panel chrome
// above the first card: kicker (1) + rule (1) + leading blank (1).
// Encoded as a package constant so callers don't have to remember
// the magic number when they ask for SubtasksRows.
const SubtasksHeader = 3

// ActivityHeader is the row cost of the activity panel chrome above
// the first card line: kicker (1) + leading blank inside the box
// before the cards begin (1). Match this against the prior
// activityChromeBase accounting in render_activity.go.
const ActivityHeader = 2

// Separator is the blank-line cost of joining stacked sections via
// "\n\n" — one blank row between each adjacent pair.
const Separator = 1

// TaskViewBudget is the input contract for per-section row budgets
// in the task detail view. Computed once per render at the top of
// renderTaskView; consumed by sub-tasks and activity panels via
// SubtasksRows / ActivityRows so neither panel reads m.height on
// its own.
//
// FormHeight is the measured row count of the rendered form box
// (read through taskDetailsBoxHeightCache). OuterHeight is the row
// budget the outer detail-view slice has after subtracting screen
// chrome (header / footer / status / leading blank).
type TaskViewBudget struct {
	Kind        Kind
	FormHeight  int
	OuterHeight int
}

// SubtasksRows returns the row budget the sub-tasks panel body
// (card list inside the chrome) has after accounting for the form
// box and the separator. In stacked layout the form sits above the
// panel; in side-by-side the form box is the panel's vertical
// sibling within the left column. Both cases subtract the form
// height and the sub-tasks chrome.
//
// Floors at 0 — callers fall back to "render every card and let
// outer slicing chop" on tiny terminals.
func (b TaskViewBudget) SubtasksRows() int {
	switch b.Kind {
	case Stacked:
		// outer = form + sep + (header + cards) + sep + (activity)
		// sub-tasks body = outer - form - sep - subtasks-header
		// (activity row budget is computed separately, not
		// pre-deducted here, so sub-tasks can claim what's left
		// regardless of how activity ends up sized; the outer
		// scroll handles overflow when the user expands cards).
		budget := b.OuterHeight - b.FormHeight - Separator - SubtasksHeader
		if budget < 0 {
			return 0
		}
		return budget
	case SideBySide:
		// In side-by-side the sub-tasks panel sits directly under
		// the form inside the left column. Outer height is the
		// column's; subtract form + header. No separator — the two
		// boxes are JoinVertical'd, not "\n\n"-joined.
		budget := b.OuterHeight - b.FormHeight - SubtasksHeader
		if budget < 0 {
			return 0
		}
		return budget
	}
	return 0
}

// ActivityRows returns the row budget the activity panel body has.
// In stacked layout the activity reads as "what's left after form
// + sub-tasks + their separators + activity-header"; this is the
// bug-2 fix — the panel no longer thinks it owns the whole
// terminal column.
//
// In side-by-side the activity rail caps at the left column's
// height (so the panel never grows taller than its visual sibling
// and the outer JoinHorizontal stays aligned).
//
// SubtasksHeight is the measured row count of the rendered sub-
// tasks panel including chrome; pass 0 when sub-tasks is not yet
// measured (early render path before the panel has assembled) —
// the activity then takes the whole leftover column. The caller
// is responsible for re-rendering with the measured value on the
// next pass when accuracy matters.
func (b TaskViewBudget) ActivityRows(subtasksHeight int) int {
	switch b.Kind {
	case Stacked:
		// outer = form + sep + subtasks + sep + activity
		// activity body = outer - form - sep - subtasks - sep - activity-header
		budget := b.OuterHeight - b.FormHeight - Separator - subtasksHeight - Separator - ActivityHeader
		if budget < 0 {
			return 0
		}
		return budget
	case SideBySide:
		// activity height ≤ left column height (form + subtasks
		// stack). Cap so the panel cannot grow taller than the
		// left rail; the outer slicer never has to chop a runaway
		// activity column.
		leftHeight := b.FormHeight + subtasksHeight
		budget := leftHeight - ActivityHeader
		if budget < 0 {
			return 0
		}
		return budget
	}
	return 0
}
