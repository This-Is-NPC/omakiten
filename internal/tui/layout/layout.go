// Package layout centralises per-section viewport budgets for
// composite TUI screens.
//
// The bug class it closes: the task detail view juggles three
// panels (form / sub-tasks / activity) whose row budgets depend on
// the focus AND the terminal geometry. Pre-W14 every panel computed
// its own budget from m.height with subtly different chrome math,
// and the side-by-side activity rail asked for a row count the
// kanbanColumnSized helper interpreted as inner content rows (not
// total rows), so every panel rendered 2 rows taller than intended
// and the outer slice chopped the bottom.
//
// The fix lives here: TaskViewBudget owns the policy. Callers ask
// for "should I render this panel?" via Show*; "how tall is the
// final box?" via *BoxHeight (total rows including chrome + borders);
// "how many cardlist/linelist rows fit inside?" via *Rows (body
// only). The constants are intentionally lined up with the actual
// chrome each renderer emits so the math composes without surprise.
//
// Lives outside `tui` so viewport sub-components (cardlist /
// linelist consumers) can import it without dragging the parent
// package's dependency graph.
package layout

// Kind enumerates the packing shapes the task detail view can take.
// Mirrors taskViewLayoutKind in render_task.go — kept as its own
// type here so the layout package owns the contract.
type Kind int

const (
	// Stacked stacks every visible panel vertically.
	Stacked Kind = iota
	// SideBySide packs form + sub-tasks vertically on the left, with
	// activity in a right rail. Always renders all three panels.
	SideBySide
)

// Focus enumerates which panel currently owns navigation keys. The
// stacked path uses Focus to decide single-pane vs 3-pane (form
// focus = 3-pane cascade; sub-tasks / activity focus = single-pane
// fullscreen). The side-by-side path ignores Focus for sizing — it
// always renders all three panels and the renderer only swaps the
// kicker/border accent based on the focused section.
type Focus int

const (
	FocusForm Focus = iota
	FocusSubtasks
	FocusActivity
)

// SubtasksHeader is the row cost of the sub-tasks panel chrome
// above the first card inside the bordered box: kicker (1) + rule
// (1) + leading blank (1).
const SubtasksHeader = 3

// ActivityHeader is the row cost of the activity panel chrome above
// the first card line inside the box: kicker (1) + leading blank
// (1). Matches the prior activityChromeBase accounting.
const ActivityHeader = 2

// Separator is the blank-line cost of joining two stacked panels
// via "\n\n" — one blank row between each adjacent pair.
const Separator = 1

// PanelBorders is the row cost of the top + bottom borders the
// kanban-column / fixed-box renderers wrap around each panel body.
// Critically, lipgloss `Height(n)` treats n as the INNER content
// rows — borders sit outside. Total box height = inner + borders.
// Callers express budgets in TOTAL rows; this constant is the
// adapter to the lipgloss inner-height contract.
const PanelBorders = 2

// SubtasksMinRows is the minimum body rows the sub-tasks panel
// needs to read as "usable" — roughly one card visible. Below
// this floor the cascade drops the panel entirely in stacked
// form-focus.
const SubtasksMinRows = 4

// ActivityMinRows is the minimum body rows the activity panel
// needs to read as "usable" — roughly one full comment card +
// padding. Below this floor the cascade drops the panel.
const ActivityMinRows = 6

// TaskViewBudget is the input contract for per-section row budgets
// in the task detail view. Computed once per render at the top of
// renderTaskView; consumed by every panel helper.
type TaskViewBudget struct {
	Kind        Kind
	Focus       Focus
	FormHeight  int
	OuterHeight int
}

// ShowForm reports whether the form panel renders in the current
// budget. Side-by-side always shows the form (it owns the top of
// the left column); stacked shows the form when its focus is the
// form, OR when the terminal is too tight to host either sibling
// (cascade fallback — there is always at least one panel on screen,
// and the form is the default home for the cascade's last step).
func (b TaskViewBudget) ShowForm() bool {
	if b.Kind == SideBySide {
		return true
	}
	// Stacked: form draws when its own focus or as the form-focus
	// 3/2/1 cascade base.
	return b.Focus == FocusForm
}

// ShowSubtasks reports whether the sub-tasks panel renders in the
// current budget.
func (b TaskViewBudget) ShowSubtasks() bool {
	switch b.Kind {
	case SideBySide:
		return true
	case Stacked:
		switch b.Focus {
		case FocusSubtasks:
			return true
		case FocusActivity:
			return false
		case FocusForm:
			sub, _ := b.stackedFormSplit()
			return sub > 0
		}
	}
	return false
}

// ShowActivity reports whether the activity panel renders in the
// current budget.
func (b TaskViewBudget) ShowActivity() bool {
	switch b.Kind {
	case SideBySide:
		return true
	case Stacked:
		switch b.Focus {
		case FocusActivity:
			return true
		case FocusSubtasks:
			return false
		case FocusForm:
			_, act := b.stackedFormSplit()
			return act > 0
		}
	}
	return false
}

