package screenlayout

import (
	"omakiten/internal/tui/components/screenkit"
)

// State is the home of every per-section cursor and scroll offset.
//
// # Why it exists at all
//
// Screens are Elm value types — `func (s Screen) View(frame) string` — so
// nothing a View computes can survive the frame. An arranger that returned only
// a string would have nowhere to keep an offset, and every screen would grow
// the `fooScroll int` / `fooCursor int` pair that cardlist and linelist were
// built to abolish, one pair per section, with a hand-rolled sync beside it.
//
// So the offsets live here, in a value the screen embeds and carries. The
// fields are unexported for the same reason cardlist's scroll field is: a
// screen that cannot write an offset cannot write one in the wrong unit.
//
// # Wiring
//
// Three calls, and a screen makes all three or the compiler and the tests
// notice:
//
//	Update, on a key:     s.layout, handled = s.layout.HandleKey(kit, key, s.sections()...)
//	Update, on a resize:  s.layout = s.layout.Resync(kit, s.sections()...)
//	View:                 return screenlayout.Arrange(kit, s.layout, s.sections()...).View
//
// HandleKey resyncs before it applies anything, so a stale offset — the frame
// after a resize that the Update path has not seen — self-corrects on the next
// keystroke rather than compounding. Resync makes that correction eagerly so the
// "▲ N above" counts are right on the very first frame at the new size. Arrange
// clamps for the frame it paints without persisting, so even a screen that
// wires neither of the first two cannot overdraw; it just scrolls stiffly.
//
// # Value semantics
//
// Every mutator returns a new State and the backing slice is copied on write,
// so an older screen value never observes a newer offset. That matters because
// the host holds screen values across an Update that may be discarded.
//
// The measurement a keystroke moves within is carried the same way, and holds
// to the same rule: [frame] is frozen once built, every update returns a new
// one, and [State.mutate] drops the field outright. Two screen values may share
// a frame and neither can make the other observe a change, because nothing ever
// writes one. See [frame] for what it may and may not decide.
type State struct {
	entries []entry
	focus   ID
	// frame is the measurement of the body the last resolve took, or nil. Only
	// [State.HandleKey] and [State.Resync] may attach one, and only alongside
	// the very cursors and offsets it measured.
	frame *frame
}

type entry struct {
	id     ID
	cursor int
	offset int
}

// NewState is an empty layout state: no section has been scrolled, no section
// holds focus yet, and every cursor is the no-selection sentinel until a
// section declares SelectFirst or a key moves it.
func NewState() State { return State{} }

// Focus is the section the standard keys are routed to, or "" when nothing has
// been focused yet — in which case [State.HandleKey] routes to the first
// scrollable section, so a screen never has to initialise it.
func (s State) Focus() ID { return s.focus }

// WithFocus points the standard keys at a section.
func (s State) WithFocus(id ID) State {
	s.focus = id
	return s
}

// Cursor is the stored ITEM index for a section, or -1 for no selection. It is
// an item index at every layer of this package; there is no line index to
// confuse it with.
func (s State) Cursor(id ID) int {
	if e, ok := s.find(id); ok {
		return e.cursor
	}
	return -1
}

// Offset is the stored scroll offset for a section, also an ITEM index.
func (s State) Offset(id ID) int {
	if e, ok := s.find(id); ok {
		return e.offset
	}
	return 0
}

// WithCursor points a section's cursor at an item. The value is a request: the
// next Resync, HandleKey or Arrange clamps it against the items that actually
// exist, so a caller may pass a stale index after a refresh shrank the list.
func (s State) WithCursor(id ID, index int) State {
	return s.mutate(id, func(e *entry) { e.cursor = index })
}

// Resync re-clamps every section's cursor and offset against the CURRENT
// geometry and item set, and persists the result. Call it from Update on a
// resize, and after any refresh that changed a section's items.
//
// It runs the same resolver Arrange runs, so the clamp it persists is the clamp
// the next frame would have applied anyway — there is one implementation of
// "where does this cursor land", not one for painting and one for storing.
//
// It is also the package's REFRESH: it always resolves, never consults the
// measurement it is replacing, and attaches the fresh one. A screen that calls
// it after a refresh changed a section's items — which is what the wiring above
// asks for — hands the next keystroke a measurement of the items that now
// exist.
func (s State) Resync(kit screenkit.Kit, sections ...Section) State {
	return s.ResyncIn(kit, HostBox(kit), sections...)
}

// ResyncIn is [State.Resync] for a caller that already has its box — the same
// reason [ArrangeIn] exists.
func (s State) ResyncIn(kit screenkit.Kit, box Box, sections ...Section) State {
	laid, _ := resolveAll(kit, box, s, sections)
	out := s.absorb(measuresOf(laid))
	return out.remember(newFrame(box, laid))
}

// measuresOf is the measurement half of a resolve.
func measuresOf(laid []resolved) []measured {
	out := make([]measured, len(laid))
	for i, r := range laid {
		out[i] = r.measured
	}
	return out
}

// absorb writes the resolver's clamped cursors and offsets back into the state.
func (s State) absorb(sections []measured) State {
	out := s
	for _, m := range sections {
		if m.dropped {
			continue
		}
		out = out.mutate(m.spec.ID, func(e *entry) {
			e.cursor = m.cursor
			e.offset = m.offset
		})
	}
	return out
}

func (s State) find(id ID) (entry, bool) {
	for _, e := range s.entries {
		if e.id == id {
			return e, true
		}
	}
	return entry{}, false
}

// mutate copies the entry slice before writing, so a State handed to an older
// screen value never observes a later mutation.
//
// It also DROPS the carried measurement, which is what makes staleness a matter
// of construction rather than of discipline: a measurement describes the pairs
// it was taken with, and the moment one of those pairs is written it no longer
// does. Only [State.HandleKey] and [State.Resync] attach one back, at the end,
// out of a resolve they have just taken.
func (s State) mutate(id ID, apply func(*entry)) State {
	s.frame = nil
	next := make([]entry, len(s.entries), len(s.entries)+1)
	copy(next, s.entries)
	for i := range next {
		if next[i].id == id {
			apply(&next[i])
			s.entries = next
			return s
		}
	}
	fresh := entry{id: id, cursor: -1}
	apply(&fresh)
	s.entries = append(next, fresh)
	return s
}
