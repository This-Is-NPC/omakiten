package table

import (
	"omakiten/internal/domain"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if len(s.rows) == 0 {
		if len(s.projection.Tasks(taskprojection.Query{})) == 0 {
			return kit.Panel(kit.T("tui.empty.table_no_tasks"))
		}
		return kit.Panel(kit.T("tui.empty.table_filtered"))
	}

	// Panel owns the leading blank + border; the root Cell fills the panel
	// content box with zero overdraw (panelBox.Rows == PanelChrome viewport).
	return "\n" + screenkit.Indent(screengrid.Render(kit, s.grid, s.panelBox(kit), s.rowsRoot(kit)).View, 2)
}

// compact reports whether the Cell is too narrow for the full column set.
// Its 70-column threshold is the former AvailableWidth() < 74 breakpoint
// expressed in panel-content coordinates (AvailableWidth()-4).
func (s Screen) compact(width int) bool { return width < 70 }

// compactWidth is the rule width the narrow layout draws at. It is derived
// from the Cell canvas, not from terminal geometry (P5 classification: fixed).
func (s Screen) compactWidth(width int) int {
	return screenkit.Clamp(width, 32, 68)
}

// header is the chrome above the scrolled task rows, for whichever layout is
// on screen. Pinned as Block.Header so the arranger keeps the crown fixed
// while rows scroll underneath.
func (s Screen) headerExtra(kit screenkit.Kit, width int) []string {
	if s.compact(width) {
		return nil
	}
	// Data rows sit after CursorMarker + space. The catalog is the column
	// labels only; this indent keeps ID over the first cell instead of flush
	// against the box.
	label := kit.CursorMarker(false) + " " + kit.T("tui.table.header")
	return []string{kit.Styles.Info.Render(label)}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s Screen) priorityLabel(priority domain.Priority) string {
	return s.projection.PriorityLabel(priority)
}

func (s Screen) dependencyCount(taskID int64) int {
	return len(s.projection.Blockers(taskID))
}

func (s Screen) commentCount(taskID int64) int {
	return s.projection.Badges(taskID).Comments
}
