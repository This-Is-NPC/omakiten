package screenlayout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/scrollwindow"
)

// Arrangement is the breakpoint the arranger chose for this frame.
type Arrangement int

const (
	// Stacked puts every section full-width, one above another.
	Stacked Arrangement = iota
	// SideBySide puts the sections in columns. Chosen when every section's
	// declared MinWidth fits at once, gaps included, AND the painted columns
	// still occupy no more than the box — a SideBySide that overdraws is
	// restacked. Declared mins pick the candidate; measured occupancy confirms it.
	SideBySide
)

func (a Arrangement) String() string {
	if a == SideBySide {
		return "SideBySide"
	}
	return "Stacked"
}

// Placement is everything the arranger decided about one section, reported back
// so a screen can ASK rather than re-derive.
//
// Every field here is a number a screen used to compute for itself and get
// wrong: the width, the row budget, the chrome cost, the scroll offset, the
// visible range, and — the one that produced the Flow defect — the terminal
// line the cursor landed on.
type Placement struct {
	// ID and Spec identify the section, normalised.
	ID   ID
	Spec Spec
	// Width and Rows are the geometry the section was rendered against; the
	// same numbers its Canvas carried.
	Width int
	Rows  int
	// TopLine is the index, into the lines of Result.View, of this section's
	// first line.
	TopLine int
	// HeaderRows and FooterRows are the rows the section's chrome occupies AT
	// ITS WIDTH — measured from the strings it produced, so a wrapped kicker
	// costs the rows it wraps to.
	HeaderRows int
	FooterRows int
	// ItemViewport is the rows left for the item window once chrome is paid.
	ItemViewport int
	// Items are the section's items as they were painted, wrapped to Width.
	Items []string
	// Heights are the MEASURED terminal rows of each item at Width. Item i
	// occupies Heights[i] rows; this is the variable-height model that makes an
	// item cursor and a line cursor different things.
	//
	// For a section that declared [Block.PerRow] these are still per ITEM, so
	// PerRow items sharing a line each report the rows that item paints. The
	// line costs the tallest of them, not their sum; see PerRow.
	Heights []int
	// PerRow is the fold the arranger applied: how many items were joined onto
	// one terminal line, from [Block.PerRow]. One — the common case — is one
	// item per line.
	//
	// It is reported for the same reason Width and ItemViewport are. Every
	// number on this struct that counts items (Offset, First, Last, Above,
	// Below, Cursor) is an item count, and a caller reconciling them with the
	// LINES the section painted would otherwise have to re-derive the fold from
	// the geometry — which is the private copy this package exists to delete.
	PerRow int
	// DeclaredHeightsDisagree is set when the section supplied Block.Heights and
	// they did not match what the items actually paint. The measured heights win;
	// this flag is how a lying section is found rather than tolerated.
	DeclaredHeightsDisagree bool
	// CursorUnread is set when the arranger held a selection for this section —
	// Cursor is >= 0 and the Block inherited it rather than overriding it — and
	// the section's Render never called [Canvas.Cursor].
	//
	// It is the cursor's DeclaredHeightsDisagree. A section that does not read
	// its cursor paints no highlight, yet Cursor and CursorLine still report a
	// selection that moved, so every number agrees with every other number and
	// the screen simply looks like it has nothing selected. Nothing measurable
	// is wrong, which is exactly why it needs saying out loud.
	//
	// The arranger does not guess and does not correct: it cannot paint the
	// highlight itself (appearance is the screen's, see the package doc), so it
	// reports the disagreement and leaves the decision where the knowledge is.
	// A section that legitimately has no selection to draw sets
	// Block.Cursor = NoSelection(), which resolves to -1 and never flags.
	CursorUnread bool
	// LeadingPartialRows and TrailingPartialRows are the terminal rows of the
	// items just OUTSIDE the visible range that were previewed at each edge to
	// spend the section's allocation exactly. They are visual only: First, Last,
	// Above and Below are all whole-item, and a previewed item is still counted
	// as hidden on its own side.
	LeadingPartialRows  int
	TrailingPartialRows int
	// Cursor is the resolved ITEM index under the cursor, or -1 for none.
	Cursor int
	// Offset is the resolved scroll offset, also an ITEM index.
	Offset int
	// First and Last are the inclusive visible item range. Last < First when
	// nothing is visible.
	First int
	Last  int
	// Above and Below are the item counts hidden in each direction.
	//
	// They count ITEMS for a [Block.PerRow] section too, so Above + visible +
	// Below is always len(Items). The "▲ N above" / "▼ N below" row such a
	// section PAINTS counts hidden LINES instead, because screenkit builds it
	// from the folded slice — see perrow.go for why that split is deliberate.
	Above int
	Below int
	// CursorLine is the index, into the lines of Result.View, of the FIRST line
	// of the cursor item — or -1 when there is no cursor or it is off-screen.
	//
	// This is the direct replacement for grepping the rendered body for a
	// marker glyph, and for `2 + row` arithmetic against a bordered table. The
	// renderer knows where it put the row; it says so.
	CursorLine int
	// Dropped is set when the body could not afford this section at all. A
	// dropped section is not rendered and its Rows are zero — it is never
	// rendered into a budget it does not have.
	Dropped bool
}

