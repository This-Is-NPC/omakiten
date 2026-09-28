package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/cardtable"
)

type cardtableDemo struct {
	props []prop
}

func newCardtableDemo(demoCtx) demo {
	return cardtableDemo{props: []prop{
		numberProp("items", "already-painted cards handed to Layout", 6, 0, 40),
		autoProp("cell width", "painted cell width Cols divides the frame by", 28),
		choiceProp("crown", "demo chrome: the entity-list kicker+rule, not a cardtable field", 0,
			"no", "yes"),
	}}
}

func (d cardtableDemo) Resize(demoCtx) demo             { return d }
func (d cardtableDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d cardtableDemo) cellWidth(c demoCtx) int {
	return maxInt(propAuto(d.props, "cell width", 28), 8)
}

func (d cardtableDemo) paint(c demoCtx) []string {
	return samplePaintedCards(c, propNumber_(d.props, "items"), d.cellWidth(c))
}

func (d cardtableDemo) View(c demoCtx) string {
	cards := d.paint(c)
	cols := cardtable.Cols(c.kit.Width, d.cellWidth(c))
	rows, _ := cardtable.Layout(cards, cols)
	body := strings.Join(rows, "\n")
	if propLabel(d.props, "crown") == "yes" {
		kicker := c.kit.Styles.HintAccent.Render(fmt.Sprintf("// TASKS · %d", len(cards)))
		crown := kicker + "\n" + c.kit.HRule(c.kit.Width)
		if body == "" {
			body = crown
		} else {
			body = crown + "\n" + body
		}
	}
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	if len(lines) > c.kit.Height {
		lines = lines[:c.kit.Height]
	}
	return strings.Join(lines, "\n")
}

func (d cardtableDemo) Status(c demoCtx) string {
	cards := d.paint(c)
	cell := d.cellWidth(c)
	cols := cardtable.Cols(c.kit.Width, cell)
	rows, heights := cardtable.Layout(cards, cols)
	total := 0
	for _, h := range heights {
		total += h
	}
	clipped := ""
	if total > c.kit.Height {
		clipped = fmt.Sprintf(" · clipped %d of %d rows to the frame", total-c.kit.Height, total)
	}
	crown := ""
	if propLabel(d.props, "crown") == "yes" {
		crown = " · crown is demo chrome, not a cardtable field"
	}
	return fmt.Sprintf("%d cards · %d cols of %d · %d grid rows · gutter 1%s%s",
		len(cards), cols, cell, len(rows), clipped, crown)
}

func (d cardtableDemo) Props() []prop { return d.props }

func (d cardtableDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d
}

func (d cardtableDemo) Help() []key.Binding { return statelessHelp() }
