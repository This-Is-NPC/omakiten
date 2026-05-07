package tui

// renderSettingsEntity renders one Settings entity sub (Laws / Personas /
// Skills / Templates / Tags) as a single full-width kanban-style column.
// The horizontal 5-column grid that lived here in T1 was replaced in T2
// when each entity kind became its own sub-menu under Settings.
func (m Model) renderSettingsEntity(kind entityKind) string {
	width := m.availableWidth()
	if width < entityListWidth+2 {
		width = entityListWidth + 2
	}
	columnInner := width - 2
	cell := m.renderEntityCellWithViewport(kind, m.entityViewportRows())
	body := m.styles.kanbanColumn.Width(columnInner).Render(cell)
	return "\n" + indentBlock(body, 2)
}