// Box is the geometry a set of sections is arranged into: the content columns
// they may spend and the terminal rows they may paint.
//
// It is the arranger's input restated as a value, so a caller that HAS a
// geometry can hand it over directly instead of encoding it back into a Kit for
// the arranger to re-derive. A screen never builds one — [Arrange] derives the
// screen's box from the host — and a composition layer always does.
type Box struct {
	// Width is the content columns available. Never negative.
	Width int
	// Rows is the rows the arranged body may paint. Zero is zero rows, not
	// "unlimited": see [BodyRows].
	Rows int
}

// HostBox is the box a SCREEN body is arranged into: the honest available width,
// and the host's row budget less the leading blank row [Arrange] spends itself.
//
// It is the single derivation the whole host path shares — the paint, the
// resync and the keystroke — so no two of them can disagree about the geometry
// a frame was measured at.
func HostBox(kit screenkit.Kit) Box {
	return Box{Width: kit.AvailableWidth(), Rows: max(0, BodyRows(kit)-leadingBlankRows)}
}

// Result is one arranged frame.
type Result struct {
	// View is the complete screen body, leading blank row included. A screen
	// returns it verbatim from View; there is nothing left to prepend, and
	// therefore nothing left to forget to charge for.
	View string
	// Arrangement is the breakpoint chosen.
	Arrangement Arrangement
	// BodyRows is the budget this frame was arranged against.
	BodyRows int
	// Placements is one entry per declared section, in declaration order.
	Placements []Placement
}

// Placement looks a section's placement up by id.
func (r Result) Placement(id ID) (Placement, bool) {
	for _, p := range r.Placements {
		if p.ID == id {
			return p, true
		}
	}
	return Placement{}, false
}

// Rows is the terminal rows View actually paints.
func (r Result) Rows() int {
	if r.View == "" {
		return 0
	}
	return lipgloss.Height(r.View)
}

// Arrange lays the sections out at this frame's geometry and renders the body.
//
// It is PURE: it clamps every offset and cursor for the frame it is painting,
// but persists nothing. That is what lets a screen call it from a value-typed
// View and still be unable to overdraw after a resize the Update path has not
// seen yet. The persisting twins are [State.Resync] and [State.HandleKey], and
// all three share one resolver, so the clamp cannot differ between them.
func Arrange(kit screenkit.Kit, state State, sections ...Section) Result {
	res := arrange(kit, HostBox(kit), state, sections, true)
	// The host path reports the budget INCLUDING the leading blank row it spends,
	// which is the number a screen compares against its terminal.
	res.BodyRows = BodyRows(kit)
	return res
}

// ArrangeIn lays the sections out inside an explicit box instead of deriving one
// from the host's geometry, and paints no leading blank row.
//
// It exists for a caller that is not a screen body: a composition layer that has
// already been given a width and a row allocation — a cell of
// components/screengrid — and must fill exactly that. Deriving the box from a
// Kit is wrong there twice over. The host chrome above the body has already been
// charged by the outer frame, so subtracting it again would shrink every nesting
// level; and the leading blank row is the SCREEN's opening row, so emitting one
// per level would spend a row of the terminal on every nested box.
//
// Everything else is [Arrange]: the same resolver, the same breakpoint, the same
// drop-from-the-bottom, and the same guarantee that the view never paints more
// than box.Rows rows or wider than box.Width columns.
func ArrangeIn(kit screenkit.Kit, box Box, state State, sections ...Section) Result {
	return arrange(kit, box, state, sections, false)
}

// arrange is the one implementation both entry points run.
//
// `lead` is whether the body opens with the blank row this package owns. The
// host path spends it; a nested box has no opening row to spend.
func arrange(kit screenkit.Kit, box Box, state State, sections []Section, lead bool) Result {
	res := Result{Arrangement: Stacked, BodyRows: box.Rows}
	if len(sections) == 0 || box.Rows <= 0 {
		return res
	}
	laid, arrangement := resolveAll(kit, box, state, sections)
	res.Arrangement = arrangement

	var body []string
	if lead {
		body = []string{""} // the leading blank row this package owns and spends
	}
	switch arrangement {
	case SideBySide:
		body = append(body, composeSideBySide(laid, len(body))...)
	default:
		body = append(body, composeStacked(laid, len(body))...)
	}

	budget := box.Rows
	if lead {
		budget++ // the leading blank is a row of the host body, not of the box
	}
	body = clampView(body, box.Width, budget)
	res.View = strings.Join(body, "\n")
	res.Arrangement = arrangement
	res.Placements = make([]Placement, len(laid))
	for i, r := range laid {
		res.Placements[i] = r.placement()
	}
	return res
}

