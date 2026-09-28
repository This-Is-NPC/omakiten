package screengrid

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// Placement is what the grid decided about one node, reported so a caller can
// show WHY a body looks the way it does rather than guessing from the pixels.
type Placement struct {
	// ID is the node's id.
	ID screenlayout.ID
	// Depth is 0 for the root, 1 for its children, and so on.
	Depth int
	// Leaf distinguishes a cell from a container.
	Leaf bool
	// Box is the geometry this node was resolved into.
	Box screenlayout.Box
	// Arrangement is the breakpoint a container chose. Meaningless for a leaf.
	Arrangement screenlayout.Arrangement
	// Windowed is set on a container that slid over its children.
	Windowed bool
	// First and Last are the visible child range of a windowed container.
	First, Last int
	// Hidden counts the children a windowed container could not show.
	HiddenBefore, HiddenAfter int
	// Dropped is set when the parent's box could not afford this node at all.
	Dropped bool
}

// Result is one rendered grid.
type Result struct {
	// View is the body, exactly as it should be painted. Never more than the
	// box's rows, never wider than its width.
	View string
	// Placements are every node, parents before children, in declaration order.
	Placements []Placement
	// Focus is the leaf that held focus for this frame.
	Focus screenlayout.ID
}

// Rows is the terminal rows View actually paints.
func (r Result) Rows() int {
	if r.View == "" {
		return 0
	}
	return len(strings.Split(r.View, "\n"))
}

// Placement looks a node's placement up by id.
func (r Result) Placement(id screenlayout.ID) (Placement, bool) {
	for _, p := range r.Placements {
		if p.ID == id {
			return p, true
		}
	}
	return Placement{}, false
}

// Render lays the tree out inside the box and paints it.
//
// It is PURE, exactly as [screenlayout.Arrange] is: every offset and cursor is
// clamped for the frame being painted, and nothing is persisted. The persisting
// twins are [State.Resync] and [State.HandleKey].
func Render(kit screenkit.Kit, state State, box screenlayout.Box, root Node) Result {
	w := &walker{kit: kit, state: state}
	view := w.arrange(root, box, 0)
	return Result{View: view, Placements: w.placements, Focus: state.Focus()}
}

// walker carries the one render pass: the state every level reads, and the two
// things a level produces that an outer caller needs — the placements, and the
// (box, sections) pair a keystroke has to be replayed against.
type walker struct {
	kit        screenkit.Kit
	state      State
	placements []Placement
	// frames records, per CONTAINER id, the exact arranger call that container
	// made. A keystroke aimed at a leaf is replayed through the frame whose
	// arranged children include that leaf — the call that actually placed it
	// this frame, which may not be the parent the tree declared.
	frames []containerFrame
}

// containerFrame is one level's arranger call, recorded during a render.
type containerFrame struct {
	id       screenlayout.ID
	box      screenlayout.Box
	sections []screenlayout.Section
	children []screenlayout.ID
	// members are the level's children as the LAYOUT resolved them: after any
	// grouping was dissolved, and before the window picked which of them fit.
	//
	// It is what `tab` walks, and it has to be this list rather than the
	// declared one. A stacked body dissolves its columns — `[a over b] | c`
	// becomes three zones — so a ring built from the tree would offer two stops
	// for the three zones on screen, and one of the two would be a container the
	// user cannot see. Navigation follows what was drawn.
	members []screenlayout.ID
}

// gridFrame is the arrangement metadata a keystroke needs after the body has
// already been composed. The sections stay behind the frame so the shared
// arranger can use its own measurement memo; the grid only decides whether the
// recorded tree is still the one the key is addressing.
//
// It is frozen once stored in State. A reused frame never appends to frames or
// mutates any of its slices, so value-typed State copies cannot observe one
// another's navigation metadata changing.
type gridFrame struct {
	valid  bool
	box    screenlayout.Box
	root   Node
	focus  screenlayout.ID
	path   []screenlayout.ID
	full   bool
	lanes  []lane
	frames []containerFrame
}

func newGridFrame(w *walker, box screenlayout.Box, root Node, state State) gridFrame {
	return gridFrame{
		valid:  true,
		box:    box,
		root:   root,
		focus:  state.Focus(),
		path:   append([]screenlayout.ID(nil), state.path...),
		full:   state.full,
		lanes:  append([]lane(nil), state.lanes...),
		frames: w.frames,
	}
}

