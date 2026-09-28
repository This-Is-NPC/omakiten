// Package screenbody stores composed content and delegates geometry and scrolling to screenlayout.
package screenbody

import (
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

// Body owns the prepared section and its scroll state. Methods return a new value.
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

// requireNamedSpec rejects sections without a focus identity.
func requireNamedSpec(spec screenlayout.Spec) {
	if spec.ID == "" {
		panic("screenbody: section has no Spec.ID")
	}
}

// New returns an empty single-zone body. The caller supplies the section Spec
// rather than receiving an invented one: MinRows, Weight and Scroll are the
// screen's to state, and a body that fixed them could not host a zone that
// needed different ones. split is optional.
func New(spec screenlayout.Spec, split Split) Body {
	requireNamedSpec(spec)
	return Body{zones: []zone{{spec: spec, split: split}}, layout: screenlayout.NewState()}
}

// Spec declares a scrolling content section with the default row allocation.
func Spec(id screenlayout.ID) screenlayout.Spec {
	return screenlayout.Spec{ID: id, MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems}
}

// id is the section's focus target.
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

// Compose prepares content at the supplied width and resyncs its scroll state.
func (b Body) Compose(kit screenkit.Kit, box screenlayout.Box, compose Compose) Body {
	b.width = box.Width
	b.zones = b.withZones(func(z zone) zone {
		z.lines = nil
		if compose != nil {
			z.lines = screenkit.CapRows(compose(box), b.width)
		}
		return z
	})
	return b.Resync(kit, box)
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

// sections mounts the prepared content in the arranger.
func (b Body) sections() []screenlayout.Section {
	if len(b.zones) == 0 {
		return nil
	}
	return []screenlayout.Section{screenlayout.Func{
		Def:  b.zones[0].spec,
		Body: func(canvas screenlayout.Canvas) screenlayout.Block { return b.block(canvas, b.zones[0]) },
	}}
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
		// Preserve the section's item selection.
		Cursor: screenlayout.At(b.layout.Cursor(z.spec.ID)),
	}
}