// composeSideBySide writes topLine onto each member and occupies the join so
// a short row in one column cannot slide the next column left.
func composeSideBySide(laid []resolved, lead int) []string {
	specs := specsOf(laid)
	gap := columnGapOf(specs)
	var painted []paintedColumn
	for _, members := range columnsOf(specs) {
		// A column's members paint one under another, so a member's first
		// body line is one past everything its predecessors in the SAME
		// column painted — line 1 is only where a column's HEAD starts. A
		// dropped member paints nothing and hands its position straight on.
		var column paintedColumn
		for _, i := range members {
			laid[i].topLine = lead + len(column.lines)
			if laid[i].dropped {
				continue
			}
			column.lines = append(column.lines, laid[i].lines...)
			column.width = max(column.width, laid[i].width)
		}
		if len(column.lines) > 0 {
			painted = append(painted, column)
		}
	}
	return joinPaintedColumns(painted, gap)
}

func composeStacked(laid []resolved, lead int) []string {
	var body []string
	next := lead
	for i := range laid {
		laid[i].topLine = next
		if laid[i].dropped {
			continue
		}
		body = append(body, laid[i].lines...)
		next += len(laid[i].lines)
	}
	return body
}

// paintedColumn is one column of a side-by-side body: the lines of every member
// that survived, stacked in declaration order, and the width they share.
type paintedColumn struct {
	lines []string
	width int
}

// specsOf is the normalised specs the frame was laid out against, in
// declaration order. Read back off the resolved sections rather than
// re-normalised, so nothing downstream can normalise a second time and differ.
func specsOf(laid []resolved) []Spec {
	out := make([]Spec, len(laid))
	for i, r := range laid {
		out[i] = r.spec
	}
	return out
}

// resolved is one section after the arranger has decided everything about it:
// the [measured] half every caller needs, plus the window and the lines only
// the frame that PAINTS needs.
type resolved struct {
	measured
	first int
	last  int
	above int
	below int
	// leadingPartial and trailingPartial are the rows of the items just outside
	// each edge of the window that were previewed to fill the allocation.
	leadingPartial  int
	trailingPartial int
	lines           []string
	topLine         int
	// cursorRow is the cursor item's first line RELATIVE to the section's own
	// first line, or -1. Made absolute by placement() once topLine is known.
	cursorRow int
}

func (r resolved) placement() Placement {
	cursorLine := -1
	if r.cursorRow >= 0 {
		cursorLine = r.topLine + r.cursorRow
	}
	return Placement{
		ID:                      r.spec.ID,
		Spec:                    r.spec,
		Width:                   r.width,
		Rows:                    r.rows,
		TopLine:                 r.topLine,
		HeaderRows:              len(r.header),
		FooterRows:              len(r.footer),
		ItemViewport:            r.itemViewport,
		Items:                   r.items,
		Heights:                 r.heights,
		PerRow:                  r.perRow,
		DeclaredHeightsDisagree: r.declaredBad,
		CursorUnread:            r.cursorUnread(),
		LeadingPartialRows:      r.leadingPartial,
		TrailingPartialRows:     r.trailingPartial,
		Cursor:                  r.cursor,
		Offset:                  r.offset,
		First:                   r.first,
		Last:                    r.last,
		Above:                   r.above,
		Below:                   r.below,
		CursorLine:              cursorLine,
		Dropped:                 r.dropped,
	}
}

// Widths reports the content columns each section will be given at this
// geometry, in declaration order, WITHOUT rendering anything.
//
// It exists because a section whose Render is expensive enough to memoize has
// to key that memo on the width its items were rendered at — and the width
// arrives on the [Canvas], which only exists INSIDE Render. Task Detail's feed
// is the case: sixty comment cards, each a bordered box sized to the feed
// column, memoized so a keystroke does not re-render all sixty (#2447, #2425).
// Without this the screen would have to re-derive the column width from the
// terminal, which is the private copy this package exists to delete.
//
// It runs the same geometry pass [Arrange] runs — one implementation, so the
// width a memo is keyed on cannot differ from the width the frame paints — and
// it is cheap: the arithmetic is over the static specs, and no section body is
// called.
func Widths(kit screenkit.Kit, sections ...Section) []int {
	return WidthsIn(HostBox(kit), sections...)
}

