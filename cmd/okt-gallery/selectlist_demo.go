package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/selectlist"
)

type selectlistDemo struct {
	props []prop
}

func newSelectlistDemo(demoCtx) demo {
	return selectlistDemo{props: []prop{
		numberProp("items", "already-rendered one-line rows in the body", 6, 0, 40),
		numberProp("cursor", "row index that wears the › chevron", 0, -1, 39),
		autoProp("width", "total columns including the │ sides — the canvas a screengrid cell assigns", 200),
		autoProp("height", "total rows including the top and bottom border; auto is content-sized", 80),
		textProp("kicker", "label handed to Styles.Kicker, uppercased as Studio does", "WORKFLOW"),
		textProp("kicker trail", "Hint metadata after the kicker (key · dirty) — not uppercased", ""),
		textProp("subtitle", "optional Hint line under the kicker: counts, not a second // WORKFLOW", ""),
		textProp("empty", "Hint copy when items is 0", "(empty)"),
		choiceProp("trailing", "right-aligned dest / count on each row", 0, "yes", "no"),
		choiceProp("tones", "already-painted row text through theme tokens — Warning tags, Success open, Hint muted", 0, "plain", "theme"),
	}}
}

func (d selectlistDemo) boxWidth(c demoCtx) int {
	return maxInt(minInt(propAuto(d.props, "width", c.kit.Width-4), c.kit.Width-4), 8)
}

func (d selectlistDemo) rows(c demoCtx) []selectlist.Row {
	n := propNumber_(d.props, "items")
	withRight := propLabel(d.props, "trailing") == "yes"
	themed := propLabel(d.props, "tones") == "theme"
	out := make([]selectlist.Row, n)
	for i := range out {
		out[i] = selectlistDemoRow(c.kit.Styles, i, withRight, themed)
	}
	return out
}

func selectlistDemoRow(s screenkit.Styles, i int, withRight, themed bool) selectlist.Row {
	row, tone := selectlistDemoBaseRow(s, i, withRight)
	if themed && tone != nil {
		return toneRow(*tone, row)
	}
	if withRight && row.Right == "" && i%6 == 0 {
		row.Right = "-> dest"
	}
	return row
}

func selectlistDemoBaseRow(s screenkit.Styles, i int, withRight bool) (selectlist.Row, *lipgloss.Style) {
	var row selectlist.Row
	var tone *lipgloss.Style
	switch i % 6 {
	case 1:
		row = selectlist.Row{Left: "  #self-branch"}
		if withRight {
			row.Right = "-> dest"
		}
		tone = &s.Warning
	case 2:
		row = selectlist.Row{Left: "  blockers_in"}
		if withRight {
			row.Right = "-> dest"
		}
		tone = &s.Warning
	case 3:
		row = selectlist.Row{Left: "  wave_gate"}
		if withRight {
			row.Right = "-> dest"
		}
		tone = &s.Hint
	case 4:
		row = selectlist.Row{Left: "  open"}
		if withRight {
			row.Right = "-> dest"
		}
		tone = &s.Success
	case 5:
		row = selectlist.Row{Left: fmt.Sprintf("%02d // archive", i/6+1)}
		if withRight {
			row.Right = "#documentation"
		}
		tone = &s.Hint
	default:
		row = selectlist.Row{Left: fmt.Sprintf("%02d // BACKLOG · %d", i/6+1, 19-i)}
	}
	return row, tone
}

func toneRow(style lipgloss.Style, row selectlist.Row) selectlist.Row {
	row.Left = style.Render(row.Left)
	if row.Right != "" {
		row.Right = style.Render(row.Right)
	}
	return row
}

func (d selectlistDemo) spec(c demoCtx) selectlist.Spec {
	kicker := c.kit.Styles.Kicker(propString(d.props, "kicker"))
	if trail := strings.TrimSpace(propString(d.props, "kicker trail")); trail != "" {
		kicker += c.kit.Styles.Hint.Render(" · " + trail)
	}
	subtitle := propString(d.props, "subtitle")
	if subtitle != "" {
		subtitle = c.kit.Styles.Hint.Render(subtitle)
	}
	return selectlist.Spec{
		Kicker:   kicker,
		Subtitle: subtitle,
		Rows:     d.rows(c),
		Cursor:   propNumber_(d.props, "cursor"),
		Width:    d.boxWidth(c),
		Height:   propNumber_(d.props, "height"),
		Empty:    propString(d.props, "empty"),
	}
}

func (d selectlistDemo) Resize(demoCtx) demo { return d }

func (d selectlistDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	n := propNumber_(d.props, "items")
	if n <= 0 {
		return d
	}
	cur := propNumber_(d.props, "cursor")
	switch msg.String() {
	case "j", "down":
		cur++
	case "k", "up":
		cur--
	case "g", "home":
		cur = 0
	case "G", "end":
		cur = n - 1
	default:
		return d
	}
	if cur < 0 {
		cur = 0
	}
	if cur > n-1 {
		cur = n - 1
	}
	if p, ok := findProp(d.props, "cursor"); ok {
		p.number = cur
		d.props = replaceProp(d.props, p)
	}
	return d.Resize(c)
}

func (d selectlistDemo) View(c demoCtx) string {
	return selectlist.Render(c.kit, d.spec(c))
}

func (d selectlistDemo) Status(c demoCtx) string {
	spec := d.spec(c)
	h := lipgloss.Height(d.View(c))
	return fmt.Sprintf("cursor %d/%d · box %d×%d · painted %d rows", spec.Cursor+1, len(spec.Rows), spec.Width, h, h)
}

func (d selectlistDemo) Props() []prop { return d.props }

func (d selectlistDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	n := propNumber_(d.props, "items")
	if p.name == "items" {
		if cur, ok := findProp(d.props, "cursor"); ok && n > 0 && cur.number >= n {
			cur.number = n - 1
			d.props = replaceProp(d.props, cur)
		}
	}
	return d.Resize(c)
}

func (d selectlistDemo) Help() []key.Binding { return scrollHelp() }
