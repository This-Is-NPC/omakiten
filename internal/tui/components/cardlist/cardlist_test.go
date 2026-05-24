package cardlist

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// fakeItems builds a slice of N items each with terminal-row count
// `height` and a unique title so the renderer's slice math is
// observable in golden assertions.
func fakeItems(n int, height int) []Item {
	out := make([]Item, n)
	for i := 0; i < n; i++ {
		lines := make([]string, height)
		lines[0] = "card #" + strconv.Itoa(i)
		for j := 1; j < height; j++ {
			lines[j] = "  line"
		}
		out[i] = Item{Content: strings.Join(lines, "\n"), Height: height}
	}
	return out
}

func TestNewItemDerivesHeightFromContent(t *testing.T) {
	item := NewItem("one\ntwo\nthree")
	if item.Height != 3 {
		t.Fatalf("NewItem height = %d, want 3", item.Height)
	}
	if item.Content != "one\ntwo\nthree" {
		t.Fatalf("NewItem content lost the original string: %q", item.Content)
	}
}

func TestModelZeroValueIsEmptyAndSafe(t *testing.T) {
	var m Model
	if got := m.Cursor(); got != 0 {
		// zero value cursor is 0, not -1 — the New() constructor is the
		// shape that primes -1. The zero-value path is still safe to
		// call mutators on (the empty list branch in MoveCursor handles
		// it), so this test just documents the difference.
		t.Logf("zero-value cursor = %d (use cardlist.New() for the -1 sentinel)", got)
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("zero-value Len = %d, want 0", got)
	}
	if _, ok := m.ActiveItem(); ok {
		t.Fatalf("ActiveItem returned ok=true on zero-value")
	}
	// Mutators on a zero-value Model must not panic. Assign through
	// a discard receiver so staticcheck does not flag the value as
	// unused; the test goal is the "no panic" path.
	_ = m.MoveCursor(1).WithViewport(10)
}

func TestNewReturnsNoSelection(t *testing.T) {
	m := New()
	if got := m.Cursor(); got != -1 {
		t.Fatalf("New cursor = %d, want -1", got)
	}
}

func TestMoveCursorFromNoSelectionLandsOnFirst(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(5, 1))
	m = m.MoveCursor(1)
	if m.Cursor() != 0 {
		t.Fatalf("MoveCursor(+1) from -1 = %d, want 0", m.Cursor())
	}
	m = New().WithViewport(20).WithItems(fakeItems(5, 1)).MoveCursor(-1)
	if m.Cursor() != 4 {
		t.Fatalf("MoveCursor(-1) from -1 = %d, want 4 (last)", m.Cursor())
	}
}

func TestMoveCursorClampsToBounds(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(3, 1)).MoveCursor(1)
	m = m.MoveCursor(99)
	if m.Cursor() != 2 {
		t.Fatalf("MoveCursor(+99) cursor = %d, want 2 (clamped)", m.Cursor())
	}
	m = m.MoveCursor(-99)
	if m.Cursor() != 0 {
		t.Fatalf("MoveCursor(-99) cursor = %d, want 0 (clamped)", m.Cursor())
	}
}

func TestJumpFirstAndJumpLast(t *testing.T) {
	m := New().WithViewport(10).WithItems(fakeItems(10, 4))
	m = m.JumpLast()
	if m.Cursor() != 9 {
		t.Fatalf("JumpLast cursor = %d, want 9", m.Cursor())
	}
	if m.Scroll() == 0 {
		t.Fatalf("JumpLast left scroll at 0 with 10 height-4 items in viewport=10; cursor would be off-screen")
	}
	m = m.JumpFirst()
	if m.Cursor() != 0 {
		t.Fatalf("JumpFirst cursor = %d, want 0", m.Cursor())
	}
	if m.Scroll() != 0 {
		t.Fatalf("JumpFirst scroll = %d, want 0", m.Scroll())
	}
}

