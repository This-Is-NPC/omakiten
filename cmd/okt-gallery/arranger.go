package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenlayout"
)

// screenlayoutDemo exhibits the arranger — the piece that exists so a screen
// never computes a width or a row budget for itself.
//
// It is the most important entry in the gallery for responsiveness, because it
// is the only one where you can watch a BREAKPOINT happen: narrow the frame and
// the body flips from side-by-side columns to a stack, and narrow it further and
// sections are dropped from the bottom rather than rendered into a budget they
// do not have.
type screenlayoutDemo struct {
	state screenlayout.State
	props []prop
}

func newScreenlayoutDemo(c demoCtx) demo {
	d := screenlayoutDemo{state: screenlayout.NewState(), props: []prop{
		choiceProp("body", "the arranged body, or the numbers behind it", 0, "arranged", "placements"),
		choiceProp("columns", "Spec.Column: whether the sections opt into side-by-side", 0, "opt-in", "never"),
		numberProp("activity %", "Spec.WidthPercent on the activity column", 45, 0, 100),
		numberProp("activity max", "Spec.MaxWidth on the activity column", 96, 0, 300),
		numberProp("form rows", "the fixed section's MinRows and MaxRows", 5, 1, 40),
		numberProp("subtasks min", "Spec.MinRows below which the section is dropped", 4, 1, 40),
		numberProp("column gap", "Spec.ColumnGap between columns", 1, 0, 8),
		numberProp("min width", "Spec.MinWidth every section declares", 28, 4, 200),
	}}
	return d.Resize(c)
}

func (d screenlayoutDemo) columns() bool { return propOption(d.props, "columns") == 0 }

func (d screenlayoutDemo) numbers() bool { return propOption(d.props, "body") == 1 }

func (d screenlayoutDemo) Props() []prop { return d.props }

func (d screenlayoutDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d screenlayoutDemo) Resize(c demoCtx) demo {
	d.state = d.state.Resync(c.kit, d.sections(c)...)
	return d
}

func (d screenlayoutDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	// The arranger's real table, `tab` included — the gallery surrenders its own
	// `tab` in capture mode rather than making this demo answer to a key the app
	// does not use.
	state, _ := d.state.HandleKey(c.kit, msg.String(), d.sections(c)...)
	d.state = state
	return d
}

func (d screenlayoutDemo) View(c demoCtx) string {
	result := screenlayout.Arrange(c.kit, d.state, d.sections(c)...)
	if !d.numbers() {
		return result.View
	}
	return d.placementTable(c, result)
}

// placementTable prints what the arranger decided, one row per section. Every
// column here is a number a screen used to compute for itself and get wrong.
func (d screenlayoutDemo) placementTable(c demoCtx, result screenlayout.Result) string {
	rows := [][]string{{
		c.kit.Styles.Kicker("section"), c.kit.Styles.Kicker("w×rows"),
		c.kit.Styles.Kicker("chrome"), c.kit.Styles.Kicker("window"),
		c.kit.Styles.Kicker("cursor"),
	}}
	for _, p := range result.Placements {
		id := string(p.ID)
		if p.Dropped {
			rows = append(rows, []string{
				c.kit.Styles.Warning.Render(id), c.kit.Styles.Warning.Render("dropped"),
				"—", c.kit.Styles.Hint.Render("body could not afford it"), "—",
			})
			continue
		}
		window := fmt.Sprintf("%d rows · %d-%d of %d", p.ItemViewport, p.First+1, p.Last+1, len(p.Items))
		if p.Above > 0 || p.Below > 0 {
			window += fmt.Sprintf(" · ▲%d ▼%d", p.Above, p.Below)
		}
		cursor := "—"
		if p.Cursor >= 0 {
			cursor = fmt.Sprintf("item %d · line %d", p.Cursor, p.CursorLine)
		}
		if p.CursorUnread {
			cursor += " ⚠ unread"
		}
		rows = append(rows, []string{
			id,
			fmt.Sprintf("%d×%d", p.Width, p.Rows),
			fmt.Sprintf("hdr %d · ftr %d", p.HeaderRows, p.FooterRows),
			window,
			cursor,
		})
	}
	widths := placementWidths(c.kit.Width)
	return gridtable.Render(rows, widths, c.kit.Styles.Border)
}

func placementWidths(total int) []int {
	// Five columns plus the four internal bars and the two outer borders.
	const bars = 6
	body := maxInt(total-bars, 25)
	first := maxInt(body*12/100, 5)
	second := maxInt(body*12/100, 5)
	third := maxInt(body*18/100, 7)
	fifth := maxInt(body*22/100, 8)
	fourth := maxInt(body-first-second-third-fifth, 8)
	return []int{first, second, third, fourth, fifth}
}

