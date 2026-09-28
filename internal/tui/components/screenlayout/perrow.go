package screenlayout

import (
	"omakiten/internal/tui/components/cardtable"
	"omakiten/internal/tui/components/scrollwindow"
)

// This file is [Block.PerRow]: a section whose items are laid out N to a line.
//
// # The defect it exists for
//
// A card grid has two units and this package had one. Entity List paints 42
// cards, packs them 2, 3 or 6 to a terminal line depending on the width, and
// scrolls the LINES — so the thing the window slides over is not the thing the
// cursor lands on. Before this field existed the screen resolved that by
// handing the arranger the JOINED LINES as its items and keeping a private card
// cursor beside them, mapping one onto the other with `cursor/cols` on every
// keystroke and every paint. That is a second cursor, in a second unit, held
// beside the arranger's — the exact shape of every defect this package was
// written to delete, and it survived the screengrid migration because the
// migration only moved where the body was mounted.
//
// PerRow is the layout statement that removes it. The section says "my items go
// N to a line"; the arranger folds them, measures each line as the tallest item
// on it, windows by lines, and keeps the cursor on the ITEM. There is one
// cursor, in one unit, and the screen owns neither.
//
// # What it costs
//
// It bends the invariant [State.Cursor] states: "It is an item index at every
// layer of this package; there is no line index to confuse it with." The cursor
// and the offset are still item indices — the offset is always the FIRST item
// of a line, never a line ordinal — but a line index now exists inside the
// resolver, and every window computation runs on it. That is a real second unit
// in a package that had one, and it is worth saying out loud rather than
// discovering later.
//
// Two things keep it from spreading. The line arrays never leave [measured]:
// [Placement] reports items, heights, cursor, offset and the visible range in
// ITEMS, and reports the fold itself as [Placement.PerRow] so a caller can
// account for it rather than re-derive it. And every translation goes through
// the four methods below, so there is one spelling of "which line is this item
// on" and one of "which item opens this line".
//
// The one number that stays in LINES is the "▲ N above" / "▼ N below" hint the
// body paints, because [screenkit.Kit.ScrollWindowSplit] builds it from the
// slice it is handed and that slice is the folded lines. So a grid's hint
// counts hidden LINES while [Placement.Above] and [Placement.Below] count
// hidden ITEMS. That is the pre-existing behaviour of every card grid in the
// tree, preserved deliberately: the alternative is a second hint renderer here,
// which is a copy of screenkit's.
//
// # Why zero and one are a strict no-op
//
// Twenty of the twenty-one screens lay one item to a line, and a Block literal
// that says nothing about PerRow must keep behaving exactly as it did. So
// [normalizePerRow] collapses 0 and 1 to 1, no fold is taken, the window
// accessors return the item arrays themselves, and [measured.line] /
// [measured.item] are the identity. Every expression in [renderSection],
// [reclamp], [cursorTarget] and [offsetTarget] is then literally the expression
// that shipped. Pinned by the whole pre-existing test file set, which was not
// modified when this landed.

// normalizePerRow is a Block's declared PerRow reduced to the items per line
// the resolver will actually use. Anything below two is one item per line,
// which is the no-op above.
func normalizePerRow(perRow int) int {
	if perRow < 2 {
		return 1
	}
	return perRow
}

// foldPerRow joins items into lines of perRow and measures each line as the
// tallest item on it.
//
// The join is [cardtable.Row] — the same helper the screens that pack cards
// already used to build these lines by hand — so the bytes this produces are
// the bytes those screens produced, and the migration off a private fold is a
// move rather than a rewrite. A short final line is joined from what is left.
func foldPerRow(items []string, heights []int, perRow int) (lineItems []string, lineHeights []int) {
	if len(items) == 0 {
		return nil, nil
	}
	count := (len(items) + perRow - 1) / perRow
	lineItems, lineHeights = make([]string, count), make([]int, count)
	for line := 0; line < count; line++ {
		start := line * perRow
		end := min(start+perRow, len(items))
		lineItems[line] = cardtable.Row(items[start:end])
		tallest := 1
		for _, height := range heights[start:end] {
			if height > tallest {
				tallest = height
			}
		}
		lineHeights[line] = tallest
	}
	return lineItems, lineHeights
}

// windowItems is what the window slides over: the folded lines for a PerRow
// section, and the items themselves for every other one.
func (m measured) windowItems() []string {
	if m.perRow > 1 {
		return m.lineItems
	}
	return m.items
}

// windowHeights is the terminal rows each of [measured.windowItems] occupies.
func (m measured) windowHeights() []int {
	if m.perRow > 1 {
		return m.lineHeights
	}
	return m.heights
}

// line is the line an ITEM index sits on. The no-selection sentinel passes
// through unchanged, so a caller may hand it one.
func (m measured) line(item int) int {
	if m.perRow > 1 && item > 0 {
		return item / m.perRow
	}
	return item
}

// item is the first ITEM index on a line — the only offset a PerRow section
// ever holds, which is what keeps the stored offset an item index.
func (m measured) item(line int) int {
	if m.perRow > 1 && line > 0 {
		return line * m.perRow
	}
	return line
}

// resyncPair settles a (cursor, offset) pair against this measurement's window,
// through the one [scrollwindow.Resync] every other path runs.
//
// For the common section it IS that call. For a [Block.PerRow] section the pair
// is folded onto lines, settled there, and unfolded: the cursor keeps the item
// it was already clamped and masked onto — folding cannot move it, because a
// line is where its item is — and the offset comes back as the first item of
// whichever line the window settled on.
func (m measured) resyncPair(cursor, offset int) (int, int) {
	if m.perRow <= 1 {
		return scrollwindow.Resync(cursor, offset, m.heights, m.itemViewport)
	}
	line, lineOffset := scrollwindow.Resync(m.line(cursor), m.line(offset), m.lineHeights, m.itemViewport)
	if line < 0 {
		return -1, m.item(lineOffset)
	}
	return cursor, m.item(lineOffset)
}
