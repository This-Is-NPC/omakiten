package screenlayout

// ID names a section. It is the key its scroll offset and cursor are stored
// under in [State], so it has to be stable across frames — a literal constant
// on the screen, not something derived from data.
type ID string

// ScrollPolicy is what a section declares about its item window.
type ScrollPolicy int

const (
	// ScrollNone is a section whose items are never windowed by the user: a
	// form, a summary strip, a header block. It is still CLIPPED to the rows it
	// was assigned — a non-scrollable section is not a licence to overdraw —
	// but it takes no offset, no hint rows and no keys.
	ScrollNone ScrollPolicy = iota
	// ScrollItems is a section whose items scroll. It gets a persisted offset,
	// the reserved "▲ N above" / "▼ N below" hint rows, and the standard scroll
	// keys from [StandardBindings] whenever it holds focus. A screen does not
	// opt into the keys and therefore cannot forget them.
	ScrollItems
)

// Scrolls reports whether the policy takes an offset and the standard keys.
func (p ScrollPolicy) Scrolls() bool { return p == ScrollItems }

// Spec is a section's static declaration. Everything here is a MINIMUM or a
// POLICY — never a computed geometry. The arranger turns these into the actual
// width and rows, which is the whole point: a screen states what it needs, not
// what it gets.
type Spec struct {
	// ID keys this section's persisted cursor and offset.
	ID ID
	// MinWidth is the content columns below which this section cannot be
	// placed beside another. It is the width input to the stacked /
	// side-by-side decision, and it is compared against the honest
	// screenkit.AvailableWidth (#2422) rather than an inflated floor.
	MinWidth int
	// Column opts this section into being placed BESIDE its siblings when the
	// terminal is wide enough. Side-by-side is chosen only when every section
	// in the set sets it and every MinWidth fits at once; otherwise the body
	// stacks.
	//
	// It is opt-in on purpose. Nineteen of the twenty-one screens have no
	// breakpoint at all, and inferring "these two could fit beside each other,
	// so they should" from MinWidth alone would silently turn every two-section
	// screen into columns on a wide terminal. A breakpoint is a design
	// decision; the arranger executes it, it does not invent it.
	Column bool
	// Group stacks this section with every sibling declaring the same group,
	// inside ONE column of a side-by-side body. Empty — the zero value — is a
	// section that is its own column, which is what every section was before
	// this field existed.
	//
	// It is the second level of composition both pilot screens already have:
	// `[form over subtasks] | [activity]`, and Project's `[meta over dashboard]
	// | [activity]`. A flat set of three sections can only be three columns
	// beside each other, and folding two of them into a single section IS
	// expressible — it is just the wrong answer, because one section is one
	// scroll surface and one focus zone, so the fold merges two of each. Grouping
	// is a LAYOUT statement and nothing else: members keep their own offsets,
	// their own cursors and their own turn in the focus cycle.
	//
	// The column's width is the tightest declaration of its members — the widest
	// minimum, the narrowest maximum, the strongest weight, and any proportion a
	// member declared — so no member is placed narrower than it asked or wider
	// than it allowed. Its rows are split between its members by the same rule a
	// stacked body uses, [reclaimSlack] included.
	//
	// Stacked there is one column already, so a group says nothing and is
	// ignored. A body whose sections are all in ONE group therefore has one
	// column, and one column is a stack.
	Group ID
	// MinRows is the rows this section needs before it is worth drawing.
	// Normalised to at least 1. It is a HARD floor: a kept section never
	// receives fewer rows than MinRows. When the body cannot afford every
	// section's minimum, sections are dropped from the bottom up until the
	// remaining floors fit, and a dropped section is reported as such rather
	// than silently rendered short.
	MinRows int
	// MaxWidth caps the content columns this section may be given when it is
	// placed BESIDE a sibling. Zero means unbounded there too — the section
	// takes its weighted share of the surplus.
	//
	// It exists because a measure that is comfortable to read is not the same
	// number as the terminal's width: Project's activity feed has capped itself
	// at 96 columns since before this package existed, by hand. A section that
	// wants that cap must be able to DECLARE it, or the migration reintroduces
	// the private width constant this package removes.
	//
	// Surplus a capped section refuses goes to the sections that can still take
	// it. When every section is capped the leftover columns simply go unspent,
	// exactly as leftover rows do — the contract is "never more than available".
	//
	// Stacked, a cap that is not FIXED is meaningless and is not applied: the
	// section takes the whole available width, exactly as its uncapped
	// siblings do, because there is no column left for the cap to be a share
	// of. MinWidth == MaxWidth is the exception, the same signal [Spec.MaxRows]
	// uses for a fixed height — a lane that declares one true width keeps it
	// whether or not anything sits beside it.
	MaxWidth int
	// WidthPercent declares this section's column as a SHARE of the available
	// width — `available * WidthPercent / 100`, then raised to MinWidth, cut to
	// MaxWidth, and never past the available width itself. Zero means the
	// section takes the weighted share of the surplus instead, which is what
	// every section did before this field existed.
	//
	// It is a different function from Weight, not a tuning of it, and that is
	// why it had to be added rather than fitted. A weighted share is
	// `minimum + weight/total * surplus`: the floors are constant while the
	// surplus grows, so the ratio between two columns drifts as the terminal
	// widens. A proportion does not drift — it is the same fraction of every
	// width. The pilot brute-forced all 4096 integer weight pairs from 1..64
	// against Project's two recorded side-by-side geometries and none reproduced
	// both; the closest missed by five columns (#2425, #2446).
	//
	// Both screens this package was designed against size their activity column
	// as `available*45/100` clamped to [44, 96], written out independently in two
	// packages. A section that cannot DECLARE that keeps the expression, which is
	// the private copy this package exists to delete.
	//
	// A proportion is a COLUMN rule and nothing else. Stacked there is no
	// column for it to be a share of, so it is not applied — the section takes
	// the whole available width, capped only when MaxWidth is FIXED (see
	// MaxWidth).
	WidthPercent int
	// MaxRows caps the section unconditionally, everywhere it is placed. Zero
	// means unbounded. MinRows == MaxRows is a fixed-height section — the form
	// is the case this exists for, and it keeps its declared rows whether it is
	// stacked, beside a sibling, or filling the body alone.
	//
	// A ceiling that only means something INSIDE a shared column is
	// [ColumnMaxRows], not this field: setting MaxRows for that purpose caps
	// the section even once it has the whole body to itself, which is the
	// defect this split exists to prevent.
	MaxRows int
	// ColumnMaxRows caps the section only while it shares a column with a
	// sibling — the field table over the detail box, the task grid over the
	// sub-task board. It is read by [screengrid], which is the one place that
	// knows whether this render is a column's internal split or the whole
	// body: a body that has stacked past its breakpoint, or a zone that has
	// gone full screen, is neither, and the section is then unbounded exactly
	// as an uncapped one would be.
	//
	// This package itself never reads it — [Spec.MaxRows] is the only ceiling
	// the row distribution knows about. A caller outside screengrid that hands
	// Arrange a Spec with ColumnMaxRows set and MaxRows unset gets an
	// unbounded section, not a capped one.
	ColumnMaxRows int
	// ExpectedRows is the rows a section actually expects to use, when that
	// differs from the ceiling it declared. MaxRows and ColumnMaxRows are
	// UPPER BOUNDS — a section is never given more than one of them, but
	// nothing says it will spend everything under it. A field table's ceiling
	// is the case this exists for: it pads one row per field for a VALUE that
	// might wrap, so the ceiling is what the table cannot exceed, not what it
	// needs — see inspectorFieldsCeiling's own doc.
	//
	// Zero — the default — means the ceiling IS the expectation: whichever of
	// MaxRows or ColumnMaxRows the section declared carries no slack of its
	// own. A section whose ceiling pads for a possibility it does not always
	// use states the honest number here instead.
	//
	// This package's own row distribution never reads it — MaxRows and
	// ColumnMaxRows are still the only ceilings [distributeRows] enforces, so
	// a section that reaches its expectation and keeps growing toward its
	// padded ceiling is never capped early by a number that was never a
	// ceiling. It exists for a caller outside this package that has to decide
	// whether a box can give every section what it is WORTH before deciding
	// who gets it at all — [screengrid]'s stacked-body threshold is the one
	// that reads it today.
	ExpectedRows int
	// Weight is this section's share of the surplus — rows left over when
	// stacked, columns left over when side by side. Zero is treated as 1.
	Weight int
	// ColumnGap is the blank columns this section wants between the columns of
	// a side-by-side body. Zero means the default of one, which is what shipped.
	//
	// A gap is BETWEEN two columns, so it cannot be two numbers: the widest
	// declared by any section in the body wins, and no section is ever placed
	// closer to its neighbour than it asked. Both pilot screens spend two
	// columns on their `JoinHorizontal(left, "  ", right)` today, and a screen
	// that cannot declare that keeps the join — which is where the width
	// arithmetic used to go wrong.
	ColumnGap int
	// Scroll declares the item-window policy.
	Scroll ScrollPolicy
	// SelectFirst starts a fresh section's cursor on item 0 rather than on the
	// no-selection sentinel. Sections whose first render should already show a
	// highlighted row set it; sections that want an explicit "nothing selected
	// yet" state leave it false, matching cardlist's -1 sentinel.
	SelectFirst bool
}

