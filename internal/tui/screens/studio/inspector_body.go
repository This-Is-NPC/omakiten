package studio

import (
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/selectlist"
)

// inspectorBodyMinRows is top border + kicker + rule + one item + bottom
// border. Below that the SVG drops the body box (compact stacked inspector).
const inspectorBodyMinRows = 5

// inspectorChromeRows is the kicker and rule painted inside the body box,
// matching selectlist / Stats › Logs (header chrome lives in the frame, not
// as a loose line between two boxes).
const inspectorChromeRows = 2

// inspectorBox is the detail ZONE of a Studio inspector: a height-filling box
// whose first inner rows are the section kicker and rule. HISTORY, PREVIEW and
// the workflow examples are all this box.
//
// It used to take the field table as well and paint the two as one section,
// which is what made the table unreachable by `tab` and left a table-only
// inspector with no way to show it held the focus. The table is its own zone
// now (see inspector_zones.go), so this draws one box and nothing else.
//
// `columnHeader` is a table heading pinned below the rule, above the rows it
// names. Pinned is the point: HISTORY used to pass its TIME/TYPE/ENTITY line as
// the first ITEM, so the arranger scrolled the heading off the top with the rows
// and two lines in the table stopped saying what its columns were.
//
// A missing kicker means no box — bucket hints stay loose the way the SVG paints
// them. `focused` accents a still-plain kicker with the task-detail ▸ HintAccent
// treatment; pre-styled kickers pass through.
func (m Screen) inspectorBox(canvas screenlayout.Canvas, kicker, columnHeader string, body []string, focused bool) screenlayout.Block {
	kicker = m.paintInspectorKicker(kicker, focused)
	if kicker == "" {
		return screenlayout.Block{Items: body, Cursor: screenlayout.NoSelection()}
	}
	remain := canvas.Rows()
	if remain < inspectorBodyMinRows {
		return screenlayout.Block{Header: []string{kicker}, Items: body, Cursor: screenlayout.NoSelection()}
	}
	inner := canvas.Width() - panel.Borders
	if inner < 1 {
		inner = 1
	}
	itemSlots := remain - panel.Borders - inspectorChromeRows
	if columnHeader != "" {
		itemSlots--
	}
	if itemSlots < 1 {
		itemSlots = 1
	}

	// The ROWS are deliberately not part of the key. Everything expensive here —
	// wrapping the body and bordering each line it wraps to — is a function of the
	// content and the width alone; the rows decide only how many blank rows are
	// padded on at the end. Keying on them made the memo alternate between the two
	// heights one keystroke resolves (the allocation, then reclaimSlack's grown
	// re-render) and never hit at all.
	// The ROWS are deliberately not part of the key — see [screenlayout.BlockMemo],
	// which is where that trap is written down. Everything expensive here is a
	// function of the content and the width; the rows decide only how many blank
	// rows pad the end, which happens below the memo.
	key := inspectorBoxKey{kicker: kicker, columnHeader: columnHeader, width: canvas.Width()}
	box := m.boxes.parts(key, body, func() inspectorBoxParts {
		return m.composeInspectorBox(kicker, columnHeader, body, inner)
	})

	items := box.items
	if len(items) < itemSlots {
		// Copied rather than appended in place: the cached slice's backing array
		// is the memo's, and padding into it would rewrite an entry a later frame
		// still expects to be the content alone.
		items = make([]string, 0, itemSlots)
		items = append(items, box.items...)
		for len(items) < itemSlots {
			items = append(items, box.blank)
		}
	}
	return screenlayout.Block{
		Header: box.header,
		Items:  items,
		Footer: box.footer,
		Cursor: screenlayout.NoSelection(),
		Chrome: selectlist.WrapLine(m.kit, canvas.Width()),
	}
}

// inspectorBox is the composed box, less the padding that depends on how many
// rows the zone was given.
type inspectorBoxParts struct {
	header []string
	items  []string
	footer []string
	blank  string
}

// composeInspectorBox is the expensive half: the border, the kicker, the rule,
// and every line of the body wrapped to the inner width and bordered.
func (m Screen) composeInspectorBox(kicker, columnHeader string, body []string, inner int) inspectorBoxParts {
	// Body lines are word-wrapped to the box's inner width BEFORE they reach
	// framed.Box, which only adds the side borders — the wrapping is content
	// layout, not box chrome, and framed.Box takes items exactly as wide as
	// they will be painted.
	lines := make([]string, 0, len(body))
	for _, line := range body {
		lines = append(lines, gridtable.WrapLines([]string{line}, inner)...)
	}
	var columnHeaderLines []string
	if columnHeader != "" {
		columnHeaderLines = []string{columnHeader}
	}
	block := framed.List(m.styles.Border, inner+panel.Borders, kicker, columnHeaderLines, lines)
	return inspectorBoxParts{header: block.Header, items: block.Items, footer: block.Footer, blank: block.Chrome("")}
}

// inspectorBoxKey is everything about a box's composition that is not its
// content. The rows are absent on purpose — see [Screen.inspectorBox].
type inspectorBoxKey struct {
	kicker       string
	columnHeader string
	width        int
}

// inspectorBoxCache is the detail zone's composition, kept between frames.
//
// It is [screenlayout.BlockMemo] with the Studio-shaped payload: the box is
// composed in four parts (header, items, footer and the blank row the padding
// repeats), and only the items are what the arranger windows. Every reason a
// memo is needed at all, and the one way to key it wrong, are written down on
// BlockMemo — this type is the adaptor, not a second idea.
type inspectorBoxCache struct {
	memo screenlayout.BlockMemo[inspectorBoxKey]
}

// parts memoises one composition. The four parts travel through BlockMemo as a
// Block because that is the currency it deals in: the header and footer are the
// box's chrome, the items are its rows, and the blank row rides along as the
// footer's second line so nothing has to be recomposed to pad with it.
func (c *inspectorBoxCache) parts(key inspectorBoxKey, body []string, build func() inspectorBoxParts) inspectorBoxParts {
	if c == nil {
		return build()
	}
	block := c.memo.Block(key, body, func() screenlayout.Block {
		parts := build()
		return screenlayout.Block{
			Header: parts.header,
			Items:  parts.items,
			Footer: append(append([]string{}, parts.footer...), parts.blank),
		}
	})
	return inspectorBoxParts{
		header: block.Header,
		items:  block.Items,
		footer: block.Footer[:len(block.Footer)-1],
		blank:  block.Footer[len(block.Footer)-1],
	}
}
