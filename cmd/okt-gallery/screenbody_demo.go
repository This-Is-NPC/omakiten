package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/screenbody"
	"omakiten/internal/tui/components/screenlayout"
)

// screenbodyDemo exhibits the pushed-body contract: compose when geometry
// changes, scroll without composing again. The status line is the proof —
// compose calls stay at 1 while j/k move the offset.
type screenbodyDemo struct {
	props []prop
	body  screenbody.Body
	calls int
}

func newScreenbodyDemo(c demoCtx) demo {
	d := screenbodyDemo{props: []prop{
		numberProp("lines", "rows the hook produces once, not per frame", 40, 1, 200),
	}}
	return d.Resize(c)
}

func (d screenbodyDemo) Resize(c demoCtx) demo {
	box := screenlayout.HostBox(c.kit)
	n := propNumber_(d.props, "lines")
	d.calls = 0
	d.body = screenbody.New(screenbody.Spec("gallery-body"), nil).Compose(c.kit, box, func(box screenlayout.Box) []string {
		d.calls++
		lines := make([]string, n)
		for i := range lines {
			lines[i] = fmt.Sprintf("row %03d · composed at width %d", i+1, box.Width)
		}
		return lines
	})
	return d
}

func (d screenbodyDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	d.body, _ = d.body.HandleKey(c.kit, screenlayout.HostBox(c.kit), msg.String())
	return d
}

func (d screenbodyDemo) View(c demoCtx) string {
	return d.body.HostView(c.kit)
}

func (d screenbodyDemo) Status(demoCtx) string {
	return fmt.Sprintf("compose calls: %d · offset %d", d.calls, d.body.Offset())
}

func (d screenbodyDemo) Props() []prop { return d.props }

func (d screenbodyDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d screenbodyDemo) Help() []key.Binding { return scrollHelp() }