// Status is the arranger's own diagnostic: which breakpoint it chose and how
// much of the budget the body actually paints.
func (d screenlayoutDemo) Status(c demoCtx) string {
	result := screenlayout.Arrange(c.kit, d.state, d.sections(c)...)
	dropped := 0
	for _, p := range result.Placements {
		if p.Dropped {
			dropped++
		}
	}
	status := fmt.Sprintf("%s · body %d rows, paints %d · focus %s",
		result.Arrangement, result.BodyRows, result.Rows(), orDash(string(d.state.Focus())))
	if dropped > 0 {
		status += fmt.Sprintf(" · %d dropped", dropped)
	}
	return status
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (d screenlayoutDemo) Help() []key.Binding {
	return []key.Binding{
		binding("j/k", "move in section"),
		binding("tab", "next section"),
		binding("pgup/pgdn", "page"),
		binding("g/G", "first/last"),
	}
}

// ---- the declared screen ---------------------------------------------------

// sections is a realistic three-section body: a fixed form over a scrollable
// sub-task list in one column, and an activity feed in another. It is the shape
// both pilot screens already had — `[form over subtasks] | [activity]` — and the
// shape that forced Group and WidthPercent into the Spec.
func (d screenlayoutDemo) sections(c demoCtx) []screenlayout.Section {
	group := screenlayout.ID("")
	columns := d.columns()
	if columns {
		group = "left"
	}
	formRows := propNumber_(d.props, "form rows")
	subtasksMin := propNumber_(d.props, "subtasks min")
	gap := propNumber_(d.props, "column gap")
	minWidth := propNumber_(d.props, "min width")
	return []screenlayout.Section{
		screenlayout.Func{
			Def: screenlayout.Spec{
				ID: "form", MinWidth: minWidth, MinRows: formRows, MaxRows: formRows,
				Column: columns, Group: group, ColumnGap: gap,
			},
			Body: func(canvas screenlayout.Canvas) screenlayout.Block {
				return screenlayout.Block{
					Header: []string{c.kit.Styles.Kicker("task")},
					Items: []string{
						c.kit.Styles.Hint.Render("// STATUS   ") + "in progress",
						c.kit.Styles.Hint.Render("// OWNER    ") + "howl",
						c.kit.Styles.Hint.Render("// UPDATED  ") + "2 hours ago",
					},
					Cursor: screenlayout.NoSelection(),
				}
			},
		},
		screenlayout.Func{
			Def: screenlayout.Spec{
				ID: "subtasks", MinWidth: minWidth, MinRows: subtasksMin, Weight: 1,
				Column: columns, Group: group, ColumnGap: gap,
				Scroll: screenlayout.ScrollItems, SelectFirst: true,
			},
			Body: func(canvas screenlayout.Canvas) screenlayout.Block {
				return screenlayout.Block{
					Header: []string{c.kit.Styles.Kicker("subtasks")},
					Items:  markSelected(c, sectionItems(subtaskTitles(), "#"), canvas.Cursor()),
				}
			},
		},
		screenlayout.Func{
			Def: screenlayout.Spec{
				ID: "activity", MinWidth: minWidth + 12, MinRows: 6, Weight: 1,
				Column: columns, ColumnGap: gap,
				WidthPercent: propNumber_(d.props, "activity %"),
				MaxWidth:     propNumber_(d.props, "activity max"),
				Scroll:       screenlayout.ScrollItems, SelectFirst: true,
			},
			Body: func(canvas screenlayout.Canvas) screenlayout.Block {
				return screenlayout.Block{
					Header: []string{c.kit.Styles.Kicker("activity")},
					Items:  markSelected(c, sectionItems(activityLines(), ""), canvas.Cursor()),
				}
			},
		},
	}
}

func subtaskTitles() []string {
	return []string{
		"400 resolve layout once", "401 cover Widths", "402 delete dead arranger",
		"403 pin the two gaps", "404 declare three zones", "405 extract card frame",
		"406 budget the panel", "407 memoize sections",
	}
}

func activityLines() []string {
	return []string{
		"howl moved this to doing", "howl added a comment",
		"guard refused done → backlog", "howl claimed wave 2",
		"howl edited the description", "howl linked #412",
		"howl moved this to review", "howl added a comment",
		"howl archived #388",
	}
}

func sectionItems(values []string, prefix string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = prefix + v
	}
	return out
}

// markSelected paints the arranger's cursor. A section that never reads
// Canvas.Cursor is reported as CursorUnread — a screen whose selection moves
// while nothing on it highlights.
func markSelected(c demoCtx, items []string, cursor int) []string {
	out := make([]string, len(items))
	for i, item := range items {
		marker := c.kit.CursorMarker(i == cursor)
		if i == cursor {
			out[i] = marker + " " + c.kit.Styles.HintAccent.Render(item)
			continue
		}
		out[i] = marker + " " + c.kit.Styles.Hint.Render(item)
	}
	return out
}