// fits is deliberately a direct comparison. Specs contain only values, so a
// second fingerprint would be weaker than the equality screenlayout already
// uses. Focus, path and fullscreen are witnesses too: arrange reads all three
// while deciding which branches and windows exist.
//
// So are the LANE OFFSETS, and that is the fourth witness rather than a fourth
// reason to give up. A tree holding a windowed container used to be refused
// outright — the horizontal offset it slides by was the one arrange input the
// frame did not record, so reuse could have replayed a keystroke against a
// window that had since moved. Recording it is eight lines and closes that hole
// exactly; refusing every windowed tree closed it by making the board pay a full
// composition per keystroke, which is the same answer a screen with no memo at
// all gets.
func (f gridFrame) fits(state State, box screenlayout.Box, root Node) bool {
	if !f.valid || f.box != box || f.focus != state.Focus() || f.full != state.full || len(f.path) != len(state.path) {
		return false
	}
	for i, id := range f.path {
		if id != state.path[i] {
			return false
		}
	}
	if !sameLanes(f.lanes, state.lanes) {
		return false
	}
	return equivalentNode(f.root, root)
}

// sameLanes reports whether two windowed-offset sets describe the same windows.
//
// Compared by VALUE per id rather than as two slices, because an absent entry
// and an explicit zero are the same window — [State.laneOffset] reads a missing
// id as zero — and a slice comparison would call a window that slid away and
// back a different one, which is a cache miss with no reason behind it.
func sameLanes(want, got []lane) bool {
	for _, l := range want {
		if laneOffsetIn(got, l.id) != l.offset {
			return false
		}
	}
	for _, l := range got {
		if laneOffsetIn(want, l.id) != l.offset {
			return false
		}
	}
	return true
}

func laneOffsetIn(lanes []lane, id screenlayout.ID) int {
	for _, l := range lanes {
		if l.id == id {
			return l.offset
		}
	}
	return 0
}

func (f gridFrame) walker(kit screenkit.Kit, state State) *walker {
	return &walker{kit: kit, state: state, frames: f.frames}
}

func equivalentNode(want, got Node) bool {
	if want.kind != got.kind || want.window != got.window || want.Spec != got.Spec || len(want.children) != len(got.children) {
		return false
	}
	for i := range want.children {
		if !equivalentNode(want.children[i], got.children[i]) {
			return false
		}
	}
	return true
}

// frameOf is the recorded call for a container, or nil.
func (w *walker) frameOf(id screenlayout.ID) *containerFrame {
	for i := range w.frames {
		if w.frames[i].id == id {
			return &w.frames[i]
		}
	}
	return nil
}

// frameHolding is the arranger call that actually placed this leaf this frame.
//
// The declared parent is the wrong lookup. flattenForStack dissolves a grouping
// Rows when the body stacks, so the three zones on screen belong to the ROOT's
// arranger and the declared parent is not in frames at all. Looking it up and
// finding nothing eats the keystroke — the zone is focused, it shows overflow,
// and j does nothing. Same family as stack() once deciding geometry by focus
// ("tab hides the section beside it"): both live on the stacking path, both
// passed because that path has fewer tests than side-by-side.
//
// Innermost first, so a nested container that still arranged keeps its own box.
func (w *walker) frameHolding(id screenlayout.ID) *containerFrame {
	for i := len(w.frames) - 1; i >= 0; i-- {
		if indexOf(w.frames[i].children, id) >= 0 {
			return &w.frames[i]
		}
	}
	return nil
}

