package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/scrollwindow"
)

// The demos in this file exhibit the packages that decide OCCUPANCY rather than
// appearance. They have no look of their own, so what they render is the numbers
// they resolved for the geometry they were handed — which is exactly what you
// need when a screen paints past the bottom of a terminal.

func numberTable(c demoCtx, rows [][]gridtable.Cell) string {
	label := minInt(30, maxInt(c.kit.Width/3, 10))
	value := maxInt(c.kit.Width-label-3, 8)
	return gridtable.RenderCells(rows, []int{label, value}, c.kit.Styles.Border)
}

func numberRow(c demoCtx, label, value, note string) []gridtable.Cell {
	if note != "" {
		value += c.kit.Styles.Hint.Render("   " + screenkit.Sanitize(note))
	}
	return []gridtable.Cell{gridtable.Styled(c.kit.Styles.Hint.Render(screenkit.Sanitize(label))), gridtable.Raw(value)}
}

// ---- screenkit budgets ----------------------------------------------------

type screenkitDemo struct {
	props []prop
}

func newScreenkitDemo(demoCtx) demo {
	return screenkitDemo{props: []prop{
		numberProp("chrome rows", "Kit.ChromeRows: the host chrome ABOVE the body", 0, 0, 40),
		numberProp("body chrome", "the argument ViewportRows charges for", 3, 0, 40),
		numberProp("panel chrome", "the argument PanelViewportRows charges for", 7, 0, 40),
		choiceProp("tally", "also show a measured PanelChrome tally", 0, "no", "yes"),
	}}
}

func (d screenkitDemo) kit(c demoCtx) screenkit.Kit {
	kit := c.kit
	kit.ChromeRows = propNumber_(d.props, "chrome rows")
	return kit
}

func (d screenkitDemo) Resize(demoCtx) demo             { return d }
func (d screenkitDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d screenkitDemo) View(c demoCtx) string {
	kit := d.kit(c)
	body := propNumber_(d.props, "body chrome")
	panel := propNumber_(d.props, "panel chrome")
	rows := [][]gridtable.Cell{
		{gridtable.Styled(c.kit.Styles.Kicker("geometry"))},
		numberRow(c, "Width / Height", fmt.Sprintf("%d × %d", kit.Width, kit.Height), "what the frame handed it"),
		numberRow(c, "Rows()", fmt.Sprintf("%d", kit.Rows()), "Height, or the unmeasured-terminal assumption"),
		numberRow(c, "AvailableWidth()", fmt.Sprintf("%d", kit.AvailableWidth()), "width minus the host gutters"),
		{gridtable.Styled(c.kit.Styles.Kicker("panel"))},
		numberRow(c, "BoxWidth()", fmt.Sprintf("%d", kit.BoxWidth()), "the panel's outer width"),
		numberRow(c, "PanelContentWidth()", fmt.Sprintf("%d", kit.PanelContentWidth()), "what a body inside it may use"),
		{gridtable.Styled(c.kit.Styles.Kicker("row budgets"))},
		numberRow(c, fmt.Sprintf("ViewportRows(%d)", body), fmt.Sprintf("%d", kit.ViewportRows(body)), "0 means: too small, render it all"),
		numberRow(c, fmt.Sprintf("PanelViewportRows(%d)", panel), fmt.Sprintf("%d", kit.PanelViewportRows(panel)), ""),
		numberRow(c, "ScrollDataRows(vp)", fmt.Sprintf("%d", screenkit.ScrollDataRows(kit.ViewportRows(body))), "viewport minus the two hint rows"),
	}
	if propOption(d.props, "tally") == 1 {
		chrome := kit.PanelChrome().Lines(kit.Styles.Kicker("subtasks"), kit.HRule(kit.PanelContentWidth()), "")
		rows = append(rows,
			[]gridtable.Cell{gridtable.Styled(c.kit.Styles.Kicker("measured chrome"))},
			numberRow(c, "PanelChrome()", fmt.Sprintf("%d rows", kit.PanelChrome().Rows()), "the border, measured not assumed"),
			numberRow(c, "+ Lines(kicker, rule, blank)", fmt.Sprintf("%d rows", chrome.Rows()), "charged at the width that wraps them"),
			numberRow(c, "chrome.ViewportRows()", fmt.Sprintf("%d", chrome.ViewportRows()), "what is left for the data window"),
		)
	}
	return numberTable(c, rows)
}

func (d screenkitDemo) Status(c demoCtx) string {
	kit := d.kit(c)
	if kit.ViewportRows(propNumber_(d.props, "body chrome")) == 0 {
		return "ViewportRows returned 0 — the documented \"too small, let the host clamp\" answer"
	}
	return "every number below is derived from one geometry, in one place"
}

func (d screenkitDemo) Props() []prop { return d.props }

