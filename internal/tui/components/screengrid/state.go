package screengrid

import (
	"omakiten/internal/tui/components/screenlayout"
)

// State is everything the grid persists between frames: the arranger's own
// per-leaf cursors and offsets, the horizontal offset of each windowed row of
// columns, and WHERE IN THE TREE the keyboard currently is.
//
// It is a VALUE, for the same reason [screenlayout.State] is: a screen holds it
// in a value-typed model, and two copies must not be able to observe each
// other's writes.
//
// # Focus is a path, not a place
//
// `tab` walks SIBLINGS and never descends. To get inside a container you enter
// it with `f`, and `esc` comes back out. That is one rule for the whole tool:
// the same three keys move between the gallery's columns and inside one of
// them, and between a body's zones and inside one of them.
//
// The alternative — a flat ring over every leaf in the tree — reads fine on a
// two-level body and falls apart on anything deeper: `tab` stops meaning "the
// next thing beside this" and starts meaning "the next thing somewhere", so the
// number of presses to reach a zone depends on how many children its unrelated
// neighbours happen to have.
type State struct {
	layout screenlayout.State
	lanes  []lane
	// path is the containers entered so far, outermost first. Empty is the body
	// itself, which is the level `tab` starts on.
	path []screenlayout.ID
	// full is the focused LEAF asking for the whole body. It is what `f` does
	// where there is nothing to descend into: a zone with no children cannot be
	// entered, so entering it means filling the screen with it.
	full bool
	// grid is the frozen arrangement metadata from the last composition. It is
	// checked against the current tree and state before a key reuses it.
	grid gridFrame
}

// lane is one windowed Cols node's horizontal offset — the index of its
// leftmost visible child.
type lane struct {
	id     screenlayout.ID
	offset int
}

// NewState is the zero grid state: nothing focused, every lane at its left edge.
func NewState() State { return State{layout: screenlayout.NewState()} }

// Layout is the arranger state underneath, for a caller that needs to read a
// leaf's cursor or offset directly.
func (s State) Layout() screenlayout.State { return s.layout }

// WithLayout replaces the arranger state, for a caller that drove the
// arranger itself — a screen migrating one zone at a time.
func (s State) WithLayout(layout screenlayout.State) State {
	s.layout = layout
	s.grid = gridFrame{}
	return s
}

// WithCursor points a leaf's cursor at an item WITHOUT discarding the frame
// memo, which is the whole difference between it and [State.WithLayout].
//
// A screen that seeds a selection — "open on the row the board was on", "jump
// to the activity entry that just arrived" — has to write one cursor. Spelled
// through WithLayout that write also throws away the arrangement metadata the
// last composition recorded, so the next keystroke re-renders the body to
// rediscover a frame that had not changed. Nothing about it had: a cursor is
// not one of the things [gridFrame.fits] asks about.
//
// # Why keeping the frame is safe
//
// fits compares the box, the focus, the path, the fullscreen flag, the windowed
// flag and the tree shape. The cursor lives in the carried [screenlayout.State]
// entries, and [screenlayout.State.WithCursor] writes nothing else — focus is a
// separate field, and path, full and lanes are this package's own. So no
// witness fits reads can move.
//
// Nor does the frame CACHE anything the cursor decides. The walker reads the
// state in four places — the focused child, the fullscreen branch, the lane
// offset, and the arranger call — and only the last of those sees the layout
// state at all. What the arranger returns from it is the view and the
// placements, neither of which a frame keeps: a frame holds the box, the
// section set, the child ids and the navigation ring of each level, all of them
// derived from the specs and the geometry.
//
// The write is not free of memoisation entirely, and that is the second half of
// why it is safe: screenlayout's own measurement is dropped by the write
// (State.mutate clears it), so the keystroke after this one takes a FRESH full
// resolve at the recorded box. The cursor a key settles against is therefore
// always measured under the cursor this wrote.
//
// This is also what [State.HandleKey] has always done. A motion key writes a new
// cursor into the layout and returns the state with its frame untouched, so a
// cursor write that keeps the frame is the established path, not a new one.
func (s State) WithCursor(id screenlayout.ID, index int) State {
	s.layout = s.layout.WithCursor(id, index)
	return s
}

// Focus is the node that currently holds focus — a leaf, or the container the
// keyboard is parked on before entering it. "" when nothing does.
func (s State) Focus() screenlayout.ID { return s.layout.Focus() }

// WithFocus points focus at a node WITHOUT changing the level. Entering and
// leaving are [State.Enter] and [State.Leave].
func (s State) WithFocus(id screenlayout.ID) State {
	s.layout = s.layout.WithFocus(id)
	return s
}

// WithFullscreen sets or clears the focused leaf's claim on the whole body.
func (s State) WithFullscreen(on bool) State {
	return s.withFullscreen(on)
}

// Path is the containers currently entered, outermost first. Empty is the body.
func (s State) Path() []screenlayout.ID { return append([]screenlayout.ID(nil), s.path...) }

// Fullscreen reports whether the focused leaf has been given the whole body.
func (s State) Fullscreen() bool { return s.full }

// enter descends into a container, focusing its first child.
func (s State) enter(id, child screenlayout.ID) State {
	s.path = append(append([]screenlayout.ID(nil), s.path...), id)
	return s.WithFocus(child)
}

// leave climbs back out, refocusing the container that was just left.
func (s State) leave() State {
	if len(s.path) == 0 {
		return s
	}
	left := s.path[len(s.path)-1]
	s.path = append([]screenlayout.ID(nil), s.path[:len(s.path)-1]...)
	return s.WithFocus(left)
}

// withFullscreen sets or clears the focused leaf's claim on the whole body.
func (s State) withFullscreen(on bool) State {
	s.full = on
	return s
}

// laneOffset is the leftmost visible child index of a windowed node.
func (s State) laneOffset(id screenlayout.ID) int {
	for _, l := range s.lanes {
		if l.id == id {
			return l.offset
		}
	}
	return 0
}

// withLane sets a windowed node's offset, copying the slice so no other holder
// of this state observes the write.
func (s State) withLane(id screenlayout.ID, offset int) State {
	if offset < 0 {
		offset = 0
	}
	for i, l := range s.lanes {
		if l.id == id {
			if l.offset == offset {
				return s
			}
			next := append([]lane(nil), s.lanes...)
			next[i].offset = offset
			s.lanes = next
			return s
		}
	}
	s.lanes = append(append([]lane(nil), s.lanes...), lane{id: id, offset: offset})
	return s
}
