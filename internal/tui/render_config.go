package tui

// renderSettingsEntity renders one Settings entity sub (Laws / Personas /
// Skills / Templates / Tags) as a single full-width kanban-style column
// whose inner cards wrap into a grid using the available terminal width
// (`entityGridCols`). The horizontal 5-column "all-kinds" grid that
// lived here in T1 was retired in T2 — each entity kind now owns its
// own sub. The wrap math lives on `renderEntityCellWithViewport` so a
// future caller can drive the same packing with a different width
// budget if needed.
func (m Model) renderSettingsEntity(kind entityKind) string {
	width := m.availableWidth()
	if width < entityListWidth+2 {
		width = entityListWidth + 2
	}
	columnInner := width - 2
	contentWidth := columnInner - 2
	if contentWidth < entityListWidth {
		contentWidth = entityListWidth
	}
	viewport := m.entityViewportRows()
	cell := m.renderEntityCellWithViewport(kind, viewport, contentWidth)
	// Content-sized box: short entity sets keep a small box; tall ones
	// hit the renderEntityCellWithViewport scroll cap (▲ N above /
	// ▼ N below). Forcing a fixed Height made the empty bottom show as
	// dead vertical space — the user flagged that as a regression.
	body := m.styles.kanbanColumnSized(columnInner, 0).Render(cell)
	return "\n" + indentBlock(body, 2)
}
