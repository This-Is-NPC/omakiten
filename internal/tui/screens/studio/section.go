package studio

import (
	"strings"

	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// studioBody accumulates a Studio sub-screen body as the ITEMS the arranger
// windows — one item per WRAPPED terminal row — and records the row the cursor
// anchor landed on AS IT IS APPENDED.
//
// # Why the items are wrapped rows and not source lines
//
// A Studio body is prose and tables, not cards: there is no selectable unit
// bigger than a line, and the "▲ N above" / "▼ N below" counts a user reads have
// always counted the rows they can see. Handing the arranger source lines and
// letting it wrap them would keep the bytes but change every one of those counts
// wherever a line is wider than the panel — which at the recorded geometries is
// guards and commands at 80 columns and the whole prompt preview at 80 and 120.
//
// # Why the anchor is len(lines) and not a formula
//
// This is the last piece of the defect #2416, #2417 and #2418 each closed one
// face of. The cursor used to be located by grepping the rendered text for the
// marker glyph, then by counting the newlines of every block above it, then by
// mapping a source-line index through a wrapped-line offset table. All three are
// re-derivations of a position the renderer already knew, and each was right
// until a block above it changed height.
//
// A builder has no such problem, because the renderer states the anchor at the
// only moment it is not a derivation: the instant it appends the line. There is
// no offset table, no block arithmetic and no second coordinate space to keep in
// step, and there is nothing left for a later edit to invalidate.
type studioBody struct {
	width int
	lines []string
	// sources is the number of SOURCE lines pushed. It differs from len(lines)
	// exactly when something had to be folded, which is the property #2418 left
	// behind: Overview, Buckets, Flow and Guards compose through gridtable,
	// which already fits every cell to its column, so wrapping is the identity
	// on them and their anchors cannot move because of it.
	sources int
	anchor  int
}

// newStudioBody opens a body that will wrap to the width the arranger gave the
// section. A renderer never computes that width; it is handed one.
func newStudioBody(width int) *studioBody {
	return &studioBody{width: width, anchor: -1}
}

// Break separates one block from the next with a single blank row — the row
// `strings.Join(parts, "\n\n")` used to contribute. It is a no-op at the head of
// a body, so a leading Break cannot open with an empty row.
func (b *studioBody) Break() {
	if len(b.lines) > 0 {
		b.push("")
	}
}

// Line appends one source line.
func (b *studioBody) Line(line string) { b.push(line) }

// Selected appends the source line the cursor is on and records it as the
// anchor. The recorded index is the FIRST row the line wraps to, taken before
// the append, so a wrapped anchor points at the top of what it anchors.
func (b *studioBody) Selected(line string) {
	b.anchor = len(b.lines)
	b.push(line)
}

// Block appends a rendered multi-line block that carries no selection.
func (b *studioBody) Block(block string) {
	for _, line := range strings.Split(block, "\n") {
		b.push(line)
	}
}

// BlockSelecting appends a rendered block whose renderer reported which of its
// OWN lines carries the selection, and -1 when none does.
//
// It is what a block-at-a-time renderer uses instead of Selected: gridtable
// reports its row offsets through [gridtable.Layout.RowLine] because row heights
// depend on how the column widths wrap each cell, so the offset is the table's
// to state and nobody else's to guess (#2416).
func (b *studioBody) BlockSelecting(block string, at int) {
	for i, line := range strings.Split(block, "\n") {
		if i == at {
			b.Selected(line)
			continue
		}
		b.push(line)
	}
}

// push folds one source line to the section's width and appends every row it
// occupies. Same wrapping engine the arranger measures with, at the width the
// arranger handed the section — so an item is never re-wrapped and never
// disagrees with the row it is charged for.
func (b *studioBody) push(line string) {
	b.sources++
	b.lines = append(b.lines, gridtable.WrapLines([]string{line}, b.width)...)
}

// Wrapped reports whether any source line had to be folded to fit the section
// width — whether one source line ever became more than one item.
func (b *studioBody) Wrapped() bool { return len(b.lines) != b.sources }

// studioSectionWidth asks the arranger what the body section is worth at this
// geometry, without rendering anything.
//
// It runs the same geometry pass Arrange runs, so the width the frame memo is
// keyed on is the width the frame is painted at. A screen that re-derived it
// from the terminal would be writing the private width copy this whole migration
// exists to delete — and if the two ever did disagree the memo simply misses and
// re-renders, so the failure mode is a wasted render, never wrong bytes.
func (s Screen) studioSectionWidth() int {
	widths := screenlayout.Widths(s.kit, screenlayout.Func{
		Def: screenlayout.Spec{MinRows: 1, Scroll: screenlayout.ScrollItems},
	})
	if len(widths) == 0 {
		return 0
	}
	return widths[0]
}

// renderStudioBody builds the active sub-screen's body at the width the
// arranger gave the section.
//
// It replaced studioBodyAndCursor, which handed back a body STRING and a line
// NUMBER for the caller to slice — the two halves of a viewport the screen had
// no business owning. What comes back now is a builder holding items and the
// anchor the renderer stated; nothing here knows the row budget, the offset or
// the visible range.
func (s Screen) renderStudioBody(width int) *studioBody {
	return newStudioBody(width)
}

// renderStudioFrame ALWAYS renders. Never route the update path through the
// memo: a draft mutation can leave every field of studioFrameKey untouched
// (press `a` twice on Flow and only the candidate behind the pointer moves), so
// a lookup here would hand the pre-mutation body straight back to itself.
func (s Screen) renderStudioFrame() studioFrame {
	return s.renderStudioFrameWithKey(s.studioFrameKey())
}

func (s Screen) renderStudioFrameWithKey(key studioFrameKey) studioFrame {
	body := s.renderStudioBody(key.width)
	return studioFrame{ready: true, key: key, items: body.lines, anchor: body.anchor}
}

// studioOwnKeys is what each sub-screen handles for ITSELF. Everything else it
// owns belongs to the arranger.
//
// Written this way round on purpose. The old code listed the scroll keys at
// every dispatch site and again in OwnsKey, and Flow's two lists disagreed —
// eight keys its handler accepted were never routed to it. A screen that
// enumerates only its own vocabulary cannot fall behind
// [screenlayout.StandardBindings], because it never restates it.
//
// The overlaps are the point. `g` is the arranger's jump-to-top everywhere
// except Commands, where it toggles a global law; up/down/j/k are the
// screen's own cursor keys on every remaining Studio sub.
var studioOwnKeys = map[screenhost.ID][]string{
	screenhost.StudioWorkflow: studioOwn("up", "down", "j", "k", "a", "d", "[", "]", "K", "e", "X", "C", "ctrl+s"),
	screenhost.StudioCommands: studioOwn("up", "down", "j", "k", "p", "s", "g", "w", "x", "t", "d", "ctrl+s"),
	screenhost.StudioPersonas: studioOwn("up", "down", "j", "k", "enter", "ctrl+s"),
	screenhost.StudioHooks:    studioOwn("up", "down", "j", "k", "ctrl+s"),
}

// studioOwn is the sub-screen's own keys plus the zone-advance spellings
// from the shared vocabulary. shift+tab is tops, not a Studio key.
func studioOwn(keys ...string) []string {
	zones := keynav.Default.Zones.CloneKeys()
	out := make([]string, 0, len(keys)+len(zones))
	out = append(out, keys...)
	out = append(out, zones...)
	return out
}

// studioHorizontalNavKey is ←→ / h/l. Studio never spends them: tab advances
// list/inspector/preview and j/k move the focused column. Swallowing them here keeps
// the grid from treating them as lane slides if a caller drives Update
// without going through OwnsKey.
func studioHorizontalNavKey(key string) bool {
	return studioKeyIn(key, "left", "right", "h", "l")
}

// studioScrollKeys is every spelling the arranger will consume on a Studio
// body, read off the standard table rather than restated.
//
// Section cycling is excluded because Studio declares ONE section: the arranger
// answers `handled=false` for tab with nothing to cycle to, and the host needs
// tab for the sub-tab zones. Excluding it here keeps OwnsKey from claiming a key
// the screen would then swallow.
func studioScrollKeys() []string {
	var keys []string
	for _, binding := range screenlayout.StandardBindings() {
		if binding.Action == screenlayout.ActionNextSection || binding.Action == screenlayout.ActionPrevSection {
			continue
		}
		keys = append(keys, binding.Keys...)
	}
	return keys
}