// SubtasksBoxHeight returns the TOTAL rows the sub-tasks box
// should occupy on screen (borders + chrome + body). 0 means the
// panel is hidden in the current budget — callers should not
// render it.
func (b TaskViewBudget) SubtasksBoxHeight() int {
	if !b.ShowSubtasks() {
		return 0
	}
	switch b.Kind {
	case SideBySide:
		// Left column: form on top, sub-tasks below. Sub-tasks
		// fills the leftover so the left column reaches the outer
		// viewport floor and the right rail (activity) can mirror
		// it without overshooting.
		h := b.OuterHeight - b.FormHeight
		if h < 0 {
			return 0
		}
		return h
	case Stacked:
		switch b.Focus {
		case FocusSubtasks:
			// Single-pane fullscreen.
			return b.OuterHeight
		case FocusForm:
			sub, _ := b.stackedFormSplit()
			return sub
		}
	}
	return 0
}

// ActivityBoxHeight returns the TOTAL rows the activity box should
// occupy on screen (borders + chrome + body). 0 means hidden.
func (b TaskViewBudget) ActivityBoxHeight() int {
	if !b.ShowActivity() {
		return 0
	}
	switch b.Kind {
	case SideBySide:
		// Right rail mirrors the left column's height so the
		// horizontal join lines up. Capped at OuterHeight so a
		// runaway form box does not push the rail past the outer
		// slice.
		bound := b.FormHeight + b.SubtasksBoxHeight()
		if b.OuterHeight < bound {
			bound = b.OuterHeight
		}
		if bound < 0 {
			return 0
		}
		return bound
	case Stacked:
		switch b.Focus {
		case FocusActivity:
			return b.OuterHeight
		case FocusForm:
			_, act := b.stackedFormSplit()
			return act
		}
	}
	return 0
}

// SubtasksRows is the inner cardlist body row budget (excludes
// header chrome and borders). Used by cardlist.WithViewport so the
// component clamps its visible window to what the box can hold.
func (b TaskViewBudget) SubtasksRows() int {
	box := b.SubtasksBoxHeight()
	if box <= 0 {
		return 0
	}
	rows := box - SubtasksHeader - PanelBorders
	if rows < 0 {
		return 0
	}
	return rows
}

// ActivityRows is the inner linelist body row budget (excludes
// header chrome and borders).
func (b TaskViewBudget) ActivityRows() int {
	box := b.ActivityBoxHeight()
	if box <= 0 {
		return 0
	}
	rows := box - ActivityHeader - PanelBorders
	if rows < 0 {
		return 0
	}
	return rows
}

// stackedFormSplit runs the 3/2/1 cascade for stacked + form focus.
// Returns (subBox, actBox) — the TOTAL rows each panel box should
// occupy, or 0 to indicate the panel is dropped at the current
// terminal height.
//
// Cascade rules:
//   - 3-pane: outer = form + sep + sub + sep + act. Sub-tasks +
//     activity split the leftover 50/50, both subject to their
//     min-rows floor. Empty children / no-events do NOT change the
//     decision — empty panels render their empty state inside the
//     allotted slot.
//   - 2-pane: outer = form + sep + sub. Sub-tasks takes the full
//     leftover; activity drops.
//   - 1-pane: only form fits. Both siblings drop.
//
// Drop order is activity → sub-tasks (visual bottom-up).
func (b TaskViewBudget) stackedFormSplit() (subBox, actBox int) {
	// 3-pane: leftover space for both bodies combined.
	combinedBody := b.OuterHeight - b.FormHeight - 2*Separator - SubtasksHeader - ActivityHeader - 2*PanelBorders
	if combinedBody >= SubtasksMinRows+ActivityMinRows {
		subBody := combinedBody / 2
		actBody := combinedBody - subBody
		// Both shares already ≥ their mins because combined ≥
		// SubtasksMinRows + ActivityMinRows and the halves never
		// drift more than 1 row apart (SubtasksMinRows < ActivityMinRows
		// means the smaller half goes to sub-tasks; verify with a
		// reallocation if needed).
		if subBody < SubtasksMinRows {
			delta := SubtasksMinRows - subBody
			subBody += delta
			actBody -= delta
		}
		if actBody < ActivityMinRows {
			delta := ActivityMinRows - actBody
			actBody += delta
			subBody -= delta
		}
		return subBody + SubtasksHeader + PanelBorders, actBody + ActivityHeader + PanelBorders
	}

	// 2-pane: outer = form + sep + sub.
	subBody := b.OuterHeight - b.FormHeight - Separator - SubtasksHeader - PanelBorders
	if subBody >= SubtasksMinRows {
		return subBody + SubtasksHeader + PanelBorders, 0
	}

	// 1-pane: form only.
	return 0, 0
}