// normalize applies the documented defaults so the rest of the package can read
// the spec without re-checking them at every use.
func (s Spec) normalize() Spec {
	if s.MinRows < 1 {
		s.MinRows = 1
	}
	if s.MaxRows > 0 && s.MaxRows < s.MinRows {
		s.MaxRows = s.MinRows
	}
	if s.ColumnMaxRows > 0 && s.ColumnMaxRows < s.MinRows {
		s.ColumnMaxRows = s.MinRows
	}
	s.ExpectedRows = s.normalizeExpectedRows()
	if s.Weight < 1 {
		s.Weight = 1
	}
	if s.MinWidth < 0 {
		s.MinWidth = 0
	}
	if s.MaxWidth > 0 && s.MaxWidth < s.MinWidth {
		s.MaxWidth = s.MinWidth
	}
	if s.WidthPercent < 0 {
		s.WidthPercent = 0
	}
	if s.WidthPercent > 100 {
		s.WidthPercent = 100
	}
	if s.ColumnGap < 0 {
		s.ColumnGap = 0
	}
	return s
}

// normalizeExpectedRows clamps a declared ExpectedRows to the range it can
// mean anything in: never below the hard floor MinRows already normalised to,
// and never above whichever ceiling — MaxRows or ColumnMaxRows — the section
// also declared, since expecting more than the cap the section can never
// exceed is not an expectation.
func (s Spec) normalizeExpectedRows() int {
	if s.ExpectedRows <= 0 {
		return s.ExpectedRows
	}
	expected := max(s.ExpectedRows, s.MinRows)
	ceiling := s.MaxRows
	if ceiling == 0 {
		ceiling = s.ColumnMaxRows
	}
	if ceiling > 0 {
		expected = min(expected, ceiling)
	}
	return expected
}

