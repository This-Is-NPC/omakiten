package logs

import (
	"strings"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// sectionEvents is the event feed — the inspector's subject. MinRows is a hard
// floor so the primary feed remains usable before the supplemental summary can
// claim rows.
const sectionEvents = screenlayout.ID("logs-events")

// sectionSummary is declared after the feed so the rows arranger drops it
// first when the panel cannot afford both zones.
const sectionSummary = screenlayout.ID("logs-summary")

const eventsMinRows = 10

const bodyRootID = screenlayout.ID("logs-body")

func (s Screen) bodyRoot(kit screenkit.Kit) screengrid.Node {
	events := screengrid.Cell(screenlayout.Spec{
		ID: sectionEvents, MinRows: eventsMinRows, Weight: 1,
		Scroll: screenlayout.ScrollItems, SelectFirst: true,
	}, func(canvas screenlayout.Canvas) screenlayout.Block {
		return s.eventsBlock(kit, canvas)
	})
	summary := s.summaryCell(kit)
	// Rows reports hidden children with a hint row. The legacy logs panel did
	// not paint that hint when its supplemental summary yielded, so omit the
	// already-measured summary child before mounting when the two floors cannot
	// fit. The same Rows tree is retained whenever both zones fit.
	if s.panelBox(kit).Rows < eventsMinRows+summary.Spec.MinRows {
		// A lone feed cell is already a valid Rows-tree leaf. Avoid a
		// one-child container here: its nested ArrangeIn pass narrows the
		// canvas by one column and changes the legacy compact golden.
		return events
	}
	return screengrid.Rows(screenlayout.Spec{ID: bodyRootID}, events, summary)
}

func (s Screen) eventsBlock(kit screenkit.Kit, canvas screenlayout.Canvas) screenlayout.Block {
	width := max(1, canvas.Width())
	cursor := screenlayout.NoSelection()
	if len(s.rows) > 0 {
		cursor = screenlayout.At(s.selected)
	}
	inner := max(1, width-panel.Borders)
	kicker, extra := s.panelKicker(kit, inner)
	block := framed.List(kit.Styles.Border, width, kicker, extra, s.feedItems(kit, inner))
	notes := s.panelFooter(kit)
	for i, note := range notes {
		notes[i] = block.Chrome(note)
	}
	block.Footer = append(notes, block.Footer...)
	block.Cursor = cursor
	return block
}

// summaryCell mounts the supplemental aggregate, sized to the document it
// paints. The document is composed ONCE per (aggregate, width, theme) and read
// twice: by the floor here, which is what decides whether the summary can be
// mounted at all, and by the body, which paints it. Neither reading can be
// dropped — the floor is not knowable without the height, and the height is not
// knowable without the document — so the composition is shared instead.
func (s Screen) summaryCell(kit screenkit.Kit) screengrid.Node {
	rows := max(1, len(s.summaryDocument(kit, max(1, kit.PanelContentWidth())).Items))
	return screengrid.Cell(screenlayout.Spec{
		ID: sectionSummary, MinRows: rows, MaxRows: rows,
		Scroll: screenlayout.ScrollNone,
	}, func(canvas screenlayout.Canvas) screenlayout.Block {
		return s.summaryDocument(kit, max(1, canvas.Width()))
	})
}

// logsBlockCache is the compositions this screen carries between frames.
//
// It lives behind a POINTER on the screen for the reason
// [screenlayout.BlockMemo] gives: a body is composed under a value receiver —
// once while Update resolves the keystroke, again while View paints — and
// neither may write back into a screen copy. What it holds is DERIVED, so a
// pointer shared between two screen values leaks WORK and never a decision.
type logsBlockCache struct {
	feed    screenlayout.BlockMemo[logsFeedKey]
	summary screenlayout.BlockMemo[logsSummaryKey]
}

// logsFeedKey identifies one composition of the event feed's item lines.
//
// Every input the build reads is here. `count` is the number of ITEMS, not a
// row budget: the feed composes all of its rows whatever height it is handed
// and the arranger is what windows them, so its composition does not depend on
// canvas.Rows() and canvas.Rows() is not in this key (P2 in
// .docs/internal/tui-screen-assembly.md).
//
// The CURSOR is deliberately absent too. The build renders every row unmarked
// and [Screen.feedItems] repaints the single row under the cursor on top of it,
// so a j/k that moves the selection repaints one row instead of the buffer.
type logsFeedKey struct {
	width      int
	count      int
	generation uint64
	theme      string
}

// logsSummaryKey identifies one composition of the summary document. Same rule:
// width, the buffer the aggregate was folded from, and the theme it is painted
// in — never the rows, which the document does not read.
type logsSummaryKey struct {
	width      int
	generation uint64
	theme      string
}

// feedItems are the event feed's item lines at this width, capped and ready to
// hand the arranger.
//
// The expensive half — one styled, truncated, payload-summarised line per
// loaded event — is composed once per (buffer, width, theme). A keystroke moves
// the cursor, and a cursor move changes exactly one row: the marked one. So the
// memoised set is unmarked, and the row under the cursor is re-rendered over a
// copy of it. The copy is not optional — the memo's slice is the entry, and
// writing through it would make the memo answer for content it no longer holds.
func (s Screen) feedItems(kit screenkit.Kit, width int) []string {
	build := func() screenlayout.Block {
		return screenlayout.Block{Items: screenkit.CapRows(s.rowLines(kit, width, -1), width)}
	}
	var base screenlayout.Block
	if s.body == nil {
		base = build()
	} else {
		base = s.body.feed.Block(logsFeedKey{
			width: width, count: len(s.rows), generation: s.generation,
			theme: kit.Markdown.ThemeKey,
		}, nil, build)
	}
	items := append([]string(nil), base.Items...)
	if s.selected >= 0 && s.selected < len(items) {
		items[s.selected] = capLine(s.dataRow(kit, width, s.selected, true), width)
	}
	return items
}

// summaryDocument is the summary tables as the block that paints them, composed
// once per (buffer, width, theme). See [Screen.summaryCell] for why one
// composition has to answer both the floor and the paint.
func (s Screen) summaryDocument(kit screenkit.Kit, width int) screenlayout.Block {
	build := func() screenlayout.Block {
		lines := screenkit.CapRows(strings.Split(s.renderSummaryTables(kit, width), "\n"), width)
		return screenlayout.Block{Items: lines, Cursor: screenlayout.NoSelection()}
	}
	if s.body == nil {
		return build()
	}
	// The catalog travels as INPUTS rather than in the key: the labels are
	// resolved strings the build reads, and comparing five of them is cheaper
	// than composing two bordered tables to find out they did not change.
	return s.body.summary.Block(logsSummaryKey{
		width: width, generation: s.generation, theme: kit.Markdown.ThemeKey,
	}, s.summaryLabels(kit), build)
}

// summaryLabels are the catalog strings renderSummaryTables paints, in the
// order it reads them.
func (s Screen) summaryLabels(kit screenkit.Kit) []string {
	return []string{
		kit.T("tui.log.categories"),
		kit.T("tui.log.health_tool_calls"),
		kit.T("tui.log.ok"),
		kit.T("tui.log.error"),
		kit.T("tui.log.running"),
	}
}

// capLine caps one composed line to width the way CapRows caps a slice of them.
func capLine(line string, width int) string {
	return screenkit.CapRows([]string{line}, width)[0]
}

// outerChromeBlocks are the rows View paints above PanelBox — the filter chip
// strip and its spacer. They are charged against the same panel geometry used
// by render, key handling and resync.
func (s Screen) outerChromeBlocks(kit screenkit.Kit) []string {
	return []string{kit.WrapBody(s.renderFilterChips(kit)), ""}
}

// panelBox is the geometry the grid's render, key handling and resync passes
// share: panel content width and the rows left after outer chip chrome and the
// panel border. Header/footer rows are charged by the arranger from Blocks.
func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().Lines(s.outerChromeBlocks(kit)...).ViewportRows()
	if kit.Height <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	return screenlayout.Box{Width: kit.PanelContentWidth(), Rows: rows}
}

