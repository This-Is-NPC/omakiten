package stats

import (
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// sectionModels is the one section Stats declares to the arranger. The Totals /
// Tokens summary is OUTER chrome (plannetwork-style), not a sibling: it must
// paint ABOVE the model panel when kept, but yield FIRST when floors do not
// fit — drop-from-the-bottom would drop a trailing sibling, so a two-section
// stack cannot express "summary on top, summary yields".
//
// # Drop order is a product decision
//
// Measured at the 80×24 golden floor with DefaultChromeRows=7: BodyRows=15,
// HostBox.Rows=14. The MergeNarrow Totals+Tokens stack is 17 rows; the rows the
// model panel needs for its whole shape are 9 (see
// [Screen.modelsWholeShapeRows]). Sum 17+9=26 > HostBox 14, so the summary
// yields. The screen exists to show the model/period table; the budget caption
// is supplemental. Do not keep the summary by soft-shrinking the model window
// below its floor.
const sectionModels = screenlayout.ID("stats-models")

// modelsMinViewport is the smallest item window worth giving the model table:
// one model row, and the row the "▼ N below" hint needs to say how many are
// hidden behind it. Either alone is a table that lies by omission — rows with
// nothing saying more exist, or a hint with nothing under it.
const modelsMinViewport = 2

// modelsFloorRows is the section's MinRows: the rows the panel needs before it
// is worth drawing at all. Below it the arranger drops the section, which is
// what a floor is for.
//
// # The defect this replaced
//
// One constant, modelsMinRows = 3, was declared as the section's MinRows AND
// used as the summary's yield threshold. But MinRows is a floor on the
// section's TOTAL rows, and the block pins seven rows of chrome on a live
// reading — the kicker, the column header and the rule above the window; the
// rule, the total and the since-note below it. So boxes of 3 to 7 rows cleared
// the floor with an item viewport of ZERO: the panel painted its crown and its
// total row with no model rows between them, and with nothing visible the
// arranger reports Above = 0 / Below = 0, so not even a "▼ N below" hint said
// the table had rows at all. A floor that cannot show one item is not a floor.
//
// # What it floors
//
// The COMPACT shape — header, then items. The footer is the total row and the
// since note: a caption ON the rows, so a box that can hold either the caption
// or the rows it captions holds the rows.
func (s Screen) modelsFloorRows() int {
	return modelHeaderRows + 1 + modelsMinViewport
}

// modelsWholeShapeRows is the second floor: the rows the panel needs to paint
// its WHOLE shape — all the chrome it would pin, plus an item window that can
// still hold a row AND the hint naming what is hidden, the same
// [modelsMinViewport] modelsFloorRows reserves. It is what
// [Screen.modelsBlock] asks before keeping its footer, and what
// [Screen.outerBudget] asks before keeping the summary above the panel.
//
// The hint row is not slack. ScrollWindowSplit composes the window as its rows
// plus its hints and the arranger then clips to the viewport, so a one-row
// window over an overflowing table paints the row and drops the hint: the user
// sees one model and nothing saying there are forty-one more. Keeping the
// closing chrome at that height buys a caption at the price of the table it
// captions.
//
// The two floors are deliberately different numbers. MinRows is the point below
// which the panel is not worth drawing; this is the point below which it starts
// shedding the supplemental half of itself. Yielding the summary at THIS one
// keeps the documented drop order intact: the caption above the panel goes
// before the panel's own caption does, and both go before a model row does.
//
// The chrome is [Screen.modelChromeRows], counted from the reading rather than
// composed, because this is read on every arrange and an arrange runs on every
// keystroke. That count is charged against the builders it mirrors by
// TestModelChromeRowsCountsWhatTheBuildersPin.
func (s Screen) modelsWholeShapeRows() int {
	return s.modelChromeRows() + modelsMinViewport
}

func (s Screen) modelsSection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionModels, MinRows: s.modelsFloorRows(), Weight: 1,
			Scroll: screenlayout.ScrollItems,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.modelsBlock(kit, canvas.Width(), canvas.Rows())
		},
	}
}