// Canvas is the geometry a section renders against. Width and rows arrive
// TOGETHER, from the arranger, and there is no other way to obtain either —
// which is the mechanical answer to "row budget at render time". A section
// cannot produce a line before it has been told how many lines it may produce,
// because the only thing that hands it a Canvas is the arranger, and the
// arranger has already decided.
//
// The fields are unexported so a section cannot construct a Canvas with a width
// it computed itself. [NewCanvas] exists for tests of a section in isolation.
type Canvas struct {
	width  int
	rows   int
	cursor int
	// read is set by Cursor() so the arranger can tell a section that PAINTED
	// no selection from one that was never told there was a selection to paint.
	// See [Placement.CursorUnread].
	read *bool
}

// NewCanvas builds a Canvas for testing a section on its own. Production code
// receives its Canvas from [Arrange]; a screen that calls this is computing its
// own geometry, which is the thing this package exists to prevent.
//
// A Canvas built here tracks no cursor read — there is no arranger listening.
func NewCanvas(width, rows, cursor int) Canvas {
	return Canvas{width: width, rows: rows, cursor: cursor}
}

// Width is the content columns this section may paint into.
func (c Canvas) Width() int { return c.width }

// Rows is the terminal rows this section may paint, INCLUDING its own header
// and footer chrome. A section that elides content on a short terminal reads
// this; a section that merely scrolls can ignore it, because the arranger does
// the windowing.
func (c Canvas) Rows() int { return c.rows }