// FitsSideBySide reports whether these specs would be placed beside one another
// in this box, rather than stacked.
//
// It is the BREAKPOINT PREDICATE, exported for a composition layer that has to
// decide how many children to show before it hands any of them over — a
// windowed row of columns in components/screengrid. Such a caller must ask the
// same question the arranger will then answer, or the count it picks and the
// arrangement it gets can differ, and a window that slid to a width the
// arranger then stacks is worse than no window.
//
// Specs are normalised here, so a caller passes what it declared.
func FitsSideBySide(box Box, specs ...Spec) bool {
	normal := make([]Spec, len(specs))
	for i, s := range specs {
		normal[i] = s.normalize()
	}
	return fitsSideBySide(normal, max(0, box.Width))
}

// TwoSpecsFitSideBySide is [FitsSideBySide] for exactly two specs. It avoids
// the normalization and column-working slices the variadic general case needs,
// for callers that probe a two-column breakpoint on every keystroke.
func TwoSpecsFitSideBySide(box Box, first, second Spec) bool {
	first = first.normalize()
	second = second.normalize()
	if first.Group != "" && first.Group == second.Group {
		return false
	}
	if !first.Column || first.MinWidth <= 0 || !second.Column || second.MinWidth <= 0 {
		return false
	}
	available := max(0, box.Width)
	gap := max(defaultColumnGap, max(first.ColumnGap, second.ColumnGap))
	need := gap + columnFloor(first, available) + columnFloor(second, available)
	return need <= available
}

// WidthsIn is [Widths] for a caller that already has its box — the same reason
// [ArrangeIn] exists.
func WidthsIn(box Box, sections ...Section) []int {
	specs := make([]Spec, len(sections))
	for i, s := range sections {
		specs[i] = s.Spec().normalize()
	}
	widths, _, _, _ := geometry(specs, box)
	return widths
}

// StackedRows reports the rows each spec would be ASSIGNED if arranged
// STACKED in this box, in declaration order, WITHOUT rendering anything.
//
// It is [WidthsIn]'s row counterpart, exported for the same reason
// [FitsSideBySide] is: a composition layer that has to decide how many
// children to show before it hands any of them over — a stacked body in
// components/screengrid deciding whether every zone can have the size it is
// worth — must ask the row distribution the same question [Arrange] will then
// answer, through the one distributeRows this package owns, rather than
// re-deriving a prediction that can drift from what actually gets painted.
//
// A dropped spec reports 0, exactly as [Placement.Rows] does; distributeRows
// itself decides which specs survive a box too short for every floor.
func StackedRows(box Box, specs ...Spec) []int {
	normal := make([]Spec, len(specs))
	for i, s := range specs {
		normal[i] = s.normalize()
	}
	return distributeRows(normal, max(0, box.Rows))
}

// geometry is the pure half of resolving a frame: the breakpoint from
// declared minimums, each section's width and each section's rows, with
// nothing rendered. Occupancy can still restack a SideBySide after paint
// (see resolveSpecs); Widths reports this declared geometry, not that
// restack.
//
// Split out of resolveAll so [Widths] and the first-pass resolver cannot
// disagree about what a column is worth.
func geometry(specs []Spec, box Box) (widths, rows []int, arrangement Arrangement, columns [][]int) {
	if fitsSideBySide(specs, max(0, box.Width)) {
		return geometrySideBySide(specs, box)
	}
	return geometryStacked(specs, box)
}

func geometrySideBySide(specs []Spec, box Box) (widths, rows []int, arrangement Arrangement, columns [][]int) {
	available := max(0, box.Width)
	height := max(0, box.Rows)
	widths = make([]int, len(specs))
	rows = make([]int, len(specs))
	arrangement = SideBySide
	columns = columnsOf(specs)
	columnSpecs := make([]Spec, len(columns))
	for c, members := range columns {
		columnSpecs[c] = groupColumnSpec(specsAt(specs, members))
	}
	columnWidths := distributeWidths(columnSpecs, available)
	for c, members := range columns {
		memberRows := distributeRows(specsAt(specs, members), height)
		for k, i := range members {
			widths[i] = columnWidths[c]
			rows[i] = memberRows[k]
		}
	}
	return widths, rows, arrangement, columns
}

func geometryStacked(specs []Spec, box Box) (widths, rows []int, arrangement Arrangement, columns [][]int) {
	available := max(0, box.Width)
	height := max(0, box.Rows)
	widths = make([]int, len(specs))
	arrangement = Stacked
	all := make([]int, len(specs))
	for i := range specs {
		all[i] = i
	}
	columns = [][]int{all}
	rows = distributeRows(specs, height)
	for i, s := range specs {
		widths[i] = sectionWidth(s, available)
	}
	return widths, rows, arrangement, columns
}

