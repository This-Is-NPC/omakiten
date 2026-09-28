package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/list"
)

// listDemo is the four surfaces package list owns, selected by shape:
// cards, rows (Window), picker, viewport. scrollwindow stays its own entry.
type listDemo struct {
	cards list.Cards
	win   list.Window
	pick  list.Picker
	vp    list.Viewport
	items []string
	props []prop
}

func newListDemo(c demoCtx) demo {
	d := listDemo{
		cards: list.NewCards(),
		win:   list.NewWindow(c.kit.Height),
		pick:  list.NewPicker(list.Single),
		vp:    list.NewViewport(),
		props: []prop{
			choiceProp("shape", "cards · rows · picker · viewport — four state machines, one package", 0,
				"cards", "rows", "picker", "viewport"),
			choiceProp("units", "cards: bordered cards, or one row each (the old linelist shape)", 0, "cards", "lines"),
			choiceProp("mode", "picker: Single enter-confirm, or Multi space-toggle", 0, "single", "multi"),
			autoProp("viewport", "row budget handed to the active surface", 200),
			numberProp("items", "how many items the cards / rows / picker surfaces hold", 12, 0, 60),
			numberProp("lines", "viewport document lines", 40, 0, 200),
			numberProp("scroll", "viewport offset, also settable by hand", 0, 0, 200),
			textProp("label", "the card title, before the #id prefix", "resolve layout once"),
			numberProp("cursor", "cards item index handed to WithCursor", 0, -1, 59),
		},
	}
	d.items = syntheticListPickerItems(propNumber_(d.props, "items"))
	return d.Resize(c)
}

func (d listDemo) shape() string { return propLabel(d.props, "shape") }

func (d listDemo) Resize(c demoCtx) demo {
	switch d.shape() {
	case "rows":
		d.win = d.win.
			WithItemCount(propNumber_(d.props, "items")).
			WithViewport(maxInt(propAuto(d.props, "viewport", c.kit.Height), 1))
	case "picker":
		n := propNumber_(d.props, "items")
		if n != len(d.items) {
			d.items = syntheticListPickerItems(n)
		}
	case "viewport":
		d.vp = d.vp.WithScroll(propNumber_(d.props, "scroll"))
	default:
		d.cards = d.cards.
			WithViewport(propAuto(d.props, "viewport", c.kit.Height)).
			WithCursor(propNumber_(d.props, "cursor")).
			WithItems(d.cardItems(c))
	}
	return d
}

func (d listDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	switch d.shape() {
	case "rows":
		d.updateRows(msg)
	case "picker":
		d.updatePicker(msg, c)
	case "viewport":
		d.updateViewport(msg, c)
	default:
		cards, moved := moveCardlist(d.cards, msg)
		if !moved {
			return d
		}
		d.cards = cards
		if p, ok := findProp(d.props, "cursor"); ok {
			p.number = d.cards.Cursor()
			d.props = replaceProp(d.props, p)
		}
		d.cards = d.cards.WithItems(d.cardItems(c))
	}
	return d
}

func (d *listDemo) updateRows(msg tea.KeyMsg) {
	switch msg.String() {
	case "j", "down":
		d.win = d.win.MoveCursor(1)
	case "k", "up":
		d.win = d.win.MoveCursor(-1)
	case "g", "home":
		d.win = d.win.JumpFirst()
	case "G", "end":
		d.win = d.win.JumpLast()
	case "pgdown", "ctrl+d":
		d.win = d.win.PageDown()
	case "pgup", "ctrl+u":
		d.win = d.win.PageUp()
	}
}

func (d *listDemo) updatePicker(msg tea.KeyMsg, c demoCtx) {
	n := propNumber_(d.props, "items")
	if n != len(d.items) {
		d.items = syntheticListPickerItems(n)
	}
	mode := list.Single
	if propLabel(d.props, "mode") == "multi" {
		mode = list.Multi
	}
	if d.pick.Mode != mode {
		d.pick = list.NewPicker(mode).WithCursor(d.pick.Cursor, len(d.items), propAuto(d.props, "viewport", c.kit.Height))
	}
	next, _ := d.pick.Update(msg, len(d.items), propAuto(d.props, "viewport", c.kit.Height))
	d.pick = next
}

