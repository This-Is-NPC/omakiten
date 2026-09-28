package screenlayout

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
)

// testKit is a kit at an explicit geometry. Styles are left zero-valued: an
// unset lipgloss.Style renders its input unchanged, which keeps every
// assertion in this file about GEOMETRY rather than about theme bytes.
func testKit(width, height, chromeRows int) screenkit.Kit {
	return screenkit.Kit{Width: width, Height: height, ChromeRows: chromeRows}
}

// lines is the rendered body split for row-level assertions.
func lines(view string) []string { return strings.Split(view, "\n") }

// numbered builds n single-line items.
func numbered(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + "-" + string(rune('a'+i%26))
	}
	return out
}

func listSection(id ID, spec Spec, items []string) Func {
	return Func{Def: spec, Body: constantBody(items)}
}

// constantBody is a section body that ignores its canvas and returns the same
// items every frame — the fixture for assertions about GEOMETRY.
func constantBody(items []string) func(Canvas) Block {
	return func(Canvas) Block { return Block{Items: items} }
}

func TestBodyRowsAgreesWithKitViewportRowsAboveTheSentinel(t *testing.T) {
	// screenkit.ViewportRows returns 0 below four rows, and that 0 means
	// "render everything" to its existing callers. screenlayout may not
	// inherit that sentinel — a 0 budget here has to mean zero rows — so it
	// derives the body budget itself. This test pins the ONLY difference
	// between the two derivations: above the sentinel they agree exactly,
	// modulo the leading blank row Arrange spends and ViewportRows subtracts.
	for height := 8; height <= 60; height++ {
		for _, chrome := range []int{0, 2, 5} {
			kit := testKit(120, height, chrome)
			if kit.ViewportRows(0) == 0 {
				continue
			}
			if got, want := BodyRows(kit), kit.ViewportRows(0)+1; got != want {
				t.Fatalf("BodyRows at height=%d chrome=%d = %d, want ViewportRows(0)+1 = %d",
					height, chrome, got, want)
			}
		}
	}
}

func TestBodyRowsIsZeroNotUnlimitedOnATinyTerminal(t *testing.T) {
	// The wart being designed around: Kit.ViewportRows reports 0 for a
	// terminal under four rows and every existing caller reads that as
	// "unlimited". BodyRows reports the rows that actually exist, and
	// Arrange spends no more than them.
	//
	// Height 1 is where that starts: height 0 is not a tiny terminal, it is an
	// UNMEASURED one — see the test below.
	for height := 1; height <= 4; height++ {
		kit := testKit(80, height, 2)
		want := max(0, height-4)
		if got := BodyRows(kit); got != want {
			t.Fatalf("BodyRows at height=%d = %d, want %d", height, got, want)
		}
	}
}

// TestAnUnmeasuredTerminalStillGetsABody separates the two things a zero height
// can mean, which this package used to conflate (#2425).
//
// A terminal of one to four rows is TINY: it genuinely has no room for a body,
// and reporting zero there is the whole reason BodyRows exists rather than
// Kit.ViewportRows. A terminal of ZERO rows is UNMEASURED — the host has not
// received its first WindowSizeMsg yet — and reporting zero there means a
// headless render paints nothing at all, which is what happened to Task Detail
// the moment it stopped rendering unclipped and started asking the arranger.
//
// screenkit already draws that distinction on the width axis, with a documented
// 120-column assumption "so a headless render still produces a sane layout".
// This is the same rule on the height axis, read off the same accessor.
func TestAnUnmeasuredTerminalStillGetsABody(t *testing.T) {
	kit := testKit(100, 0, 2)
	if got := BodyRows(kit); got <= 0 {
		t.Fatalf("BodyRows on an unmeasured terminal = %d; a headless render paints nothing", got)
	}
	sec := feed("feed", 40)
	res := Arrange(kit, NewState(), sec)
	if res.View == "" {
		t.Fatal("an unmeasured terminal arranged an empty body")
	}
	if got, budget := res.Rows(), BodyRows(kit); got != budget {
		t.Fatalf("an unmeasured body painted %d rows against a %d-row assumption", got, budget)
	}
	// The assumption is exactly the one screenkit makes for a measured terminal
	// of the same height, so there is one rule and not two.
	if got, want := BodyRows(kit), BodyRows(testKit(100, kit.Rows(), 2)); got != want {
		t.Fatalf("unmeasured BodyRows = %d, measured at the assumed height = %d", got, want)
	}
}