// resolveAll is the single implementation of "what does this frame look like",
// shared by Arrange, State.Resync and State.HandleKey. One resolver means the
// offsets a keystroke moves and the offsets a frame paints can never be
// computed two different ways.
func resolveAll(kit screenkit.Kit, box Box, state State, sections []Section) ([]resolved, Arrangement) {
	return resolveSpecs(kit, box, state, sections, normalized(sections))
}

// resolveSpecs is resolveAll for a caller that has already normalised the specs,
// so a keystroke does not normalise them twice on its way to the same answer.
//
// The one thing it carries over from a previous resolve is the ITEM
// MEASUREMENT, and only where the item is the same string at the same width.
// That is a pure function — WrapAt is deterministic — so a carried measurement
// is byte-identical to a retaken one and cannot change what gets painted. It is
// also total: a section that changed one card changes one string, and the other
// fifty-nine are still the strings they were.
func resolveSpecs(kit screenkit.Kit, box Box, state State, sections []Section, specs []Spec) ([]resolved, Arrangement) {
	widths, rows, arrangement, columns := geometry(specs, box)
	out := paintResolved(kit, box, state, sections, specs, widths, rows, columns)
	if arrangement == SideBySide && sideBySideMustRestack(out, specs, columns, box) {
		widths, rows, arrangement, columns = geometryStacked(specs, box)
		out = paintResolved(kit, box, state, sections, specs, widths, rows, columns)
	}
	occupyResolved(out)
	return out, arrangement
}

func paintResolved(kit screenkit.Kit, box Box, state State, sections []Section, specs []Spec, widths, rows []int, columns [][]int) []resolved {
	prev := state.frame.reusable(specs, box)
	out := make([]resolved, len(sections))
	for i, section := range sections {
		out[i] = renderSection(kit, specs[i], widths[i], rows[i], state, section, prev.at(i))
	}
	for _, members := range columns {
		if len(members) > 1 {
			out = reclaimSlack(kit, specs, rows, out, state, sections, members)
		}
	}
	return out
}

func sideBySideMustRestack(laid []resolved, specs []Spec, columns [][]int, box Box) bool {
	gap := columnGapOf(specs)
	if sideBySideOverflows(laid, columns, gap, box.Width) {
		return true
	}
	return linesOverflow(joinPaintedColumns(paintedFrom(laid, specs), gap), box.Width)
}

func paintedFrom(laid []resolved, specs []Spec) []paintedColumn {
	var painted []paintedColumn
	for _, members := range columnsOf(specs) {
		var column paintedColumn
		for _, i := range members {
			if laid[i].dropped {
				continue
			}
			column.lines = append(column.lines, laid[i].lines...)
			column.width = max(column.width, laid[i].width)
		}
		if len(column.lines) > 0 {
			painted = append(painted, column)
		}
	}
	return painted
}

func occupyResolved(laid []resolved) {
	for i := range laid {
		if laid[i].dropped {
			continue
		}
		laid[i].lines = occupyLines(laid[i].lines, laid[i].width, false)
		if laid[i].rows > 0 && len(laid[i].lines) > laid[i].rows {
			laid[i].lines = laid[i].lines[:laid[i].rows]
		}
	}
}

// reclaimSlack hands rows a section did not use to a section that is hiding
// content, and re-renders the ones that grew.
//
// Without it the defect class survives in its cross-section form: two budgets
// that are each individually correct and together add up to less than the
// terminal. A form that declares eight rows and paints two leaves six idle
// while the feed underneath it hides items — every one of those rows is a row
// of content the user was not shown, which is the same harm as overdrawing,
// arriving from the other direction.
//
// Only rows freed by sections that are NOT growing are handed out, so the
// assigned total can never rise above the body budget: the arithmetic is a
// transfer, not a top-up.
//
// # What it does and does not cover, restated for partial rendering
//
// This pass used to carry a second job it was never able to do. Before
// renderSection previewed items at the window edges, a section could be BOTH
// hiding content and leaving rows blank, and #2425 measured 5-7 such rows on
// the two pilot screens. reclaimSlack could not recover them: a section hiding
// content is a recipient, never a donor, so when every section was hiding there
// was nothing idle to move. Those rows are now spent by the section that owns
// them, at the edge where they occur, which is where the fix belongs — a
// section short of its own allocation is not a cross-section problem.
//
// What is left for this pass is the case it was actually written for, and it is
// a real one: a section with LESS CONTENT than rows, beside a section with more
// content than rows. That is a genuine transfer between two sections and cannot
// be resolved inside either.
//
// It stays ONE pass rather than a fixpoint, and the reason is now stronger: a
// section that grows spends its new allocation exactly, so a second pass would
// find nothing a first pass did not. Rows still go unpainted when every hiding
// section has reached its MaxRows and there is nowhere left to put them.
//
// # Why it runs on a COLUMN, and what that means side by side
//
// The transfer only makes sense between sections that are stacked, because a
// row idle at the bottom of a short column is BESIDE the tall column, not above
// it: moving it would mean re-flowing the columns into a stack. So the unit is
// the column — every section when the body stacks, and a group's members when
// it does not.
//
// Before [Spec.Group] existed that made this pass dead side by side, and it was
// dead for a stated reason rather than by omission: every side-by-side section
// was assigned the whole body height, so no section held a row another one could
// use and the quantity being moved was identically zero. Grouping changes that
// assignment — a column's members SPLIT its height — so a stub member above a
// hiding member is now a genuine transfer inside one column, and this pass runs
// there. What has not changed is that no row ever crosses between columns.
// Pinned by TestSideBySideHasNoSlackToReclaimBecauseColumnsAreNotFungible and
// TestAStackedColumnReclaimsRowsAMemberDidNotUse.
func reclaimSlack(kit screenkit.Kit, specs []Spec, rows []int, laid []resolved, state State, sections []Section, members []int) []resolved {
	needy, any := reclaimNeedy(specs, rows, laid, members)
	if !any {
		return laid
	}
	freed := reclaimFreedRows(rows, laid, members, needy)
	if freed == 0 {
		return laid
	}

	grown := make([]int, len(members))
	shares := make([]share, len(members))
	for k, i := range members {
		grown[k] = rows[i]
		shares[k] = share{floor: specs[i].MinRows, ceiling: specs[i].MaxRows, weight: specs[i].Weight, eligible: needy[i]}
	}
	spreadSurplus(grown, shares, freed)
	reclaimRerender(kit, specs, rows, grown, laid, state, sections, members)
	return laid
}

