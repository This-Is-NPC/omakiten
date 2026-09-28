package screenkit

// The width vocabulary a screen declares to the arranger. Each name is WHAT
// the measure is, not which screen uses it: a feed's comfortable cap is one
// number whether Project or Task Detail writes the Spec, and a list|inspector
// breakpoint is one number whether Commands or Workflow writes the tree.
//
// # Why this exists
//
// Before these constants every multi-column screen invented the same integers
// locally — feed 44/96/45%, list|inspector 40/40/gap 2, a panel floor of 32, a
// details column of 60 — twelve copies of three archetypes. WidthPercent
// deleted the duplicated *expression* and left the duplicated numbers.
// Changing the list|inspector breakpoint was six edits in five files.
//
// The row dimension already refused that copy: Chrome measures, the screen
// does not count. This is the same refusal for columns. A Spec's MinWidth,
// MaxWidth, WidthPercent and ColumnGap read a name from here; they do not
// receive a literal, and they do not receive a constant the screen declared.
//
// # These values do not change here
//
// The numbers are the census, not a redesign. A measure that should be a
// different integer is a decision taken after the vocabulary is in place,
// against goldens, in one edit — which is the point of having the name.

const (
	// ComfortableReadingWidth is the columns a feed stays comfortable at. A
	// section that wants that cap declares it as MaxWidth; surplus it refuses
	// goes to the columns that can still take it.
	ComfortableReadingWidth = 96

	// FeedFloor is the columns below which a feed is not worth placing beside
	// another zone. It is the MinWidth input to the stacked / side-by-side
	// decision for that column.
	FeedFloor = 44

	// FeedProportion is the feed column as a percent of available width —
	// `available * FeedProportion / 100`, then raised to FeedFloor and cut to
	// ComfortableReadingWidth. It is a different function from Weight, which
	// is a share of the surplus and drifts as the terminal widens.
	FeedProportion = 45

	// ListFloor is the columns below which a list is not worth placing beside
	// an inspector.
	ListFloor = 40

	// InspectorFloor is the columns below which an inspector is not worth
	// placing beside a list.
	InspectorFloor = 40

	// PanelFloor is the columns below which a meta/dashboard panel is not
	// worth placing beside a feed.
	PanelFloor = 32

	// DetailColumnFloor is the columns below which a details+subtasks column
	// is not worth placing beside a feed.
	DetailColumnFloor = 60

	// ZoneGap is the blank columns between side-by-side zones. It is BETWEEN
	// two columns, so it cannot be two numbers: the widest declared by any
	// section in the body wins.
	//
	// The arranger's default for a body that declares nothing is still one
	// column — what shipped before Spec.ColumnGap existed. ZoneGap is the
	// measure the six multi-column screens actually spend.
	ZoneGap = 2
)