// modelsBlock pins the kicker / column header / rule as Header and the total /
// since note as Footer so the crown and closing chrome do not scroll away.
// Items are CapRows-clamped model rows; Cursor is NoSelection so j/k page the
// offset (body-scroll), matching the pre-migration cursorless window.
//
// # The compact shape
//
// Below [Screen.modelsWholeShapeRows] the footer is NOT pinned. The panel is a
// different shape there rather than a shorter version of the same one — the
// same move the empty reading makes, which drops the total row it has nothing
// to sum and the date it must not claim.
//
// It is the closing chrome that goes, not the data, because the closing chrome
// is a caption on the data: `total` is the column sum of the model rows and the
// since note is the window they were read over. At 60×14 — a split pane, not a
// degenerate probe — this is the difference between a crown, two adjacent rules
// and a total row for models the user cannot see, and the period picker with
// the model rows themselves underneath it.
//
// rows is the canvas's, which the Canvas contract names as the read a section
// makes when it elides content on a short terminal. It is not a soft shrink:
// what is pinned is pinned, and every row the section is given is spent.
func (s Screen) modelsBlock(kit screenkit.Kit, width, rows int) screenlayout.Block {
	width = max(1, width)
	inner := max(1, width-panel.Borders)
	block := framed.List(kit.Styles.Border, width, "", nil, s.modelDataRows(kit, inner))
	block.Header = s.modelPanelHeader(kit, inner)
	if rows >= s.modelsWholeShapeRows() {
		block.Footer = s.modelPanelFooter(kit, inner)
	}
	block.Cursor = screenlayout.NoSelection()
	return block
}

// outerBudget is the Totals + Tokens block when the HostBox can still afford
// the model section's MinRows underneath it (plus the blank gap View joins
// with), or "" when the summary yields.
//
// Floors restated for the outer-chrome form of the two-section decision above:
// HostBox.Rows must cover height(budget)+1 (gap) + panel border +
// modelsWholeShapeRows. At 80×24 that is 17+1+2+9=29 against HostBox 14 —
// summary yields.
//
// The threshold is modelsWholeShapeRows and not the section's MinRows floor. A
// yield rule that protects a number the panel cannot show a row at is how this
// screen painted an empty table at 120×25-29; a yield rule that protects only
// the MinRows floor would keep the summary while the panel underneath it sheds
// its own total row, which inverts the drop order this screen documents.
func (s Screen) outerBudget(kit screenkit.Kit) string {
	budget := s.renderBudgetTables(kit)
	if budget == "" {
		return ""
	}
	host := screenlayout.HostBox(kit)
	outer := screenkit.BlockRows(budget) + 1 // blank row between budget and panel
	if host.Rows-outer < s.modelsWholeShapeRows() {
		return ""
	}
	return budget
}

// panelBox is the one geometry the render and the two persisting twins share —
// [screengrid.Render], [screengrid.State.Resync] and [screengrid.State.HandleKey]:
// panel content width and the rows left inside the bordered model panel after
// outer budget chrome (when kept) and the panel border are charged.
//
// When the terminal is still unmeasured (Height<=0), Kit.ViewportRows returns
// the historical "0 means unlimited" sentinel. The arranger underneath the grid
// treats 0 as ZERO rows, so HostBox supplies the unmeasured-height assumption
// instead.
func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	host := screenlayout.HostBox(kit)
	rows := host.Rows
	if budget := s.outerBudget(kit); budget != "" {
		rows -= screenkit.BlockRows(budget) + 1
	}
	if kit.Height <= 0 {
		rows = host.Rows
	}
	return screenlayout.Box{
		Width: kit.PanelContentWidth(),
		Rows:  max(0, rows),
	}
}

// syncModelsWindow re-clamps the model offset against the live reading and
// geometry. Call it before scroll keys and on enter/resize so a period reload
// that shrinks ByModel cannot leave a stale ceiling on the next keystroke.
func (s Screen) syncModelsWindow(kit screenkit.Kit) Screen {
	root := s.modelsRoot(kit)
	s.grid = s.grid.WithFocus(sectionModels).Resync(kit, s.panelBox(kit), root)
	return s
}

// modelsRoot mounts the model panel as the Stats screen's single grid cell.
func (s Screen) modelsRoot(kit screenkit.Kit) screengrid.Node {
	section := s.modelsSection(kit)
	return screengrid.Cell(section.Def, section.Body)
}

// modelPanelView is the bordered model cell. Screengrid delegates the leaf's
// item window to screenlayout, preserving its exact body geometry and pixels.
func (s Screen) modelPanelView(kit screenkit.Kit) string {
	return screengrid.Render(kit, s.grid, s.panelBox(kit), s.modelsRoot(kit)).View
}
