package screenlayout

import (
	"omakiten/internal/tui/components/screenkit"
)

// measured is everything the resolver learns about one section BEFORE it
// assembles the lines that section paints: the geometry it was handed, the
// chrome and items it produced, the terminal rows each item occupies, and the
// cursor and offset those measurements settle to.
//
// It is split out of [resolved] because it is exactly the half a KEYSTROKE
// needs. A key moves a cursor and an offset; it never needs the strings. So the
// measurement is the part worth carrying across calls, and [frame] carries it.
type measured struct {
	spec        Spec
	width, rows int
	dropped     bool
	header      []string
	footer      []string
	// raw are the section's items exactly as the Block handed them over, COPIED
	// into a slice this package owns. They are the key of the wrapped items and
	// heights beside them: an item that is the same string at the same width
	// wraps to the same rows, so a resolve handed a string it has already
	// measured does not measure it twice.
	//
	// The copy is what makes the comparison total. A section that rebuilt its
	// items in place would otherwise compare its NEW string against itself and
	// be handed the OLD height; against a snapshot, a changed item is a changed
	// string, and strings are values.
	raw []string
	// items are raw wrapped to width, and heights the terminal rows each one
	// then occupies. Item i occupies heights[i] rows.
	items   []string
	heights []int
	// declaredBad records that Block.Heights disagreed with the measurement.
	declaredBad bool
	// perRow is the section's normalised [Block.PerRow] — how many items share
	// one terminal line. Always at least 1, where a line IS an item and nothing
	// below is populated.
	perRow int
	// lineItems are items joined PerRow to a line, and lineHeights the terminal
	// rows each joined line occupies. Nil for perRow == 1. They are what the
	// WINDOW slides over; items and heights stay the unit the cursor moves in.
	// See perrow.go.
	//
	// Spelled lineItems rather than lines because [resolved] declares a `lines`
	// of its own — the section's assembled output — and a promoted field that
	// silently loses to a shadow is exactly the confusion this fold introduces.
	lineItems   []string
	lineHeights []int
	// itemViewport is the rows left for the item window once chrome is paid.
	itemViewport int
	// declared is the Block's own statement about the cursor, kept so a
	// keystroke can settle a section's pair WITHOUT rendering it again.
	declared Cursor
	// selectable is the section's normalised [Block.Selectable] mask, or nil
	// for a section whose every item is a landing place. Carried for the same
	// reason declared is: a keystroke steps within it, and stepping must not
	// cost a render.
	selectable []int
	// cursorRead records that the body called [Canvas.Cursor]. It is the
	// witness that a body's output DEPENDS on the selection, which is what
	// decides whether this measurement survives a move of that selection.
	cursorRead bool
	cursor     int
	offset     int
	// stale marks a measurement whose body read the cursor and whose cursor has
	// moved since it was taken. Such a body may render differently for another
	// selection, so the measurement is retaken before it is trusted again — see
	// [frame.settledBy]. A measurement just taken is never stale.
	stale bool
}

// cursorUnread is the arranger holding a selection for a section that never
// asked for one. See [Placement.CursorUnread].
func (m measured) cursorUnread() bool {
	return m.cursor >= 0 && !m.cursorRead && m.declared.mode == cursorInherit
}

// frame is the measurement of one whole body, kept in [State] so the keystroke
// after it does not have to take the measurement again.
//
// # Why it is safe to carry, and what carrying it may and may not decide
//
// A frame is FROZEN. Nothing writes through the pointer once it is built: every
// update ([frame.with], [frame.settledBy]) returns a new frame over a new
// slice. That is what keeps [State] a value type in substance as well as in
// form — two screen values may share a frame, and neither can make the other
// observe a change, because there is no change to observe. It is the memo
// #2424 rejected a `*store` for, without the mutation that rejection was about.
//
// It is also deliberately NOT consulted by [Arrange]. What gets painted is
// always freshly rendered and freshly measured, so nothing a frame remembers
// can reach the screen. The one thing a frame decides is where a keystroke
// settles a cursor — and a keystroke that settles against a measurement the
// next paint would retake is the case this package already handles and already
// documents: [Arrange] re-resolves and re-clamps whatever it is handed and
// cannot overdraw. The worst a carried frame can cost is the same one-frame
// correction a resize the Update path has not seen already costs.
//
// What it witnesses is therefore what a keystroke's arithmetic reads: the
// available width and the row budget the geometry pass runs on, and every
// section's normalised [Spec], compared by value, in declaration order. A frame
// that does not witness the present inputs is not consulted — [State.frameFor]
// takes a fresh one. And every write to the state a frame describes drops it:
// [State.mutate] clears the field, so only the two calls that just resolved the
// body — [State.HandleKey] and [State.Resync] — can attach one, and only
// alongside the very pairs it measured.
type frame struct {
	box      Box
	sections []measured
}