// syncEventsWindow parks the selected row on the grid and resyncs so its
// window follows every selection move, filter cycle and resize.
func (s Screen) syncEventsWindow(kit screenkit.Kit) Screen {
	s.grid = s.grid.WithFocus(sectionEvents)
	if len(s.rows) > 0 {
		s.grid = s.grid.WithCursor(sectionEvents, s.selected)
	}
	s.grid = s.grid.Resync(kit, s.panelBox(kit), s.bodyRoot(kit))
	if c := s.grid.Layout().Cursor(sectionEvents); c >= 0 {
		s.selected = c
	}
	return s
}

// viewportRows is the event item-window budget after its pinned chrome. When
// the summary floor cannot fit, bodyRoot omits it before mounting, so the
// event box is the whole panel and no second grid arrange is needed for page
// steps. The fitting path reads the actual placement.
func (s Screen) viewportRows(kit screenkit.Kit) int {
	box := s.panelBox(kit)
	width := max(1, box.Width)
	rows := box.Rows
	summary := s.summaryCell(kit)
	if rows >= eventsMinRows+summary.Spec.MinRows {
		res := screengrid.Render(kit, s.grid, box, s.bodyRoot(kit))
		place, ok := res.Placement(sectionEvents)
		if !ok || place.Dropped {
			return 0
		}
		width = max(1, place.Box.Width)
		rows = place.Box.Rows
	}
	inner := max(1, width-panel.Borders)
	kicker, extra := s.panelKicker(kit, inner)
	chrome := framed.Box(kit.Styles.Border, width, kicker, nil)
	header := len(chrome.Header) + len(extra)
	footer := measureWrappedLines(s.panelFooter(kit), width) + len(chrome.Footer)
	return max(0, rows-header-footer)
}

// measureWrappedLines counts the rows wrapLines charges for a chrome slice at
// width — empty strings cost one row and over-wide lines soft-wrap.
func measureWrappedLines(lines []string, width int) int {
	if width <= 0 {
		return len(lines)
	}
	wrapped := screenkit.WrapAt(strings.Join(lines, "\n"), width)
	return len(strings.Split(wrapped, "\n"))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
