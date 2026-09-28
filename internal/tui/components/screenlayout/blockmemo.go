package screenlayout

import (
	"slices"
)

// BlockMemo is the one place a section body keeps what it composed, so the next
// screen does not design a sixth.
//
// # Why a section needs one at all
//
// A [Section] hands back ITEMS and this package is what windows them, so a body
// composes ALL of its content on every render however few rows are on screen.
// That is deliberate rather than an oversight: heights are MEASURED here and
// never believed from a declaration, which is exactly what makes a section unable
// to overdraw. The cost is a property of the contract.
//
// It is also invisible until a body is a document. One keystroke renders a body
// two or three times — the key is routed through a resolve, the frame that paints
// is another, and [reclaimSlack] re-renders a section it grew — so a body of a
// thousand composed rows is thousands of wraps and thousands of styled rows per
// press. Studio › Commands' prompt preview reached forty-one milliseconds a
// keystroke that way, which is slower than a key repeat: the input queue grew
// while the user held `j` and the TUI stopped responding.
//
// What SCROLLING changes is the offset, and the offset belongs to this package.
// It does not change one byte of what the body produced. So the composition is
// memoised on what it was composed from, and a scroll costs a comparison.
//
// # Why a pointer, and why that is safe
//
// A body renders under a value receiver — during Update and again during View,
// and neither may write back to a screen copy — so a memo it can fill has to live
// behind a pointer the screen carries. This is [frame]'s situation with the
// ownership inverted: a frame is the measurement THIS package carries for itself,
// a BlockMemo is the composition a SCREEN carries for its own body.
//
// What it holds is DERIVED: a pure function of the key and the inputs stored
// beside it. Serving an entry requires both to match what the caller has just
// built, so a pointer shared between two screen values can leak WORK between
// them, which is the point, and cannot leak a DECISION, which is what would make
// it a bug.
//
// # Using it
//
// Hold one per zone that composes something expensive, key it on everything the
// composition depends on except the rows, and pass the content as `inputs`:
//
//	func (s Screen) previewBody(canvas screenlayout.Canvas) screenlayout.Block {
//	    lines := s.previewLines()
//	    return s.memo.Block(previewKey{width: canvas.Width()}, lines, func() screenlayout.Block {
//	        return compose(lines, canvas.Width())   // the expensive part
//	    })
//	}
//
// # Leave the rows out of the key
//
// This is the mistake worth writing down, because it fails silently: a memo keyed
// on canvas.Rows() never hits. One keystroke resolves a section at TWO heights —
// the allocation, then the taller re-render [reclaimSlack] gives it — so a
// rows-keyed entry is evicted by the second render and rebuilt by the first,
// forever, at full cost, with a memo in the code to say it was handled.
//
// Rows rarely belong in a composition anyway. What they decide is how many blank
// rows pad the end, which is cheap and belongs outside the memo. A body that
// genuinely composes differently at different heights needs two memos or none —
// never one key that thrashes between them.
type BlockMemo[K comparable] struct {
	key     K
	inputs  []string
	block   Block
	ready   bool
	entries map[K]blockMemoEntry
}

type blockMemoEntry struct {
	inputs []string
	block  Block
}

// Block returns the memoised composition, or builds it and keeps it.
//
// `inputs` is the content the build reads — the lines, the rows, whatever the
// body would compose. It is compared element by element, which is orders of
// magnitude cheaper than composing them, and is what makes the memo correct
// without a caller inventing a revision counter it then has to maintain.
//
// A nil memo simply builds, so a screen constructed without one (a test, a
// fixture) is slower and never wrong.
func (m *BlockMemo[K]) Block(key K, inputs []string, build func() Block) Block {
	if m == nil {
		return build()
	}
	if m.ready && m.key == key && slices.Equal(m.inputs, inputs) {
		return m.block
	}
	// The inputs are COPIED. They are the caller's slice, and a caller that reuses
	// its backing array would otherwise rewrite the very thing this entry is
	// matched on — and the memo would answer for content it no longer holds.
	m.key, m.inputs, m.block, m.ready = key, slices.Clone(inputs), build(), true
	return m.block
}

// BlockMany keeps one composition per key. It is useful when a body contains
// several independent, cursor-free documents that are all rebuilt during one
// paint. Rows and arrangement remain the caller's responsibility.
func (m *BlockMemo[K]) BlockMany(key K, inputs []string, build func() Block) Block {
	if m == nil {
		return build()
	}
	if m.entries != nil {
		if entry, ok := m.entries[key]; ok && slices.Equal(entry.inputs, inputs) {
			return entry.block
		}
	} else {
		m.entries = make(map[K]blockMemoEntry)
	}
	block := build()
	m.entries[key] = blockMemoEntry{inputs: slices.Clone(inputs), block: block}
	return block
}

// Reset drops the entry. Screens do not normally need it — an entry that no
// longer matches is never served — but a screen that knows its content is gone
// (a session ended, a route left) can hand the memory back.
func (m *BlockMemo[K]) Reset() {
	if m != nil {
		*m = BlockMemo[K]{}
	}
}