// arrange resolves one node into its box and returns what it paints.
//
// A leaf is arranged as a one-section body rather than special-cased: a root
// that is a single cell then goes through the same clip and the same row
// budget as a cell nested three levels down, instead of through a second code
// path that could disagree with the first.
func (w *walker) arrange(node Node, box screenlayout.Box, depth int) string {
	if box.Rows <= 0 || box.Width <= 0 {
		w.placements = append(w.placements, Placement{
			ID: node.Spec.ID, Depth: depth, Leaf: node.IsLeaf(), Box: box, Dropped: true,
		})
		return ""
	}
	if node.IsLeaf() {
		return w.arrangeLeaf(node, box, depth)
	}

	children := node.children
	place := Placement{ID: node.Spec.ID, Depth: depth, Box: box, Last: len(children) - 1}

	// Which axis this node is resolved on, and what it does when the children do
	// not fit. Three cases, and the order matters:
	//
	//   1. A WINDOWED Cols always slides. Asking whether all seven lanes fit side
	//      by side is the wrong question — of course they do not, that is what
	//      the window is for. The slide picks the subset that does.
	//   2. A plain Cols is side by side only while every minimum width fits at
	//      once; otherwise the arranger stacks it, and a stack is case 3.
	//   3. Everything else is a stack, and a stack that cannot afford its
	//      children's minimum ROWS windows them rather than crushing them.
	var hint string
	var win window
	sideBySide := false
	// share is whether the children about to be arranged are still shown
	// TOGETHER — the signal [Spec.ColumnMaxRows] is read against, in
	// [columnSpec]. It starts true because every branch below shows more than
	// one zone (a genuine side-by-side row, a windowed slide, or a stack whose
	// zones all fit) except the one that explicitly hands a single zone the
	// whole body: the `f` takeover. [walker.stack] reports its own answer for
	// the fourth branch, the one that can ALSO end in a single zone — see its
	// doc for why that is a report of which branch fired rather than a
	// property of the box.
	share := true
	members := navMembers(children)
	switch {
	case w.fullscreenHere(children):
		// `f` asked for the whole body, and the branch holding the focused leaf
		// is the only one that stays. Every level on the way down does the same,
		// so the leaf ends up with the BODY rather than with its own column.
		at := focusedChild(children, w.state.Focus())
		children, win = children[at:at+1], window{
			first: at, last: at, before: at, after: len(children) - at - 1,
		}
		share = false
	case node.kind == kindCols && node.window:
		children, win = w.slide(node, box, children)
		sideBySide = true
	case node.kind == kindCols && screenlayout.FitsSideBySide(box, specsOf(children)...):
		win = window{last: len(children) - 1}
		sideBySide = true
	default:
		// A stack of stacks is one stack. Grouping is a statement about
		// COLUMNS — `[details over subtasks] | activity` — and once the columns
		// are gone the group says nothing, exactly as screenlayout says of
		// Spec.Group ("stacked there is one column already, so a group is
		// ignored", section.go:70).
		//
		// Leaving the nesting in place is not merely redundant, it is wrong
		// twice: the focused zone would get the whole of its PARENT's slice
		// rather than the whole body, and each level would paint a hint of its
		// own, so a body hiding two zones would say so twice in two different
		// numbers.
		children = w.flattenForStack(children, box)
		// body is whether this stack IS the body, rather than one column's
		// internal split inside a side-by-side body — see [walker.stack]. It
		// selects what happens when the zones do not all fit.
		body := depth == 0 && node.kind == kindCols
		children, win, share = w.stack(node, box, children, body)
	}
	if win.hidden() {
		place.Windowed = true
		// `f` already asked for the whole body; a second (and third, nested)
		// row of "zones hidden" chrome is leftover space the leaf cannot use.
		// Stacked auto-fill still charges the hint so the user can see what
		// tab would bring back.
		if !w.state.full {
			hint = w.hint(node, win, box.Width, sideBySide)
			box.Rows = max(0, box.Rows-1)
		}
	}
	place.First, place.Last = win.first, win.last
	place.HiddenBefore, place.HiddenAfter = win.before, win.after

	return w.renderContainer(node, box, children, members, depth, place, hint, share)
}

func (w *walker) renderContainer(node Node, box screenlayout.Box, children []Node, members []screenlayout.ID, depth int, place Placement, hint string, share bool) string {
	sections := w.sections(children, depth+1, share)
	w.frames = append(w.frames, containerFrame{
		id: node.Spec.ID, box: box, sections: sections,
		children: idsOf(children), members: members,
	})
	res := screenlayout.ArrangeIn(w.kit, box, w.state.layout, sections...)
	place.Arrangement = res.Arrangement
	w.placements = append(w.placements, place)
	w.reportChildren(res, children, depth+1)
	if hint == "" {
		return res.View
	}
	if res.View == "" {
		return hint
	}
	return res.View + "\n" + hint
}

// arrangeLeaf runs a leaf through the arranger as a body of one section, so the
// clip that keeps a cell inside its box is the arranger's and not a second one.
func (w *walker) arrangeLeaf(node Node, box screenlayout.Box, depth int) string {
	spec := node.Spec
	spec.Column, spec.Group = false, ""
	section := screenlayout.Func{Def: spec, Body: node.body}
	// A root that is a single cell has no parent to replay a keystroke through,
	// so it records its own frame and stands in as its own parent.
	w.frames = append(w.frames, containerFrame{
		id: spec.ID, box: box, sections: []screenlayout.Section{section},
		children: []screenlayout.ID{spec.ID}, members: []screenlayout.ID{spec.ID},
	})
	res := screenlayout.ArrangeIn(w.kit, box, w.state.layout, section)
	p, _ := res.Placement(spec.ID)
	w.placements = append(w.placements, Placement{
		ID: spec.ID, Depth: depth, Leaf: true, Box: box, Dropped: p.Dropped,
	})
	return res.View
}

