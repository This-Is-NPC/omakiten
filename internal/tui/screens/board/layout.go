package board

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/lane"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// sectionLanes is the carousel: a WINDOWED row of columns, one child per
// bucket.
//
// It used to be a single ScrollNone cell with the lanes joined inside it by
// hand, because the board owned two cursors the arranger had no spelling for.
// Both spellings exist now — a windowed Cols slides over the lanes the way `h`
// and `l` always did, and each lane is a scrollable leaf with its own cursor and
// its own offset — so the private carousel, the private width allocator's
// viewport half, the per-lane list.Cards map and the whole-view cache are gone.
// What is left of the board's own arithmetic is the WIDTH one lane is worth,
// which is [Screen.computeLayout], and that is an allocator the grid asks for
// rather than a layout the screen paints.
const sectionLanes = screenlayout.ID("board-lanes")

// laneID keys one lane's cursor and scroll offset.
//
// It is the BUCKET key and not the lane's position on purpose: a workflow that
// reorders its buckets moves a lane, and a lane that arrived at position two
// must not inherit the window of whatever used to be there.
func laneID(bucket string) screenlayout.ID { return screenlayout.ID("board-lane-" + bucket) }

const (
	// laneBorders is the columns [lane.Paint] spends outside its content width on
	// the left and right │ edges. A lane asks the grid for the OUTER number,
	// because what the arranger hands a section is the cells it may paint into,
	// borders included.
	laneBorders = 2
	// laneMinRows is 1, and that is a floor rather than the height a usable lane
	// wants.
	//
	// MinRows is a HARD floor the arranger DROPS a section for missing, and in a
	// row of columns every lane is the same height — so a floor of five does not
	// trade a short lane for a tall one, it empties the board the moment the body
	// has four rows. A sixty-by-fourteen terminal has exactly four, which is the
	// geometry the host records as `resize_tasks_board_tiny`: it would go from a
	// clipped lane header to a carousel hint over nothing.
	//
	// This is the same sentence the single board-lanes section carried before the
	// migration — "a high MinRows would drop the whole board rather than let
	// cardlist compress" — and it survives the move to columns unchanged, because
	// what it is really about is a body with no sibling to drop IN FAVOUR OF.
	laneMinRows = 1
)

// layout is the width one lane is worth at this terminal.
type layout struct {
	columnInner int
	cardWidth   int
	cardContent int
}

// laneMemoKey is EVERY input one lane's composition reads, and no others.
//
// `rows` is deliberately absent, for the reason BlockMemo spells out: a key that
// carries the row budget never hits, because one keystroke resolves a section at
// two heights. Nothing about a lane's cards depends on how many of them are on
// screen — that is the window, and the window is the arranger's.
//
// `data` is the snapshot fingerprint the screen already computes for Bind, so a
// refresh that changes one card changes the key for every lane, which is
// correct and costs one comparison. `theme` and the two catalog probes are the
// inputs that live OUTSIDE the snapshot: a palette rotation or a language switch
// changes what a card paints without changing a single task.
type laneMemoKey struct {
	data    uint64
	theme   laneTheme
	width   int
	cursor  int
	focused bool
}

// laneTheme is what a painted lane reads off the kit rather than off the data.
//
// The theme key is the host's own identifier for the palette, so a rotation is a
// string comparison instead of a reflected dump of the style table — which is
// what the deleted whole-view cache spent on every single frame. The catalog
// probes stand in for the strings the lane resolves: the empty-lane line, and
// one of the badge labels, whose plural form moves with the language pack.
type laneTheme struct {
	palette  string
	empty    string
	blockers string
}

func themeOf(kit screenkit.Kit) laneTheme {
	return laneTheme{
		palette:  kit.Markdown.ThemeKey,
		empty:    kit.T("tui.board.empty"),
		blockers: kit.T("tui.badge.blockers"),
	}
}

