// Package screenbody is the pushed-body contract: a screen composes when the
// payload or the geometry changes, and View only slices. It lives here rather
// than in screenhost because it is an implementation a screen embeds, not a
// capability the host type-asserts — screenhost stays identity and outcomes.
//
// The screen's only body-path function is the compose hook the base calls on
// Enter/Resize. The Body: callback that slices stored lines lives here, so a
// screen using this package has no slot in which to compose on paint.
//
// # State ownership: ONE Body serves N sections
//
// A multi-zone screen holds one [Body] with N zones. It does NOT hold N Bodies,
// and that is the arranger's requirement rather than a preference here.
//
// [screenlayout.Arrange] takes ONE state and N sections. A [screenlayout.State]
// carries a SINGLE focus ID and a single frame — the measurement of the one
// resolve that produced the cursors and offsets stored beside it. So two Bodies
// each holding their own State cannot be arranged together: there would be two
// focuses with no tiebreak, and each frame would describe a resolve the other
// zone never took part in.
//
// The consequence for callers: per-zone geometry travels in each zone's
// [screenlayout.Spec] — MinRows, Weight and Scroll may all differ — while the
// cursor that moves between zones is single-valued and lives on the Body. A
// screen that wants two independently-scrolling zones is asking for two
// focuses, which this contract cannot express and the arranger cannot either.
package screenbody

