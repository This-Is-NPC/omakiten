package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// shellDemo is the grid every screen body is built out of, with COLOURED BLOCKS
// where the components go.
//
// It is the one entry in the gallery that is about SHAPE rather than about a
// widget. Every other entry answers "what does this component look like"; this
// one answers "where do the components end up when the terminal changes", which
// is what a screen is actually a decision about.
//
// The shapes are TRANSCRIBED, not generated — see shell_shapes.go for why that
// distinction is the whole value of this entry. What the props change is
// policy: a minimum, a cap, how much content there is. The topology belongs to
// the screen.
//
// The blocks are solid on purpose. A cell paints every row it was given, at the
// full width it was given, so a row the layout failed to spend shows up as a
// hole and a row it overspent shows up as a block pushed past the frame. There
// is nothing to count and no legend to read.
type shellDemo struct {
	state screengrid.State
	props []prop
}

func newShellDemo(c demoCtx) demo {
	d := shellDemo{state: screengrid.NewState(), props: []prop{
		choiceProp("screen", "which screen's body is being laid out; the topology is that screen's, not a knob", 0, shapeNames()...),
		autoProp("min width", "override Spec.MinWidth on every cell — the breakpoint input. auto keeps the screen's own", 200),
		autoProp("min rows", "override Spec.MinRows on every cell. auto keeps the screen's own", 40),
		autoProp("max width", "override Spec.MaxWidth on every cell — the comfortable-measure cap. auto keeps the screen's own", 300),
		autoProp("gap", "override Spec.ColumnGap between columns. auto keeps the screen's own", 8),
		autoProp("content", "items each cell paints; auto fills the cell exactly, any number lets it window", 400),
		choiceProp("labels", "the id and resolved geometry, painted on each block", 0, "on", "off"),
	}}
	return d.Resize(c)
}

func (d shellDemo) Props() []prop { return d.props }

func (d shellDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d shellDemo) Resize(c demoCtx) demo {
	d.state = d.state.Resync(c.kit, d.box(c), d.root(c))
	return d
}

func (d shellDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	// Every key goes to the grid unchanged, `tab` included. The gallery's own
	// `tab` is surrendered by the capture mode rather than worked around here:
	// a demo that had to remap its keys would be exercising a key table the app
	// does not ship.
	state, _ := d.state.HandleKey(c.kit, d.box(c), msg.String(), d.root(c))
	d.state = state
	return d
}

func (d shellDemo) View(c demoCtx) string {
	return screengrid.Render(c.kit, d.state, d.box(c), d.root(c)).View
}

// Status is the grid's own diagnostic: the breakpoint each container chose, and
// anything it had to drop or hide to fit. It ends with the SOURCE of the shape,
// so a transcription that has drifted from its screen is visible here rather
// than trusted.
func (d shellDemo) Status(c demoCtx) string {
	res := screengrid.Render(c.kit, d.state, d.box(c), d.root(c))
	box := d.box(c)

	var containers []string
	dropped, hidden := 0, 0
	for _, p := range res.Placements {
		if p.Dropped {
			dropped++
			continue
		}
		if p.Leaf {
			continue
		}
		note := p.Arrangement.String()
		if p.Windowed && (p.HiddenBefore > 0 || p.HiddenAfter > 0) {
			hidden += p.HiddenBefore + p.HiddenAfter
			note = fmt.Sprintf("showing %d-%d of %d", p.First+1, p.Last+1, p.Last+1+p.HiddenAfter)
		}
		containers = append(containers, fmt.Sprintf("%s %s", p.ID, note))
	}

	status := fmt.Sprintf("box %dx%d · paints %d rows · focus %s",
		box.Width, box.Rows, res.Rows(), orDash(string(res.Focus)))
	if len(containers) > 0 {
		status += " · " + strings.Join(containers, " · ")
	}
	if dropped > 0 {
		status += fmt.Sprintf(" · %d dropped", dropped)
	}
	if hidden > 0 {
		status += fmt.Sprintf(" · %d off-screen", hidden)
	}
	return status + " · from " + d.shape().source
}

func (d shellDemo) Help() []key.Binding {
	return []key.Binding{
		binding("j/k", "move"),
		binding("tab", "cell"),
		binding("h/l", "lane"),
		binding("pgup/pgdn", "page"),
		binding("g/G", "ends"),
	}
}

// box is the geometry the grid is handed. The frame IS the terminal here — the
// gallery already subtracted its own chrome — so the shell spends all of it,
// which is what makes the frame controls an honest simulation.
func (d shellDemo) box(c demoCtx) screenlayout.Box {
	return screenlayout.Box{Width: c.kit.AvailableWidth(), Rows: c.kit.Rows()}
}

// shape is the screen currently selected.
func (d shellDemo) shape() shellShape {
	shapes := shellShapes()
	at := propOption(d.props, "screen")
	if at < 0 || at >= len(shapes) {
		at = 0
	}
	return shapes[at]
}

