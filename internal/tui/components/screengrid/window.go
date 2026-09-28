package screengrid

import (
	"sort"

	"omakiten/internal/tui/components/screenlayout"
)

// window is the visible slice of a windowed row of columns.
type window struct {
	first, last   int
	before, after int
}

func (w window) hidden() bool { return w.before > 0 || w.after > 0 }

// slide picks the children a windowed Cols can show at this width, and returns
// them alongside the window it chose.
//
// # Why the window follows focus rather than the offset alone
//
// A lane the user just tabbed into and cannot see is worse than no window at
// all: the cursor moves, the highlight is off-screen, and the body looks
// frozen. So the stored offset is a REQUEST, and the focused child is a
// constraint that overrides it — exactly what the board does when `l` walks
// past the right edge.
func (w *walker) slide(node Node, box screenlayout.Box, children []Node) ([]Node, window) {
	return w.windowAround(node, children, w.fitCount(children, box))
}

// windowAround picks the `perView` children the window shows, with the stored
// offset as a REQUEST and the focused child as a constraint that overrides it.
//
// It is the one implementation of "a window that follows the focus", shared by
// the horizontal slide and the vertical share. The two carried a byte-identical
// copy of this arithmetic differing only in which count they asked for, which
// is the shape of duplication that lets an edge case be fixed on one axis and
// stay broken on the other.
func (w *walker) windowAround(node Node, children []Node, perView int) ([]Node, window) {
	if len(children) == 0 {
		return children, window{last: -1}
	}
	if perView >= len(children) {
		return children, window{first: 0, last: len(children) - 1}
	}

	offset := clamp(w.state.laneOffset(node.Spec.ID), 0, len(children)-perView)
	// A child the user just tabbed into and cannot see is worse than no window
	// at all: the cursor moves, the highlight is off-screen, and the body looks
	// frozen.
	if at := focusedChild(children, w.state.Focus()); at >= 0 {
		if at < offset {
			offset = at
		}
		if at >= offset+perView {
			offset = at - perView + 1
		}
	}
	end := offset + perView
	return children[offset:end], window{
		first: offset, last: end - 1,
		before: offset, after: len(children) - end,
	}
}

// stack picks the children a STACK can show at this height.
//
// # Why a minimum is a floor here and not a preference
//
// A stack that cannot afford every child's EXPECTED size — see
// [expectedSize] — shows the ones it CAN afford and windows the rest, exactly
// as a row of columns slides rather than crushing its lanes. `tab` is what
// reaches a hidden one — the focus ring already walks every leaf, and the
// window follows the focus, so nothing becomes unreachable by being hidden.
//
// `body` is whether this stack IS the body, rather than one column's internal
// split inside a side-by-side body — the field table over the detail box, the
// task grid over the sub-task board. A column's internal split always takes
// this answer regardless of fit: no zone goes full screen there, because the
// body around it is still side by side and hiding a sibling would throw away
// room the layout has.
//
// # Why the box decides, and only the box
//
// Which zones paint is a property of whether the box can give every child
// what it is worth, never of which one holds focus. A body whose zones can
// all have their expected size shares them, exactly as a shared column does —
// this is the fix for the defect Studio shipped, where focusing a read-only
// pane on a terminal with room for both deleted its sibling and left blank
// rows under it (`fc15000f`).
//
// # Why the focused zone decides once the box cannot
//
// Past that point — this is THE STACKED RULE, taken from internal/tui/layout
// rather than invented here (layout.go:169-212, `SubtasksBoxHeight` /
// `ActivityBoxHeight` returning OuterHeight on their own focus, with
// `ShowSubtasks` / `ShowActivity` returning false on each other's) — a
// FLEXIBLE zone (see [isFlexible]) goes full screen on its own focus, and a
// FIXED zone keeps the siblings its own [cascadeCount] still affords,
// dropping from the end as the terminal tightens further.
//
// The third return, shared, is not part of either answer — it is a report of
// WHICH branch this call took, read by [walker.arrange] to decide what a
// column-scoped ceiling ([screenlayout.Spec.ColumnMaxRows]) means for this
// render. It is true whenever more than one zone is sharing the body — the
// default share, a column's internal split, and the fixed-zone cascade — and
// false only for the flexible zone's full-screen takeover, which leaves
// nothing beside it for a ceiling to be a share of.
func (w *walker) stack(node Node, box screenlayout.Box, children []Node, body bool) ([]Node, window, bool) {
	if len(children) == 0 {
		return children, window{last: -1}, true
	}

	at := focusedChild(children, w.state.Focus())
	if at < 0 {
		at = 0
	}
	if !body || w.fitsExpected(children, box) {
		shown, win := w.share(node, box, children)
		return shown, win, true
	}

	// A FLEXIBLE zone — one that declared no ceiling of its own, neither
	// [screenlayout.Spec.MaxRows] nor [screenlayout.Spec.ColumnMaxRows] —
	// therefore goes FULL SCREEN. There is no width to split when stacked and
	// no honest way to give three zones a usable share of twenty rows, so the
	// screen stops pretending and gives the one you are in everything it has.
	// `tab` is how the others come back.
	if isFlexible(children[at].Spec) {
		return children[at : at+1], window{
			first: at, last: at,
			before: at, after: len(children) - at - 1,
		}, false
	}

	// A FIXED zone is the exception, and it is the same exception the app makes
	// for the form: it declared a ceiling of its own, so it never claims a
	// whole body it does not need. It keeps its focus and the siblings after it
	// cascade into what is left, dropping from the bottom as the terminal
	// tightens — activity before sub-tasks in task detail, because declaration
	// order puts activity last (layout.go:243, `stackedFormSplit`).
	//
	// share stays true here, deliberately: it is what lets [columnSpec] fold
	// [screenlayout.Spec.ColumnMaxRows] into the fixed zone's effective ceiling
	// for this render, the same fold a shared COLUMN gets. The fixed zone is
	// still sharing the body with the siblings the cascade kept, and a ceiling
	// that means "what I am worth beside a sibling" means exactly that here too.
	rest := children[at:]
	perView := w.cascadeCount(rest, box)
	end := at + perView
	return children[at:end], window{
		first: at, last: end - 1,
		before: at, after: len(children) - end,
	}, true
}

