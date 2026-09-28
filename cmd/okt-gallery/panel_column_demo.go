package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/panel"
)

type panelDemo struct {
	props []prop
}

func newPanelDemo(demoCtx) demo {
	return panelDemo{props: []prop{
		choiceProp("kind", "the framed body, a fixed box, a rule, or the selection chevron", 0,
			"wrap", "fixed box", "rule", "chevron"),
		numberProp("body lines", "rows inside the framed body or fixed box", 4, 0, 40),
		autoProp("width", "columns the rule or fixed box spans", 200),
		choiceProp("selected", "chevron only — on when the row owns the cursor", 0, "yes", "no"),
	}}
}

func (d panelDemo) Resize(demoCtx) demo             { return d }
func (d panelDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d panelDemo) body() []string {
	n := propNumber_(d.props, "body lines")
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf("row %d of the panel body", i+1))
	}
	if n == 0 {
		lines = []string{""}
	}
	return lines
}

func (d panelDemo) View(c demoCtx) string {
	width := maxInt(minInt(propAuto(d.props, "width", c.kit.Width-4), c.kit.Width-4), 8)
	switch propLabel(d.props, "kind") {
	case "fixed box":
		return panel.FixedBox(d.body(), width, c.kit.Styles.Border)
	case "rule":
		return panel.HRule(c.kit.Styles.Separator, width)
	case "chevron":
		return panel.Chevron(c.kit.Styles.HintAccent, propLabel(d.props, "selected") == "yes") + "selected row"
	default:
		return c.kit.Panel(strings.Join(d.body(), "\n"))
	}
}

func (d panelDemo) Status(demoCtx) string {
	switch propLabel(d.props, "kind") {
	case "fixed box":
		n := propNumber_(d.props, "body lines")
		return fmt.Sprintf("FixedBox · %d content rows · height %d (content+2)", n, panel.FixedBoxHeight(n))
	case "rule":
		return "HRule · empty when the width is non-positive"
	case "chevron":
		return "Chevron · › when selected, empty otherwise"
	default:
		return "Kit.Panel · leading blank + indent + PanelBox clamp"
	}
}

func (d panelDemo) Props() []prop { return d.props }

func (d panelDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d
}

func (d panelDemo) Help() []key.Binding { return nil }
