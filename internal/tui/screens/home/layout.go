package home

import (
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionCards is the one section Home declares. MinRows is a hard floor (K1):
// with a single Weight:1 section there is no sibling to drop when the HostBox
// is short — the section simply receives whatever rows the column viewport
// budgets. The screen exists to list projects; there is no secondary zone to
// yield.
const sectionCards = screenlayout.ID("home-cards")

func (s Screen) cardsSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionCards, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems, SelectFirst: true,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.cardsBlock(frame, canvas)
		},
	}
}

// cardsBlock pins the PROJECTS kicker as Header and exposes each project card
// as one variable-height item. Selection is painted into the card border from
// canvas.Cursor() so HandleKeyIn can move the highlight without a Block.At
// override folding it back.
func (s Screen) cardsBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	kit := frame.Kit()
	width := canvas.Width()
	// Failed routes through screenstate in View; compose skips the body on the same
	// condition (#126545).
	if s.err != nil {
		boxed := framed.Box(kit.Styles.Border, width, s.kicker(kit), nil)
		return screenlayout.Block{
			Header: boxed.Header,
			Footer: boxed.Footer,
			Cursor: screenlayout.NoSelection(),
		}
	}
	contentWidth := framed.ContentWidth(width)
	cards := make([]string, len(s.projects))
	cursor := canvas.Cursor()
	for i, project := range s.projects {
		cards[i] = s.renderCard(kit, project, i == cursor, contentWidth)
	}
	block := framed.List(kit.Styles.Border, width, s.kicker(kit), nil, cards)
	return block
}

// columnBox is the geometry ArrangeIn / HandleKeyIn / ResyncIn share: the
// column width and host viewport, with frame chrome charged by the arranger.
func (s Screen) columnBox(frame screenhost.Frame) screenlayout.Box {
	kit := frame.Kit()
	host := screenlayout.HostBox(kit)
	width := s.columnInner(host.Width)
	return screenlayout.Box{Width: width, Rows: host.Rows}
}

func (s Screen) root(frame screenhost.Frame) screengrid.Node {
	section := s.cardsSection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

func (s Screen) gridResult(frame screenhost.Frame) screengrid.Result {
	return screengrid.Render(frame.Kit(), s.grid, s.columnBox(frame), s.root(frame))
}

// syncCardsWindow seeds the arranger from the picker's cursor and re-clamps
// the window against live geometry. picker.Scroll is synced FROM layout.Offset
// so selection helpers stay aligned without the picker owning the window.
func (s Screen) syncCardsWindow(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.WithFocus(sectionCards).WithCursor(sectionCards, s.picker.Cursor).
		Resync(kit, s.columnBox(frame), s.root(frame))
	return s.syncPickerFromGrid()
}

func (s Screen) syncPickerFromGrid() Screen {
	layout := s.grid.Layout()
	if c := layout.Cursor(sectionCards); c >= 0 {
		s.picker.Cursor = c
	}
	s.picker = s.picker.WithScroll(layout.Offset(sectionCards))
	return s
}

// wrapColumn paints the arranged body into Home's Column chrome. Empty projects
// still append the register hint under the column, outside the arranger.
func (s Screen) wrapColumn(frame screenhost.Frame, arranged string) string {
	kit := frame.Kit()
	columnInner := s.columnBox(frame).Width
	out := "\n" + screenkit.Indent(arranged, 2)
	if !s.loading && s.err == nil && len(s.projects) == 0 {
		out += "\n\n" + screenkit.Indent(s.emptyHint(kit, columnInner), 2)
	}
	return out
}
