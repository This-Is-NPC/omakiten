package screengrid

import "omakiten/internal/tui/components/screenlayout"

// kind is what a node IS. It is unexported and set by the three constructors,
// so there is no way to build a node that is a leaf and a container at once.
type kind int

const (
	kindCell kind = iota
	kindRows
	kindCols
)

// Node is one region of a body: a leaf that paints, or a container that splits
// its box between children.
//
// The zero value is a leaf that paints nothing, which is what a container with
// no children collapses to.
type Node struct {
	// Spec is how this node asks its PARENT to size it. A root's spec is only
	// read for its ID.
	Spec screenlayout.Spec

	kind     kind
	body     func(screenlayout.Canvas) screenlayout.Block
	children []Node
	window   bool
	hint     func(first, last, total int) string
}

// Cell is a leaf: the node that actually paints something.
//
// The body is the same [screenlayout.Section] body a screen writes today — it
// is handed a Canvas carrying the width, the rows and the cursor its parent
// resolved, and returns a Block. A cell never learns how deep it is.
func Cell(spec screenlayout.Spec, body func(screenlayout.Canvas) screenlayout.Block) Node {
	return Node{Spec: spec, kind: kindCell, body: body}
}

// Rows stacks its children: they share this node's width and split its rows.
//
// The split is the arranger's — minimums first, surplus by weight, and a child
// the box cannot afford is DROPPED from the bottom rather than rendered into a
// budget it does not have.
func Rows(spec screenlayout.Spec, children ...Node) Node {
	out := Node{Spec: spec, kind: kindRows, children: copyNodes(children)}
	for i := range out.children {
		// A stack has one column, so a child that was placed side by side
		// somewhere else does not carry the opt-in here.
		out.children[i].Spec.Column, out.children[i].Spec.Group = false, ""
	}
	return out
}

// Cols places its children side by side: they share this node's rows and split
// its width.
//
// Side by side is not unconditional. When the children's minimum widths cannot
// all fit at once the arranger stacks them instead — the breakpoint every
// responsive body in this TUI is built on. A Cols that would rather SLIDE than
// stack says so with [Node.Windowed].
func Cols(spec screenlayout.Spec, children ...Node) Node {
	out := Node{Spec: spec, kind: kindCols, children: copyNodes(children)}
	for i := range out.children {
		// Each child is its own column: the opt-in is set here rather than asked
		// of the caller, and the group is cleared so two children cannot be
		// folded into one column by a spec written for another parent.
		out.children[i].Spec.Column, out.children[i].Spec.Group = true, ""
	}
	return out
}

// copyNodes is why a constructor may normalise its children's specs: the
// variadic slice belongs to the CALLER, and a node built from another node's
// children must not reach back and rewrite them.
func copyNodes(nodes []Node) []Node {
	return append([]Node(nil), nodes...)
}

// Windowed makes a [Cols] slide over its children instead of stacking them when
// they do not all fit: the ones that fit are painted, the rest are reachable
// with `h` and `l`, and a hint reports what is off-screen on each side.
//
// It is the board's lane carousel, which is a different answer to the same
// question a breakpoint answers. A board with nine buckets on an eighty-column
// terminal cannot stack — nine stacked lanes is not a board — and cannot shrink
// nine lanes into eighty columns either. Sliding is the only one of the three
// that keeps the shape.
//
// No-op on a node that is not a Cols.
func (n Node) Windowed() Node {
	if n.kind == kindCols {
		n.window = true
	}
	return n
}

// WithHint replaces the glyph count a windowed container paints under itself
// with this node's own wording for the same fact.
//
// The default is `‹ 2` / `3 ›` — correct, universal and anonymous. A body whose
// children are NAMED things the user is navigating between has more to say:
// the board's lanes are the case it was added for, and "lanes 2–4 / 4 ·
// left/right scrolls" is what its users have read since before this package
// existed. Deleting that sentence to adopt the grid would be a migration
// removing information, so the hint became a hook rather than a constant.
//
// The closure is handed the window as the user counts it — the first and last
// visible child, ONE-BASED, and how many there are — and returns exactly one
// already-styled line. The grid still owns whether a hint is painted at all,
// charges its row before the children are given anything, and truncates what
// comes back to the container's width; a node that declares none keeps the
// glyphs, which is what every other windowed body gets.
//
// Invisible to [equivalentNode] by construction: a func is not comparable, so
// the frame memo witnesses the tree's kind, window flag, specs and children and
// nothing here.
func (n Node) WithHint(hint func(first, last, total int) string) Node {
	n.hint = hint
	return n
}

// IsLeaf reports whether this node paints rather than splits.
func (n Node) IsLeaf() bool { return n.kind == kindCell }

// IsUnit reports whether this node is a THING rather than an arrangement of
// things.
//
// It is the one definition the package has of that distinction, and both the
// layout and the navigation read it. They used to each carry their own, and the
// two drifted the way two copies of a rule always do: the layout dissolved a
// grouping column when it stacked and kept it when it did not, while the
// navigation dissolved it always. The same body then offered three stops at one
// width and two at another, and the extra stop was a container that paints
// nothing.
//
// A leaf is a unit: it has content. A WINDOWED row of columns is a unit: it
// cannot show all its children at once, so it owns a level you have to enter to
// see the rest. Everything else is pure grouping — a statement about where the
// boxes go, with no header, no border and no cursor of its own — and grouping is
// invisible to the user, so it must be invisible to the keyboard.
func (n Node) IsUnit() bool {
	return n.IsLeaf() || (n.kind == kindCols && n.window)
}

// Children are this node's children, empty for a leaf.
func (n Node) Children() []Node { return n.children }

// leaves is the focus ring: every leaf under this node, depth first, in
// declaration order — the order a reader's eye takes through the body.
func (n Node) leaves() []screenlayout.ID {
	if n.kind == kindCell {
		return []screenlayout.ID{n.Spec.ID}
	}
	var out []screenlayout.ID
	for _, child := range n.children {
		out = append(out, child.leaves()...)
	}
	return out
}

// navMembers is what `tab` walks over a set of children: the units, with every
// pure GROUPING container dissolved. See [Node.IsUnit].
//
// It is the ONE definition of a focus stop. There used to be a second — the
// scrollable leaves, flattened depth first — and the two disagreed about
// whether a grouping column was a place you could be.
func navMembers(children []Node) []screenlayout.ID {
	out := make([]screenlayout.ID, 0, len(children))
	for _, child := range children {
		if child.IsUnit() {
			out = append(out, child.Spec.ID)
			continue
		}
		out = append(out, navMembers(child.children)...)
	}
	return out
}

// firstZone is the id a caller lands on when it arrives at a node: the node
// itself when it is a unit, otherwise its first member.
func firstZone(node Node) (screenlayout.ID, bool) {
	if node.IsUnit() {
		return node.Spec.ID, true
	}
	if members := navMembers(node.children); len(members) > 0 {
		return members[0], true
	}
	return "", false
}

// pathTo is the chain of nodes from this one down to `id`, inclusive.
//
// One walk answers the three questions the key routing asks — what IS this id,
// who declares it, and which windowed ancestor owns it — which used to be three
// separate descents with three slightly different stopping rules.
func (n Node) pathTo(id screenlayout.ID) ([]Node, bool) {
	if n.Spec.ID == id {
		return []Node{n}, true
	}
	for _, child := range n.children {
		if tail, ok := child.pathTo(id); ok {
			return append([]Node{n}, tail...), true
		}
	}
	return nil, false
}
