package screenlayout

import (
	"math/rand"
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

// maskOf is every step-th index below n, offset by from — the sparse shape a
// projection has: nodes with unselectable separators between them.
func maskOf(n, from, step int) []int {
	var out []int
	for i := from; i < n; i += step {
		out = append(out, i)
	}
	return out
}

// maskedFeed is a scrollable section of n single-line items whose cursor may
// only land on `mask`.
func maskedFeed(id ID, n int, mask []int) Func {
	items := numbered("row", n)
	return Func{
		Def:  Spec{ID: id, MinWidth: 20, MinRows: 3, Weight: 1, Scroll: ScrollItems, SelectFirst: true},
		Body: func(Canvas) Block { return Block{Items: items, Selectable: mask} },
	}
}

func cursorOf(kit screenkit.Kit, state State, id ID, sections ...Section) int {
	p, ok := Arrange(kit, state, sections...).Placement(id)
	if !ok {
		return -2
	}
	return p.Cursor
}

func press(t *testing.T, kit screenkit.Kit, state State, key string, sections ...Section) State {
	t.Helper()
	next, handled := state.HandleKey(kit, key, sections...)
	if !handled {
		t.Fatalf("key %q was not handled", key)
	}
	return next
}

// TestAnAbsentMaskChangesNothing is the no-op half of the contract, stated three
// ways: the field unset, the field set to an empty slice, and the field set to a
// list that addresses nothing. All three must step ±1 exactly as before.
//
// The rest of this package's test files are the other half of this proof: they
// were not modified when the mask landed, and they still pass.
func TestAnAbsentMaskChangesNothing(t *testing.T) {
	kit := testKit(100, 30, 2)
	plain := feed("feed", 40)
	masks := map[string][]int{
		"nil":           nil,
		"empty":         {},
		"all dangling":  {40, 41, 99},
		"all negative":  {-3, -1},
		"every item":    maskOf(40, 0, 1),
		"unset control": nil,
	}
	for name, mask := range masks {
		t.Run(name, func(t *testing.T) {
			masked := maskedFeed("feed", 40, mask)
			state, want := NewState(), NewState()
			for _, key := range []string{"j", "j", "j", "pgdown", "j", "k", "pgup", "G", "k", "g"} {
				state = press(t, kit, state, key, masked)
				want = press(t, kit, want, key, plain)
				if got, expected := cursorOf(kit, state, "feed", masked), cursorOf(kit, want, "feed", plain); got != expected {
					t.Fatalf("after %q the masked section is on item %d and the unmasked one on %d", key, got, expected)
				}
			}
		})
	}
}

func TestSingleStepsMoveByMemberNotByItem(t *testing.T) {
	kit := testKit(100, 30, 2)
	// Members at 0, 4, 8, ... : three unselectable items between each pair, so a
	// ±1 cursor would stop on item 1 and this test would read 1 instead of 4.
	sec := maskedFeed("feed", 40, maskOf(40, 0, 4))

	state := NewState()
	for step, want := range []int{4, 8, 12} {
		state = press(t, kit, state, "j", sec)
		if got := cursorOf(kit, state, "feed", sec); got != want {
			t.Fatalf("j number %d landed on item %d, want member %d", step+1, got, want)
		}
	}
	for step, want := range []int{8, 4, 0} {
		state = press(t, kit, state, "k", sec)
		if got := cursorOf(kit, state, "feed", sec); got != want {
			t.Fatalf("k number %d landed on item %d, want member %d", step+1, got, want)
		}
	}
	// The ends hold: k on the first member and j on the last both stay put,
	// which is what an unmasked cursor clamped at 0 and at count-1 does.
	state = press(t, kit, state, "k", sec)
	if got := cursorOf(kit, state, "feed", sec); got != 0 {
		t.Fatalf("k on the first member moved to %d", got)
	}
	last := press(t, kit, NewState(), "G", sec)
	last = press(t, kit, last, "j", sec)
	if got := cursorOf(kit, last, "feed", sec); got != 36 {
		t.Fatalf("j on the last member moved to %d, want 36", got)
	}
}

func TestFirstAndLastGoToTheEndsOfTheMask(t *testing.T) {
	kit := testKit(100, 30, 2)
	// Neither end of the mask is an end of the item list, so `g` landing on 0 or
	// `G` landing on 39 would be the unmasked answer and is caught here.
	mask := maskOf(40, 3, 5) // 3, 8, 13, 18, 23, 28, 33, 38
	sec := maskedFeed("feed", 40, mask)

	last := press(t, testKit(100, 30, 2), NewState(), "G", sec)
	if got := cursorOf(kit, last, "feed", sec); got != mask[len(mask)-1] {
		t.Fatalf("G landed on item %d, want the last member %d of %d items", got, mask[len(mask)-1], 40)
	}
	first := press(t, kit, last, "g", sec)
	if got := cursorOf(kit, first, "feed", sec); got != mask[0] {
		t.Fatalf("g landed on item %d, want the first member %d", got, mask[0])
	}
}

// TestPageKeysStepTheSameDistanceCountedInMembers pins both halves of the page
// contract: the step is the very integer pageStep hands the unmasked path, and
// what it counts is CURSOR STOPS — which without a mask are items and with one
// are members. The distance is read off an unmasked section of the same shape
// rather than recomputed, so this cannot agree with the implementation by
// sharing its arithmetic.
func TestPageKeysStepTheSameDistanceCountedInMembers(t *testing.T) {
	kit := testKit(100, 30, 2)
	mask := maskOf(120, 0, 3)
	masked := maskedFeed("feed", 120, mask)
	plain := feed("feed", 120)

	unmasked := press(t, kit, NewState(), "pgdown", plain)
	distance := cursorOf(kit, unmasked, "feed", plain)
	if distance < 2 || distance >= len(mask) {
		t.Fatalf("the unmasked page step is %d items; this fixture cannot say anything about paging", distance)
	}

	down := press(t, kit, NewState(), "pgdown", masked)
	got := cursorOf(kit, down, "feed", masked)
	if !onMask(got, mask) {
		t.Fatalf("pgdown landed on item %d, which is not a member of the mask", got)
	}
	if want := mask[distance]; got != want {
		t.Fatalf("pgdown landed on item %d, want member number %d, which is item %d — the page step is the same integer, counted in members", got, distance, want)
	}
	up := press(t, kit, down, "pgup", masked)
	if back := cursorOf(kit, up, "feed", masked); back != mask[0] {
		t.Fatalf("pgup after pgdown landed on item %d, want item %d — the two must be the same distance", back, mask[0])
	}

	// Both ends clamp to the ends of the MASK, not of the item list.
	far := press(t, kit, NewState(), "pgdown", masked)
	for i := 0; i < 100; i++ {
		far = press(t, kit, far, "pgdown", masked)
	}
	if got := cursorOf(kit, far, "feed", masked); got != mask[len(mask)-1] {
		t.Fatalf("paging to the bottom stopped on item %d, want the last member %d", got, mask[len(mask)-1])
	}

	// A mask with only two members still makes progress: a page key with
	// somewhere to go must go there rather than absorbing the step.
	sparse := []int{0, 90}
	two := maskedFeed("feed", 120, sparse)
	jump := press(t, kit, NewState(), "pgdown", two)
	if got := cursorOf(kit, jump, "feed", two); got != 90 {
		t.Fatalf("pgdown over a mask of {0, 90} landed on %d, want 90", got)
	}
	if got := cursorOf(kit, press(t, kit, jump, "pgup", two), "feed", two); got != 0 {
		t.Fatalf("pgup over a mask of {0, 90} landed on %d, want 0", got)
	}
}

// TestASeedOffTheMaskSnapsOntoIt covers the two ways a cursor arrives off the
// mask without a key: SelectFirst seeding item 0 when 0 is not selectable, and a
// screen writing an index of its own through WithCursor.
func TestASeedOffTheMaskSnapsOntoIt(t *testing.T) {
	kit := testKit(100, 30, 2)
	mask := []int{3, 9, 15, 27}
	sec := maskedFeed("feed", 40, mask)

	if got := cursorOf(kit, NewState(), "feed", sec); got != 3 {
		t.Fatalf("SelectFirst seeded item %d on a section whose first member is 3", got)
	}
	for _, seed := range []int{0, 1, 4, 10, 26, 30, 39, 500} {
		state := NewState().WithCursor("feed", seed).ResyncIn(kit, HostBox(kit), sec)
		got := state.Cursor("feed")
		if !onMask(got, mask) {
			t.Fatalf("a cursor seeded at %d resynced to %d, which is off the mask %v", seed, got, mask)
		}
		if painted := cursorOf(kit, state, "feed", sec); painted != got {
			t.Fatalf("a cursor seeded at %d stored %d and painted %d", seed, got, painted)
		}
		// A key pressed straight after the seed must step from the member the
		// seed snapped to, not from the raw index.
		next := press(t, kit, state, "j", sec)
		if moved := next.Cursor("feed"); !onMask(moved, mask) {
			t.Fatalf("j after a seed at %d landed on %d, off the mask %v", seed, moved, mask)
		}
	}
}

// TestTheCursorNeverLandsOffTheMask is the invariant under a random sequence of
// everything that can move it, asserted after EACH op rather than at the end.
// The item set and the geometry both change under the cursor, which is the case
// a resize produces.
func TestTheCursorNeverLandsOffTheMask(t *testing.T) {
	t.Parallel()
	seed := int64(20260820)
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))

	keys := []string{"j", "k", "pgdown", "pgup", "g", "G"}
	state := NewState()
	kit := testKit(100, 30, 2)
	count, step, from := 60, 3, 1
	mask := maskOf(count, from, step)
	sec := maskedFeed("feed", count, mask)

	for op := 0; op < 400; op++ {
		what := rng.Intn(10)
		switch {
		case what < 6:
			key := keys[rng.Intn(len(keys))]
			state = press(t, kit, state, key, sec)
		case what < 8:
			// A refresh that changed the items under the stored cursor.
			count = 1 + rng.Intn(80)
			from, step = rng.Intn(4), 1+rng.Intn(5)
			mask = maskOf(count, from, step)
			sec = maskedFeed("feed", count, mask)
			state = state.ResyncIn(kit, HostBox(kit), sec)
		case what < 9:
			// A resize.
			kit = testKit(20+rng.Intn(140), 8+rng.Intn(50), rng.Intn(4))
			state = state.ResyncIn(kit, HostBox(kit), sec)
		default:
			// A screen seeding a selection of its own, wherever it likes.
			state = state.WithCursor("feed", rng.Intn(count+10)-2)
			state = state.ResyncIn(kit, HostBox(kit), sec)
		}
		stored := state.Cursor("feed")
		if len(mask) == 0 {
			continue
		}
		if !onMask(stored, mask) {
			t.Fatalf("op %d left the stored cursor on item %d, off a mask of %d members over %d items", op, stored, len(mask), count)
		}
		if painted := cursorOf(kit, state, "feed", sec); painted != stored {
			t.Fatalf("op %d stored cursor %d but painted %d", op, stored, painted)
		}
	}
}