import (
	"fmt"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// Compose is the only hook a screen implements for the body path: geometry in,
// lines out. The base calls it from Lifecycle. View never receives it.
//
// The argument is the box the section will receive, not a Canvas: NewCanvas is
// a test seam, and production code under internal/tui must not construct one.
// Body: still receives the arranger's Canvas and only slices.
type Compose func(box screenlayout.Box) []string

// Split turns stored lines into a header that does not scroll and items that
// do. Nil means every line is an item. It is a constructor option, not a Body:
// the screen writes — the base applies it while slicing.
type Split func(lines []string) (header, items []string)

// Body holds the lines a screen composed and the width they were composed at.
// Apply invalidates; Lifecycle Enter/Resize composes; View slices.
//
// # State ownership — one Body serves N sections
//
// The [screenlayout.State] here is the state for EVERY zone this Body holds,
// not one zone's share of it, and that is forced by the arranger rather than
// chosen. [screenlayout.Arrange] takes ONE state and N sections; a State
// carries a SINGLE focus ID and a single frame — the measurement of the one
// resolve that produced its cursors and offsets. So two Bodies, each with their
// own State, cannot be arranged together: there would be two focuses and the
// arranger has no way to choose between them, and each frame would describe a
// resolve the other did not take part in.
//
// A multi-zone screen therefore holds ONE Body with N zones, not N Bodies. The
// per-zone geometry lives in each zone's [screenlayout.Spec]; the cross-zone
// cursor lives once, here.
type Body struct {
	zones  []zone
	layout screenlayout.State
	width  int
}

// zone is one section this Body contributes to the arrangement: its geometry,
// how its lines are split, and the lines themselves. Lines are per zone because
// each zone composes its own; width is not, because every zone is resolved
// against the same box.
type zone struct {
	spec  screenlayout.Spec
	split Split
	lines []string
}

// Zone is one zone's declaration at construction time. Its fields are
// UNEXPORTED and it is built through [NewZone], which is not ceremony: when
// Spec was a plain optional field, Zone{Split: f} compiled with the Spec
// omitted and produced a zone that took the whole box and answered no key —
// measured at 20 of 20 rows with all eight scroll keys returning false. An
// omitted Spec is a zero Spec, and a zero Spec is loud and unreachable rather
// than obviously broken, so the compiler refuses the shape instead.
type Zone struct {
	spec  screenlayout.Spec
	split Split
}

// NewZone declares one zone. The Spec is positional because it is not
// optional — see [Zone].
func NewZone(spec screenlayout.Spec, split Split) Zone {
	return Zone{spec: spec, split: split}
}

// NewZones returns an empty body holding one section per zone, in order. The
// zones share this Body's single [screenlayout.State] — see the type comment
// for why that is the arranger's requirement rather than a convenience.
//
// A zone whose Spec has no ID is a construction error and panics. That is the
// residual [NewZone] cannot catch: a bare Zone{} still compiles inside this
// package's own callers, and the ID is the one field that is never meaningfully
// empty — every section is keyed by it, and it is the Body's only focus target.
// Deliberate geometry is untouched: a zone may still choose Weight 0 or
// ScrollNone, which are real choices; what it may not do is arrive without a
// name because someone omitted the whole Spec.
func NewZones(zones ...Zone) Body {
	out := make([]zone, len(zones))
	for i, z := range zones {
		requireNamedSpec(z.spec, i)
		out[i] = zone{spec: z.spec, split: z.split}
	}
	return Body{zones: out, layout: screenlayout.NewState()}
}

// requireNamedSpec is the ONE check, called from EVERY door that builds a zone
// — [New] and [NewZones] both, because they are separate doors to the same
// place and closing one leaves the other open.
//
// It rejects the VALUE, not the syntax. Unexporting Zone's fields stops
// Zone{Split: f}, but New(screenlayout.Spec{}, nil) states the field and
// arrives at the identical zero Spec; a remedy aimed at omission would close
// the door it happened to name and leave that one standing.
//
// It checks IDENTITY, not geometry. An empty ID is not one bad field among
// three: it is the Body's only focus target being empty, so the zone is never
// a focus candidate and answers no key while taking its rows. Weight and
// MinRows are deliberate choices a zone may legitimately make; a name is not.
func requireNamedSpec(spec screenlayout.Spec, index int) {
	if spec.ID != "" {
		return
	}
	panic(fmt.Sprintf("screenbody: zone %d has no Spec.ID. A zone with a zero Spec takes its rows "+
		"and answers no key, because ScrollNone is the zero ScrollPolicy and an empty ID is never a "+
		"focus candidate. Use screenbody.Spec(id) for the pre-existing geometry, or state an ID.", index))
}

// New returns an empty single-zone body. The caller supplies the section Spec
// rather than receiving an invented one: MinRows, Weight and Scroll are the
// screen's to state, and a body that fixed them could not host a zone that
// needed different ones. split is optional.
func New(spec screenlayout.Spec, split Split) Body {
	requireNamedSpec(spec, 0)
	return Body{zones: []zone{{spec: spec, split: split}}, layout: screenlayout.NewState()}
}

// Spec is the Spec [New] used before it accepted one, kept so a caller that
// wants the historical single-zone geometry says so instead of restating three
// literals — and so that a change to the default is a change to one line here
// rather than to every call site that copied it.
func Spec(id screenlayout.ID) screenlayout.Spec {
	return screenlayout.Spec{ID: id, MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems}
}

// id is the focus target: the first zone's section. Focus is single-valued on
// State, so a multi-zone Body focuses its first zone until a caller moves it.
func (b Body) id() screenlayout.ID {
	if len(b.zones) == 0 {
		return ""
	}
	return b.zones[0].spec.ID
}

// Offset is the arranger's item offset for this body's focused zone.
func (b Body) Offset() int { return b.layout.Offset(b.id()) }

// Width is the canvas width the stored lines were composed at.
func (b Body) Width() int { return b.width }

// Invalidate drops every zone's stored lines so the next Compose rebuilds them.
func (b Body) Invalidate() Body {
	b.zones = b.withZones(func(z zone) zone { z.lines = nil; return z })
	b.width = 0
	return b
}

// withZones copies the zone slice with fn applied. The copy is what keeps a
// Body a value: two Bodies sharing a backing array would leak one's lines into
// the other, and every method here returns a new Body by contract.
func (b Body) withZones(fn func(zone) zone) []zone {
	out := make([]zone, len(b.zones))
	for i, z := range b.zones {
		out[i] = fn(z)
	}
	return out
}

// ResetWindow clears scroll and focus without touching the stored lines.
func (b Body) ResetWindow() Body {
	b.layout = screenlayout.NewState()
	return b
}

// WithCursor seeds a zone's item selection before the next arrange or key.
// The request is clamped against the composed items by Resync, matching the
// underlying screenlayout.State contract.
func (b Body) WithCursor(id screenlayout.ID, index int) Body {
	b.layout = b.layout.WithCursor(id, index)
	return b
}

// Compose stores compose(box) for the FIRST zone, recaps to that width, and
// resyncs the window. It is ComposeZones for the single-zone case, which is
// every screen that has adopted this contract so far.
func (b Body) Compose(kit screenkit.Kit, box screenlayout.Box, compose Compose) Body {
	return b.ComposeZones(kit, box, compose)
}

// ComposeZones stores one composed line-set per zone, in zone order, and
// resyncs the window once against all of them. A nil composer, or a zone with
// no composer supplied, stores no lines for that zone — the same meaning a nil
// Compose has always had, per zone instead of per body.
func (b Body) ComposeZones(kit screenkit.Kit, box screenlayout.Box, composers ...Compose) Body {
	b.width = box.Width
	b.zones = b.withZonesIndexed(func(i int, z zone) zone {
		if i >= len(composers) || composers[i] == nil {
			z.lines = nil
			return z
		}
		z.lines = screenkit.CapRows(composers[i](box), b.width)
		return z
	})
	return b.Resync(kit, box)
}

func (b Body) withZonesIndexed(fn func(int, zone) zone) []zone {
	out := make([]zone, len(b.zones))
	for i, z := range b.zones {
		out[i] = fn(i, z)
	}
	return out
}

// ComposeOn composes for Enter and Resize and is a no-op for every other event.
func (b Body) ComposeOn(event screenhost.LifecycleEvent, kit screenkit.Kit, box screenlayout.Box, compose Compose) Body {
	switch event {
	case screenhost.LifecycleEnter, screenhost.LifecycleResize:
		return b.Compose(kit, box, compose)
	}
	return b
}

// View paints the stored lines inside box. It does not compose.
func (b Body) View(kit screenkit.Kit, box screenlayout.Box) string {
	return b.Arrange(kit, box).View
}

// HostView paints the stored lines in the host body box. It does not compose.
func (b Body) HostView(kit screenkit.Kit) string {
	return b.ArrangeHost(kit).View
}

// Arrange is ArrangeIn against the stored section. Tests that need Placement
// use this; screens use View.
func (b Body) Arrange(kit screenkit.Kit, box screenlayout.Box) screenlayout.Result {
	return screenlayout.ArrangeIn(kit, box, b.layout, b.sections()...)
}

// ArrangeHost is Arrange against the stored sections.
func (b Body) ArrangeHost(kit screenkit.Kit) screenlayout.Result {
	return screenlayout.Arrange(kit, b.layout, b.sections()...)
}

// HandleKey routes a scrolling keystroke through the stored sections. Every
// zone is offered, because the arranger decides from the focus which one the
// key belongs to — the Body does not pre-select.
func (b Body) HandleKey(kit screenkit.Kit, box screenlayout.Box, key string) (Body, bool) {
	next, handled := b.layout.HandleKeyIn(kit, box, key, b.sections()...)
	if handled {
		b.layout = next
	}
	return b, handled
}

// Resync re-clamps the window against the stored lines at this box.
func (b Body) Resync(kit screenkit.Kit, box screenlayout.Box) Body {
	b.layout = b.layout.WithFocus(b.id()).ResyncIn(kit, box, b.sections()...)
	return b
}

// AfterApply is the host half of the contract. Apply has no kit, so the host
// issues LifecycleResize on the stored instance before the next View. Call it
// once at the Apply site; do not copy it into each screen.
func AfterApply(screen screenhost.Screen, frame screenhost.Frame) screenhost.Screen {
	return screen.Lifecycle(frame, screenhost.LifecycleResize).Screen
}

// sections is one Section per zone, in declaration order. The Spec is the
// caller's — this is the line that used to invent MinRows, Weight and Scroll,
// and inventing them is what stopped a zone from differing from its neighbour.
func (b Body) sections() []screenlayout.Section {
	// The single-zone path is kept allocation-identical to the shape that
	// preceded zones, and that is a keystroke-budget requirement rather than a
	// tidiness one. sections is reached four times per keystroke — Arrange,
	// ArrangeHost, HandleKey and Resync — so a closure that captures anything
	// beyond the receiver escapes once per call and the budget moves. Capturing
	// the zone VALUE cost four allocations a keystroke on both migrated screens
	// and the gate caught it; indexing through the already-captured receiver
	// costs none.
	if len(b.zones) == 1 {
		return []screenlayout.Section{screenlayout.Func{
			Def: b.zones[0].spec,
			Body: func(canvas screenlayout.Canvas) screenlayout.Block {
				return b.block(canvas, b.zones[0])
			},
		}}
	}
	out := make([]screenlayout.Section, len(b.zones))
	for i := range b.zones {
		out[i] = screenlayout.Func{
			Def: b.zones[i].spec,
			// i is indexed through the receiver rather than captured as a zone
			// value. Go 1.22 gives a per-iteration i, so this closure reads the
			// zone it was built for.
			Body: func(canvas screenlayout.Canvas) screenlayout.Block {
				return b.block(canvas, b.zones[i])
			},
		}
	}
	return out
}

func (b Body) block(canvas screenlayout.Canvas, z zone) screenlayout.Block {
	width := canvas.Width()
	lines := z.lines
	if width != b.width {
		lines = screenkit.CapRows(lines, width)
	}
	header, items := []string(nil), lines
	if z.split != nil {
		header, items = z.split(lines)
	}
	if len(lines) == 0 {
		return screenlayout.Block{Cursor: screenlayout.At(b.layout.Cursor(z.spec.ID))}
	}
	return screenlayout.Block{
		Header: header,
		Items:  items,
		// Carry the current item selection through the Body mount. The old
		// NoSelection override discarded every cursor after Resync.
		Cursor: screenlayout.At(b.layout.Cursor(z.spec.ID)),
	}
}
