// Package layout centralises per-section viewport budgets for
// composite TUI screens. The bug class it closes: the task detail
// view stacks three panels (form / sub-tasks / activity), and each
// panel used to compute its own row budget from m.height as if it
// owned the whole vertical column. The result was a multi-way race
// where two panels both claimed the leftover space after the form
// box, with the activity panel reliably losing — floored to a 6-row
// minimum even when half the terminal was empty.
//
// The fix lives in this package: a single TaskViewBudget value
// computed once per render that hands each section its correct row
// allocation. The panels stop guessing; the layout struct is the
// source of truth and the joint split between sub-tasks and activity
// is deterministic.
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

// Focus enumerates which section currently owns navigation keys.
// Drives the stacked-mode split between sub-tasks and activity so
// the focused panel gets the larger share and the unfocused one
// stays usable instead of collapsing to a minimum-row sliver.
type Focus int

const (
	// FocusNone is the neutral / unknown signal — split is 50/50.
	// Use this when the budget is computed before a focus decision
	// is available (e.g. early-render path).
	FocusNone Focus = iota
	// FocusForm — form column owns navigation. Sub-tasks / activity
	// split 50/50 of their joint leftover.
	FocusForm
	// FocusSubtasks — sub-tasks panel gets the larger share so the
	// user can see more cards while navigating; activity stays
	// usable at its minimum-plus.
	FocusSubtasks
	// FocusActivity — activity panel gets the larger share so the
	// user can read / navigate threads; sub-tasks stays usable.
	FocusActivity
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

// PanelBorders is the row cost of the top + bottom borders the
// kanban-column / fixed-box renderers wrap around each panel body.
// Subtracted from the joint leftover so the rendered panel boxes
// (chrome + borders + content) all fit inside the outer viewport
// without forcing an outer slice.
const PanelBorders = 2

// SubtasksMinRows is the floor enforced on the sub-tasks panel body
// in stacked mode. Roughly one card height — guarantees the user
// can always see at least one sub-task card even when activity is
// focused and claiming the larger share.
const SubtasksMinRows = 4

// ActivityMinRows is the floor enforced on the activity panel body
// in stacked mode. Mirrors the prior activityViewportMinLines so
// the visual floor stays consistent with what the renderer expected.
const ActivityMinRows = 6

// TaskViewBudget is the input contract for per-section row budgets
// in the task detail view. Computed once per render at the top of
// renderTaskView; consumed by sub-tasks and activity panels via
// SubtasksRows / ActivityRows so neither panel reads m.height on
// its own.
//
// FormHeight is the measured row count of the rendered form box
// (read through taskDetailsBoxHeightCache). OuterHeight is the row
// budget the outer detail-view slice has after subtracting screen
// chrome (header / footer / status / leading blank). Focus drives
// the stacked-mode split between sub-tasks and activity — pass
// FocusNone when the focus is unknown / irrelevant.
type TaskViewBudget struct {
	Kind        Kind
	Focus       Focus
	FormHeight  int
	OuterHeight int
}

// SubtasksRows returns the row budget the sub-tasks panel body
// (card list inside the chrome) has after accounting for the form
// box and the joint share with activity. In stacked layout the
// leftover under the form is split between sub-tasks and activity
// per the Focus field; in side-by-side the panel sits directly
// under the form inside the left column and takes the full leftover
// (activity occupies the right rail, not the left column).
//
// Floors at 0 — callers fall back to "render every card and let
// outer slicing chop" on tiny terminals.
func (b TaskViewBudget) SubtasksRows() int {
	switch b.Kind {
	case Stacked:
		sub, _ := b.stackedSplit()
		return sub
	case SideBySide:
		budget := b.OuterHeight - b.FormHeight - SubtasksHeader - PanelBorders
		if budget < 0 {
			return 0
		}
		return budget
	}
	return 0
}

// ActivityRows returns the row budget the activity panel body has.
// In stacked layout the budget is the activity half of the joint
// split — Focus-aware so the focused panel gets the larger share.
// The subtasksHeight argument is IGNORED in stacked mode (the split
// is independent of the measured sub-tasks panel height) — left in
// the signature so side-by-side callers can keep their existing
// "rail matches the left column" math.
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
		_, act := b.stackedSplit()
		return act
	case SideBySide:
		leftHeight := b.FormHeight + subtasksHeight
		// Cap by the outer viewport too — otherwise the activity rail
		// can grow taller than the outer detail-view slice (e.g. when
		// the form alone almost fills the column and leftHeight blows
		// the outer budget), and `applyTaskViewScroll` chops the
		// bottom of the rail. The chop hits exactly where
		// syncActivityScrollToCursor expects the cursor to be
		// visible, so the user reports "after resize the focused
		// card disappears".
		bound := leftHeight
		if b.OuterHeight < bound {
			bound = b.OuterHeight
		}
		budget := bound - ActivityHeader - PanelBorders
		if budget < 0 {
			return 0
		}
		return budget
	}
	return 0
}

// stackedSplit divides the row budget leftover under the form box
// between the sub-tasks and activity panels deterministically. The
// split is Focus-aware so the focused panel gets the larger share,
// while the unfocused one stays usable instead of collapsing to a
// single-line sliver. Mins (SubtasksMinRows / ActivityMinRows) are
// enforced by reallocating from the other side when one share
// would otherwise underflow.
//
// Returns (0, 0) when the terminal is too short to host both panels
// at their minimums — the renderer falls back to "let the outer
// slice chop" in that pathological case.
func (b TaskViewBudget) stackedSplit() (subtasks, activity int) {
	// outer = form + sep + (subtasks-header + sub-body + borders)
	//             + sep + (activity-header + act-body + borders)
	// leftover = outer - form - 2*sep - sub-header - act-header
	//                  - 2*panel-borders
	leftover := b.OuterHeight - b.FormHeight - 2*Separator - SubtasksHeader - ActivityHeader - 2*PanelBorders
	if leftover <= 0 {
		return 0, 0
	}

	// Pick a sub-share fraction based on focus. Numbers are
	// integer-ratios over 100 so the math stays predictable on
	// every terminal height (no float drift, no rounding surprises
	// at the panel boundary).
	subPct := 50
	switch b.Focus {
	case FocusSubtasks:
		subPct = 65
	case FocusActivity:
		subPct = 35
	}
	sub := leftover * subPct / 100
	act := leftover - sub

	// Min enforcement. If one share would underflow, top it up by
	// shifting rows from the other; subsequent floor check catches
	// the pathological case where even the donor falls below 0.
	if sub < SubtasksMinRows {
		delta := SubtasksMinRows - sub
		sub += delta
		act -= delta
	}
	if act < ActivityMinRows {
		delta := ActivityMinRows - act
		act += delta
		sub -= delta
	}
	if sub < 0 {
		sub = 0
	}
	if act < 0 {
		act = 0
	}
	return sub, act
}