// sections turns a container's children into the arranger's sections. A leaf
// contributes its own body; a container contributes a body that recurses into
// the canvas the arranger just gave it.
//
// share is [walker.arrange]'s answer to "are these children still sharing a
// column", and it is read here rather than passed any further: it decides
// what a LEAF's [screenlayout.Spec.ColumnMaxRows] means for this call, through
// [columnSpec], and a container has no ceiling of its own to fold — its
// leaves state theirs when ITS children are turned into sections, one
// recursion level down.
func (w *walker) sections(children []Node, depth int, share bool) []screenlayout.Section {
	out := make([]screenlayout.Section, len(children))
	for i, child := range children {
		if child.IsLeaf() {
			out[i] = screenlayout.Func{Def: columnSpec(child.Spec, share), Body: child.body}
			continue
		}
		nested := child
		out[i] = screenlayout.Func{
			Def: containerSpec(child.Spec),
			Body: func(canvas screenlayout.Canvas) screenlayout.Block {
				inner := screenlayout.Box{Width: canvas.Width(), Rows: canvas.Rows()}
				view := w.arrange(nested, inner, depth)
				if view == "" {
					return screenlayout.Block{Cursor: screenlayout.NoSelection()}
				}
				return screenlayout.Block{
					Items:  strings.Split(view, "\n"),
					Cursor: screenlayout.NoSelection(),
				}
			},
		}
	}
	return out
}

// reportChildren records what the arranger decided about each child, from the
// arranger's own placements rather than from anything this package predicted.
//
// A CONTAINER child reports itself, because its body recursed and knows its own
// breakpoint; here it is recorded only when the arranger dropped it, in which
// case its body never ran. A LEAF child has no recursion to report from, so this
// is the only place its resolved geometry is written down — and without it the
// leaves are invisible to every caller reading Placements, which is a hole that
// makes an assertion about them pass by matching nothing.
func (w *walker) reportChildren(res screenlayout.Result, children []Node, depth int) {
	for i, p := range res.Placements {
		if i >= len(children) {
			continue
		}
		child := children[i]
		if !child.IsLeaf() && !p.Dropped {
			continue // it reported itself, with its own arrangement
		}
		w.placements = append(w.placements, Placement{
			ID: child.Spec.ID, Depth: depth, Leaf: child.IsLeaf(),
			Box:     screenlayout.Box{Width: p.Width, Rows: p.Rows},
			Dropped: p.Dropped,
		})
	}
}

// hint is the off-screen count a windowed container paints under itself, in the
// axis it was windowed on: `‹ 2` / `3 ›` for columns, `▲ 2` / `▼ 3` for a stack.
// The glyphs are the ones the scroll hints already use, so a hidden SECTION and
// a hidden row read the same way round.
//
// A node that declared its own wording with [Node.WithHint] gets asked instead.
// It is handed the window one-based, the way the user counts lanes, and returns
// a line already styled — so the truncation is TruncateStyled rather than the
// rune walk below, which would cut an escape sequence in half. Everything else
// stays the grid's: whether a hint is painted at all, and the row it is charged.
func (w *walker) hint(node Node, win window, width int, sideBySide bool) string {
	if node.hint != nil {
		line := node.hint(win.first+1, win.last+1, win.last+1+win.after)
		if lipgloss.Width(line) > width {
			line = screenkit.TruncateStyled(line, width)
		}
		return line
	}
	before, after := "▲ %d", "▼ %d"
	if sideBySide {
		before, after = "‹ %d", "%d ›"
	}
	var parts []string
	if win.before > 0 {
		parts = append(parts, fmt.Sprintf(before, win.before))
	}
	if win.after > 0 {
		parts = append(parts, fmt.Sprintf(after, win.after))
	}
	hint := strings.Join(parts, "   ")
	// Truncated by CELLS, not by bytes: the chevrons are multibyte, and a
	// byte-sliced hint both overflows and cuts a rune in half.
	for lipgloss.Width(hint) > width && hint != "" {
		hint = hint[:len(hint)-len(string([]rune(hint)[len([]rune(hint))-1]))]
	}
	return w.kit.Styles.Hint.Render(hint)
}

