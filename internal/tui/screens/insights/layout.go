package insights

import (
	"strings"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// sectionBody is the one section Insights declares. MinRows is a hard floor
// (K1): with a single Weight:1 section there is no sibling to drop when the
// HostBox is short — the section simply receives whatever rows the panel
// viewport budgets.
const sectionBody = screenlayout.ID("insights-body")

var bodySpec = screenlayout.Spec{
	ID: sectionBody, MinRows: 1, Weight: 1,
	Scroll: screenlayout.ScrollItems,
}

func (s Screen) bodySection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: bodySpec,
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.bodyBlock(kit, canvas.Width())
		},
	}
}

func (s Screen) bodyRoot(kit screenkit.Kit) screengrid.Node {
	section := s.bodySection(kit)
	return screengrid.Cell(section.Def, section.Body)
}

// bodyBlock splits renderBody into a pinned kicker header and line-items so
// scroll stays line-based (one string per line) while the editorial crown does
// not scroll away. CapRows keeps every item one terminal row at the panel
// content width, so the arranger's scroll window always counts single-line
// rows.
func (s Screen) bodyBlock(kit screenkit.Kit, width int) screenlayout.Block {
	width = max(1, width)
	inner := max(1, width-panel.Borders)
	lines := screenkit.CapRows(strings.Split(s.renderBody(kit, inner), "\n"), inner)
	if len(lines) == 0 {
		return screenlayout.Block{Cursor: screenlayout.NoSelection()}
	}
	kicker, items := lines[0], lines[1:]
	// Pin the blank spacer under the kicker with the header so section content
	// scrolls under a fixed crown.
	var columnHeader []string
	if len(items) > 0 && items[0] == "" {
		columnHeader, items = []string{""}, items[1:]
	}
	block := framed.List(kit.Styles.Border, width, kicker, columnHeader, items)
	block.Cursor = screenlayout.NoSelection()
	return block
}

// panelBox is the geometry shared by the grid's render, key handling and
// resync passes: panel content width and the classified chrome viewport. Raw
// terminal height is deliberately not a section-body concern.
func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	return screenlayout.Box{
		Width: kit.PanelContentWidth(),
		Rows:  kit.Chrome().ViewportRows(),
	}
}
