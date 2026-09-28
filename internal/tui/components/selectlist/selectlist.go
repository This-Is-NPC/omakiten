// Package selectlist paints a framed one-line selection list: bordered box,
// kicker, rule, then rows with a › cursor. It is the chrome list.Window
// leaves to its parent — Studio's left column on workflow, commands,
// personas and hooks.
//
// It is a paint organism, not an arranger. Screens hand it already-rendered
// row text and a cursor index; scroll persistence stays in screenlayout.
// Height, when set, only sizes the box (clip or pad the item window) so a
// gallery overflow scenario can hold a frame without a second scroll engine.
//
// The package is a leaf: kit + strings in, bytes out. No config, domain,
// app, sqlite, or parent tui import.
package selectlist

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// Row is one selectable line. Left is the body; Right is optional trailing
// copy (`-> dest`, `14 cmd`) right-aligned inside the inner width.
type Row struct {
	Left  string
	Right string
}

// Spec is one framed list. Kicker and Subtitle are already styled — the
// same contract as lane.Header. Width is the TOTAL columns including the
// │ sides, matching the canvas a screengrid cell already assigned.
type Spec struct {
	Kicker   string
	Subtitle string
	// ColumnHeader is the table heading painted BELOW the rule, immediately
	// above the rows it names — where Stats › Logs and Studio › Hooks' HISTORY
	// have always put theirs.
	//
	// It is a different thing from Subtitle, which sits above the rule and
	// belongs to the kicker: a subtitle is a second line ABOUT the list, a column
	// header is the first line OF it. Painting one as the other is what made
	// Studio › Personas' RELATED table read differently from the HISTORY table it
	// is meant to match. Already styled, like the other two.
	ColumnHeader string
	Rows         []Row
	Cursor       int
	Width        int
	// Height is the TOTAL rows including the top and bottom border. Zero
	// keeps the content-sized default (len(inner)+2). A positive value
	// clips or pads the item window so the box holds that height.
	Height int
	Empty  string
}

// InnerWidth is the content columns inside a box of total Width.
func InnerWidth(width int) int {
	inner := width - 2
	if inner < 1 {
		inner = 1
	}
	return inner
}

// blockView joins a Block's Header, Items and Footer into the boxed string
// Render emits when Height is unset.
func blockView(block screenlayout.Block) string {
	n := len(block.Header) + len(block.Items) + len(block.Footer)
	lines := make([]string, 0, n)
	lines = append(lines, block.Header...)
	lines = append(lines, block.Items...)
	lines = append(lines, block.Footer...)
	return strings.Join(lines, "\n")
}

// WrapLine paints one row of a framed list the way Task Detail does: │ +
// PadRight (SGR kept) + │. Studio hands this to screenlayout.Block.Chrome so
// arranger-injected ▲/▼ hints sit inside the box instead of on a bare line.
func WrapLine(kit screenkit.Kit, width int) func(string) string {
	return wrapInner(kit, InnerWidth(width))
}

func wrapInner(kit screenkit.Kit, inner int) func(string) string {
	return panel.WrapLine(kit.Styles.Border, inner)
}

// Paint draws every row as a screenlayout.Block: Header carries the top
// border, kicker, subtitle, rule and column header; Items are the rows;
// Footer is the bottom border; Chrome is the same side-wrapper every row was
// painted through, so an arranger-injected hint lands inside the box.
// Height is ignored — the arranger windows Items.
func Paint(kit screenkit.Kit, spec Spec) screenlayout.Block {
	inner := InnerWidth(spec.Width)
	wrap := wrapInner(kit, inner)
	header := []string{panel.Top(kit.Styles.Border, inner)}
	if spec.Kicker != "" {
		header = append(header, wrap(spec.Kicker))
	}
	if spec.Subtitle != "" {
		header = append(header, wrap(spec.Subtitle))
	}
	header = append(header, panel.Join(kit.Styles.Border, inner))
	if spec.ColumnHeader != "" {
		header = append(header, wrap(spec.ColumnHeader))
	}
	items := itemLines(kit, spec, inner)
	for i, line := range items {
		items[i] = wrap(line)
	}
	return screenlayout.Block{
		Header: header,
		Items:  items,
		Footer: []string{panel.Bottom(kit.Styles.Border, inner)},
		Chrome: wrap,
	}
}

// Render draws the framed list as one string. Height, when set, clips or
// pads the item window so the box is exactly that many rows.
func Render(kit screenkit.Kit, spec Spec) string {
	block := Paint(kit, spec)
	if spec.Height > 0 {
		want := spec.Height - len(block.Header) - len(block.Footer)
		if want < 1 {
			want = 1
		}
		if len(block.Items) > want {
			block.Items = block.Items[:want]
		} else {
			blank := wrapInner(kit, InnerWidth(spec.Width))("")
			for len(block.Items) < want {
				block.Items = append(block.Items, blank)
			}
		}
	}
	return blockView(block)
}

func itemLines(kit screenkit.Kit, spec Spec, inner int) []string {
	if len(spec.Rows) == 0 {
		empty := spec.Empty
		if empty == "" {
			empty = " "
		}
		return []string{kit.Styles.Hint.Render(empty)}
	}
	lines := make([]string, len(spec.Rows))
	for i, row := range spec.Rows {
		lines[i] = rowLine(kit, row, inner, i == spec.Cursor)
	}
	return lines
}

func rowLine(kit screenkit.Kit, row Row, inner int, selected bool) string {
	mark := panel.Chevron(kit.Styles.HintAccent, selected)
	if mark == "" {
		mark = "  "
	}
	markW := lipgloss.Width(mark)
	budget := inner - markW
	if budget < 1 {
		line := screenkit.PadRight(screenkit.TruncateStyled(mark, inner), inner)
		if selected {
			return kit.Styles.Cursor.Render(line)
		}
		return line
	}
	body := rowBody(row.Left, row.Right, budget)
	line := screenkit.PadRight(screenkit.TruncateStyled(mark+body, inner), inner)
	if selected {
		return kit.Styles.Cursor.Render(line)
	}
	return line
}

func rowBody(left, right string, budget int) string {
	if right == "" {
		return screenkit.Truncate(left, budget)
	}
	rightW := lipgloss.Width(right)
	if rightW >= budget {
		return screenkit.Truncate(right, budget)
	}
	leftBudget := budget - rightW - 1
	if leftBudget < 1 {
		return screenkit.Truncate(right, budget)
	}
	clipped := screenkit.Truncate(left, leftBudget)
	pad := budget - lipgloss.Width(clipped) - rightW
	if pad < 1 {
		pad = 1
	}
	return clipped + strings.Repeat(" ", pad) + right
}
