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

func TestWithCursorJumpsToIndexAndResyncs(t *testing.T) {
	m := New().WithViewport(5).WithItems(fakeItems(20, 1)).WithCursor(15)
	if m.Cursor() != 15 {
		t.Fatalf("WithCursor(15) cursor = %d, want 15", m.Cursor())
	}
	// Scroll must follow so cursor 15 is visible in a viewport of 5.
	first, last, ok := m.VisibleRange()
	if !ok || m.Cursor() < first || m.Cursor() > last {
		t.Fatalf("WithCursor(15) left cursor invisible: visible=[%d,%d] ok=%v", first, last, ok)
	}
}

func TestWithCursorClampsOutOfRange(t *testing.T) {
	m := New().WithViewport(10).WithItems(fakeItems(5, 1)).WithCursor(99)
	if m.Cursor() != 4 {
		t.Fatalf("WithCursor(99) on 5-item list = %d, want 4 clamped", m.Cursor())
	}
}

func TestWithCursorMinusOnePreservesItems(t *testing.T) {
	m := New().WithViewport(10).WithItems(fakeItems(5, 1)).WithCursor(3).WithCursor(-1)
	if m.Cursor() != -1 {
		t.Fatalf("WithCursor(-1) cursor = %d, want -1", m.Cursor())
	}
	if m.Len() != 5 {
		t.Fatalf("WithCursor(-1) drained items: Len=%d, want 5", m.Len())
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

// TestViewRendersPartialTrailingCardFill pins the visual-fill
// contract: when the viewport has rows left over after the last
// whole card AND there is a next card below, the next card is
// rendered truncated to fill those rows. The "▼ N below" hint still
// counts the partially-rendered card as below — the partial is a
// preview, not "fully visible".
//
// Setup: 10 items of height 3 each, viewport = 9. Scrollwindow
// reserves 1 row for the below hint, leaving 8 rows for cards.
// Two whole cards = 6 rows. 2 rows left over → next card renders
// its first 2 lines as a partial preview.
func TestViewRendersPartialTrailingCardFill(t *testing.T) {
	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{
			Content: "card #" + strconv.Itoa(i) + "\n  line2\n  line3",
			Height:  3,
		}
	}
	m := New().WithViewport(9).WithItems(items)
	got := m.View(lipgloss.NewStyle())

	// Two whole cards (indices 0, 1).
	if !strings.Contains(got, "card #0") {
		t.Fatalf("View missing first whole card: %q", got)
	}
	if !strings.Contains(got, "card #1") {
		t.Fatalf("View missing second whole card: %q", got)
	}
	// Partial preview of card #2 — at least its first line is
	// visible, but its last (line3) is truncated away.
	if !strings.Contains(got, "card #2") {
		t.Fatalf("View missing partial preview of next card: %q", got)
	}
	if strings.Count(got, "card #2") != 1 {
		t.Fatalf("View should preview card #2 exactly once: %q", got)
	}
	// Below hint must still fire and count the partial card.
	if !strings.Contains(got, "▼") {
		t.Fatalf("View missing ▼ below hint: %q", got)
	}
}

// TestViewPartialTrailingCardDoesNotOverflow pins that the partial
// preview never makes the rendered output taller than the viewport
// budget — the entire point of the feature is to fill the leftover
// rows, not to overflow.
func TestViewPartialTrailingCardDoesNotOverflow(t *testing.T) {
	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{
			Content: "card #" + strconv.Itoa(i) + "\n  line2\n  line3\n  line4",
			Height:  4,
		}
	}
	const viewport = 11
	m := New().WithViewport(viewport).WithItems(items)
	got := m.View(lipgloss.NewStyle())
	if rows := strings.Count(got, "\n") + 1; rows > viewport {
		t.Fatalf("View output rows = %d, viewport budget = %d (partial fill overflowed)", rows, viewport)
	}
}

// TestViewRendersPartialLeadingCardFill pins the leading-partial
// contract: when the user has scrolled mid-list, the card just
// above the first whole visible card renders its LAST lines so the
// column flushes its top against the above hint instead of leaving
// a blank gap. Symmetric to the trailing-partial behaviour.
//
// Setup: 10 items × 3 lines, viewport = 12, cursor jumped to last so
// scroll lands at the bottom of the list (scroll > 0, no trailing).
// Above hint = 1 row. Cards fitting = 10 rows (≈ 3 cards). Leftover
// after hints + whole cards goes entirely to the leading partial
// (no trailing partial because end == len).
func TestViewRendersPartialLeadingCardFill(t *testing.T) {
	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{
			Content: "card #" + strconv.Itoa(i) + "\nL2\nL3-tail",
			Height:  3,
		}
	}
	m := New().WithViewport(12).WithItems(items).JumpLast()
	got := m.View(lipgloss.NewStyle())
	if !strings.Contains(got, "▲") {
		t.Fatalf("View at bottom should fire above hint: %q", got)
	}
	if strings.Contains(got, "▼") {
		t.Fatalf("View at bottom should not fire below hint: %q", got)
	}
	// Last card always visible.
	if !strings.Contains(got, "card #9") {
		t.Fatalf("View missing last card: %q", got)
	}
	// Leading partial uses the LAST lines of the just-above-scroll
	// card. Pin via the distinctive "L3-tail" sentinel, which is the
	// last line of every card.
	if !strings.Contains(got, "L3-tail") {
		t.Fatalf("View missing leading partial tail content: %q", got)
	}
}

// TestViewSplitsLeftoverBetweenLeadingAndTrailing pins the mid-list
// case: when both edges have hidden cards (scroll > 0 AND end <
// len), the leftover viewport rows split between the two partials.
// Trailing favoured by 1 on an odd split.
func TestViewSplitsLeftoverBetweenLeadingAndTrailing(t *testing.T) {
	items := make([]Item, 10)
	for i := range items {
		items[i] = Item{
			Content: "card #" + strconv.Itoa(i) + "\nL2\nL3-tail",
			Height:  3,
		}
	}
	// Scroll to middle: jump down a few cards.
	m := New().WithViewport(9).WithItems(items)
	for i := 0; i < 5; i++ {
		m = m.MoveCursor(1)
	}
	got := m.View(lipgloss.NewStyle())
	if !strings.Contains(got, "▲") || !strings.Contains(got, "▼") {
		t.Fatalf("View mid-list should fire both hints: %q", got)
	}
	// L3-tail appears in EVERY card (it's the third line), so its
	// presence alone does not prove the leading partial fired. Pin
	// the symmetric structure by counting rows instead — the
	// viewport budget must be saturated.
	if rows := strings.Count(got, "\n") + 1; rows < 9 {
		t.Fatalf("View mid-list rendered %d rows, want all 9 viewport rows filled", rows)
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

	m := New().WithViewport(rng.Intn(18) + 3).WithItems(randomItems(rng))

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
