package main

import (
	"fmt"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenlayout"
)

// A shape is one screen's REAL body, transcribed.
//
// # Why these are written out and not generated
//
// The first version of this file generated trees from two numbers — columns and
// cells-per-column — and every shape it produced was a rectangle. Task detail
// came out as two columns of two cells; it has THREE sections, `[details over
// subtasks] | activity`, and the activity feed is one cell running the full
// height of its column. No pair of counts says that, and a symmetric grid is
// not a smaller version of the real thing — it is a different layout that
// happens to have a similar area.
//
// So each shape is transcribed from the screen that has it, minimum widths,
// proportions and caps included, with the source named. What the props then
// override is POLICY, never topology: you can widen a minimum to watch the
// breakpoint move, but you cannot turn task detail into a grid, because task
// detail is not a grid.
//
// The numbers below are the constants the screens actually declare. When a
// screen's constants change, these go stale and the note beside them is the
// place to notice.

// cellSpec is one leaf's declared policy, transcribed from a screen.
type cellSpec struct {
	id string
	// minWidth, maxWidth, widthPercent are the width declaration. Zero means
	// unset, exactly as on screenlayout.Spec.
	minWidth, maxWidth, widthPercent int
	// minRows and maxRows are the row declaration; equal values are a fixed
	// band, which is what a form field or a kicker is.
	minRows, maxRows int
	weight           int
	gap              int
}

// fixed is the cellSpec fragment for a band of exactly n rows.
func fixed(id string, rows int) cellSpec {
	return cellSpec{id: id, minRows: rows, maxRows: rows, weight: 1}
}

// shellShape is one screen's layout plus what to look for at each terminal.
type shellShape struct {
	// name is the screen the shape is taken from, and the scenario's prefix.
	name string
	// source names where the numbers came from, shown in the status line so a
	// stale transcription is visible rather than assumed.
	source string
	// build assembles the tree. leaf turns a cellSpec into a coloured block and
	// keeps the tint sequence going.
	build func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node
	// content seeds the item count for this screen's scenarios. Empty is `auto`
	// — the cell fills its box exactly, which reads the layout most clearly. A
	// screen whose point is the SCROLL WINDOW sets a number instead.
	content string

	small, medium, large string
}