// root builds the selected screen's tree, with the prop overrides folded into
// every spec on the way past.
func (d shellDemo) root(c demoCtx) screengrid.Node {
	tint := 0
	leaf := func(cell cellSpec) screengrid.Node {
		node := d.cell(c, cell, tint)
		tint++
		return node
	}
	return d.shape().build(leaf, d.spec)
}

// spec turns a transcribed cellSpec into a screenlayout.Spec, applying whatever
// the props override. An override that is `auto` leaves the screen's own number
// alone — which is what makes the default view the real layout rather than a
// version of it the gallery invented.
func (d shellDemo) spec(cell cellSpec) screenlayout.Spec {
	spec := screenlayout.Spec{
		ID:           screenlayout.ID(cell.id),
		MinWidth:     cell.minWidth,
		MaxWidth:     cell.maxWidth,
		WidthPercent: cell.widthPercent,
		MinRows:      cell.minRows,
		MaxRows:      cell.maxRows,
		Weight:       cell.weight,
		ColumnGap:    cell.gap,
	}
	if v := propNumber_(d.props, "min width"); v > 0 {
		spec.MinWidth = v
	}
	if v := propNumber_(d.props, "min rows"); v > 0 {
		spec.MinRows = v
		if spec.MaxRows > 0 && spec.MaxRows < v {
			spec.MaxRows = v
		}
	}
	if v := propNumber_(d.props, "max width"); v > 0 {
		spec.MaxWidth = v
	}
	if v := propNumber_(d.props, "gap"); v > 0 {
		spec.ColumnGap = v
	}
	return spec
}

// cell is one coloured block. Its spec is the screen's real Spec and its body
// is a real section body — the substitution is the CONTENT, not the contract.
func (d shellDemo) cell(c demoCtx, cell cellSpec, tint int) screengrid.Node {
	spec := d.spec(cell)
	spec.Scroll = screenlayout.ScrollItems
	spec.SelectFirst = true
	style := blockTint(c.kit.Styles, tint)
	focused := d.state.Focus() == spec.ID
	labels := propOption(d.props, "labels") == 0
	items := propNumber_(d.props, "content")

	return screengrid.Cell(spec, func(canvas screenlayout.Canvas) screenlayout.Block {
		width := maxInt(canvas.Width(), 1)
		block := screenlayout.Block{Cursor: screenlayout.NoSelection()}
		if labels {
			block.Header = []string{focusHeader(c, cell.id, canvas, focused, width)}
		}
		// A badge tone is a PILL: it carries its own horizontal padding, and a
		// fill sized to the canvas would render two cells wider than the canvas
		// and wrap to a second, nearly blank row. The frame comes off the fill,
		// not off the block, so the rectangle still ends exactly at the edge.
		inner := maxInt(width-style.GetHorizontalFrameSize(), 1)
		// Zero content means FILL: the cell paints exactly the rows it was given,
		// so the block is the cell and its edges are the layout's edges. Any other
		// value is a fixed item count, which is how you watch it window.
		count := items
		if count == 0 {
			count = maxInt(canvas.Rows()-len(block.Header), 0)
		}
		// The focused cell is drawn SOLID and the rest are drawn as a texture, so
		// which zone has the keys is legible from across the room rather than
		// from one chevron. Colour alone will not do it: the blocks are already
		// eight different colours, so a ninth means nothing, and a terminal
		// without colour would have no focus indicator at all.
		glyph := "░"
		if focused {
			glyph = "█"
		}
		rows := make([]string, count)
		for i := range rows {
			rows[i] = style.Render(strings.Repeat(glyph, inner))
		}
		block.Items = rows
		return block
	})
}

// focusHeader is the cell's label row: `▸ id 38×12` in the accent when the cell
// holds focus, `  id 38×12` muted when it does not.
//
// It is the app's own convention rather than one invented here — a focused zone
// on task detail and project is exactly a `▸` and a swap from the structural
// tone to the accent — so a layout approved in the gallery reads the same way
// the screen will.
func focusHeader(c demoCtx, id string, canvas screenlayout.Canvas, focused bool, width int) string {
	label := fmt.Sprintf("%s %d×%d", id, canvas.Width(), canvas.Rows())
	if focused {
		return c.kit.Styles.HintAccent.Render(padTo("▸ "+strings.ToUpper(label), width))
	}
	return c.kit.Styles.Hint.Render(padTo("  "+label, width))
}

// padTo pads a label out to the block's width so the header reads as part of
// the same solid rectangle rather than as a floating string.
func padTo(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// blockTint cycles the theme's badge tones, so neighbouring blocks are always
// distinguishable and the colours are the theme's rather than invented here.
func blockTint(styles screenkit.Styles, i int) lipgloss.Style {
	tints := []lipgloss.Style{
		styles.BadgeNormal, styles.BadgeComment, styles.BadgeSubtask,
		styles.BadgeHigh, styles.BadgeScope, styles.BadgeActive,
		styles.BadgeFix, styles.BadgeLow,
	}
	return tints[((i%len(tints))+len(tints))%len(tints)]
}