func (d screenkitDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d screenkitDemo) Help() []key.Binding { return statelessHelp() }

// ---- scrollwindow ---------------------------------------------------------

type scrollwindowDemo struct {
	props []prop
}

func newScrollwindowDemo(demoCtx) demo {
	return scrollwindowDemo{props: []prop{
		autoProp("viewport", "the row budget Slice is given", 200),
		numberProp("offset", "the first visible item index", 0, 0, 40),
		numberProp("cursor", "the selected item, moved with j/k", 0, 0, 40),
		choiceProp("hints", "whether the window reserves its own hint rows", 0, "HintsSplit", "HintsNone"),
		numberProp("items", "how many variable-height items are in the band", 12, 0, 40),
	}}
}

func (d scrollwindowDemo) mode() scrollwindow.HintMode {
	if propOption(d.props, "hints") == 1 {
		return scrollwindow.HintsNone
	}
	return scrollwindow.HintsSplit
}

// heights is a deliberately uneven set: one-line rows next to four-line cards,
// which is where a line offset and an item index stop being the same number.
func (d scrollwindowDemo) heights() []int {
	pattern := []int{1, 3, 2, 1, 4, 1, 2, 3, 1, 1, 5, 2}
	n := propNumber_(d.props, "items")
	out := make([]int, n)
	for i := range out {
		out[i] = pattern[i%len(pattern)]
	}
	return out
}

func (d scrollwindowDemo) viewport(c demoCtx) int {
	return maxInt(propAuto(d.props, "viewport", c.kit.Height-4), 1)
}

func (d scrollwindowDemo) Resize(demoCtx) demo { return d }

func (d scrollwindowDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	cursor := propNumber_(d.props, "cursor")
	switch msg.String() {
	case "j", "down":
		cursor++
	case "k", "up":
		cursor--
	case "g", "home":
		cursor = 0
	case "G", "end":
		cursor = len(d.heights()) - 1
	default:
		return d
	}
	// Resync is the invariant: it moves cursor and offset together, and it is the
	// one function cardlist, linelist and the arranger all route through.
	cursor, offset := scrollwindow.Resync(cursor, propNumber_(d.props, "offset"), d.heights(), d.viewport(c))
	return d.write("cursor", cursor).write("offset", offset)
}

func (d scrollwindowDemo) write(name string, value int) scrollwindowDemo {
	if p, ok := findProp(d.props, name); ok {
		p.number = clamp(value, p.min, p.max)
		d.props = replaceProp(d.props, p)
	}
	return d
}

func (d scrollwindowDemo) View(c demoCtx) string {
	heights := d.heights()
	viewport := d.viewport(c)
	offset := propNumber_(d.props, "offset")
	cursor := propNumber_(d.props, "cursor")
	end := scrollwindow.Slice(offset, heights, viewport, d.mode())
	leading, trailing := scrollwindow.PartialRows(offset, end, heights, viewport, d.mode())

	lines := make([]string, 0, len(heights)+4)
	for i, h := range heights {
		label := fmt.Sprintf("%2d  %-5s %d row(s)", i, strings.Repeat("█", h), h)
		switch {
		case i == cursor:
			lines = append(lines, c.kit.Styles.HintAccent.Render("▌ "+label))
		case i >= offset && i < end:
			lines = append(lines, c.kit.Styles.Info.Render("  "+label))
		default:
			lines = append(lines, c.kit.Styles.Hint.Render("· "+label))
		}
	}
	// The two diagnostic rows are the widest thing this demo paints, and they
	// used to be written at whatever length the numbers happened to make — so
	// the demo about spending a budget was the one ignoring it. Wrapped at the
	// frame, like everything else here.
	readout := fmt.Sprintf("Slice(offset=%d, viewport=%d) → end=%d   Above=%d  Below=%d\nPartialRows → leading=%d trailing=%d   MaxOffset=%d",
		offset, viewport, end, scrollwindow.Above(offset), scrollwindow.Below(end, len(heights)),
		leading, trailing, scrollwindow.MaxOffset(len(heights), viewport, d.mode()))
	return strings.Join(append(lines, "",
		c.kit.Styles.Hint.Render(screenkit.WrapAt(readout, c.kit.Width)),
	), "\n")
}

func (d scrollwindowDemo) Status(demoCtx) string {
	if propOption(d.props, "hints") == 1 {
		return "HintsNone — the caller draws its own hint outside the budget"
	}
	return "HintsSplit — two rows reserved inside the viewport"
}

func (d scrollwindowDemo) Props() []prop { return d.props }

func (d scrollwindowDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d scrollwindowDemo) Help() []key.Binding {
	return []key.Binding{binding("j/k", "move the cursor"), binding("g/G", "first/last")}
}