// shellShapes are the shapes, in the order they appear in the shape prop and in
// the scenario index.
func shellShapes() []shellShape {
	return []shellShape{
		{
			name:   "task detail",
			source: "internal/tui/screens/taskdetail — leftColumnMinWidth 60, feedMinWidth 44, feedMaxWidth 96, feedWidthPercent 45, zoneGap 2",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				// THREE sections: the details block and the sub-task board
				// stacked in one column, the activity feed beside them running
				// the full height of its own.
				return screengrid.Cols(group(cellSpec{id: "body", gap: 2}),
					screengrid.Rows(group(cellSpec{id: "left-column", minWidth: 60, minRows: 12, weight: 1, gap: 2}),
						leaf(cellSpec{id: "details", minWidth: 60, minRows: 6, weight: 1}),
						leaf(cellSpec{id: "subtasks", minWidth: 60, minRows: 6, weight: 1}),
					),
					leaf(cellSpec{id: "activity", minWidth: 44, maxWidth: 96, widthPercent: 45, minRows: 4, weight: 2, gap: 2}),
				)
			},
			small:  "60 + 44 + the gap does not fit in 76, so the feed drops UNDER the left column instead of beside it",
			medium: "the shape as it ships: details over subtasks, the feed beside them at 45% capped to 96",
			large:  "45% of 196 is 88, still under the feed's 96-column cap — the left column takes the other 106",
		},
		{
			name:   "project",
			source: "internal/tui/screens/project — metaColumnMinWidth 32, feedMinWidth 44, feedMaxWidth 96, feedWidthPercent 45, zoneMinRows 3, zoneGap 2",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				// The same three-section shape as task detail, with a much
				// narrower left column — which is why it keeps its columns at a
				// width where task detail has already given them up.
				return screengrid.Cols(group(cellSpec{id: "body", gap: 2}),
					screengrid.Rows(group(cellSpec{id: "meta-column", minWidth: 32, minRows: 6, weight: 1, gap: 2}),
						leaf(cellSpec{id: "meta", minWidth: 32, minRows: 3, weight: 1}),
						leaf(cellSpec{id: "dashboard", minWidth: 32, minRows: 3, weight: 1}),
					),
					leaf(cellSpec{id: "activity", minWidth: 44, maxWidth: 96, widthPercent: 45, minRows: 3, weight: 2, gap: 2}),
				)
			},
			small:  "32 + 44 + the gap DOES fit in 76 — same shape as task detail, still side by side at the floor",
			medium: "meta over dashboard, the feed beside them",
			large:  "the feed is 88 (45%, under its cap) and the meta column takes the other 106",
		},
		{
			name:   "board",
			source: "internal/tui/screens/board — computeLayout's minInner 28 / maxInner 44, plus the column border",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				lanes := make([]screengrid.Node, 0, 7)
				for i := 1; i <= 7; i++ {
					lanes = append(lanes, leaf(cellSpec{
						id: fmt.Sprintf("lane %d", i), minWidth: 30, maxWidth: 46, minRows: 6, weight: 1,
					}))
				}
				return screengrid.Cols(group(cellSpec{id: "board", gap: 1}), lanes...).Windowed()
			},
			small:  "two lanes of 38 fit; the other five slide in with h/l and are counted in the hint",
			medium: "three lanes of 38 — a lane never shrinks below its 30-column minimum, it goes off-screen instead",
			large:  "six lanes of 32 fit at once; the seventh is still off the right edge",
		},
		{
			name:   "logs",
			source: "internal/tui/screens/logs — the chip strip, the summary block View drops when it cannot afford it, and the event panel",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				return screengrid.Rows(group(cellSpec{id: "body"}),
					leaf(cellSpec{id: "chips", minWidth: 24, minRows: 1, maxRows: 3, weight: 1}),
					leaf(cellSpec{id: "summary", minWidth: 24, minRows: 8, weight: 1}),
					leaf(cellSpec{id: "panel", minWidth: 24, minRows: 6, weight: 3}),
				)
			},
			small:  "the strip caps at 3 rows; the two minimums (8 and 6) eat most of the 21 left, so weight barely gets a say",
			medium: "room for weight to matter at last — the panel takes 23 against the summary's 14",
			large:  "the strip is STILL 3 rows — a cap does not grow — and the two bands split the rest",
		},
		{
			name:   "task form",
			source: "internal/tui/screens/taskform — kicker, hint, then the screen-owned title / description / priority / tags / parent form",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				return screengrid.Rows(group(cellSpec{id: "body"}),
					leaf(fixed("kicker", 1)),
					leaf(fixed("hint", 1)),
					leaf(fixed("title", 3)),
					leaf(cellSpec{id: "description", minWidth: 24, minRows: 6, weight: 1}),
					leaf(fixed("priority", 3)),
					leaf(fixed("tags", 3)),
					leaf(fixed("parent", 3)),
				)
			},
			small:  "six fixed bands spend 14 rows and the description takes the other 10 — this is the budget #2443 ignores today",
			medium: "the fixed fields are unchanged and every extra row goes to the description",
			large:  "still 14 rows of fixed field: a cap refuses surplus in both directions",
		},
		{
			name:   "detail",
			source: "internal/tui/screens/{description,entitydetail,commentdetail,table} — one body over gridtable.Detail + viewport",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				return leaf(cellSpec{id: "body", minWidth: 24, minRows: 4, weight: 1})
			},
			content: "60",
			small:   "sixty items into 24 rows: one surface, one window, a hint on each edge",
			medium:  "the same list with 40 rows to show it in — fewer items hidden, same single surface",
			large:   "50 rows still cannot hold 60 items, so the window survives even on a desktop",
		},
		{
			name:   "grid 2×2",
			source: "no screen has this yet — the shape a flat section list cannot say",
			build: func(leaf func(cellSpec) screengrid.Node, group func(cellSpec) screenlayout.Spec) screengrid.Node {
				return screengrid.Rows(group(cellSpec{id: "body"}),
					screengrid.Cols(group(cellSpec{id: "band 1", minRows: 6, weight: 1, gap: 1}),
						leaf(cellSpec{id: "1·1", minWidth: 38, minRows: 5, weight: 1}),
						leaf(cellSpec{id: "1·2", minWidth: 38, minRows: 5, weight: 1}),
					),
					screengrid.Cols(group(cellSpec{id: "band 2", minRows: 6, weight: 1, gap: 1}),
						leaf(cellSpec{id: "2·1", minWidth: 38, minRows: 5, weight: 1}),
						leaf(cellSpec{id: "2·2", minWidth: 38, minRows: 5, weight: 1}),
					),
				)
			},
			small:  "two 38-column minimums do not fit in 76: each band gives up and stacks, so the grid degrades to a list of four",
			medium: "the grid proper — the row boundary lines up across both columns, which is what nesting buys",
			large:  "the full grid with every cell at its comfortable size",
		},
	}
}

// shellScenarios is the shell's index: one row per SCREEN per TERMINAL, so
// "what does this screen do when the window changes" is answered by walking
// three adjacent rows instead of remembering the last one.
//
// small / medium / large are 80x24, 120x40 and 200x50 — the same three
// geometries screentest.Geometries records every screen golden at. Reusing them
// is the point: the sizes a layout is approved at here are the sizes the fit
// gate then holds it to, so an approval cannot be given at a geometry nothing
// checks.
//
// The three rows of a screen differ in NOTHING but width and height. A size
// that also changed a minimum would make the comparison between two adjacent
// rows meaningless, which is the whole reason to put them next to each other.
func shellScenarios() []scenario {
	shapes := shellShapes()
	out := make([]scenario, 0, len(shapes)*3)
	for _, shape := range shapes {
		out = append(out,
			shape.at("small", 80, 24, shape.small),
			shape.at("medium", 120, 40, shape.medium),
			shape.at("large", 200, 50, shape.large),
		)
	}
	return out
}

func (s shellShape) at(size string, width, height int, note string) scenario {
	return scenario{
		name:   s.name + " · " + size,
		note:   note,
		width:  width,
		height: height,
		props:  s.scenarioProps(),
	}
}

// scenarioProps is what a scenario of this shape seeds: the screen, and the
// item count when the shape asked for one.
func (s shellShape) scenarioProps() map[string]string {
	props := map[string]string{"screen": s.name}
	if s.content != "" {
		props["content"] = s.content
	}
	return props
}

// shapeNames is the shape prop's option list, in declaration order.
func shapeNames() []string {
	shapes := shellShapes()
	out := make([]string, len(shapes))
	for i, shape := range shapes {
		out[i] = shape.name
	}
	return out
}