// newFrame is the measurement half of a resolve that has just been taken.
func newFrame(box Box, laid []resolved) *frame {
	out := &frame{box: box, sections: make([]measured, len(laid))}
	for i, r := range laid {
		out.sections[i] = r.measured
	}
	return out
}

// fits reports whether this frame describes the body these specs would lay out
// at this geometry. Nil-safe, so the zero [State] simply misses.
//
// Specs are compared with == , field for field, rather than by a fingerprint: a
// Spec holds nothing but numbers, strings and flags, so equality is total and a
// hash could only make it less so.
func (f *frame) fits(specs []Spec, box Box) bool {
	if f == nil || f.box != box || len(f.sections) != len(specs) {
		return false
	}
	for i, spec := range specs {
		if f.sections[i].spec != spec {
			return false
		}
	}
	return true
}

// at is the i-th section's measurement, or nil when there is no frame to read.
func (f *frame) at(i int) *measured {
	if f == nil || i < 0 || i >= len(f.sections) {
		return nil
	}
	return &f.sections[i]
}

// with is this frame with one section's measurement replaced, as a NEW frame.
func (f *frame) with(i int, m measured) *frame {
	out := &frame{box: f.box, sections: append([]measured(nil), f.sections...)}
	out.sections[i] = m
	return out
}

// settledBy is this frame carrying the cursors and offsets the state now holds,
// as a NEW frame — so the frame and the state it is attached to always describe
// the same pairs, and the next keystroke's absorb is a statement of fact rather
// than a second opinion.
//
// It is also where staleness is declared: a section whose body READ the cursor
// and whose cursor has just moved may render differently for the new selection,
// so its measurement is marked for retaking. A body that never asked for the
// cursor cannot render differently because of it, and keeps its measurement.
func (f *frame) settledBy(s State) *frame {
	out := &frame{box: f.box, sections: append([]measured(nil), f.sections...)}
	for i := range out.sections {
		m := &out.sections[i]
		if m.dropped {
			continue
		}
		cursor := s.Cursor(m.spec.ID)
		if m.cursorRead && cursor != m.cursor {
			m.stale = true
		}
		m.cursor, m.offset = cursor, s.Offset(m.spec.ID)
	}
	return out
}

// reusable is the per-section measurement a fresh resolve may carry over, or
// nil. It is the same witness [frame.fits] applies, asked once for the whole
// body so the resolver does not ask it per section.
func (f *frame) reusable(specs []Spec, box Box) *frame {
	if !f.fits(specs, box) {
		return nil
	}
	return f
}

// normalized is the specs a set of sections declares, in declaration order.
func normalized(sections []Section) []Spec {
	specs := make([]Spec, len(sections))
	for i, s := range sections {
		specs[i] = s.Spec().normalize()
	}
	return specs
}

// frameFor is the measurement this keystroke moves within: the one the last
// resolve took, when every input this package can observe still describes it,
// and a fresh full resolve otherwise.
//
// A miss is the behaviour that shipped — one full resolve per keystroke — so
// the first key after a resize, after a screen moved a cursor itself, or after
// a screen declared different sections re-measures everything and every
// section's offset self-corrects, exactly as it did before this frame existed.
func (s State) frameFor(kit screenkit.Kit, box Box, sections []Section) *frame {
	specs := normalized(sections)
	if s.frame.fits(specs, box) {
		return s.frame
	}
	laid, _ := resolveSpecs(kit, box, s, sections, specs)
	return newFrame(box, laid)
}

// remember attaches a frame to the state that frame describes.
//
// It is unexported and called from exactly two places, both of which have just
// written the pairs the frame carries. There is no way for a caller outside
// this package to attach a measurement to a state it does not describe.
func (s State) remember(f *frame) State {
	s.frame = f
	return s
}