func (d *listDemo) updateViewport(msg tea.KeyMsg, c demoCtx) {
	d.vp, _ = d.vp.Update(msg, propAuto(d.props, "viewport", c.kit.Height))
	if p, ok := findProp(d.props, "scroll"); ok {
		p.number = clamp(d.vp.Scroll, p.min, p.max)
		d.props = replaceProp(d.props, p)
	}
}

func (d listDemo) cardItems(c demoCtx) []list.Item {
	count := propNumber_(d.props, "items")
	if propLabel(d.props, "units") == "lines" {
		rows := sampleRows(c, d.cards.Cursor(), count)
		items := make([]list.Item, len(rows))
		for i, row := range rows {
			items[i] = list.NewItem(row)
		}
		return items
	}
	return sampleCards(c, d.cards.Cursor(), count, c.kit.Width, propString(d.props, "label"))
}

func (d listDemo) View(c demoCtx) string {
	switch d.shape() {
	case "rows":
		start, end := d.win.VisibleRange()
		rows := make([]string, 0, maxInt(end-start, 0))
		for i := start; i < end; i++ {
			label := fmt.Sprintf("%2d  row %d of %d", i, i+1, d.win.ItemCount())
			if i == d.win.Cursor() {
				rows = append(rows, c.kit.CursorMarker(true)+" "+c.kit.Styles.HintAccent.Render(label))
				continue
			}
			rows = append(rows, c.kit.CursorMarker(false)+" "+c.kit.Styles.Hint.Render(label))
		}
		return strings.Join(rows, "\n")
	case "picker":
		viewport := propAuto(d.props, "viewport", c.kit.Height)
		start := d.pick.Scroll
		end := start + viewport
		if end > len(d.items) {
			end = len(d.items)
		}
		var b strings.Builder
		b.WriteString(c.kit.Styles.Kicker(fmt.Sprintf("picker · %s", propLabel(d.props, "mode"))))
		b.WriteByte('\n')
		for i := start; i < end; i++ {
			marker := "  "
			if i == d.pick.Cursor {
				marker = c.kit.CursorMarker(true) + " "
			}
			b.WriteString(marker)
			b.WriteString(d.items[i])
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	case "viewport":
		return d.vp.WithWidth(c.kit.Width).View(proseLines(c, propNumber_(d.props, "lines")),
			propAuto(d.props, "viewport", c.kit.Height), c.kit.Styles.Hint)
	default:
		return d.cards.View(c.kit.Styles.Hint)
	}
}

func (d listDemo) Status(c demoCtx) string {
	switch d.shape() {
	case "rows":
		start, end := d.win.VisibleRange()
		return fmt.Sprintf("cursor %d · scroll %d · viewport %d rows · visible %d-%d of %d",
			d.win.Cursor(), d.win.Scroll(), d.win.ViewportRows(), start, end, d.win.ItemCount())
	case "picker":
		return fmt.Sprintf("cursor %d · scroll %d · last %v", d.pick.Cursor, d.pick.Scroll, d.pick.LastEvent())
	case "viewport":
		return fmt.Sprintf("scroll %d · %d lines into a %d-row window",
			d.vp.Scroll, propNumber_(d.props, "lines"), propAuto(d.props, "viewport", c.kit.Height))
	default:
		return listStatus(d.cards.Cursor(), d.cards.Len(), d.cards.Scroll(), d.cards.VisibleRange)
	}
}

func (d listDemo) Props() []prop { return d.props }

func (d listDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	if p.name == "items" {
		d.cards = list.NewCards()
		d.items = syntheticListPickerItems(propNumber_(d.props, "items"))
	}
	return d.Resize(c)
}

func (d listDemo) Help() []key.Binding {
	switch d.shape() {
	case "picker":
		return []key.Binding{
			binding("j/k", "move"),
			binding("space", "toggle (multi)"),
			binding("enter", "select (single)"),
		}
	case "viewport":
		return []key.Binding{binding("j/k", "scroll"), binding("g/G", "top/end"), binding("pgup/pgdn", "page")}
	default:
		return scrollHelp()
	}
}

func syntheticListPickerItems(n int) []string {
	out := make([]string, maxInt(n, 0))
	for i := range out {
		out[i] = fmt.Sprintf("option %02d", i+1)
	}
	return out
}