// containerSpec is the spec a CONTAINER is placed by: every geometry field it
// declared, and none of the item fields.
//
// It is spelled out as a literal rather than copied-and-blanked because the
// distinction is the point. A container participates in its parent's width and
// row arithmetic exactly as a cell does, but it is not a scroll surface and
// holds no cursor — its LEAVES are and do. Copying the whole spec and clearing
// the item half would leave the guarantee resting on remembering to clear each
// new field; naming the half that carries over cannot rot the same way, because
// a field nobody listed is a field nobody gave the container.
func containerSpec(spec screenlayout.Spec) screenlayout.Spec {
	return screenlayout.Spec{
		ID:           spec.ID,
		MinWidth:     spec.MinWidth,
		MaxWidth:     spec.MaxWidth,
		WidthPercent: spec.WidthPercent,
		MinRows:      spec.MinRows,
		MaxRows:      spec.MaxRows,
		Weight:       spec.Weight,
		Column:       spec.Column,
		Group:        spec.Group,
		ColumnGap:    spec.ColumnGap,
	}
}

// columnSpec is the spec a LEAF is placed by: its declared Spec, with
// [screenlayout.Spec.ColumnMaxRows] folded into the effective MaxRows the
// arranger enforces — but only while `share` says this render is still one
// column's internal split.
//
// share false is the two ways a render stops being a column at all: the `f`
// takeover, and a stack that gave up on being side by side. Both hand the
// zone the whole body, and a ceiling that means "what I am worth beside a
// sibling" has nothing left to cap once there is no sibling.
//
// A section that declared MaxRows directly — the form's fixed height — is
// never loosened by this: the two ceilings combine to the tighter one, the
// same rule [screenlayout.Spec.normalize] applies to MaxRows against MinRows.
func columnSpec(spec screenlayout.Spec, share bool) screenlayout.Spec {
	if !share || spec.ColumnMaxRows <= 0 {
		return spec
	}
	if spec.MaxRows == 0 || spec.ColumnMaxRows < spec.MaxRows {
		spec.MaxRows = spec.ColumnMaxRows
	}
	return spec
}

// fullscreenHere reports whether this container holds the branch the focused
// leaf lives in. Every level on that path keeps only that branch, so the leaf
// is handed the whole BODY rather than the whole of its own column — which is
// what "full screen" has to mean to be worth asking for.
func (w *walker) fullscreenHere(children []Node) bool {
	return w.state.full && focusedChild(children, w.state.Focus()) >= 0
}

// navMembers is what `tab` walks: the children with every pure GROUPING
// container dissolved.
//
// Layout and navigation are different questions about the same tree, and this
// is where they part. `[details over subtasks] | activity` needs the column to
// arrange the boxes; it does not need it as a stop, because the column paints
// nothing — no header, no border, no cursor. Leaving it in the ring puts the
// focus somewhere the eye cannot find it, and the two zones inside it become
// unreachable by the key that is supposed to reach zones.
//
// flattenForStack dissolves the children that are only groupings, so a stacked
// body is the flat list of zones the user sees.
//
// It asks [Node.IsUnit] — the same question the navigation asks — plus one thing
// only the layout cares about: a grouping row of columns that STILL FITS side by
// side is a real arrangement in this box and has to survive to make it, even
// though the keyboard walks straight through it. That is the whole difference
// between the two, and it lives here rather than in a second copy of the rule.
func (w *walker) flattenForStack(children []Node, box screenlayout.Box) []Node {
	out := make([]Node, 0, len(children))
	for _, child := range children {
		arranges := child.kind == kindCols &&
			screenlayout.FitsSideBySide(box, specsOf(child.children)...)
		if child.IsUnit() || arranges {
			out = append(out, child)
			continue
		}
		out = append(out, w.flattenForStack(child.children, box)...)
	}
	return out
}

// specsOf is the children's declared specs, for a question that has to be asked
// before any of them is rendered.
func specsOf(nodes []Node) []screenlayout.Spec {
	out := make([]screenlayout.Spec, len(nodes))
	for i, n := range nodes {
		out[i] = n.Spec
	}
	return out
}

func idsOf(nodes []Node) []screenlayout.ID {
	out := make([]screenlayout.ID, len(nodes))
	for i, n := range nodes {
		out[i] = n.Spec.ID
	}
	return out
}