func TestArrangeNeverPaintsMoreRowsThanTheBodyHas(t *testing.T) {
	spec := Spec{ID: "list", MinWidth: 20, MinRows: 3, Scroll: ScrollItems}
	for height := 0; height <= 40; height++ {
		kit := testKit(100, height, 2)
		res := Arrange(kit, NewState(), listSection("list", spec, numbered("row", 60)))
		if got, budget := res.Rows(), BodyRows(kit); got > budget {
			t.Fatalf("height=%d painted %d rows into a %d-row body", height, got, budget)
		}
	}
}

func TestArrangeSpendsTheWholeBudgetWhenContentOverflows(t *testing.T) {
	kit := testKit(100, 30, 2)
	spec := Spec{ID: "list", MinWidth: 20, MinRows: 3, Scroll: ScrollItems}
	res := Arrange(kit, NewState(), listSection("list", spec, numbered("row", 200)))
	if got, budget := res.Rows(), BodyRows(kit); got != budget {
		t.Fatalf("overflowing body painted %d rows against a %d-row budget; a windowed body spends it exactly", got, budget)
	}
}

func TestStackedSectionsSplitTheBudgetByWeightUnderTheirMaximums(t *testing.T) {
	kit := testKit(100, 30, 2) // BodyRows = 24, one spent on the leading blank
	form := Spec{ID: "form", MinWidth: 20, MinRows: 5, MaxRows: 5}
	feed := Spec{ID: "feed", MinWidth: 20, MinRows: 3, Weight: 1, Scroll: ScrollItems}
	res := Arrange(kit, NewState(),
		listSection("form", form, numbered("f", 5)),
		listSection("feed", feed, numbered("a", 100)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked", res.Arrangement)
	}
	got := map[ID]int{}
	for _, p := range res.Placements {
		got[p.ID] = p.Rows
	}
	if got["form"] != 5 {
		t.Fatalf("fixed section got %d rows, want its declared 5", got["form"])
	}
	if want := BodyRows(kit) - 1 - 5; got["feed"] != want {
		t.Fatalf("flexible section got %d rows, want the whole remainder %d", got["feed"], want)
	}
}

func TestStackedSectionsShrinkAndDropWhenTheBudgetCannotHoldTheirMinimums(t *testing.T) {
	kit := testKit(100, 12, 2) // BodyRows = 8, 7 to spend
	a := Spec{ID: "a", MinWidth: 20, MinRows: 6, Scroll: ScrollItems}
	b := Spec{ID: "b", MinWidth: 20, MinRows: 6, Scroll: ScrollItems}
	res := Arrange(kit, NewState(),
		listSection("a", a, numbered("a", 30)),
		listSection("b", b, numbered("b", 30)),
	)
	total := 0
	for _, p := range res.Placements {
		total += p.Rows
		if !p.Dropped && p.Rows > 0 && p.Rows < p.Spec.MinRows {
			t.Fatalf("section %s kept at %d rows below its MinRows floor of %d", p.ID, p.Rows, p.Spec.MinRows)
		}
	}
	if total > BodyRows(kit)-1 {
		t.Fatalf("assigned %d rows against %d available", total, BodyRows(kit)-1)
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
	// 6+6 does not fit in 7: drop b, keep a at its floor (plus any surplus).
	aPlace, _ := res.Placement("a")
	bPlace, _ := res.Placement("b")
	if aPlace.Dropped || aPlace.Rows < 6 {
		t.Fatalf("leading section a = dropped=%v rows=%d, want kept at >= 6", aPlace.Dropped, aPlace.Rows)
	}
	if !bPlace.Dropped {
		t.Fatal("trailing section b must drop when both floors cannot fit")
	}
}

// MinRows is a hard floor: the soft shrink that used to drive a kept section
// below its declared minimum is the defect wave K closes. A section that cannot
// have its floor is dropped, not rendered short.
func TestAKeptSectionNeverReceivesFewerRowsThanMinRows(t *testing.T) {
	kit := testKit(100, 12, 2) // 7 rows to spend
	res := Arrange(kit, NewState(),
		listSection("a", Spec{ID: "a", MinWidth: 20, MinRows: 6, Scroll: ScrollItems}, numbered("a", 30)),
		listSection("b", Spec{ID: "b", MinWidth: 20, MinRows: 6, Scroll: ScrollItems}, numbered("b", 30)),
	)
	for _, p := range res.Placements {
		if p.Dropped {
			if p.Rows != 0 {
				t.Fatalf("dropped %s still has %d rows", p.ID, p.Rows)
			}
			continue
		}
		if p.Rows < p.Spec.MinRows {
			t.Fatalf("kept section %s has %d rows, below MinRows %d", p.ID, p.Rows, p.Spec.MinRows)
		}
	}
}

func TestSectionsGetTheirRowBudgetBeforeTheyProduceLines(t *testing.T) {
	// Acceptance criterion 4. The canvas a section renders against carries
	// BOTH numbers, so a section physically cannot produce lines without
	// having been told how many rows it may spend on them.
	kit := testKit(100, 30, 2)
	var sawWidth, sawRows int
	sec := Func{
		Def: Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(c Canvas) Block {
			sawWidth, sawRows = c.Width(), c.Rows()
			return Block{Items: numbered("x", c.Rows()*2)}
		},
	}
	res := Arrange(kit, NewState(), sec)
	if sawWidth != kit.AvailableWidth() {
		t.Fatalf("canvas width = %d, want AvailableWidth %d", sawWidth, kit.AvailableWidth())
	}
	p, _ := res.Placement("s")
	if sawRows != p.Rows || sawRows == 0 {
		t.Fatalf("canvas rows = %d, want the assigned %d", sawRows, p.Rows)
	}
}

func TestChromeLinesAreChargedAgainstTheItemWindow(t *testing.T) {
	kit := testKit(100, 30, 2)
	sec := Func{
		Def: Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(Canvas) Block {
			return Block{
				Header: []string{"// KICKER", "────"},
				Footer: []string{"hint"},
				Items:  numbered("row", 100),
			}
		},
	}
	res := Arrange(kit, NewState(), sec)
	p, _ := res.Placement("s")
	if p.HeaderRows != 2 || p.FooterRows != 1 {
		t.Fatalf("chrome rows = %d header / %d footer, want 2 / 1", p.HeaderRows, p.FooterRows)
	}
	if want := p.Rows - 3; p.ItemViewport != want {
		t.Fatalf("item viewport = %d, want rows(%d) - chrome(3) = %d", p.ItemViewport, p.Rows, want)
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

func TestAnOverWideChromeLineIsChargedTheRowsItActuallyOccupies(t *testing.T) {
	kit := testKit(40, 30, 2) // AvailableWidth = 36
	long := strings.Repeat("wide ", 20)
	sec := Func{
		Def: Spec{ID: "s", MinWidth: 10, MinRows: 4, Scroll: ScrollItems},
		Body: func(Canvas) Block {
			return Block{Header: []string{long}, Items: numbered("row", 100)}
		},
	}
	res := Arrange(kit, NewState(), sec)
	p, _ := res.Placement("s")
	if p.HeaderRows < 3 {
		t.Fatalf("a %d-cell header at width %d charged only %d rows", lipgloss.Width(long), kit.AvailableWidth(), p.HeaderRows)
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

// --- variable heights over a real bordered table ---------------------------

// borderedTable renders rows through gridtable and splits the rendered string
// back into one ITEM per input row using the Layout the renderer reports.
// Wrapped cells therefore give items of 2+ lines and the item heights differ
// from each other — the exact shape a line-index cursor gets wrong.
func borderedTable(id ID, rows [][]string, widths []int) Func {
	return Func{
		Def: Spec{ID: id, MinWidth: 20, MinRows: 4, Scroll: ScrollItems, SelectFirst: true},
		Body: func(Canvas) Block {
			rendered, layout := gridtable.RenderWithLayout(rows, widths, lipgloss.NewStyle())
			all := lines(rendered)
			items := make([]string, len(rows))
			heights := make([]int, len(rows))
			for r := range rows {
				start := layout.RowOffsets[r]
				end := start + layout.RowHeights[r]
				if end < len(all) {
					end++ // the divider (or bottom border) that closes the row
				}
				items[r] = strings.Join(all[start:end], "\n")
				heights[r] = end - start
			}
			return Block{Header: []string{all[0]}, Items: items, Heights: heights}
		},
	}
}

func tableRows() ([][]string, []int) {
	widths := []int{10, 18}
	rows := [][]string{
		{"short", "one line"},
		{"wrapping", "this cell is long enough that it has to wrap onto several lines"},
		{"short2", "one line"},
		{"wrapping2", "another long cell that also wraps across more than a single line"},
		{"short3", "one line"},
		{"wrapping3", "a third long cell wrapping well past a single terminal row here"},
	}
	return rows, widths
}

func TestBorderedTableRowsWrapToSeveralLinesAndTheEngineMeasuresEach(t *testing.T) {
	kit := testKit(120, 40, 2)
	rows, widths := tableRows()
	res := Arrange(kit, NewState(), borderedTable("table", rows, widths))
	p, ok := res.Placement("table")
	if !ok {
		t.Fatal("no placement for the table section")
	}
	if len(p.Heights) != len(rows) {
		t.Fatalf("measured %d item heights for %d rows", len(p.Heights), len(rows))
	}
	tall := 0
	for _, h := range p.Heights {
		if h >= 3 {
			tall++
		}
	}
	if tall == 0 {
		t.Fatalf("no row wrapped to 2+ content lines; heights = %v — the fixture no longer proves anything", p.Heights)
	}
	if p.DeclaredHeightsDisagree {
		t.Fatalf("the section's declared heights disagree with the measured ones: %v", p.Heights)
	}
}

func TestTheCursorIsAnItemIndexAndItsLineIsReportedNotGuessed(t *testing.T) {
	kit := testKit(120, 40, 2)
	rows, widths := tableRows()
	state := NewState().WithCursor("table", 3)
	res := Arrange(kit, state, borderedTable("table", rows, widths))
	p, _ := res.Placement("table")

	if p.Cursor != 3 {
		t.Fatalf("cursor = %d, want the item index 3", p.Cursor)
	}
	// The line the cursor item starts on is the section top, plus its header,
	// plus the MEASURED heights of every visible item before it — never
	// cursor+1, which is what a line-index model would produce.
	want := p.TopLine + p.HeaderRows
	if p.Above > 0 {
		want++
	}
	for i := p.Offset; i < p.Cursor; i++ {
		want += p.Heights[i]
	}
	if p.CursorLine != want {
		t.Fatalf("CursorLine = %d, want %d (heights %v, offset %d)", p.CursorLine, want, p.Heights, p.Offset)
	}
	// And the reported line really is where the item was painted.
	body := lines(res.View)
	if p.CursorLine < 0 || p.CursorLine >= len(body) {
		t.Fatalf("CursorLine %d outside the %d-line body", p.CursorLine, len(body))
	}
	first := lines(p.Items[p.Cursor])[0]
	if body[p.CursorLine] != first {
		t.Fatalf("line %d is %q, want the cursor item's first line %q", p.CursorLine, body[p.CursorLine], first)
	}
	if lineIndexEqualsItemIndex(p) {
		t.Fatal("the fixture degenerated to one line per item; it no longer distinguishes a line cursor from an item cursor")
	}
}

// lineIndexEqualsItemIndex reports whether every item happens to be one line
// tall, in which case an item cursor and a line cursor are indistinguishable
// and the test above proves nothing.
func lineIndexEqualsItemIndex(p Placement) bool {
	for _, h := range p.Heights {
		if h != 1 {
			return false
		}
	}
	return true
}

func TestAVariableHeightCursorStaysVisibleInASmallWindow(t *testing.T) {
	kit := testKit(120, 14, 2) // a window far smaller than the table
	rows, widths := tableRows()
	sec := borderedTable("table", rows, widths)
	state := NewState().WithCursor("table", len(rows)-1)
	res := Arrange(kit, state, sec)
	p, _ := res.Placement("table")

	if p.Cursor < p.First || p.Cursor > p.Last {
		t.Fatalf("cursor %d outside the visible range [%d,%d]", p.Cursor, p.First, p.Last)
	}
	if p.CursorLine < 0 {
		t.Fatal("cursor is visible but no line was reported for it")
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

func TestDeclaredHeightsThatLieAreReportedAndIgnored(t *testing.T) {
	kit := testKit(120, 40, 2)
	sec := Func{
		Def: Spec{ID: "s", MinWidth: 20, MinRows: 4, Scroll: ScrollItems},
		Body: func(Canvas) Block {
			return Block{
				Items:   []string{"a\nb\nc", "d"},
				Heights: []int{1, 1}, // a lie: the first item is three rows
			}
		},
	}
	res := Arrange(kit, NewState(), sec)
	p, _ := res.Placement("s")
	if !p.DeclaredHeightsDisagree {
		t.Fatal("a section under-declared its item heights and the engine did not notice")
	}
	if p.Heights[0] != 3 {
		t.Fatalf("measured height = %d, want the 3 rows the item actually paints", p.Heights[0])
	}
}

// --- slack reclaim ---------------------------------------------------------

func TestRowsASectionDoesNotUseGoToASectionThatIsHidingContent(t *testing.T) {
	// A section holding rows while its neighbour hides items is the defect
	// class in its cross-section form: two private budgets that add up to less
	// than the terminal. The arranger owns both, so it can move the slack.
	kit := testKit(100, 30, 2)
	shortForm := listSection("form", Spec{ID: "form", MinWidth: 20, MinRows: 8, MaxRows: 8}, numbered("f", 2))
	longFeed := feed("feed", 200)

	res := Arrange(kit, NewState(), shortForm, longFeed)
	form, _ := res.Placement("form")
	fed, _ := res.Placement("feed")

	if form.Rows != 8 {
		t.Fatalf("the form was assigned %d rows, want the 8 it declared", form.Rows)
	}
	// The form declared 8 and paints 2, so 6 rows were idle while the feed had
	// items it could not show. They belong to the feed.
	if want := BodyRows(kit) - 1 - 2; fed.Rows != want {
		t.Fatalf("the feed got %d rows; with six idle rows reclaimed it should have %d", fed.Rows, want)
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
}

func TestNothingIsReclaimedWhenNoSectionIsHidingContent(t *testing.T) {
	kit := testKit(100, 30, 2)
	a := listSection("a", Spec{ID: "a", MinWidth: 20, MinRows: 8, MaxRows: 8, Scroll: ScrollItems}, numbered("a", 2))
	b := listSection("b", Spec{ID: "b", MinWidth: 20, MinRows: 6, MaxRows: 6, Scroll: ScrollItems}, numbered("b", 2))
	res := Arrange(kit, NewState(), a, b)
	for _, p := range res.Placements {
		if p.Rows != p.Spec.MinRows {
			t.Fatalf("section %s moved from its declared %d rows to %d with nothing to reclaim for",
				p.ID, p.Spec.MinRows, p.Rows)
		}
	}
}

func TestReclaimedRowsStopAtTheReceivingSectionsMaximum(t *testing.T) {
	kit := testKit(100, 30, 2)
	idle := listSection("idle", Spec{ID: "idle", MinWidth: 20, MinRows: 10, MaxRows: 10}, numbered("i", 1))
	capped := listSection("capped", Spec{ID: "capped", MinWidth: 20, MinRows: 4, MaxRows: 6, Scroll: ScrollItems}, numbered("c", 200))
	res := Arrange(kit, NewState(), idle, capped)
	p, _ := res.Placement("capped")
	if p.Rows > 6 {
		t.Fatalf("the capped section grew to %d rows past its declared maximum of 6", p.Rows)
	}
}

func TestReclaimStopsWhenEveryHidingSectionHasReachedItsMaximum(t *testing.T) {
	// Freed rows with nowhere left to go are simply not painted. The residue is
	// the documented cost of reclaiming in one pass rather than to a fixpoint.
	kit := testKit(100, 30, 2) // 25 rows to spend
	res := Arrange(kit, NewState(),
		listSection("idle", Spec{ID: "idle", MinWidth: 20, MinRows: 12, MaxRows: 12}, numbered("i", 1)),
		listSection("soak", Spec{ID: "soak", MinWidth: 20, MinRows: 10, MaxRows: 10}, numbered("s", 10)),
		listSection("capped", Spec{ID: "capped", MinWidth: 20, MinRows: 3, MaxRows: 5, Scroll: ScrollItems}, numbered("c", 200)),
	)
	capped, _ := res.Placement("capped")
	if capped.Rows != 5 {
		t.Fatalf("the hiding section got %d rows, want it grown to its maximum of 5", capped.Rows)
	}
	if res.Rows() > BodyRows(kit) {
		t.Fatalf("painted %d rows into a %d-row body", res.Rows(), BodyRows(kit))
	}
	if res.Rows() >= BodyRows(kit) {
		t.Fatalf("the body spent all %d rows; freed rows with nowhere to go should stay unpainted", BodyRows(kit))
	}
}