func reclaimNeedy(specs []Spec, rows []int, laid []resolved, members []int) (map[int]bool, bool) {
	needy := make(map[int]bool, len(members))
	any := false
	for _, i := range members {
		hiding := laid[i].above > 0 || laid[i].below > 0
		if !laid[i].dropped && hiding && !atMax(specs[i], rows[i]) {
			needy[i], any = true, true
		}
	}
	return needy, any
}

func reclaimFreedRows(rows []int, laid []resolved, members []int, needy map[int]bool) int {
	freed := 0
	for _, i := range members {
		if needy[i] || laid[i].dropped {
			continue
		}
		if unused := rows[i] - len(laid[i].lines); unused > 0 {
			freed += unused
		}
	}
	return freed
}

func reclaimRerender(kit screenkit.Kit, specs []Spec, rows, grown []int, laid []resolved, state State, sections []Section, members []int) {
	for k, i := range members {
		if grown[k] != rows[i] {
			laid[i] = renderSection(kit, specs[i], laid[i].width, grown[k], state, sections[i], &laid[i].measured)
		}
	}
}

// renderSection hands one section its canvas, measures what came back, and does
// the windowing on its behalf.
// prev is this section's previous measurement, or nil. It is consulted for one
// thing only — the rows an item already known to be the same string at the same
// width occupies — and never for anything the section is about to say.
func renderSection(kit screenkit.Kit, spec Spec, width, rows int, state State, section Section, prev *measured) resolved {
	r := resolved{measured: measured{spec: spec, width: width, rows: rows, cursor: -1, perRow: 1}, first: 0, last: -1, cursorRow: -1}
	if rows <= 0 {
		r.dropped = true
		r.rows = 0
		return r
	}

	seed := state.Cursor(spec.ID)
	if seed < 0 && spec.SelectFirst {
		seed = 0
	}
	block := section.Render(Canvas{width: width, rows: rows, cursor: seed, read: &r.cursorRead})

	r.header = wrapLines(block.Header, width)
	r.footer = wrapLines(block.Footer, width)
	r.raw, r.items, r.heights, r.declaredBad = measureItems(block, width, prev)
	// The fold is taken from the MEASURED items, so a line costs the tallest
	// thing actually painted on it rather than the tallest thing the section
	// claimed. For the twenty sections that lay one item to a line this is a
	// single comparison and nothing below changes; see perrow.go.
	if r.perRow = normalizePerRow(block.PerRow); r.perRow > 1 {
		r.lineItems, r.lineHeights = foldPerRow(r.items, r.heights, r.perRow)
	}

	r.settleCursor(block, seed)
	r.itemViewport = max(0, rows-len(r.header)-len(r.footer))
	itemLines := r.windowLines(kit, state, width, block.Chrome)

	r.lines = append(append(append([]string{}, r.header...), itemLines...), r.footer...)
	// The only way the assembly can exceed its rows is chrome alone exceeding
	// them, and in that case the item window was already zero and no cursor line
	// was reported — so the clip cannot orphan a cursor.
	if len(r.lines) > rows {
		r.lines = r.lines[:rows]
	}
	return r
}