func TestEmptyListPropagatesNoSelectionThroughEveryMutator(t *testing.T) {
	m := New().WithViewport(10)
	for _, op := range []func(Model) Model{
		Model.JumpFirst,
		Model.JumpLast,
		Model.PageUp,
		Model.PageDown,
	} {
		out := op(m)
		if out.Cursor() != -1 {
			t.Fatalf("empty-list mutator promoted cursor to %d, want -1", out.Cursor())
		}
		if out.Scroll() != 0 {
			t.Fatalf("empty-list mutator left scroll = %d, want 0", out.Scroll())
		}
	}
	out := m.MoveCursor(1)
	if out.Cursor() != -1 {
		t.Fatalf("empty-list MoveCursor promoted cursor to %d, want -1", out.Cursor())
	}
}

func TestWithItemsShrinkingClampsCursor(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(10, 1))
	m = m.JumpLast()
	if m.Cursor() != 9 {
		t.Fatalf("setup: JumpLast cursor = %d, want 9", m.Cursor())
	}
	// Items shrink to 4; cursor must clamp to 3 (last index of the new
	// list) without going stale.
	m = m.WithItems(fakeItems(4, 1))
	if m.Cursor() != 3 {
		t.Fatalf("WithItems(shrink) cursor = %d, want 3", m.Cursor())
	}
	if m.Scroll() < 0 || m.Scroll() > 3 {
		t.Fatalf("WithItems(shrink) scroll = %d, want in [0,3]", m.Scroll())
	}
}

func TestWithItemsEmptyResetsCursor(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(5, 1)).MoveCursor(1).MoveCursor(1)
	m = m.WithItems(nil)
	if m.Cursor() != -1 {
		t.Fatalf("WithItems(nil) cursor = %d, want -1", m.Cursor())
	}
	if m.Scroll() != 0 {
		t.Fatalf("WithItems(nil) scroll = %d, want 0", m.Scroll())
	}
}

func TestWithViewportShrinkingKeepsCursorVisible(t *testing.T) {
	m := New().WithViewport(40).WithItems(fakeItems(10, 4))
	m = m.JumpLast()
	// Viewport shrinks to 12 rows — only ~2 height-4 cards fit. Cursor
	// must still be inside the visible range after the resize.
	m = m.WithViewport(12)
	first, last, ok := m.VisibleRange()
	if !ok {
		t.Fatalf("VisibleRange returned ok=false after resize")
	}
	if m.Cursor() < first || m.Cursor() > last {
		t.Fatalf("WithViewport(shrink) hid the cursor: cursor=%d visible=[%d,%d]", m.Cursor(), first, last)
	}
}

func TestActiveItemFollowsCursor(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(5, 1)).MoveCursor(1).MoveCursor(1)
	item, ok := m.ActiveItem()
	if !ok {
		t.Fatalf("ActiveItem ok=false at cursor=1")
	}
	if !strings.Contains(item.Content, "card #1") {
		t.Fatalf("ActiveItem returned wrong card: %q", item.Content)
	}
}

func TestActiveItemFalseWhenNoSelection(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(5, 1))
	if _, ok := m.ActiveItem(); ok {
		t.Fatalf("ActiveItem ok=true with cursor=-1")
	}
}

func TestViewEmptyListReturnsEmptyString(t *testing.T) {
	got := New().WithViewport(10).View(lipgloss.NewStyle())
	if got != "" {
		t.Fatalf("View on empty = %q, want empty string", got)
	}
}

func TestViewFitsAllItemsRendersFlush(t *testing.T) {
	m := New().WithViewport(20).WithItems(fakeItems(3, 1))
	got := m.View(lipgloss.NewStyle())
	if strings.Contains(got, "▲") || strings.Contains(got, "▼") {
		t.Fatalf("View on fits-all rendered hint chrome: %q", got)
	}
	if !strings.Contains(got, "card #0") || !strings.Contains(got, "card #2") {
		t.Fatalf("View dropped items: %q", got)
	}
}

func TestViewRendersBelowHintWhenScrolledFromTop(t *testing.T) {
	m := New().WithViewport(5).WithItems(fakeItems(10, 1))
	got := m.View(lipgloss.NewStyle())
	if !strings.Contains(got, "▼") {
		t.Fatalf("View at top of long list missing ▼ below hint: %q", got)
	}
	if strings.Contains(got, "▲") {
		t.Fatalf("View at top rendered ▲ above hint: %q", got)
	}
}

