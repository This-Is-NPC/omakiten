package table

import (
	"fmt"
	"strings"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// sectionRows is the one section Table declares. MinRows is a hard floor (K1):
// with a single Weight:1 section there is no sibling to drop when the HostBox
// is short — the section simply receives whatever rows the panel viewport
// budgets.
const sectionRows = screenlayout.ID("table-rows")

var rowsSpec = screenlayout.Spec{
	ID: sectionRows, MinRows: 1, Weight: 1,
	Scroll: screenlayout.ScrollItems, SelectFirst: true,
}

func (s Screen) rowsSection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: rowsSpec,
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.rowsBlock(kit, canvas)
		},
	}
}

func (s Screen) rowsRoot(kit screenkit.Kit) screengrid.Node {
	section := s.rowsSection(kit)
	return screengrid.Cell(section.Def, section.Body)
}

// rowsBlock inherits the grid cursor. syncRowsWindow parks the screen's private
// selected cursor in the grid before Resync, then reads the clamped value back.
func (s Screen) rowsBlock(kit screenkit.Kit, canvas screenlayout.Canvas) screenlayout.Block {
	width := max(1, canvas.Width())
	inner := max(1, width-panel.Borders)
	kicker := kit.Styles.FocusKickerCount(kit.T("tui.kicker.tasks"), len(s.rows))
	return framed.List(kit.Styles.Border, width, kicker, s.headerExtra(kit, inner), s.dataRows(kit, inner))
}

// dataRows paints every projected task as one terminal row at the given panel
// content width — wide FormatRow grid or compact density layout.
func (s Screen) dataRows(kit screenkit.Kit, width int) []string {
	if s.compact(width) {
		// The compact rule consumes the Cell's exact width. Before the mount,
		// AvailableWidth()-4 reached the same number; carrying Canvas.Width
		// closes the P5 geometry side door instead of recomputing it.
		return s.compactDataRows(kit, s.compactWidth(width))
	}
	// Six data cells joined by FormatRow (5 gaps) sit after marker+space.
	// That chrome is 2+5 = 7; the remaining budget is what FitWidths may spend
	// on the six widths so the painted row never wraps inside the panel.
	natural := []int{4, 11, 8, 5, 9, max(8, width-44)}
	widths := gridtable.FitWidths(natural, width-7, 3)
	rows := make([]string, 0, len(s.rows))
	for i, task := range s.rows {
		rows = append(rows, kit.CursorMarker(i == s.selected)+" "+gridtable.FormatRow([]string{
			fmt.Sprintf("%d", task.ID),
			screenkit.Sanitize(task.BucketKey),
			screenkit.Sanitize(s.priorityLabel(task.Priority)),
			fmt.Sprintf("%d", s.dependencyCount(task.ID)),
			fmt.Sprintf("%d", s.commentCount(task.ID)),
			screenkit.Sanitize(task.Title),
		}, widths))
	}
	return rows
}

func (s Screen) compactDataRows(kit screenkit.Kit, width int) []string {
	// Caps come from FitWidths so a squeezed panel shrinks the same way the
	// wide FormatRow path does. Cells are Truncate'd then joined without
	// PadLine — compact is a density layout, not an aligned grid, and padding
	// short buckets would steal title space the old painter gave the title.
	caps := gridtable.FitWidths([]int{1, 6, 12, 10, max(8, width-1-6-12-10-4)}, width-4, 1)
	rows := make([]string, 0, len(s.rows))
	for i, task := range s.rows {
		prefix := fmt.Sprintf("%s %s %s %s ",
			gridtable.Truncate(kit.CursorMarker(i == s.selected), caps[0]),
			gridtable.Truncate(fmt.Sprintf("#%d", task.ID), caps[1]),
			gridtable.Truncate(screenkit.Sanitize(task.BucketKey), caps[2]),
			gridtable.Truncate(screenkit.Sanitize(s.priorityLabel(task.Priority)), caps[3]),
		)
		remaining := width - screenkit.VisibleWidth(prefix)
		if remaining < 8 {
			rows = append(rows, gridtable.Truncate(strings.TrimRight(prefix, " "), width))
			continue
		}
		rows = append(rows, prefix+gridtable.Truncate(screenkit.Sanitize(task.Title), remaining))
	}
	return rows
}

// panelBox is the geometry Render / HandleKey / Resync share: panel content
// width and the PanelChrome viewport (no leading blank — Panel owns that row
// outside the arranged body). Header rows are charged by the Cell's Block.Header,
// not subtracted here.
//
// When the terminal is still unmeasured (Height<=0), Kit.ViewportRows returns
// the historical "0 means unlimited" sentinel. ArrangeIn treats 0 as ZERO rows,
// so HostBox supplies the unmeasured-height assumption instead.
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

// syncRowsWindow parks the screen's private selected cursor in the grid before
// Resync. Reading it back afterward preserves the private owner while letting
// the grid clamp it when data or geometry changes.
func (s Screen) syncRowsWindow(kit screenkit.Kit) Screen {
	s.grid = s.grid.WithFocus(sectionRows).WithCursor(sectionRows, s.selected).
		Resync(kit, s.panelBox(kit), s.rowsRoot(kit))
	if c := s.grid.Layout().Cursor(sectionRows); c >= 0 {
		s.selected = c
	}
	return s
}
