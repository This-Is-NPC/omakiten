package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/lane"
	"omakiten/internal/tui/components/list"
)

type laneDemo struct {
	cards list.Cards
	props []prop
}

func newLaneDemo(c demoCtx) demo {
	d := laneDemo{
		cards: list.NewCards(),
		props: []prop{
			choiceProp("focused", "accent kicker and cursor chevron — the lane the board is on", 1,
				"yes", "no"),
			numberProp("items", "already-painted cards stacked in the body", 6, 0, 40),
			autoProp("viewport", "row budget handed to Cards; auto is the box minus header and rule", 200),
			textProp("header", "bucket name in the kicker, uppercased as the board does", "BACKLOG"),
			textProp("empty", "Styles.Empty copy when items is 0", "(empty)"),
			numberProp("cursor", "card index handed to WithCursor", 0, -1, 39),
		},
	}
	return d.Resize(c)
}

func (d laneDemo) inner(c demoCtx) int { return maxInt(c.kit.Width-2, 8) }

func (d laneDemo) boxHeight(c demoCtx) int { return maxInt(c.kit.Height-2, 3) }

func (d laneDemo) cardViewport(c demoCtx) int {
	return maxInt(propAuto(d.props, "viewport", d.boxHeight(c)-2), 1)
}

func (d laneDemo) header(c demoCtx) string {
	n := propNumber_(d.props, "items")
	label := fmt.Sprintf("// %s · %d", propString(d.props, "header"), n)
	style := c.kit.Styles.Hint
	if propLabel(d.props, "focused") == "yes" {
		style = c.kit.Styles.HintAccent
	}
	return style.Render(label)
}

func (d laneDemo) Resize(c demoCtx) demo {
	inner := d.inner(c)
	withCursor := propLabel(d.props, "focused") == "yes"
	d.cards = list.NewCards().
		WithText(c.kit.Text).
		WithViewport(d.cardViewport(c)).
		WithItems(sampleKanbanItems(c, propNumber_(d.props, "cursor"), propNumber_(d.props, "items"),
			inner, "resolve the layout exactly once per keystroke", withCursor)).
		WithCursor(propNumber_(d.props, "cursor"))
	return d
}

func (d laneDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	cards, moved := moveCardlist(d.cards, msg)
	if !moved {
		return d
	}
	d.cards = cards
	if p, ok := findProp(d.props, "cursor"); ok {
		p.number = d.cards.Cursor()
		d.props = replaceProp(d.props, p)
	}
	return d.Resize(c)
}

func (d laneDemo) View(c demoCtx) string {
	return lane.Render(c.kit, lane.Spec{
		Header:    d.header(c),
		Inner:     d.inner(c),
		Height:    d.boxHeight(c),
		EmptyText: propString(d.props, "empty"),
		Cards:     d.cards,
	})
}

func (d laneDemo) Status(c demoCtx) string {
	n := propNumber_(d.props, "items")
	return fmt.Sprintf("%s · inner %d · box %d rows · cards viewport %d · %d items · cursor %d",
		propLabel(d.props, "focused"), d.inner(c), d.boxHeight(c), d.cardViewport(c), n, d.cards.Cursor())
}

func (d laneDemo) Props() []prop { return d.props }

func (d laneDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	if p.name == "items" {
		d.cards = list.NewCards()
	}
	return d.Resize(c)
}

func (d laneDemo) Help() []key.Binding { return scrollHelp() }
