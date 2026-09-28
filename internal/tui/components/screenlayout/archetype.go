package screenlayout

import (
	"omakiten/internal/tui/components/screenkit"
)

// The two multi-column archetypes, as ready-made Spec fragments. A screen that
// is `lista | inspector` or `[a over b] | feed` calls these; it does not
// redeclare the widths, the proportion or the gap.
//
// There is no SingleColumn constructor. The other seventeen screens already
// get a single column by not opting into Spec.Column, which is a Spec literal
// they state directly. A constructor that set ID and ScrollItems and nothing
// else would save a literal at the cost of a name nobody needs to look up —
// it does not pay.

const (
	// listInspectorPaneMinRows is the honest floor of a list|inspector pane —
	// list or inspector column — lifted from the four Studio sub-screens.
	//
	// Ten is what a pane costs before it says anything: two borders, a kicker,
	// a rule, plus the five content rows below which a list is a scroll hint
	// with a row inside it.
	listInspectorPaneMinRows = 10

	// inspectorFieldsMinRows and inspectorDetailMinRows are the two inspector
	// zones' real floors: two borders, a title or kicker, its rule and one row.
	inspectorFieldsMinRows = 5
	inspectorDetailMinRows = 5

	inspectorFieldsWeight = 4
	inspectorDetailWeight = 1
	inspectorColumnWeight = 2
	listColumnWeight      = 1
	feedColumnWeight      = 2
	stackedColumnWeight   = 1
)

// List is the Spec fragment for the left column of `lista | inspector`.
func List(id ID) Spec {
	return Spec{
		ID: id, MinWidth: screenkit.ListFloor, MinRows: listInspectorPaneMinRows,
		Weight: listColumnWeight, ColumnGap: screenkit.ZoneGap,
		Scroll: ScrollItems, SelectFirst: true,
	}
}

// Inspector is the Spec fragment for the inspector COLUMN of `lista |
// inspector`: the container that stacks the field table over the detail box.
// The leaves inside it are [InspectorIDs.FieldsSpec] and
// [InspectorIDs.DetailSpec].
func Inspector(id ID) Spec {
	return Spec{
		ID: id, MinWidth: screenkit.InspectorFloor, MinRows: listInspectorPaneMinRows,
		Weight: inspectorColumnWeight, ColumnGap: screenkit.ZoneGap,
	}
}

// Feed is the Spec fragment for the feed column of `[a over b] | feed`.
//
// SelectFirst is left off: Project wants a card under the cursor at all times,
// Task Detail does not. A screen that wants it sets it on the value.
func Feed(id ID, minRows int) Spec {
	return Spec{
		ID: id, MinWidth: screenkit.FeedFloor, MaxWidth: screenkit.ComfortableReadingWidth,
		WidthPercent: screenkit.FeedProportion, MinRows: minRows,
		Column: true, ColumnGap: screenkit.ZoneGap, Weight: feedColumnWeight,
		Scroll: ScrollItems,
	}
}

// BesideFeed is the Spec fragment for a zone stacked in the left column of
// `[a over b] | feed`. minWidth is [screenkit.PanelFloor] for a meta panel
// or [screenkit.DetailColumnFloor] for a details column — the two left
// columns are different measures, so the constructor does not pick one.
//
// Scroll comes from the caller because the two callers of this archetype
// disagree: Project's panels want ScrollItems, Task Detail's sub-task board
// wants ScrollNone. A constructor whose doc told the screen to undo a field
// had the field wrong.
func BesideFeed(id, group ID, minWidth, minRows int, scroll ScrollPolicy) Spec {
	return Spec{
		ID: id, MinWidth: minWidth, MinRows: minRows,
		Column: true, Group: group, ColumnGap: screenkit.ZoneGap,
		Weight: stackedColumnWeight, Scroll: scroll,
	}
}