// isFlexible reports whether a zone declared no ceiling of its own — neither
// an unconditional [screenlayout.Spec.MaxRows] nor a while-sharing
// [screenlayout.Spec.ColumnMaxRows]. A flexible zone always wants the rest of
// whatever body it is given; a zone that capped itself is the fixed exception
// the stacked rule cascades around.
func isFlexible(spec screenlayout.Spec) bool {
	return spec.MaxRows == 0 && spec.ColumnMaxRows == 0
}

// expectedSize is the rows a zone is worth when the box can afford to give it
// what it actually asked for, rather than merely enough to be worth drawing.
//
// A zone that declared [screenlayout.Spec.ExpectedRows] states its own
// answer, and this reads it before either ceiling: [screenlayout.Spec.MaxRows]
// and [screenlayout.Spec.ColumnMaxRows] are UPPER BOUNDS a zone is never given
// more than, not a promise it will use all of it — the field table's ceiling
// pads one row per value that MIGHT wrap, so demanding the padded number
// before a stacked body will even share the zone asks for slack the zone may
// never spend.
//
// Absent that, a zone that declared a ceiling of its own is worth exactly
// that ceiling: task detail's own details-block ceiling carries no such
// slack, so its ColumnMaxRows already IS the expectation. A zone that
// declared neither has no ceiling to name, so the only size it ever stated
// is its floor,
// [screenlayout.Spec.MinRows]: a list or a feed always wants more than that,
// but it never said how much more, and MinRows is the one number it is
// answerable to.
func expectedSize(spec screenlayout.Spec) int {
	switch {
	case spec.ExpectedRows > 0:
		return spec.ExpectedRows
	case spec.MaxRows > 0:
		return spec.MaxRows
	case spec.ColumnMaxRows > 0:
		return spec.ColumnMaxRows
	default:
		return max(spec.MinRows, 1)
	}
}

// fitsExpected asks the allocator itself whether every child can have its
// expected size, rather than re-deriving a prediction of what it will do.
//
// A sum of expected sizes against box.Rows is NOT that question: distributeRows
// spends surplus by [spreadSurplus]'s weighted round-robin, splitting it
// between a ceiling zone and its uncapped siblings AT ONCE rather than filling
// the ceiling zone first, so a zone can fall short of a ceiling the raw
// arithmetic says the box affords — exactly the case a top-level "▼ N below"
// hint reports on the ceiling zone's own content once the arranger runs.
// [screenlayout.StackedRows] runs the SAME distributeRows [screenlayout.Arrange]
// runs, so what this predicts and what gets painted cannot drift apart.
//
// A box narrower than a child's MinWidth cannot be repaired by giving fewer
// rows to anyone — every child is under-width regardless of how the rows
// split — so that collapses to "does not fit" directly, the same floor
// [walker.rowFitCount] applies.
func (w *walker) fitsExpected(children []Node, box screenlayout.Box) bool {
	for _, child := range children {
		if child.Spec.MinWidth > box.Width {
			return false
		}
	}
	specs := make([]screenlayout.Spec, len(children))
	for i, child := range children {
		// share is true: a stack that shares is a stack whose zones stay
		// together in the body, and that is exactly the render columnSpec's
		// ColumnMaxRows fold is for — see [columnSpec].
		specs[i] = columnSpec(child.Spec, true)
	}
	assigned := screenlayout.StackedRows(box, specs...)
	for i, child := range children {
		if assigned[i] < expectedSize(child.Spec) {
			return false
		}
	}
	return true
}