// Cursor is the item index the arranger currently holds for this section, or -1
// for no selection. A section reads it to paint the selected item's highlight;
// it does not store it.
//
// The read is RECORDED. A section that never calls this while the arranger is
// holding a selection for it paints no highlight, and the arranger reports that
// disagreement on [Placement.CursorUnread] rather than letting the screen look
// like it has no selection at all.
func (c Canvas) Cursor() int {
	if c.read != nil {
		*c.read = true
	}
	return c.cursor
}

// cursorMode distinguishes the three things a Block can say about the cursor.
type cursorMode int

const (
	// cursorInherit is the ZERO VALUE, deliberately: a Block literal that says
	// nothing about the cursor keeps the arranger's, rather than accidentally
	// asserting item 0.
	cursorInherit cursorMode = iota
	cursorAt
	cursorNone
)

// Cursor is a section's optional statement about which ITEM is selected.
//
// It is item-indexed by construction — there is no way to spell a line index —
// which is the type-level form of the fix for the Flow defect, where a cursor
// counted lines against a table whose rows were two and three lines tall.
//
// The zero value inherits the arranger's cursor, so the common section says
// nothing and keeps working.
type Cursor struct {
	mode  cursorMode
	index int
}

// At states that the given ITEM index is selected. Use it when the selection is
// authoritative somewhere else — a board whose focused card is driven by the
// host, a list that re-selects by domain id after a refresh.
func At(index int) Cursor { return Cursor{mode: cursorAt, index: index} }

// NoSelection states that nothing is selected this frame.
func NoSelection() Cursor { return Cursor{mode: cursorNone} }

// resolve folds the Block's statement against the arranger's stored cursor.
func (c Cursor) resolve(stored int) int {
	switch c.mode {
	case cursorAt:
		return c.index
	case cursorNone:
		return -1
	default:
		return stored
	}
}

