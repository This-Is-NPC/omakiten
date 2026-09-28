package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/tokenstrip"
)

// demo is one component under inspection.
//
// Props are the component's OWN inputs — the arguments and fields it really
// takes — so editing one at runtime changes what the component was asked to do,
// not what the gallery decided to show. Scenarios seed them in bundles; the
// props stay editable underneath.
type demo interface {
	Resize(c demoCtx) demo
	Update(msg tea.KeyMsg, c demoCtx) demo
	View(c demoCtx) string
	Status(c demoCtx) string
	Props() []prop
	SetProp(p prop, c demoCtx) demo
	Help() []key.Binding
}

func binding(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

func scrollHelp() []key.Binding {
	return []key.Binding{
		binding("j/k", "move"),
		binding("g/G", "first/last"),
		binding("pgup/pgdn", "page"),
	}
}

func statelessHelp() []key.Binding {
	return []key.Binding{binding("—", "stateless: edit the properties")}
}

func moveCardlist(cards list.Cards, msg tea.KeyMsg) (list.Cards, bool) {
	switch msg.String() {
	case "j", "down":
		return cards.MoveCursor(1), true
	case "k", "up":
		return cards.MoveCursor(-1), true
	case "g", "home":
		return cards.JumpFirst(), true
	case "G", "end":
		return cards.JumpLast(), true
	case "pgdown", "ctrl+d":
		return cards.PageDown(), true
	case "pgup", "ctrl+u":
		return cards.PageUp(), true
	}
	return cards, false
}

func listStatus(cursor, total, scroll int, visible func() (int, int, bool)) string {
	if total == 0 {
		return "empty · View renders nothing"
	}
	first, last, _ := visible()
	return fmt.Sprintf("cursor %d/%d · scroll %d · visible %d-%d", cursor+1, total, scroll, first+1, last+1)
}

// ---- gridtable ------------------------------------------------------------

type gridtableDemo struct {
	vp    list.Viewport
	props []prop
}

func newGridtableDemo(demoCtx) demo {
	return gridtableDemo{vp: list.NewViewport(), props: []prop{
		choiceProp("shape", "cells · summaries · detail · matrix", 0, "cells", "summaries", "detail", "matrix"),
		numberProp("label width", "widths[0] for cells; Options.LabelWidth for summaries", 18, 1, 200),
		autoProp("value width", "widths[1] for cells; Options.ValueWidth for summaries; NewDetail valueW for detail", 300),
		choiceProp("rows", "which row set cells Render receives", 0, "label/value", "spanned", "wrapping"),
		choiceProp("layout", "Summaries: SideBySide / stacked / MergeNarrow", 0, "side-by-side", "stacked", "merge"),
		choiceProp("auto", "Summaries Options.Auto — grow label to widest kicker", 1, "yes", "no"),
		numberProp("body lines", "detail spanned body rows", 24, 0, 200),
		choiceProp("labels", "detail kickers: short, or the long ones a translated catalog produces", 0, "short", "translated"),
		numberProp("scroll", "detail viewport offset — scroll stays in the demo, not in gridtable", 0, 0, 200),
		choiceProp("snapshot", "matrix cells: slugs on allowed edges, or Empty throughout", 0, "with guards", "nil snapshot"),
	}}
}

func (d gridtableDemo) shape() int { return propOption(d.props, "shape") }

func (d gridtableDemo) Resize(demoCtx) demo {
	if d.shape() == 2 {
		d.vp = d.vp.WithScroll(propNumber_(d.props, "scroll"))
	}
	return d
}

func (d gridtableDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	if d.shape() != 2 {
		return d
	}
	d.vp, _ = d.vp.Update(msg, c.kit.Height)
	if p, ok := findProp(d.props, "scroll"); ok {
		p.number = clamp(d.vp.Scroll, p.min, p.max)
		d.props = replaceProp(d.props, p)
	}
	return d
}

// widths are the two column widths handed to Render, fitted to the frame.
//
// The authored numbers are a REQUEST, not a guarantee: the table draws borders
// and separators around them, so two columns declared at their natural size add
// up to more than the frame and the top border ran past it. FitWidths is the
// component's own answer to that question — asking it here is what keeps the
// demo from being the one surface that ignores the width it was given.
func (d gridtableDemo) cellWidths(c demoCtx) []int {
	label := clamp(propNumber_(d.props, "label width"), 1, maxInt(c.kit.Width-5, 1))
	value := propAuto(d.props, "value width", maxInt(c.kit.Width-label-3, 4))
	// 3 columns of chrome: the left border, the separator and the right border.
	return gridtable.FitWidths([]int{label, maxInt(value, 1)}, maxInt(c.kit.Width-3, 2), 1)
}

func (d gridtableDemo) cellRows(c demoCtx) [][]gridtable.Cell {
	switch propOption(d.props, "rows") {
	case 1:
		return spannedRows(c)
	case 2:
		return wrappingRows(c)
	default:
		return labelValueRows(c)
	}
}

func (d gridtableDemo) summaryTables(c demoCtx) [][][]gridtable.Cell {
	k := c.kit.Styles.Kicker
	return [][][]gridtable.Cell{
		gridtable.Rows(k, "totals", [2]string{"tasks", "12"}, [2]string{"comments", "40"}, [2]string{"tags", "3"}),
		gridtable.Rows(k, "period", [2]string{"errors", "7"}, [2]string{"searches", "4"}, [2]string{"solutions", "2"}),
	}
}

func (d gridtableDemo) summaryOpts() gridtable.Options {
	opts := gridtable.Options{
		LabelWidth: propNumber_(d.props, "label width"),
		ValueWidth: propAuto(d.props, "value width", 27),
		Auto:       propOption(d.props, "auto") == 0,
	}
	switch propOption(d.props, "layout") {
	case 1:
		// stacked: SideBySide off, no merge
	case 2:
		opts.MergeNarrow = true
	default:
		opts.SideBySide = true
		opts.MergeNarrow = true
	}
	return opts
}

func (d gridtableDemo) detailLabels() []string {
	if propOption(d.props, "labels") == 1 {
		return []string{"situação", "responsável", "atualizado", "comentários", "impedimentos"}
	}
	return []string{"status", "owner", "updated", "comments", "blockers"}
}

func (d gridtableDemo) buildDetail(c demoCtx) gridtable.Detail {
	l := d.detailLabels()
	return gridtable.NewDetail(propAuto(d.props, "value width", detailValueWidth(c)), c.kit.Styles.Info).
		Kicker("task").
		Row(l[0], "in progress").
		Row(l[1], "howl").
		Row(l[2], "2 hours ago").
		KickerCount(l[4], 2).
		Row("#412", "waiting on the migration").
		Kicker(l[3]).
		Span(gridtable.Raw(joinProse(propNumber_(d.props, "body lines"))))
}

func (d gridtableDemo) matrixRows(c demoCtx) [][]gridtable.Cell {
	k := c.kit.Styles.Kicker
	empty, disallowed := "Empty", "Disallowed"
	backToDev, devToDone := "blockers_clear", "tests_green, review_ok"
	if propOption(d.props, "snapshot") == 1 {
		backToDev, devToDone = empty, empty
	}
	return [][]gridtable.Cell{
		{gridtable.Styled(k("guards"))},
		{gridtable.Styled(k("from \\ to")), gridtable.Styled(k("backlog")), gridtable.Styled(k("dev")), gridtable.Styled(k("done"))},
		{gridtable.Styled(k("backlog")), gridtable.Raw(disallowed), gridtable.Raw(backToDev), gridtable.Raw(disallowed)},
		{gridtable.Styled(k("dev")), gridtable.Raw(disallowed), gridtable.Raw(disallowed), gridtable.Raw(devToDone)},
		{gridtable.Styled(k("done")), gridtable.Raw(disallowed), gridtable.Raw(disallowed), gridtable.Raw(disallowed)},
	}
}

func (d gridtableDemo) matrixWidths(c demoCtx, rows [][]gridtable.Cell) []int {
	label := 10
	for _, row := range rows {
		if len(row) >= 2 {
			if w := gridtable.CellWidth(row[0]); w > label {
				label = w
			}
		}
	}
	const buckets = 3
	value := 12
	if width := (c.kit.Width - label - buckets - 1) / buckets; width > value {
		value = width
	}
	natural := []int{label, value, value, value}
	return gridtable.FitWidths(natural, maxInt(c.kit.Width-5, buckets+1), 1)
}

func (d gridtableDemo) View(c demoCtx) string {
	switch d.shape() {
	case 1:
		tables := d.summaryTables(c)
		return gridtable.Summaries(c.kit.Width, c.kit.Styles.Border, d.summaryOpts(), tables...)
	case 2:
		return d.vp.Fit(d.buildDetail(c).View(c.kit.Styles.Border), c.kit.Height, c.kit.Styles.Hint, nil)
	case 3:
		rows := d.matrixRows(c)
		return gridtable.RenderCells(rows, d.matrixWidths(c, rows), c.kit.Styles.Border)
	default:
		return gridtable.RenderCells(d.cellRows(c), d.cellWidths(c), c.kit.Styles.Border)
	}
}

func (d gridtableDemo) Status(c demoCtx) string {
	switch d.shape() {
	case 1:
		tables := d.summaryTables(c)
		opts := d.summaryOpts()
		return fmt.Sprintf("Width reports %d · available %d", gridtable.Width(c.kit.Width, opts, tables...), c.kit.Width)
	case 2:
		return fmt.Sprintf("scroll %d · label column %d, grown to the widest kicker · value %d",
			d.vp.Scroll, gridtable.LabelWidth,
			propAuto(d.props, "value width", detailValueWidth(c)))
	case 3:
		return fmt.Sprintf("available width %d · 3-bucket [][]string", c.kit.Width)
	default:
		w := d.cellWidths(c)
		_, layout := gridtable.RenderCellsWithLayout(labelValueRows(c), w, c.kit.Styles.Border)
		return fmt.Sprintf("columns %d + %d · RenderWithLayout reports %d lines", w[0], w[1], layout.Lines)
	}
}

func (d gridtableDemo) Props() []prop { return d.props }

func (d gridtableDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d gridtableDemo) Help() []key.Binding {
	if d.shape() == 2 {
		return []key.Binding{binding("j/k", "scroll"), binding("g/G", "top/end"), binding("pgup/pgdn", "page")}
	}
	return statelessHelp()
}

// ---- keyfooter ------------------------------------------------------------

type keyfooterDemo struct {
	props []prop
}

func newKeyfooterDemo(demoCtx) demo {
	return keyfooterDemo{props: []prop{
		numberProp("max primaries", "how many primary tokens keep the accent", 3, 0, 12),
		textProp("separator", "the string between tokens", "  "),
		choiceProp("align", "how a wrapped row sits in the width", 0, "left", "center", "right"),
		numberProp("tokens", "how many bindings the footer is given", 8, 0, 8),
	}}
}

func (d keyfooterDemo) Resize(demoCtx) demo             { return d }
func (d keyfooterDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d keyfooterDemo) styles(c demoCtx) tokenstrip.KeyStyles {
	styles := tokenstrip.FromStyles(c.kit.Styles)
	styles.MaxPrimaries = propNumber_(d.props, "max primaries")
	styles.Separator = propString(d.props, "separator")
	switch propOption(d.props, "align") {
	case 1:
		styles.Align = tokenstrip.AlignCenter
	case 2:
		styles.Align = tokenstrip.AlignRight
	default:
		styles.Align = tokenstrip.AlignLeft
	}
	return styles
}

func (d keyfooterDemo) tokens() []tokenstrip.Key {
	all := footerTokens()
	n := clamp(propNumber_(d.props, "tokens"), 0, len(all))
	return all[:n]
}

func (d keyfooterDemo) View(c demoCtx) string {
	return tokenstrip.KeysWrapped(d.tokens(), d.styles(c), c.kit.Width)
}

func (d keyfooterDemo) Status(c demoCtx) string {
	primaries := 0
	for _, t := range d.tokens() {
		if t.Primary {
			primaries++
		}
	}
	return fmt.Sprintf("%d primary tokens, budget %d — the rest fall back to the muted tone",
		primaries, d.styles(c).MaxPrimaries)
}

func (d keyfooterDemo) Props() []prop { return d.props }

func (d keyfooterDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d keyfooterDemo) Help() []key.Binding { return statelessHelp() }