// windowLines settles this section's (cursor, offset) pair against the
// measurement just taken and assembles the rows the item window selects.
//
// The window slides over LINES — the section's own items for the common
// section, its [Block.PerRow] fold for a grid — while the cursor and the offset
// stay ITEM indices, and the offset is always the first item of a line. Every
// expression here is the expression that shipped wherever a line is an item.
func (r *resolved) windowLines(kit screenkit.Kit, state State, width int, chrome func(string) string) []string {
	lineItems, lineHeights := r.windowItems(), r.windowHeights()

	mode := scrollwindow.HintsNone
	if r.spec.Scroll.Scrolls() {
		mode = scrollwindow.HintsSplit
		r.cursor, r.offset = r.resyncPair(r.cursor, state.Offset(r.spec.ID))
	}
	if len(lineItems) == 0 || r.itemViewport <= 0 {
		return nil
	}

	offsetLine := r.line(r.offset)
	endLine := scrollwindow.Slice(offsetLine, lineHeights, r.itemViewport, mode)
	r.first = r.offset
	r.last = min(r.item(endLine), len(r.items)) - 1
	windowed := offsetLine > 0 || endLine < len(lineItems)
	var blocks []string
	if r.spec.Scroll.Scrolls() {
		blocks = kit.ScrollWindowSplit(lineItems, lineHeights, offsetLine, r.itemViewport)
		// ScrollWindowSplit builds hints without the section width; cap every
		// block so a narrow column cannot be overflowed by "▲ N above".
		blocks = screenkit.CapRows(blocks, width)
		// Then, and only then, the section's own chrome: the cap bounds what the
		// closure is handed, and nothing caps what it returns. Reversed, the cap
		// would shave the border the decoration just added back off.
		blocks = decorateHints(blocks, chrome, offsetLine > 0, endLine < len(lineItems))
		if windowed {
			r.above = scrollwindow.Above(r.first)
			r.below = scrollwindow.Below(r.last+1, len(r.items))
		}
	} else {
		blocks = lineItems[:endLine]
		r.below = scrollwindow.Below(r.last+1, len(r.items))
	}
	// The window closed on a line boundary; whatever rows are left between it
	// and the section's allocation are spent previewing the lines just outside
	// each edge, which is what cardlist has always done.
	r.leadingPartial, r.trailingPartial = scrollwindow.PartialRows(offsetLine, endLine, lineHeights, r.itemViewport, mode)
	itemLines := splitAll(r.spliceEdges(blocks, lineItems, offsetLine, endLine))
	if len(itemLines) > r.itemViewport {
		itemLines = itemLines[:r.itemViewport]
	}
	r.cursorRow = cursorRow(*r, len(itemLines))
	return itemLines
}

// decorateHints hands [Block.Chrome] the rows the ARRANGER injected into the
// window and nothing else: the "▲ N above" ScrollWindowSplit prepends and the
// "▼ N below" it appends.
//
// Which rows those are is not searched for — it is read off the same two
// predicates ScrollWindowSplit itself decides on, offset past the top and end
// short of the bottom, so the two cannot drift apart. Nothing between them is
// touched, because everything between them is an item the section painted.
//
// A nil closure returns blocks untouched and uncopied: the field costs the
// sections that do not declare it nothing at all.
func decorateHints(blocks []string, chrome func(string) string, above, below bool) []string {
	if chrome == nil || len(blocks) == 0 || (!above && !below) {
		return blocks
	}
	out := append([]string(nil), blocks...)
	last := len(out) - 1
	if above {
		out[0] = chrome(out[0])
	}
	// The guard is for the degenerate window where the two hints would be the
	// same row: one row is decorated once, never twice.
	if below && (!above || last != 0) {
		out[last] = chrome(out[last])
	}
	return out
}

// spliceEdges puts the partial-item previews into the assembled window, INSIDE
// the hint rows rather than outside them: "▲ N above" stays the section's top
// line and the tail of the item it counts sits directly under it, so the two
// edges read the same way round as the list they bracket.
//
// The previews are the reason a section spends its allocation exactly. They are
// visual only — [Placement.First], Last, Above and Below stay whole-item, and a
// partially previewed item is still counted as hidden on its own side, because
// the user has not seen all of it.
// lineItems and the two indices are the WINDOW's unit — the section's own items
// for the common section, its folded lines for a [Block.PerRow] one.
func (r resolved) spliceEdges(blocks, lineItems []string, offsetLine, endLine int) []string {
	if r.leadingPartial == 0 && r.trailingPartial == 0 {
		return blocks
	}
	head, tail := 0, len(blocks)
	if r.above > 0 {
		head++ // the "▲ N above" row ScrollWindowSplit prepended
	}
	if r.below > 0 && r.spec.Scroll.Scrolls() {
		tail-- // the "▼ N below" row it appended
	}
	out := make([]string, 0, len(blocks)+2)
	out = append(out, blocks[:head]...)
	if r.leadingPartial > 0 {
		out = append(out, scrollwindow.TailRows(lineItems[offsetLine-1], r.leadingPartial))
	}
	out = append(out, blocks[head:tail]...)
	if r.trailingPartial > 0 {
		out = append(out, scrollwindow.HeadRows(lineItems[endLine], r.trailingPartial))
	}
	return append(out, blocks[tail:]...)
}

