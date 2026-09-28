package graph

import (
	"strconv"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// sectionBody is the one section Graph declares. MinRows is a hard floor (K1):
// with a single Weight:1 section there is no sibling to drop when the HostBox
// is short — the section simply receives whatever rows the panel viewport
// budgets.
const sectionBody = screenlayout.ID("graph-body")

func (s Screen) bodySection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionBody, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems, SelectFirst: true,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.bodyBlock(kit, canvas)
		},
	}
}

func (s Screen) bodyRoot(kit screenkit.Kit) screengrid.Node {
	section := s.bodySection(kit)
	return screengrid.Cell(section.Def, section.Body)
}

// bodyBlock pins the dependency-count kicker (and the blank under it) as
// Header so the crown does not scroll away, and exposes every projected graph
// line as an item.
//
// The blank line between two roots is an item like any other — it occupies a
// row and the window has to count it — so what keeps the cursor off it is
// Block.Selectable, the ascending indices of the lines that carry a node. The
// cursor itself is the arranger's, read from the canvas: there is one statement
// of where the selection is, and this block does not make a second one.
func (s Screen) bodyBlock(kit screenkit.Kit, canvas screenlayout.Canvas) screenlayout.Block {
	width := max(1, canvas.Width())
	inner := max(1, width-panel.Borders)
	items := make([]string, len(s.lines))
	for i, line := range s.lines {
		items[i] = screenkit.Truncate(screenkit.Sanitize(line.Text), inner)
	}
	if cursor := canvas.Cursor(); cursor >= 0 && cursor < len(items) {
		items[cursor] = kit.Styles.HintAccent.Render(screenkit.Truncate(screenkit.Sanitize(s.lines[cursor].Text), inner))
	}
	block := framed.List(kit.Styles.Border, width, s.headerKicker(kit, inner), nil, items)
	block.Selectable = selectableIndices(s.lines)
	return block
}

// panelBox is the geometry ArrangeIn / HandleKeyIn / Resync share: panel
// content width and the PanelChrome viewport (no leading blank — Panel owns
// that row outside the arranged body). Header rows are charged by the
// arranger from Block.Header, not subtracted here.
//
// When the terminal is still unmeasured (Height<=0), Kit.ViewportRows returns
// the historical "0 means unlimited" sentinel. ArrangeIn treats 0 as ZERO rows,
// so falling through would paint an empty panel before the first WindowSizeMsg.
// HostBox applies the unmeasured-height assumption instead.
func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().ViewportRows()
	if kit.Height <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	return screenlayout.Box{
		Width: kit.PanelContentWidth(),
		Rows:  rows,
	}
}

// resyncBody re-clamps the body against the CURRENT projection and geometry and
// parks focus on the one zone. It is the refresh — bind, enter, resize, and a
// selection the screen seeded itself — and it is deliberately NOT on the
// keystroke path: screengrid.HandleKey resolves the body it is moving within
// and settles the pair itself, so calling this beside it would compose the body
// a second time for a measurement the keystroke already took.
func (s Screen) resyncBody(kit screenkit.Kit) Screen {
	s.grid = s.grid.Resync(kit, s.panelBox(kit), s.bodyRoot(kit))
	return s
}

// header is the chrome the graph panel draws above its scrolled rows: the
// dependency-count kicker and the blank spacer under it.
func (s Screen) headerKicker(kit screenkit.Kit, contentWidth int) string {
	kickerLabel := screenkit.Sanitize(kit.T("tui.kicker.dependency_graph"))
	if budget := contentWidth - len("▸  · ") - len(strconv.Itoa(len(s.deps.Dependencies))); budget > 0 {
		kickerLabel = screenkit.Truncate(kickerLabel, budget)
	}
	return kit.Styles.FocusKickerCount(kickerLabel, len(s.deps.Dependencies))
}