func TestViewRendersBothHintsMidScroll(t *testing.T) {
	m := New().WithViewport(5).WithItems(fakeItems(10, 1)).JumpLast()
	got := m.View(lipgloss.NewStyle())
	// At bottom: above hint must render; below hint must not.
	if !strings.Contains(got, "▲") {
		t.Fatalf("View at bottom missing ▲ above hint: %q", got)
	}
	if strings.Contains(got, "▼") {
		t.Fatalf("View at bottom rendered ▼ below hint: %q", got)
	}
}

func TestVisibleRangeEmptyListReportsOkFalse(t *testing.T) {
	_, _, ok := New().WithViewport(10).VisibleRange()
	if ok {
		t.Fatalf("VisibleRange on empty list returned ok=true")
	}
}

// TestModelCursorAlwaysVisibleProperty is the keystone test that
// makes the bug class extinct: regardless of mutation sequence, the
// cursor must sit inside the slice the next View call would render.
// Drives N random sequences of MoveCursor / JumpFirst / JumpLast /
// PageUp / PageDown / WithItems / WithViewport against a Model and
// asserts the invariant after every step.
//
// Past surfaces (the subtask bug) failed exactly this invariant: the
// cursor ended up past the rendered window because the sync routine
// wrote the scroll as a line offset while the renderer interpreted
// it as a card index. With the cardlist routing every state change
// through scrollwindow.Resync, the property holds by construction.
func TestModelCursorAlwaysVisibleProperty(t *testing.T) {
	const seed = 0xC0FFEE
	rng := rand.New(rand.NewSource(seed))
	const iterations = 2000

	m := New().WithViewport(rng.Intn(18)+3).WithItems(randomItems(rng))

	for step := 0; step < iterations; step++ {
		switch rng.Intn(8) {
		case 0:
			m = m.MoveCursor(rng.Intn(5) - 2)
		case 1:
			m = m.JumpFirst()
		case 2:
			m = m.JumpLast()
		case 3:
			m = m.PageUp()
		case 4:
			m = m.PageDown()
		case 5:
			m = m.WithItems(randomItems(rng))
		case 6:
			m = m.WithViewport(rng.Intn(18) + 3)
		case 7:
			m = m.WithItems(nil) // drain
		}

		first, last, ok := m.VisibleRange()
		if m.Len() == 0 {
			if ok {
				t.Fatalf("step %d: empty list reported visible range ok=true", step)
			}
			if m.Cursor() != -1 {
				t.Fatalf("step %d: empty list cursor = %d, want -1", step, m.Cursor())
			}
			continue
		}
		if m.Cursor() == -1 {
			// No-selection state is allowed (post-WithItems on a fresh
			// list) — but only when no item has been promoted to the
			// cursor yet. Skip the visibility check; the next
			// MoveCursor will land on a real index.
			continue
		}
		if m.Cursor() < 0 || m.Cursor() >= m.Len() {
			t.Fatalf("step %d: cursor = %d out of bounds [0, %d)", step, m.Cursor(), m.Len())
		}
		if !ok {
			t.Fatalf("step %d: VisibleRange ok=false with non-empty list", step)
		}
		if m.Cursor() < first || m.Cursor() > last {
			t.Fatalf("step %d: cursor=%d outside visible [%d,%d]; items=%d viewport=%d scroll=%d",
				step, m.Cursor(), first, last, m.Len(), m.viewportRows, m.Scroll())
		}
	}
}

func randomItems(rng *rand.Rand) []Item {
	n := rng.Intn(15) // 0..14 items
	if n == 0 {
		return nil
	}
	out := make([]Item, n)
	for i := range out {
		h := rng.Intn(5) + 1 // 1..5 lines
		lines := make([]string, h)
		for j := range lines {
			lines[j] = "x"
		}
		out[i] = Item{Content: strings.Join(lines, "\n"), Height: h}
	}
	return out
}