// laneMemo is the composition memo for one lane, or nil.
//
// One memo PER LANE, each holding exactly one entry, rather than one memo keyed
// by lane: the focused lane's key moves on every j and would otherwise evict the
// three lanes beside it that did not change, and a many-entry memo keyed on a
// moving cursor grows an entry per card the user has ever visited.
//
// A nil memo builds, so a Screen assembled without New() is slower and never
// wrong — the contract BlockMemo already documents.
func (s Screen) laneMemo(bucket string) *screenlayout.BlockMemo[laneMemoKey] {
	if s.memos == nil {
		return nil
	}
	memo, ok := s.memos[bucket]
	if !ok {
		memo = &screenlayout.BlockMemo[laneMemoKey]{}
		s.memos[bucket] = memo
	}
	return memo
}

// computeLayout is the board's width allocator, and the only geometry it still
// owns. It answers "how wide is one lane" — the number every lane then DECLARES
// as its minimum and maximum — and the grid answers everything else: how many of
// them fit, which of them are on screen, how many rows each gets, and where each
// lane's card window sits inside those rows.
//
// It stays a method because the preferred inner width is a property of the
// board's cards, not of the terminal. Width is supplied by the body box so the
// allocator cannot quietly measure a second geometry source.
func (s Screen) computeLayout(available, n int) layout {
	const minInner, maxInner, preferredInner = 28, 44, 32
	if n <= 0 {
		n = 1
	}
	capacity := (available + 1) / (preferredInner + 3)
	if capacity < 1 {
		capacity = 1
	}
	if capacity > n {
		capacity = n
	}
	inner := (available-(capacity-1))/capacity - 2
	inner = clamp(inner, minInner, maxInner)
	return layout{columnInner: inner, cardWidth: inner - 2, cardContent: inner - 4}
}

// root is the whole body: every lane, side by side, sliding rather than
// shrinking when the terminal cannot hold them all.
//
// Windowed is what keeps the shape. Nine buckets on an eighty-column terminal
// cannot stack — nine stacked lanes is not a board — and cannot be squeezed into
// eighty columns either, so the grid shows the ones that fit and `h` / `l` reach
// the rest, which is exactly what the board's private carousel did.
func (s Screen) root(kit screenkit.Kit) screengrid.Node {
	buckets := s.projection.Buckets()
	l := s.computeLayout(screenlayout.HostBox(kit).Width, len(buckets))
	focus := s.grid.Focus()
	theme := themeOf(kit)
	lanes := make([]screengrid.Node, len(buckets))
	for i, bucket := range buckets {
		lanes[i] = s.laneCell(kit, bucket, laneID(bucket.Key) == focus, l, theme)
	}
	// No ColumnGap is declared, and that is the declaration: the arranger's
	// default is one blank column, which is exactly the `" "` the hand-rolled
	// JoinHorizontal used to put between two lanes. ZoneGap — the two columns the
	// multi-zone screens spend — is a measure for zones that are different
	// THINGS; lanes are the same thing repeated, and two columns between them
	// would cost a lane at eighty columns.
	return screengrid.Cols(screenlayout.Spec{ID: sectionLanes}, lanes...).
		Windowed().
		WithHint(s.lanesHint(kit))
}

// lanesHint is the carousel's own wording for what the window hid: `lanes 2–4 /
// 4 · left/right scrolls`, the sentence this screen has printed since before the
// grid existed and the one forty-two language packs already carry under
// `tui.board.lanes_hint_fmt`.
//
// The grid's default is `‹ 2` / `3 ›` — right, and anonymous. Lanes are named
// things a user counts, so the count is worth spelling out; adopting the grid by
// deleting the sentence would be a migration that removed information.
func (s Screen) lanesHint(kit screenkit.Kit) func(first, last, total int) string {
	return func(first, last, total int) string {
		return kit.Styles.Hint.Render(fmt.Sprintf(kit.T("tui.board.lanes_hint_fmt"), first, last, total))
	}
}

// laneCell is one bucket: a leaf that paints a bordered kanban column and lets
// the grid window the cards inside it.
//
// MinWidth == MaxWidth is the lane's whole width statement. Lanes are uniform by
// design — a board whose columns were different widths would read as a table —
// so the allocator picks one number and every lane pins itself to it, which also
// means the grid's own slide arithmetic (`FitsSideBySide` over the k widest
// children) lands on the same count the board used to compute by hand.
func (s Screen) laneCell(kit screenkit.Kit, bucket domain.Bucket, focused bool, l layout, theme laneTheme) screengrid.Node {
	section := s.laneSection(kit, bucket, focused, l, theme)
	return screengrid.Cell(section.Def, section.Body)
}

