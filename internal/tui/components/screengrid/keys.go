package screengrid

import (
	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// laneKeys are the horizontal vocabulary, which neither the arranger nor keynav
// owns: `h` and `l` mean "the next lane", not "the next character", and only a
// body with a second axis has anywhere to spend them.
var laneKeys = map[string]int{"h": -1, "left": -1, "l": 1, "right": 1}

// EnterKey and LeaveKey are re-exported from the shared model so a screen can
// print its footer without importing two packages to do it.
const (
	EnterKey = keynav.EnterKey
	LeaveKey = keynav.LeaveKey
)

// HandleKey routes one keystroke and returns the next state plus whether the
// grid consumed it.
//
// # The rule, which is one rule at every depth
//
//	tab               the next sibling, and NEVER a child
//	                  (the zone ring only advances; see keynav.Vocabulary)
//	f                 enter the focused container — or, on a leaf, fill the body
//	esc               back out one level, or drop fullscreen
//	h / l             slide the level's lanes, when the level windows them
//	everything else   the focused leaf's own keys, through the arranger that placed it
//
// `tab` refusing to descend is the whole point. A ring flattened over every leaf
// in the tree reads fine two levels deep and falls apart below that: `tab` stops
// meaning "the next thing beside this" and starts meaning "the next thing
// somewhere", so the presses needed to reach a zone depend on how many children
// its unrelated neighbours happen to have.
func (s State) HandleKey(kit screenkit.Kit, box screenlayout.Box, key string, root Node) (State, bool) {
	if next, handled := s.handleReusableRootStackMotion(kit, box, key, root); handled {
		return next, true
	}

	// The ring comes from a RENDER, not from the tree. A stacked body dissolves
	// its columns, so what the user sees as three zones is three zones — and if
	// `tab` walked the declared children it would offer two stops, one of them a
	// container that is not on screen.

	// A frame is reusable only when the tree specs, geometry and every state
	// input arrange reads are unchanged. The shared arranger then uses its own
	// measurement memo and no section body is called merely to rediscover the
	// frame that placed the focused leaf.
	var w *walker
	if s.grid.fits(s, box, root) {
		w = s.grid.walker(kit, s)
	} else {
		w = &walker{kit: kit, state: s}
		w.arrange(root, box, 0)
		s.grid = newGridFrame(w, box, root, s)
	}
	ring, ok := w.ringFor(s.path)
	if !ok || len(ring) == 0 {
		return s, false
	}

	// A grid with no focus yet is focused on its FIRST sibling. Seeding is a
	// repair of the state, not an action the user asked for, so the keystroke
	// that discovers it does not ALSO spend itself stepping — otherwise the
	// first `tab` lands on the second zone and the first is reachable only by
	// going all the way round.
	focus := s.Focus()

	if next, handled := s.handleNavigationIntent(w, ring, focus, key, root); handled {
		return next, true
	}

	if indexOf(ring, focus) < 0 {
		focus = ring[0]
		s = s.WithFocus(focus)
	}
	w.state = s

	if step, ok := laneKeys[key]; ok {
		return s.slideFocused(w, root, focus, step)
	}

	return s.handleFocusedLeafKey(kit, w, focus, key)
}

func (s State) handleNavigationIntent(w *walker, ring []screenlayout.ID, focus screenlayout.ID, key string, root Node) (State, bool) {
	switch intent := keynav.Resolve(key, false); intent {
	case keynav.NextSibling, keynav.PrevSibling:
		step := 1
		if intent == keynav.PrevSibling {
			step = -1
		}
		next, seeded := ringOf(ring).Step(string(focus), step)
		if seeded {
			return s.WithFocus(screenlayout.ID(next)), true
		}
		if len(ring) < 2 {
			return s, false
		}
		return s.withFullscreen(false).WithFocus(screenlayout.ID(next)), true
	case keynav.Enter:
		if indexOf(ring, focus) < 0 {
			return s.WithFocus(ring[0]), true
		}
		return s.enterFocused(w, root, focus)
	case keynav.Leave:
		if s.full {
			return s.withFullscreen(false), true
		}
		if len(s.path) == 0 {
			return s, false
		}
		return s.leave(), true
	default:
		return s, false
	}
}

func (s State) handleReusableRootStackMotion(kit screenkit.Kit, box screenlayout.Box, key string, root Node) (State, bool) {
	if !s.canReuseRootStackFrame(box, key, root) {
		return s, false
	}
	return s.handleRootStackMotion(kit, box, key, root)
}

func (s State) handleFocusedLeafKey(kit screenkit.Kit, w *walker, focus screenlayout.ID, key string) (State, bool) {
	// Replay through the arranger that PLACED the leaf this frame, not the
	// parent the tree declared. flattenForStack dissolves a grouping Rows when
	// the body stacks, so the declared parent is not arranged and looking it
	// up eats the key. A root leaf is its own frame (arrangeLeaf records the
	// leaf under its own id). Same family as stack() once deciding geometry
	// by focus: both live on the stacking path, both passed because that path
	// has fewer tests than side-by-side.
	frame := w.frameHolding(focus)
	if frame == nil {
		return s, false
	}
	layout, handled := s.layout.HandleKeyIn(kit, frame.box, key, frame.sections...)
	if !handled {
		return s, false
	}
	s.layout = layout
	return s, true
}

// canReuseRootStackFrame recognizes the non-windowed, root-level
// List|inspector motion path whose flattened stack frame can be reconstructed
// without rendering. Windowed columns always belong to the slide arranger.
func (s State) canReuseRootStackFrame(box screenlayout.Box, key string, root Node) bool {
	intent := keynav.Resolve(key, false)
	_, laneKey := laneKeys[key]
	return (intent == keynav.None || intent == keynav.NextChild || intent == keynav.PrevChild) &&
		!laneKey && len(s.path) == 0 && !s.full && root.kind == kindCols && !root.window &&
		len(root.children) == 2 &&
		!screenlayout.TwoSpecsFitSideBySide(box, root.children[0].Spec, root.children[1].Spec)
}

// handleRootStackMotion takes the body-past-its-breakpoint fast path gated by
// handleReusableRootStackMotion for the two-column List|inspector archetype.
// Its flattening, window and hint charge depend only on the tree specs, box and
// focus, so it can recover the root's exact section set without rendering any body.
func (s State) handleRootStackMotion(kit screenkit.Kit, box screenlayout.Box, key string, root Node) (State, bool) {
	w := &walker{kit: kit, state: s}
	children := w.flattenForStack(root.children, box)
	children, win, share := w.stack(root, box, children, true)
	if win.hidden() {
		box.Rows = max(0, box.Rows-1)
	}
	if indexOf(idsOf(children), s.Focus()) < 0 {
		return s, false
	}
	layout, handled := s.layout.HandleKeyIn(kit, box, key, w.sections(children, 1, share)...)
	if !handled {
		return s, false
	}
	s.layout = layout
	return s, true
}

// ringFor is what `tab` walks at the level the path points at: the members the
// LAYOUT resolved for that container this frame.
//
// A path that no longer names a container on screen is not an error to report,
// it is a level that stopped existing — a column dissolved by a breakpoint, say
// — so the walk falls back to the body rather than refusing every key.
func (w *walker) ringFor(path []screenlayout.ID) ([]screenlayout.ID, bool) {
	id := screenlayout.ID("")
	if len(path) > 0 {
		id = path[len(path)-1]
	}
	if id != "" {
		if frame := w.frameOf(id); frame != nil {
			return frame.members, true
		}
	}
	if len(w.frames) == 0 {
		return nil, false
	}
	// frames[0] is the body: arrange records a container before it recurses.
	return w.frames[0].members, true
}

// enterFocused is `f`: descend into a container, or hand a nested leaf the whole body.
//
// A nested leaf has a parent level to fullscreen into. A root Cell does not: its
// arranger frame is already the body, and the render path has no child branch
// to isolate. Leaving `f` unhandled there preserves the root screen's binding
// instead of consuming a key that cannot produce a visible change.
func (s State) enterFocused(w *walker, root Node, focus screenlayout.ID) (State, bool) {
	chain, ok := root.pathTo(focus)
	if !ok {
		return s, false
	}
	target := chain[len(chain)-1]
	if target.IsLeaf() && len(chain) == 1 {
		return s, false
	}
	// A container that the layout DISSOLVED this frame is not enterable: its
	// children are already the level you are on, so there is nothing inside it
	// to go into. Whether it survived is a fact about the render, which is why
	// the frame is consulted rather than the tree.
	if w.frameOf(focus) == nil || len(target.children) == 0 {
		return s.withFullscreen(!s.full), true
	}
	child, ok := firstZone(target.children[0])
	if !ok {
		return s, false
	}
	return s.enter(target.Spec.ID, child), true
}

// Resync re-measures every level and writes back the clamped cursors and
// offsets, for a caller whose data changed under a stored selection.
//
// Levels are settled DEEPEST FIRST. A parent's arranger call re-runs its
// children's bodies, so settling a parent before its children would absorb the
// children's measurements twice — once stale, once fresh — and the stale write
// would win.
func (s State) Resync(kit screenkit.Kit, box screenlayout.Box, root Node) State {
	// A body renders FOCUSED from its first frame. Nothing is focused until
	// something says so, and waiting for a keystroke to say it means the first
	// thing the user sees is a layout with no zone in it — which also hides the
	// windowing, because the window is the one that follows the focus.
	w := &walker{kit: kit, state: s}
	w.arrange(root, box, 0)
	if ring, ok := w.ringFor(s.path); ok && len(ring) > 0 && indexOf(ring, s.Focus()) < 0 {
		s = s.WithFocus(ring[0])
	}
	for i := len(w.frames) - 1; i >= 0; i-- {
		f := w.frames[i]
		s.layout = s.layout.ResyncIn(kit, f.box, f.sections...)
	}
	w.state = s
	s.grid = newGridFrame(w, box, root, s)
	return s
}

// KeyHints is what a screen puts in its footer: the arranger's advertised keys
// for the leaves that scroll, plus the lane keys when some row of columns can
// actually slide.
func KeyHints(root Node) []screenlayout.Binding {
	var sections []screenlayout.Section
	if root.IsLeaf() {
		sections = append(sections, screenlayout.Func{Def: root.Spec})
	} else {
		for _, id := range navMembers(root.children) {
			sections = append(sections, screenlayout.Func{
				Def: screenlayout.Spec{ID: id, Scroll: screenlayout.ScrollItems},
			})
		}
	}
	return screenlayout.KeyHints(sections...)
}

// slideFocused moves the nearest windowed ancestor of the focused leaf, and
// moves focus with it so the lane the user slid to is the lane they land in.
func (s State) slideFocused(w *walker, root Node, focus screenlayout.ID, step int) (State, bool) {
	owner, children := windowedAncestor(root, focus)
	if owner == nil {
		return s, false
	}
	at := focusedChild(children, focus)
	if at < 0 {
		return s, false
	}
	next := clamp(at+step, 0, len(children)-1)
	if next == at {
		return s, false
	}
	// Focus lands on the new lane's first zone; a lane with nothing to focus
	// keeps the focus where it was and only the window moves.
	if zone, ok := firstZone(children[next]); ok {
		s = s.WithFocus(zone)
	}
	return s.withLane(owner.Spec.ID, s.slideOffset(w, *owner, children, next)), true
}

// slideOffset is the leftmost visible child AFTER the slide.
//
// It used to store `next` — the index of the child that just took the focus —
// into a field documented as "the index of its leftmost visible child", and the
// two are not the same number. The effect was a window that dragged the focused
// lane to the left edge on every press: on a board showing three of four lanes,
// `l` from the first to the second scrolled the whole carousel even though the
// second lane was already on screen, and `h` back again did not bring it back.
//
// What a window owes its user is the LEAST movement that keeps the focus
// visible, which is what [windowAround] then applies to whatever is stored here
// — so this has to store a real offset or the two disagree about what the field
// means. The arithmetic is windowAround's own, taken against the same fit count
// the render just used, read off the frame that placed this container rather
// than re-derived from a box this function was never given.
func (s State) slideOffset(w *walker, owner Node, children []Node, next int) int {
	frame := w.frameOf(owner.Spec.ID)
	if frame == nil {
		return next
	}
	perView := w.fitCount(children, frame.box)
	if perView >= len(children) {
		return 0
	}
	offset := clamp(s.laneOffset(owner.Spec.ID), 0, len(children)-perView)
	if next < offset {
		offset = next
	}
	if next >= offset+perView {
		offset = next - perView + 1
	}
	return offset
}

// windowedAncestor is the nearest windowed row of columns above the focused
// node, with its children. Read off the one descent [Node.pathTo] takes.
func windowedAncestor(root Node, focus screenlayout.ID) (*Node, []Node) {
	chain, ok := root.pathTo(focus)
	if !ok {
		return nil, nil
	}
	for i := len(chain) - 2; i >= 0; i-- {
		if chain[i].kind == kindCols && chain[i].window {
			owner := chain[i]
			return &owner, owner.children
		}
	}
	return nil, nil
}

// ringOf adapts the grid's typed ids to the shared model's plain strings, which
// is the price of the model having no dependency on this package.
func ringOf(ids []screenlayout.ID) keynav.Ring {
	out := make(keynav.Ring, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func indexOf(ids []screenlayout.ID, id screenlayout.ID) int {
	for i, candidate := range ids {
		if candidate == id {
			return i
		}
	}
	return -1
}