// cursorRow is the line the cursor item starts on relative to the section's
// first line: past the header, past the "▲ N above" row when one is showing,
// past the leading partial preview under it, then the MEASURED height of every
// visible item ahead of it.
//
// This is the whole point of item-indexed heights. `header + cursor` would be
// right only for a list of one-line items, and wrong by the accumulated
// wrapping for anything else — which is precisely how a cursor drifted down a
// bordered table until it had to be located by grepping the rendered text.
func cursorRow(r resolved, painted int) int {
	if r.cursor < r.first || r.cursor > r.last {
		return -1
	}
	row := len(r.header)
	if r.above > 0 {
		row++
	}
	row += r.leadingPartial
	// The walk is over LINES, so a grid's cursor lands on the first row of the
	// line its card sits on rather than that many cards down. Identical to the
	// item walk wherever a line is an item.
	heights := r.windowHeights()
	for i := r.line(r.offset); i < r.line(r.cursor); i++ {
		row += heights[i]
	}
	if row >= len(r.header)+painted {
		return -1
	}
	return row
}

// settleCursor is where a section's selection is decided, once, for every
// consumer of this measurement: the Block's own statement folded over the
// arranger's seed, clamped to the items that exist, then snapped onto the
// section's selectable mask.
//
// The snap is HERE, before anything reads the cursor, which is what makes
// "the cursor never lands off the mask" a property of the resolver rather than
// of the keys. A seed a screen wrote, a refresh that shrank the list and a
// resize that changed the item set all arrive through a re-render, and a
// re-render passes through this.
func (r *resolved) settleCursor(block Block, seed int) {
	r.declared = block.Cursor
	r.selectable = normalizeMask(block.Selectable, len(r.items))
	r.cursor = maskFloor(clampCursor(block.Cursor.resolve(seed), len(r.items)), r.selectable)
}

// measureItems wraps each item to the section's width and MEASURES the rows it
// occupies. A height the section declared is compared, never believed.
//
// It also returns the raw items, snapshotted into a slice this package owns, so
// the next measurement of this section can be handed them as prev.
//
// # Carrying a measurement over
//
// prev is this section's previous measurement, or nil. Wrapping is a pure
// function of an item and a width, so an item that is the SAME STRING at the
// same width occupies the same rows and does not need measuring twice — and it
// is the measuring, not the section's own render, that a deep feed spends its
// keystroke on: sixty bordered comment cards are sixty grapheme walks, and a
// cursor move changes exactly two of them.
//
// The comparison is by value against a snapshot, never against the section's
// own slice, so a section that rebuilds its items in place cannot compare a new
// string with itself and be handed an old height. A changed item is a changed
// string; strings are values; there is no third case.
func measureItems(block Block, width int, prev *measured) (raw, items []string, heights []int, disagree bool) {
	if len(block.Items) == 0 {
		return nil, nil, nil, len(block.Heights) > 0
	}
	raw = append([]string(nil), block.Items...)
	items = make([]string, len(raw))
	heights = make([]int, len(raw))
	if prev != nil && prev.width != width {
		prev = nil
	}
	for i, item := range raw {
		if prev != nil && i < len(prev.raw) && prev.raw[i] == item {
			items[i], heights[i] = prev.items[i], prev.heights[i]
			continue
		}
		items[i] = screenkit.WrapAt(item, width)
		heights[i] = max(1, lipgloss.Height(items[i]))
	}
	if len(block.Heights) == 0 {
		return raw, items, heights, false
	}
	if len(block.Heights) != len(heights) {
		return raw, items, heights, true
	}
	for i, declared := range block.Heights {
		if declared != heights[i] {
			disagree = true
			break
		}
	}
	return raw, items, heights, disagree
}

// wrapLines folds chrome blocks to the section's width and flattens them to one
// entry per terminal row, so len() is the row charge. Same wrapping engine
// screenkit.Chrome measures with, at the width this section actually got.
func wrapLines(blocks []string, width int) []string {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, strings.Split(screenkit.WrapAt(block, width), "\n")...)
	}
	return out
}

func splitAll(blocks []string) []string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, strings.Split(block, "\n")...)
	}
	return out
}

func clampCursor(cursor, count int) int {
	if count == 0 || cursor < -1 {
		return -1
	}
	if cursor >= count {
		return count - 1
	}
	return cursor
}