// Block is what a section produces: content, and the item boundaries between
// pieces of it. It carries no geometry — no offset, no visible range, no line
// numbers — because those are the arranger's and a second copy is the defect.
type Block struct {
	// Header is chrome painted above the item window and never scrolled: a
	// kicker, a column header, a rule. Charged the rows it OCCUPIES at the
	// section's width, so a header that wraps costs what it wraps to.
	Header []string
	// Items are the section's selectable units, each already rendered. An item
	// may be any number of terminal rows — a bordered table row, a card, a
	// wrapped cell. One item is one cursor position.
	Items []string
	// Heights is the section's own statement of each item's terminal-row count,
	// or nil.
	//
	// It is ADVISORY. The arranger measures every item itself at the section's
	// width and uses what it measures, reporting a disagreement on
	// [Placement.DeclaredHeightsDisagree]. The field exists so a section that
	// already knows its heights can say so and have the claim checked — not so
	// it can be believed. A believed height is a private copy of a number the
	// rendered string already carries, which is the defect this package exists
	// to remove.
	Heights []int
	// PerRow is how many items share one terminal line, for a section whose
	// items are laid out side by side: a card grid.
	//
	// Zero and one — the zero value included — are one item per line, which is
	// what every section was before this field existed and what the resolver
	// still does expression for expression. Above one the arranger JOINS the
	// items into lines of PerRow, measures each line as the tallest item on it,
	// and windows the LINES; the cursor keeps stepping one ITEM at a time and
	// the offset always lands on the first item of a line.
	//
	// It is a LAYOUT statement, and that is the point. A screen that packs
	// cards has two units — the card it selects and the line it scrolls — and
	// before this field the only way to say so was to hand this package the
	// joined lines and keep a private card cursor beside them, converting on
	// every keystroke. Declaring the fold moves both units inside the resolver,
	// where there is one cursor and one clamp.
	//
	// The number is the SCREEN's, because only the screen knows how wide one of
	// its cells paints. It reads the width off its [Canvas] and says how many
	// fit; see cardtable.Cols. See perrow.go for what the fold costs.
	PerRow int
	// Footer is chrome painted below the item window and never scrolled.
	Footer []string
	// Chrome optionally decorates the rows the ARRANGER injects into this
	// section: the "▲ N above" and "▼ N below" hints the scroll window adds
	// between Header and Items. It is handed those rows and nothing else — never
	// an item, never a Header or Footer row — because those the section painted
	// itself and they already carry whatever chrome they need.
	//
	// Nil — the zero value — is a strict no-op. The hints are emitted exactly as
	// they were before this field existed, which is what every section that does
	// not declare it keeps getting.
	//
	// It exists because a section whose Header and Footer draw a BOX has no way
	// to reach the rows the arranger puts inside that box. The hints arrive as
	// bare lines, so they land between the borders without sides and without
	// padding to the inner width, and the box breaks on exactly the frames where
	// the section scrolls. A closure is the only shape that keeps the arranger
	// from having to know what a border is: the arranger says WHICH rows are its
	// own, the section says how one of its rows should look.
	//
	// Ordering against the arranger's width cap is deliberate, and settles both
	// ends of the contract. ScrollWindowSplit builds the hint without knowing the
	// section's width, so the arranger caps it — FIRST, then decorates. That
	// bounds the closure's INPUT by the section width, and leaves its OUTPUT
	// uncapped: decoration ADDS columns, and a cap running last would shave the
	// right border straight back off, which is the same broken box one column
	// further in.
	//
	// So the closure owns the fit of what it returns. Truncate and pad to your
	// own inner width, put your sides back, return exactly one line — the three
	// steps a bordered section already takes for every row it paints itself.
	Chrome func(string) string
	// Cursor optionally overrides the arranger's item cursor. The zero value
	// inherits it.
	Cursor Cursor
	// Selectable is the ascending ITEM indices the cursor may land on, or nil.
	//
	// Nil — the zero value — is every item, which is what a list is and what
	// every section was before this field existed. A section declares it when
	// some of its items are not landing places: a projection whose blank
	// separators occupy rows and must stay visible without becoming stops, a
	// feed with non-interactive day headers between its entries.
	//
	// It moves the CURSOR and nothing else. Masked-out items are still painted,
	// still measured, still counted by the window and the "▲ N above" hints —
	// the mask is not a filter, and a section that wants an item gone omits the
	// item. See selectable.go for the mask's own contract; the declaration is
	// normalised against the items that exist rather than believed, the same way
	// Heights is compared rather than believed.
	Selectable []int
}

// Section is one declared region of a screen body.
//
// # Why Render is single-phase
//
// The alternative considered was a two-phase Measure(width) / Render(width,
// rows). It was rejected: two phases means the section answers the same
// question twice, and the arranger has to believe the first answer while
// painting the second. A section that measures 12 rows and renders 14 puts the
// overdraw straight back — a private copy of a number, held across a phase
// boundary instead of across a package boundary, which is the same defect
// wearing a different hat.
//
// One phase gives the number exactly one author. The section is told its budget
// before it produces anything (via [Canvas]), and what it produces is then
// MEASURED, not asked about. The arranger never needs a natural height, because
// row distribution runs off the static [Spec] and windowing runs off measured
// item heights — neither needs a speculative render.
type Section interface {
	Spec() Spec
	Render(Canvas) Block
}

// Func adapts a spec plus a closure into a Section, which is what a screen
// method usually is.
type Func struct {
	Def  Spec
	Body func(Canvas) Block
}

func (f Func) Spec() Spec { return f.Def }

func (f Func) Render(c Canvas) Block {
	if f.Body == nil {
		return Block{}
	}
	return f.Body(c)
}
