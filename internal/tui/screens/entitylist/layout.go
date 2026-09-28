package entitylist

import (
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/cardtable"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionGrid is the one section Entity List declares. Its ITEMS are the
// individual cards; how many of them share a terminal line is stated as
// Block.PerRow, and the arranger does the fold, the window and the cursor from
// there. MinRows is a hard floor (K1): with a single Weight:1 section there is
// no sibling to drop when the content box is short.
const sectionGrid = screenlayout.ID("entity-list-grid")

func (s Screen) gridSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionGrid, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems, SelectFirst: true,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.gridBlock(frame, canvas)
		},
	}
}

// gridBlock pins the kind kicker + rule as Header, hands over one item per
// CARD, and states how many cards go on a line.
//
// The selection is read off the canvas — it is the arranger's cursor and this
// screen has no other. Before the fold was declarable the block handed over the
// joined ROWS and overrode Block.Cursor with `cursor/cols`, which is the
// private second cursor PerRow exists to delete.
func (s Screen) gridBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	kit := frame.Kit()
	width := max(1, canvas.Width())
	header := screenkit.CapRows([]string{s.kicker(kit)}, width)
	if len(s.items) == 0 {
		return screenlayout.Block{
			Header: header,
			Items:  []string{kit.Styles.Hint.Render(kit.T("tui.empty.no_items"))},
			Cursor: screenlayout.NoSelection(),
		}
	}
	cols, cellWidth := s.gridCells(frame, width)
	cards := s.paintedCards(frame, canvas.Cursor())
	if cellWidth > width {
		// A terminal too narrow for one whole cell is the only geometry where a
		// card overflows its section, and it has to be TRUNCATED there: the
		// arranger measures an item by wrapping it, and a wrapped card would be
		// charged rows it does not paint. Above that width this is a no-op the
		// grid does not pay for.
		cards = screenkit.CapRows(cards, width)
	}
	return screenlayout.Block{Header: header, Items: cards, PerRow: cols}
}

// kicker is the pinned crown. View frames the arranged body afterwards so
// wrapping each card here would put a second box around every cell in a PerRow
// grid. The joining ├─┤ lives in that outer Frame, not in this header.
func (s Screen) kicker(kit screenkit.Kit) string {
	return kit.Styles.FocusKickerCount(string(s.descriptor.Kind), len(s.items))
}

// paintedCards reuses the cursor-independent card paints and repaints only the
// selected one. The cached source makes sharing the derived cache between value
// copies safe: a copy with different bound items can never reuse another copy's
// paint.
//
// It no longer joins anything. cardtable.Layout used to run here on every
// composition; the join is the arranger's now, taken from the cards it has
// already measured.
func (s Screen) paintedCards(frame screenhost.Frame, cursor int) []string {
	painted := make([]string, len(s.items))
	if s.cards != nil && sameItems(s.cards.source, s.items) {
		copy(painted, s.cards.items)
	} else {
		for i := range s.items {
			painted[i] = s.paintCard(frame, i, false)
		}
		if s.cards != nil {
			s.cards.source = cloneItems(s.items)
			s.cards.items = append(s.cards.items[:0], painted...)
		}
	}
	if cursor >= 0 && cursor < len(painted) {
		painted[cursor] = s.paintCard(frame, cursor, true)
	}
	return painted
}

// paintCard paints one grid cell. Selection is marked with the border alone —
// no cursor chevron — matching the pre-migration entity grid.
func (s Screen) paintCard(frame screenhost.Frame, index int, selected bool) string {
	item := s.items[index]
	return card.Painter{Styles: frame.Kit().Styles}.Render(card.Spec{
		Title:    item.Label,
		Badges:   item.Badges,
		Selected: selected,
		BoxWidth: cardBoxWidth, InnerWidth: cardContentWidth,
	})
}

// entityFrameExtraRows is Top + Join + Bottom that View paints around the
// arranged body. The kicker stays Block.Header (pinned). Charging those three
// inside the arranger as Frame header/footer left the cards outside the box
// and clipped the last card row against a full-width └.
const entityFrameExtraRows = 3

// contentBox is the geometry ArrangeIn / ResyncIn share: the interior of the
// frame View wraps around the arranged body. Header rows are charged by the
// arranger from Block.Header, not subtracted here.
//
// When ViewportRows is 0 — unmeasured Height, or a terminal too short for its
// <4 floor — ArrangeIn would paint nothing. HostBox supplies the shared
// assumption instead (same fallback plans/table use for unmeasured Height).
func (s Screen) contentBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().ViewportRows()
	if rows <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	rows = max(1, rows-entityFrameExtraRows)
	_, contentWidth := s.columnWidths(kit)
	return screenlayout.Box{Width: max(1, contentWidth), Rows: rows}
}

// resyncGrid re-clamps the body against the CURRENT items and geometry and
// parks focus on the one zone. It is the refresh — enter and resize — and it is
// deliberately NOT on the keystroke path: screengrid.HandleKey resolves the
// body it is moving within and settles the pair itself, so calling this beside
// it would compose the body a second time for a measurement the keystroke
// already took.
//
// What stood here was syncGridWindow, which pushed `cursor/cols` into the
// arranger through WithLayout on every key. Its comment justified the private
// cursor as "settingspicker-style dual ownership", and that citation was WRONG:
// settingspicker READS its cursor back from the picker it drives, so there is
// one authority there. entitylist never read anything back — it wrote a derived
// row index and kept the card ordinal for itself, which is not dual ownership
// but two cursors. Both the function and the excuse are gone.
func (s Screen) resyncGrid(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.WithFocus(sectionGrid).Resync(kit, s.contentBox(kit), s.root(frame))
	return s
}

// gridCells is how many cards fit on a line at this width, and how wide one
// painted cell is. Only the screen can answer either — the card painter is
// this package's — so the count is what it DECLARES to the arranger through
// Block.PerRow rather than something it acts on itself.
func (s Screen) gridCells(frame screenhost.Frame, width int) (cols, cellWidth int) {
	cellWidth = 30
	if len(s.items) > 0 {
		cellWidth = screenkit.VisibleWidth(s.paintCard(frame, 0, false))
	}
	return cardtable.Cols(width, cellWidth), cellWidth
}

// columnWidths is the grid's own width clamp — entitylist cannot paint below
// 30 columns, and AvailableWidth carries no floor of its own.
func (s Screen) columnWidths(kit screenkit.Kit) (columnInner, contentWidth int) {
	width := max(kit.AvailableWidth(), 30)
	return width - 2, width - 4
}
