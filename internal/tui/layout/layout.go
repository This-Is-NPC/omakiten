// Package layout centralises per-section viewport budgets for
// composite TUI screens.
//
// The bug class it closes: the task detail view stacks three panels
// (form / sub-tasks / activity), and each panel used to compute its
// own row budget from m.height as if it owned the whole vertical
// column. The result was a multi-way race where two panels both
// claimed the leftover space after the form box, with the activity
// panel reliably losing.
//
// The fix lives in this package: a single TaskViewBudget value
// computed once per render that hands each section its correct row
// allocation. The packing policy:
//
//   - Stacked: single-pane focus. The terminal is too narrow to
//     show both sub-tasks and activity side-by-side, so the
//     renderer picks ONE panel to fill the outer viewport based on
//     m.taskFocus. Each panel's row budget is the full outer height
//     minus its own chrome — no panel ever has to share.
//   - SideBySide: form + sub-tasks stack vertically on the left,
//     activity occupies a right rail. The sub-tasks panel locks to
//     OuterHeight - FormHeight so the left column reaches the outer
//     viewport floor and the activity rail (which mirrors the left
//     column's height) lines up with the bottom border.
//
// Lives outside `tui` for the same reason scrollwindow does: it has
// to be importable by viewport sub-components without dragging the
// parent package and its dependency graph along.
package layout

// Kind enumerates the packing shapes the task detail view can take.
// Mirrors the existing taskViewLayoutKind in render_task.go — kept
// as its own type here so the layout package owns the contract.
type Kind int

const (
	// Stacked renders one panel at a time, full outer height.
	// Picked when the terminal is too narrow to carve out a right
	// rail for activity; the renderer switches on m.taskFocus to
	// decide which panel to draw.
	Stacked Kind = iota
	// SideBySide packs form + sub-tasks stacked in a left column
	// with activity in a right rail.
	SideBySide
)

// SubtasksHeader is the row cost of the sub-tasks panel chrome
// above the first card: kicker (1) + rule (1) + leading blank (1).
const SubtasksHeader = 3

// ActivityHeader is the row cost of the activity panel chrome above
// the first card line: kicker (1) + leading blank inside the box
// before the cards begin (1).
const ActivityHeader = 2

// PanelBorders is the row cost of the top + bottom borders the
// kanban-column / fixed-box renderers wrap around each panel body.
const PanelBorders = 2

// TaskViewBudget is the input contract for per-section row budgets
// in the task detail view. Computed once per render; consumed by
// sub-tasks and activity panels via SubtasksRows / ActivityRows so
// neither panel reads m.height on its own.
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

// SubtasksRows returns the row budget the sub-tasks cardlist body
// has after accounting for panel chrome + borders.
//
// In stacked mode the panel is the only one rendered when focused,
// so it takes the full outer viewport. The cardlist viewport math
// stays valid regardless of focus — sync handlers can refresh the
// cardlist state even when activity is focused, and the budget
// reflects "what subtasks would render if focused right now".
//
// In side-by-side mode the panel sits inside the left column under
// the form box, so the budget is the column leftover after the form.
//
// Floors at 0 — callers fall back to "render every card and let
// outer slicing chop" on tiny terminals.
func (b TaskViewBudget) SubtasksRows() int {
	var budget int
	switch b.Kind {
	case Stacked:
		budget = b.OuterHeight - SubtasksHeader - PanelBorders
	case SideBySide:
		budget = b.OuterHeight - b.FormHeight - SubtasksHeader - PanelBorders
	}
	if budget < 0 {
		return 0
	}
	return budget
}

// ActivityRows returns the row budget the activity panel body has.
//
// In stacked mode the panel is the only one rendered when focused,
// so it takes the full outer viewport. The sub-tasks height
// argument is ignored (single-pane = no sibling to share with).
//
// In side-by-side mode the activity rail caps at min(leftHeight,
// outerHeight) - chrome — the right rail mirrors the left column's
// height but never exceeds the outer slice, otherwise
// applyTaskViewScroll would chop the bottom of the rail and the
// focused card could slide off-screen.
//
// SubtasksHeight is the measured row count of the rendered
// sub-tasks panel including chrome; pass 0 when sub-tasks is not
// yet measured (early render path) — the activity then caps at the
// outer viewport alone.
func (b TaskViewBudget) ActivityRows(subtasksHeight int) int {
	var budget int
	switch b.Kind {
	case Stacked:
		budget = b.OuterHeight - ActivityHeader - PanelBorders
	case SideBySide:
		bound := b.FormHeight + subtasksHeight
		if b.OuterHeight < bound {
			bound = b.OuterHeight
		}
		budget = bound - ActivityHeader - PanelBorders
	}
	if budget < 0 {
		return 0
	}
	return budget
}