// cascadeCount is how many of a FIXED zone's cascade — the fixed zone itself
// plus the flexible siblings declared after it — the box can give their
// expected size, dropping from the END one at a time — activity before
// sub-tasks in task detail, because declaration order puts activity last
// (layout.go:243, `stackedFormSplit`) — until [walker.fitsExpected] agrees.
//
// It is not [walker.rowFitCount]: that picks its candidates by SIZE so a
// window fits wherever the focus sends it, which is right for a window that
// slides. The fixed cascade's window never slides — it always starts at the
// fixed zone — so its drop order is POSITIONAL rather than a function of
// which sibling is bigger. And it is not a second arithmetic on top of
// [walker.fitsExpected] either: asking distributeRows for `n` children and
// asking it for `n-1` are both the allocator's own answer, so the fixed zone
// this cascade exists for is never left short of the ceiling it declared just
// because a sibling's floor happened to still fit at the wider count.
//
// The floor of 1 is the fixed zone alone: with nothing left to drop, it gets
// whatever the box has, ceiling or not — the same base case
// stackedFormSplit's 1-pane step is.
func (w *walker) cascadeCount(children []Node, box screenlayout.Box) int {
	fit := func(available screenlayout.Box) int {
		for kept := len(children); kept > 1; kept-- {
			if w.fitsExpected(children[:kept], available) {
				return kept
			}
		}
		return 1
	}
	if all := fit(box); all >= len(children) {
		return all
	}
	return fit(screenlayout.Box{Width: box.Width, Rows: box.Rows - hintRows})
}

// share is the stack rule for a COLUMN's internal split: every child that can
// have the minimum it declared is shown, and the ones the box cannot afford are
// windowed off the bottom with the focus pulling them back in.
//
// No zone goes full screen here. The body around this column is side by side and
// still fits, so hiding a sibling would be throwing away room the layout has.
func (w *walker) share(node Node, box screenlayout.Box, children []Node) ([]Node, window) {
	return w.windowAround(node, children, w.rowFitCount(children, box))
}

// rowFitCount is how many children fit stacked at this box, counted against
// the MINIMUM they declared — the size below which a child is not worth
// windowing in at all — rather than the size they would be squeezed into.
//
// # The width floor, and why it collapses to one
//
// A stack's children all share its width, so a box narrower than a child's
// MinWidth cannot be fixed by showing fewer of them — every child is under-width
// whatever the count. There is exactly one thing left to give, and that is all
// of the box to one zone: the focused child, full screen, with the rest a `tab`
// away.
//
// That is the floor of the same rule the row count implements, not a second
// rule. Three zones at a third of the width they asked for are three zones you
// cannot read; one zone at a third of the width is one zone you can at least
// scroll. Below the point where a LAYOUT is possible, the answer stops being a
// layout.
//
// # The rows
//
// One row is reserved for the hint the moment anything is hidden, and the count
// is retaken with that row gone — otherwise the hint is paid for out of the last
// visible child and the window it advertises is off by one.
//
// The candidates are the k TALLEST children, for the same reason [fitCount]
// takes the widest: the answer has to be a window size that fits wherever the
// focus sends it, not one that fits at the top of the list.
func (w *walker) rowFitCount(children []Node, box screenlayout.Box) int {
	for _, child := range children {
		if child.Spec.MinWidth > box.Width {
			return 1
		}
	}
	mins := make([]int, len(children))
	for i, child := range children {
		mins[i] = max(child.Spec.MinRows, 1)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(mins)))

	count := func(available int) int {
		spent, n := 0, 0
		for _, need := range mins {
			if spent+need > available {
				break
			}
			spent += need
			n++
		}
		return max(n, 1)
	}
	if all := count(box.Rows); all >= len(children) {
		return all
	}
	return count(box.Rows - hintRows)
}

// hintRows is the row a windowed container spends telling you what it hid.
const hintRows = 1

// fitCount is how many children a windowed row can show at this width.
//
// It asks the ARRANGER'S OWN QUESTION — [screenlayout.FitsSideBySide] — rather
// than re-deriving the arithmetic, so the count this picks and the arrangement
// the arranger then chooses for the slice cannot disagree. A window that slid
// to a width the arranger turns around and stacks is worse than no window.
//
// The candidates are the k WIDEST children, not the first k, so the answer is a
// window size that fits wherever it lands. For the uniform lanes of a board the
// two are the same number; for a row of unequal columns, picking by the prefix
// would let a window slide right into a slice that no longer fits.
//
// A window of one is the floor: a child too wide for the terminal is still
// shown, clipped, because a board showing nothing is not an improvement on a
// board showing one lane.
func (w *walker) fitCount(children []Node, box screenlayout.Box) int {
	widest := make([]screenlayout.Spec, len(children))
	for i, child := range children {
		widest[i] = child.Spec
	}
	sort.SliceStable(widest, func(a, b int) bool { return widest[a].MinWidth > widest[b].MinWidth })

	count := 1
	for k := 2; k <= len(widest); k++ {
		if !screenlayout.FitsSideBySide(box, widest[:k]...) {
			break
		}
		count = k
	}
	return count
}

// focusedChild is the index of the child that IS the focused node or contains
// it, or -1.
//
// Both halves matter now that focus can rest on a container: `tab` at the body
// level parks on a column without entering it, and the window still has to keep
// that column on screen.
func focusedChild(children []Node, focus screenlayout.ID) int {
	if focus == "" {
		return -1
	}
	for i, child := range children {
		if child.Spec.ID == focus {
			return i
		}
		for _, id := range child.leaves() {
			if id == focus {
				return i
			}
		}
	}
	return -1
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