// laneSection is the lane as the ARRANGER sees it, kept separable from the node
// so a caller that has already been told a lane's box — [Screen.CursorVisible] —
// can ask that one arranger call what it windowed, instead of restating the
// window arithmetic and then agreeing with any bug in it.
func (s Screen) laneSection(kit screenkit.Kit, bucket domain.Bucket, focused bool, l layout, theme laneTheme) screenlayout.Func {
	outer := l.columnInner + laneBorders
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID:       laneID(bucket.Key),
			MinWidth: outer, MaxWidth: outer,
			MinRows: laneMinRows,
			Scroll:  screenlayout.ScrollItems,
			// A lane opens on its first card rather than on the no-selection
			// sentinel: the board has never had an unselected lane, and `enter` on
			// a freshly opened board has always had a task to open.
			SelectFirst: true,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.laneBlock(kit, bucket, focused, l, theme, canvas.Cursor())
		},
	}
}

// laneBlock paints one lane and hands it over SPLIT: the top border, the kicker
// and the rule as Header, one entry per card as Items, the bottom border as
// Footer, and the lane's own box as Chrome.
//
// The split is what lets the grid scroll a bordered lane. Header and Footer are
// pinned, the cards window between them, and the "▲ N above" rows the window
// injects go through Chrome so they land INSIDE the borders instead of between
// them as bare lines. One item is one card, never one terminal row, so the
// cursor steps cards and a three-row card is one stop.
func (s Screen) laneBlock(kit screenkit.Kit, bucket domain.Bucket, focused bool, l layout, theme laneTheme, cursor int) screenlayout.Block {
	key := laneMemoKey{data: s.dataKey, theme: theme, width: l.columnInner, cursor: cursor, focused: focused}
	return s.laneMemo(bucket.Key).Block(key, nil, func() screenlayout.Block {
		tasks := s.tasksByBucket[bucket.Key]
		painter := card.Painter{Styles: kit.Styles}
		cards := make([]string, len(tasks))
		for i, task := range tasks {
			// Only the FOCUSED lane paints a selection. Every lane holds a cursor
			// — that is what makes `l` land back where you left a lane — but a
			// board showing four highlighted cards would be saying the cursor is
			// in four places.
			cards[i] = painter.Render(s.cardSpec(kit, task, focused && i == cursor, l))
		}
		frame := lane.Paint(kit, lane.PaintSpec{
			Header:    s.columnHeader(kit, bucket, focused),
			Inner:     l.columnInner,
			EmptyText: kit.T("tui.board.empty"),
			Cards:     cards,
		})
		return screenlayout.Block{
			Header: frame.Header,
			Items:  frame.Items,
			Footer: frame.Footer,
			Chrome: frame.Chrome,
		}
	})
}

// bodyBox is the box the grid is handed, and the one every call about this body
// shares — Render, HandleKey and Resync alike, because a key routed against a
// different box than the one that painted is a key that moves the wrong window.
//
// It is the host box less whatever [Screen.emptyChrome] is going to occupy. The
// chrome is charged BEFORE the grid sees the box rather than appended after it,
// which is the same rule the grid applies to its own hint row: a row painted out
// of the leftover is a row that pushes the body off the bottom of the terminal.
func (s Screen) bodyBox(kit screenkit.Kit) screenlayout.Box {
	box := screenlayout.HostBox(kit)
	box.Rows = max(0, box.Rows-len(s.emptyChrome(kit)))
	return box
}

// emptyChrome is the call to action a board with buckets but no cards paints
// under its lanes, or nil.
//
// It is the SCREEN's, deliberately, and it is the one thing here the grid does
// not own. It is not a zone: it holds no cursor, takes no key and is not
// something `tab` should be able to land on — and a second child under the
// carousel would make it one, because the ring a windowed row of columns walks
// is its lanes. So it stays chrome, and it is charged the way chrome is.
func (s Screen) emptyChrome(kit screenkit.Kit) []string {
	if len(s.projection.Buckets()) == 0 || s.taskCount() > 0 {
		return nil
	}
	return append([]string{"", ""}, strings.Split(s.emptyHint(kit), "\n")...)
}