// InspectorIDs names one `lista | inspector` body's three inspector zones.
//
// It exists so the shared helpers below take one argument instead of four, and
// so a screen cannot pass the fields id where the detail id belongs. MinWidth
// used to be a field each screen filled with the same 40; it is now
// [screenkit.InspectorFloor], read here, once.
//
// Lifted from the four Studio sub-screens. The inspector is TWO zones, not one
// pane that happens to paint two boxes: the field table over the detail box,
// as a Rows the layout dissolves and the keyboard walks straight through.
type InspectorIDs struct {
	Inspector ID
	Fields    ID
	Detail    ID
}

// FieldsSpec is the view-only zone: it scrolls, and it never selects.
//
// No cursor is the whole of "view mode" as the arranger understands it — with a
// resolved cursor of -1 the standard keys move the OFFSET rather than a
// selection. So `tab` reaches the zone, j/k scroll inside it, and there is
// nothing to navigate because nothing is selectable. That is the rule stated
// once, in a Spec, instead of as a branch in each screen's key handler.
//
// It is not ScrollNone. The arranger's focus ring is the sections that SCROLL:
// declaring the zone unscrollable does not make j/k do nothing in it; it makes
// j/k jump out of it.
//
// The field table is the half that knows how big it wants to be, so it
// declares that as a CEILING and fills toward it first. MinRows is hard — a
// kept section is never driven below it — so the height is a ceiling and not
// a floor: a nine-field table declaring twenty-one rows as its floor vanished
// on an 80×24 terminal that had thirteen.
//
// The ceiling is [Spec.ColumnMaxRows], not MaxRows: it is what the table is
// worth sharing the inspector column with the detail box below it, and it
// stops meaning anything once the body has stacked past its breakpoint or the
// zone has gone full screen — both hand it the whole body instead.
func (ids InspectorIDs) FieldsSpec(fields int) Spec {
	return Spec{
		ID: ids.Fields, MinWidth: screenkit.InspectorFloor, MinRows: inspectorFieldsMinRows,
		ColumnMaxRows: inspectorFieldsCeiling(fields), ExpectedRows: inspectorFieldsExpected(fields),
		Weight: inspectorFieldsWeight, ColumnGap: screenkit.ZoneGap, Scroll: ScrollItems,
	}
}

// DetailSpec is the box below: HISTORY, PREVIEW, RELATED.
//
// `selects` is for the one of them that is a list rather than a read-only body.
// `chromeRows` is any pinned line the zone paints BEYOND the kicker and its
// rule — Personas' RELATED prints a column header — and it is added to the
// floor because a floor that does not count a zone's own chrome buys a box
// with room for the chrome and none for a row.
func (ids InspectorIDs) DetailSpec(selects bool, chromeRows int) Spec {
	return Spec{
		ID: ids.Detail, MinWidth: screenkit.InspectorFloor, MinRows: inspectorDetailMinRows + max(0, chromeRows),
		Weight: inspectorDetailWeight, ColumnGap: screenkit.ZoneGap, Scroll: ScrollItems,
		SelectFirst: selects,
	}
}

// inspectorFieldsCeiling is the terminal rows a summary table of `n` fields
// may take: 3 + 2n is its unwrapped height — the top border, the title, its
// rule, then a row and a rule per field with the last rule doubling as the
// bottom border — plus one line per field for a value that has to wrap.
//
// The wrap allowance is why this is a ceiling rather than an exact height:
// what a value wraps to is a function of the width the arranger has not
// handed over yet, so the number stated here is an upper bound and the rows
// the zone does not spend simply shorten the column.
func inspectorFieldsCeiling(n int) int {
	if n < 1 {
		return 0
	}
	return 3 + 3*n
}

// inspectorFieldsExpected is [Spec.ExpectedRows] for the field table: the
// unwrapped height alone, without inspectorFieldsCeiling's one-row-per-field
// wrap allowance. It is what a value that does NOT wrap actually spends, and
// a value wrapping to more than one line is the exception the ceiling still
// exists to cap — not the case a stacked body's threshold should demand room
// for before it will even show the table beside a sibling.
func inspectorFieldsExpected(n int) int {
	if n < 1 {
		return 0
	}
	return 3 + 2*n
}
