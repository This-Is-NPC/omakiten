package gridtable

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// LabelWidth is the standard width of the `// LABEL` column. Exposed so
// callers computing valueWidth from the available content area use the
// same constant the builder uses internally — keeping the two in sync
// avoids mysterious off-by-one alignment bugs.
const LabelWidth = 13

// Detail is a fluent two-column row builder. Custom/Kicker/KickerCount/Row/Span
// accumulate cells; View paints them through the leaf grid painter. Scroll is
// not here — screens that window a tall grid hold a list.Viewport and call
// Fit on the bytes View returns.
//
// kicker is the colour the host used to theme section labels: pass
// kit.Styles.Info so they stay in the secondary-info tone. An empty style
// leaves them unstyled, which is what component tests want.
type Detail struct {
	rows   [][]Cell
	labelW int
	valueW int
	kicker lipgloss.Style
}

// NewDetail starts a fresh detail grid scoped to the given value-column width.
// labelW is fixed at LabelWidth so screens stay aligned with each other;
// valueW is per-screen because the activity column on the task view eats into
// the available width differently from the full-width comment screen.
func NewDetail(valueW int, kicker lipgloss.Style) Detail {
	return Detail{labelW: LabelWidth, valueW: valueW, kicker: kicker}
}

// Custom appends a pre-rendered row that spans both columns. Used when
// the caller has already chosen between kicker and kickerFocused based
// on focus state — the builder doesn't take a "focused?" bool because
// only one screen needs it and exposing it would force the same flag
// on every Custom call.
func (d Detail) Custom(content Cell) Detail {
	d.rows = append(d.rows, []Cell{content})
	return d
}

// Kicker appends a section-header row.
func (d Detail) Kicker(label string) Detail {
	d.rows = append(d.rows, []Cell{d.kickerCell(label)})
	return d
}

// KickerCount appends a kicker with a trailing count, e.g.
// `// BLOCKERS · 3`.
func (d Detail) KickerCount(label string, count int) Detail {
	d.rows = append(d.rows, []Cell{d.kickerCountCell(label, count)})
	return d
}

// Row appends a `// LABEL` + value pair.
func (d Detail) Row(label, value string) Detail {
	d.rows = append(d.rows, []Cell{d.labelCell(label), Raw(value)})
	return d
}

// Span appends a single-cell row covering the full grid width — used
// for body text, hints, and any content that doesn't fit the two-column
// label/value layout.
func (d Detail) Span(content Cell) Detail {
	d.rows = append(d.rows, []Cell{content})
	return d
}

// View paints the accumulated rows through the grid layout and returns
// the string. It does not window, hint, or scroll — callers that need a
// viewport apply it to these bytes.
func (d Detail) View(border lipgloss.Style) string {
	labelW, valueW := autoSizeColumns(d.rows, d.labelW, d.valueW)
	return RenderCells(d.rows, []int{labelW, valueW}, border)
}

// autoSizeColumns returns the (labelW, valueW) pair to render with.
// labelW grows to fit the widest two-cell row's label; valueW shrinks
// by the same delta so the table's outer footprint stays at
// (defaultLabelW + defaultValueW). Single-cell spanned rows are ignored
// when measuring labels.
func autoSizeColumns(rows [][]Cell, defaultLabelW, defaultValueW int) (int, int) {
	max := 0
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		if w := lipgloss.Width(renderedText(row[0])); w > max {
			max = w
		}
	}
	if max <= defaultLabelW {
		return defaultLabelW, defaultValueW
	}
	grow := max - defaultLabelW
	if grow >= defaultValueW {
		// Pathological: would shrink value to 0. Cap growth so the value
		// column keeps at least 1 character of room and let the grid
		// wrapping handle the over-long label.
		grow = defaultValueW - 1
	}
	return defaultLabelW + grow, defaultValueW - grow
}

func (d Detail) kickerCell(label string) Cell {
	return Styled(d.kicker.Render("// " + strings.ToUpper(sanitizeCell(label))))
}

func (d Detail) kickerCountCell(label string, n int) Cell {
	return Styled(d.kicker.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(sanitizeCell(label)), n)))
}

func (d Detail) labelCell(label string) Cell {
	return Styled(d.kicker.Render("// " + strings.ToUpper(sanitizeCell(label))))
}
