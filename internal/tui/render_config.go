package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderConfig() string {
	header := m.renderConfigHeader()

	// Entity lists are rendered as separate, individually-bordered columns
	// joined horizontally with a 1-space gap — same shape as the kanban
	// board, so the user navigates with the same mental model: scroll the
	// horizontal window so the focused column is always in view.
	allKinds := configEntityKinds()
	cap := m.entityKindCapacity()
	if cap > len(allKinds) {
		cap = len(allKinds)
	}
	focused := indexOfEntityKind(allKinds, m.entityKind)
	start := scrollIntoView(m.entityKindScroll, focused, len(allKinds), cap)
	end := start + cap
	if end > len(allKinds) {
		end = len(allKinds)
	}
	visible := allKinds[start:end]

	// Compute the actual viewport budget for cards inside each column by
	// measuring everything else first. Static chrome estimates would drift
	// every time the runtime/tokens table grows — using the rendered header
	// height is exact regardless of how many rows the tables produce.
	viewport := m.entityCardsViewport(header)

	columnStyle := m.styles.kanbanColumn.Width(entityListWidth)
	cells := make([]string, 0, len(visible))
	for _, kind := range visible {
		cells = append(cells, columnStyle.Render(m.renderEntityCellWithViewport(kind, viewport)))
	}

	parts := make([]string, 0, len(cells)*2)
	for i, cell := range cells {
		parts = append(parts, cell)
		if i < len(cells)-1 {
			parts = append(parts, " ")
		}
	}
	lists := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	if cap < len(allKinds) {
		// Show which sections are off-screen so the user knows ← / → keeps
		// scrolling beyond the visible window.
		hidden := []string{}
		for i, k := range allKinds {
			if i >= start && i < end {
				continue
			}
			hidden = append(hidden, k.plural())
		}
		if len(hidden) > 0 {
			lists += "\n  " + m.styles.hint.Render(fmt.Sprintf("sections %d–%d / %d · hidden: %s · ← / → scrolls", start+1, end, len(allKinds), strings.Join(hidden, ", ")))
		}
	}

	return "\n" + indentBlock(header+"\n\n"+lists, 2)
}

// configEntityKinds is the canonical horizontal order of the config entity
// columns — used both by renderConfig and the entity-kind scroll math.
func configEntityKinds() []entityKind {
	return []entityKind{entityKindLaw, entityKindPersona, entityKindSkill, entityKindTemplate, entityKindTag}
}

func indexOfEntityKind(kinds []entityKind, target entityKind) int {
	for i, k := range kinds {
		if k == target {
			return i
		}
	}
	return 0
}

// entityKindCapacity returns how many entity columns fit horizontally at the
// current width. Identical accounting to the board: each column needs its
// inner width plus 2 for the border, and a 1-cell gap between neighbors.
func (m Model) entityKindCapacity() int {
	available := m.availableWidth()
	per := entityListWidth + 2
	if per <= 0 {
		return 1
	}
	cap := (available + 1) / (per + 1)
	if cap < 1 {
		cap = 1
	}
	return cap
}

// syncEntityKindScroll keeps entityKindScroll aligned so the focused entity
// kind stays inside the visible horizontal window.
func (m *Model) syncEntityKindScroll() {
	allKinds := configEntityKinds()
	cap := m.entityKindCapacity()
	if cap > len(allKinds) {
		cap = len(allKinds)
	}
	focused := indexOfEntityKind(allKinds, m.entityKind)
	m.entityKindScroll = scrollIntoView(m.entityKindScroll, focused, len(allKinds), cap)
}

// renderConfigHeader produces the runtime/tokens summary tables that sit at
// the top of the config view. Extracted so the viewport calculator can reuse
// the exact rendered height instead of approximating it.
func (m Model) renderConfigHeader() string {
	bucketKeys := make([]string, 0, len(m.workflow.Buckets))
	for _, bucket := range m.workflow.Buckets {
		bucketKeys = append(bucketKeys, bucket.Key)
	}
	sort.Strings(bucketKeys)

	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}
	leftRows := [][]string{
		{labelCell("Runtime"), ""},
		{labelCell("Workflow"), m.workflow.Key},
		{labelCell("Buckets"), strings.Join(bucketKeys, ", ")},
		{labelCell("Theme"), m.theme.Key},
		{labelCell("Totals"), ""},
		{labelCell("Tasks"), fmt.Sprintf("%d", len(m.tasks))},
		{labelCell("Comments"), fmt.Sprintf("%d", len(m.comments))},
		{labelCell("Context"), fmt.Sprintf("%d", len(m.entries))},
		{labelCell("Tags"), fmt.Sprintf("%d", len(m.tags))},
	}
	rightRows := [][]string{
		{labelCell("Tokens"), ""},
		{labelCell("Estimated"), fmt.Sprintf("%d", m.metrics.EstimatedTotal)},
		{labelCell("Max"), fmt.Sprintf("%d", m.metrics.MaxTokens)},
	}
	if m.metrics.Truncated {
		rightRows = append(rightRows, []string{m.styles.error.Render("[ERROR]"), m.styles.error.Render("budget exceeded")})
	}

	const (
		configLabelWidth = 13
		configValueWidth = 27
		configTableWidth = 1 + configLabelWidth + 1 + configValueWidth + 1 // 43
		configGap        = 2
	)
	widths := []int{configLabelWidth, configValueWidth}

	switch {
	case m.availableWidth() >= configTableWidth*2+configGap:
		left := renderGridTable(leftRows, widths, m.styles.border)
		right := renderGridTable(rightRows, widths, m.styles.border)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", configGap), right)
	case m.availableWidth() >= configTableWidth:
		left := renderGridTable(leftRows, widths, m.styles.border)
		right := renderGridTable(rightRows, widths, m.styles.border)
		return left + "\n\n" + right
	default:
		valueW := clampInt(m.availableWidth()-configLabelWidth-3, 8, configValueWidth)
		narrowWidths := []int{configLabelWidth, valueW}
		all := append(append([][]string{}, leftRows...), rightRows...)
		return renderGridTable(all, narrowWidths, m.styles.border)
	}
}

// entityCardsViewport returns the number of rows available for cards inside
// each entity column at the bottom of the config view. It measures the
// rendered runtime/tokens header explicitly and subtracts the screen-level
// chrome (header, footer, optional status, blank lines, column borders, and
// column kicker+separator) so the viewport tracks the real layout instead
// of relying on a static guess that drifts as the tables grow.
func (m Model) entityCardsViewport(headerBlock string) int {
	if m.height <= 0 {
		return 0
	}
	const (
		columnBorders    = 2 // top + bottom border of the kanbanColumn cell
		columnHeaderRows = 2 // kicker + separator inside the cell
		blanksBeforeGrid = 2 // "\n\n" between header tables and the grid
		viewLeadingBlank = 1 // leading "\n" prepended in renderConfig
		footerLines      = 2 // newline + indented footer text
	)

	headerLines := strings.Count(headerBlock, "\n") + 1
	screenHeader := strings.Count(m.renderHeader(), "\n") + 1
	statusLine := 0
	if m.status != "" && !m.isEmbeddedCommentInput() {
		statusLine = 2 // newline separator + the status line
	}

	chrome := screenHeader + statusLine + viewLeadingBlank + headerLines +
		blanksBeforeGrid + columnBorders + columnHeaderRows + footerLines
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}