// TestAMaskIsNormalisedRatherThanBelieved is the same rule Block.Heights is held
// to: a section's statement about itself is checked against what it produced.
func TestAMaskIsNormalisedRatherThanBelieved(t *testing.T) {
	cases := map[string]struct {
		mask  []int
		count int
		want  []int
	}{
		"already normal":     {[]int{0, 2, 4}, 6, []int{0, 2, 4}},
		"dangling tail":      {[]int{0, 2, 40}, 6, []int{0, 2}},
		"negative dropped":   {[]int{-5, 1}, 6, []int{1}},
		"duplicates dropped": {[]int{1, 1, 3, 3}, 6, []int{1, 3}},
		"out of order":       {[]int{4, 1, 5}, 6, []int{4, 5}},
		"no items":           {[]int{0, 1}, 0, nil},
		"nothing addresses":  {[]int{9, 10}, 6, nil},
		"empty":              {nil, 6, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := normalizeMask(tc.mask, tc.count)
			if len(got) != len(tc.want) {
				t.Fatalf("normalizeMask(%v, %d) = %v, want %v", tc.mask, tc.count, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("normalizeMask(%v, %d) = %v, want %v", tc.mask, tc.count, got, tc.want)
				}
			}
		})
	}
}

// TestAMaskedItemIsStillPaintedAndStillCounted is the negative half of the
// contract: the mask is not a filter. Every item still occupies its rows, the
// window still counts it, and the hints still hide it.
func TestAMaskedItemIsStillPaintedAndStillCounted(t *testing.T) {
	kit := testKit(100, 20, 2)
	mask := maskOf(60, 0, 4)
	masked := maskedFeed("feed", 60, mask)
	plain := feed("feed", 60)

	a, _ := Arrange(kit, NewState(), masked).Placement("feed")
	b, _ := Arrange(kit, NewState(), plain).Placement("feed")
	if len(a.Items) != len(b.Items) || len(a.Items) != 60 {
		t.Fatalf("the masked section reported %d items and the unmasked one %d; a mask is not a filter", len(a.Items), len(b.Items))
	}
	if a.Above != b.Above || a.Below != b.Below || a.First != b.First || a.Last != b.Last {
		t.Fatalf("the mask moved the window: masked [%d,%d] +%d/-%d, unmasked [%d,%d] +%d/-%d",
			a.First, a.Last, a.Above, a.Below, b.First, b.Last, b.Above, b.Below)
	}
	for i := range a.Heights {
		if a.Heights[i] != b.Heights[i] {
			t.Fatalf("item %d measured %d rows masked and %d unmasked", i, a.Heights[i], b.Heights[i])
		}
	}
}

func onMask(cursor int, mask []int) bool {
	for _, index := range mask {
		if index == cursor {
			return true
		}
	}
	return false
}
