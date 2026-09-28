// Package screengrid composes screen bodies out of nested rows and columns.
//
// # What it adds to the arranger
//
// components/screenlayout resolves ONE level: a flat set of sections becomes a
// stack, or a row of columns when every section opted in and every minimum
// width fits at once. That covers a screen whose body is a list, and a screen
// whose body is `[a over b] | c` — the two shapes the arranger was designed
// against.
//
// It does not cover a body whose shape is a TREE. A row band that spans the
// full width with two columns underneath it is not expressible as a flat set,
// because "spans the columns beside it" is not something a section can say. Nor
// is a board: N lanes side by side where N comes from the workflow and the
// terminal fits four of them, so the rest have to be reachable by sliding
// rather than by shrinking.
//
// screengrid is those two capabilities and nothing else:
//
//   - NESTING. A cell may be another grid. Every level is resolved by the
//     arranger, at the box its parent gave it, so width distribution, row
//     distribution, dropping from the bottom and the side-by-side breakpoint all
//     work the same at depth three as they do at depth one.
//   - HORIZONTAL WINDOWING. A row of columns that cannot fit its children slides
//     over them instead of crushing them, which is what the board does today by
//     hand.
//
// Everything else is delegated. This package computes no width, no row budget
// and no scroll offset of its own — [screenlayout.ArrangeIn] does all three, and
// the cursor and offset of every leaf live in one [screenlayout.State] keyed by
// the leaf's id.
//
// # The shapes the app has
//
// Every screen body in the TUI is one of these, which is the completeness claim
// this package is built against:
//
//	Cell(...)                          a single surface: a detail body, a table
//	Rows(a, b, c)                      bands that shrink and then drop
//	Cols(a, b)                         two zones side by side, stacking when narrow
//	Cols(Rows(a, b), c)                 a nested group used by task detail and project
//	Cols(...).Windowed()               the board's lanes (one production consumer)
//	Rows(Cols(a, b), Cols(c, d))       a true grid: no production consumer yet
//
// Five of the six declared shapes have production consumers: Cell, Rows, Cols,
// nested Cols(Rows(...), ...), and Windowed. The true-grid shape has zero shipped
// screen consumers. It remains an intentionally supported component shape exercised
// by screengrid tests and the gallery. The Windowed shape is used by
// internal/tui/screens/board/layout.go:175 and by cmd/okt-gallery/shell_shapes.go:120.
//
// # Focus
//
// The focus ring is the leaves, depth first, in declaration order — so `tab`
// walks the body in reading order regardless of how deeply a cell is nested.
// Motion keys are routed to the focused leaf through the arranger that placed
// it this frame, which is what keeps this package out of the scroll arithmetic entirely.
//
// Focus decides who takes the keys and who paints as focused. It decides NOTHING
// about geometry: what a zone is given is a function of the box and the specs,
// so `tab` never moves a box and never takes a zone off the screen. Which zones
// are on screen is the box's answer — every zone that can have the minimum it
// declared is shown, and only a box that cannot pay for all of them falls back
// to giving the focused one everything ([walker.stack]). Filling the body with
// one zone on a terminal that could hold two is a mode, and `f` is how it is
// asked for.
package screengrid
