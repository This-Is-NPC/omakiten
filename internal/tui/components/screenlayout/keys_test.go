package screenlayout

import (
	"sort"
	"strings"
	"testing"
)

func staticSection(id ID) Func {
	return listSection(id, Spec{ID: id, MinWidth: 20, MinRows: 4}, numbered("s", 4))
}

func TestAScrollableSectionGetsEveryStandardKeyWithoutDeclaringOne(t *testing.T) {
	// Acceptance criterion 5. The screen declares a POLICY; the keys come with
	// it. There is no list of key strings on the screen to be short by one.
	kit := testKit(100, 20, 2)
	sec := feed("feed", 200)
	for _, binding := range StandardBindings() {
		if binding.Action == ActionNextSection || binding.Action == ActionPrevSection {
			continue
		}
		for _, key := range binding.Keys {
			if _, handled := NewState().HandleKey(kit, key, sec); !handled {
				t.Errorf("key %q (%v) was not handled by a section declaring ScrollItems", key, binding.Action)
			}
		}
	}
}

func TestANonScrollableSectionTakesNoKeys(t *testing.T) {
	kit := testKit(100, 20, 2)
	if _, handled := NewState().HandleKey(kit, "down", staticSection("static")); handled {
		t.Fatal("a ScrollNone section swallowed a scroll key")
	}
}

func TestAnUnboundKeyIsLeftForTheScreen(t *testing.T) {
	kit := testKit(100, 20, 2)
	for _, key := range []string{"enter", "e", "esc", "?", "/"} {
		if _, handled := NewState().HandleKey(kit, key, feed("feed", 50)); handled {
			t.Errorf("the arranger swallowed %q, which is the screen's key", key)
		}
	}
}

func TestTheStandardKeysMoveTheCursorTheWayTheRestOfTheTuiDoes(t *testing.T) {
	kit := testKit(100, 30, 2)
	sec := feed("feed", 200)
	cursor := func(s State) int { p, _ := Arrange(kit, s, sec).Placement("feed"); return p.Cursor }

	down, _ := NewState().HandleKey(kit, "j", sec)
	if got := cursor(down); got != 1 {
		t.Fatalf("j from item 0 landed on item %d, want 1", got)
	}
	up, _ := down.HandleKey(kit, "k", sec)
	if got := cursor(up); got != 0 {
		t.Fatalf("k landed on item %d, want 0", got)
	}
	last, _ := NewState().HandleKey(kit, "G", sec)
	if got := cursor(last); got != 199 {
		t.Fatalf("G landed on item %d, want the last of 200", got)
	}
	first, _ := last.HandleKey(kit, "g", sec)
	if got := cursor(first); got != 0 {
		t.Fatalf("g landed on item %d, want 0", got)
	}
	page, _ := NewState().HandleKey(kit, "pgdown", sec)
	if got := cursor(page); got <= 1 {
		t.Fatalf("pgdown moved to item %d, want more than one item", got)
	}
}

func TestFocusStartsOnTheFirstScrollableSectionAndTabCyclesIt(t *testing.T) {
	kit := testKit(100, 30, 2)
	sections := []Section{staticSection("form"), feed("a", 60), feed("b", 60)}

	moved, _ := NewState().HandleKey(kit, "down", sections...)
	if a, _ := Arrange(kit, moved, sections...).Placement("a"); a.Cursor != 1 {
		t.Fatalf("with no focus set, down moved section a to item %d, want 1 — focus should default to the first scrollable section", a.Cursor)
	}

	tabbed, handled := moved.HandleKey(kit, "tab", sections...)
	if !handled {
		t.Fatal("tab was not handled with two scrollable sections")
	}
	if tabbed.Focus() != "b" {
		t.Fatalf("tab moved focus to %q, want b", tabbed.Focus())
	}
	after, _ := tabbed.HandleKey(kit, "down", sections...)
	res := Arrange(kit, after, sections...)
	a, _ := res.Placement("a")
	b, _ := res.Placement("b")
	if b.Cursor != 1 {
		t.Fatalf("after tab, down moved section b to item %d, want 1", b.Cursor)
	}
	if a.Cursor != 1 {
		t.Fatalf("down after tab also moved the unfocused section a to item %d", a.Cursor)
	}

	// Two scrollable sections: a second tab wraps. There is no reverse key.
	wrapped, handled := tabbed.HandleKey(kit, "tab", sections...)
	if !handled {
		t.Fatal("tab did not wrap with two scrollable sections")
	}
	if wrapped.Focus() != "a" {
		t.Fatalf("wrapping tab moved focus to %q, want a", wrapped.Focus())
	}
}

func TestTabIsNotClaimedWhenThereIsNothingToCycleTo(t *testing.T) {
	kit := testKit(100, 30, 2)
	if _, handled := NewState().HandleKey(kit, "tab", feed("only", 40)); handled {
		t.Fatal("tab was claimed with a single scrollable section; the screen may want it")
	}
}

func TestKeyHintsAreDerivedFromTheSameTableThatHandlesTheKeys(t *testing.T) {
	// The footer a screen shows and the keys the arranger accepts come from one
	// table, so a screen cannot advertise a key the arranger does not take, or
	// take one it does not advertise.
	handled := map[string]bool{}
	for _, b := range StandardBindings() {
		for _, k := range b.Keys {
			handled[k] = true
		}
	}
	hints := KeyHints(feed("a", 10), feed("b", 10))
	if len(hints) == 0 {
		t.Fatal("no key hints for a body with two scrollable sections")
	}
	for _, hint := range hints {
		if hint.LabelKey == "" {
			t.Errorf("hint %q has no i18n label key", hint.Footer)
		}
		for _, k := range hint.Keys {
			if !handled[k] {
				t.Errorf("hint advertises %q, which HandleKey does not accept", k)
			}
		}
	}
}

func TestKeyHintsAreEmptyWhenNothingScrolls(t *testing.T) {
	if got := KeyHints(staticSection("a"), staticSection("b")); len(got) != 0 {
		t.Fatalf("a body with nothing scrollable advertised %d keys", len(got))
	}
}

func TestSectionCyclingIsAdvertisedOnlyWhenThereAreTwoSectionsToCycle(t *testing.T) {
	has := func(hints []Binding, action Action) bool {
		for _, h := range hints {
			if h.Action == action {
				return true
			}
		}
		return false
	}
	if has(KeyHints(feed("only", 10)), ActionNextSection) {
		t.Fatal("tab advertised with a single scrollable section")
	}
	if !has(KeyHints(feed("a", 10), feed("b", 10)), ActionNextSection) {
		t.Fatal("tab not advertised with two scrollable sections")
	}
}

func TestNoKeyIsBoundToTwoActions(t *testing.T) {
	seen := map[string]Action{}
	for _, b := range StandardBindings() {
		if len(b.Keys) == 0 {
			t.Errorf("action %v has no keys", b.Action)
		}
		for _, k := range b.Keys {
			if prior, dup := seen[k]; dup {
				t.Errorf("key %q is bound to both %v and %v", k, prior, b.Action)
			}
			seen[k] = b.Action
		}
	}
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// The set the rest of the TUI already uses, so a migrated screen keeps its
	// keys rather than acquiring a private dialect.
	want := "G,ctrl+d,ctrl+u,down,end,g,home,j,k,pgdn,pgdown,pgup,tab,up"
	if got := strings.Join(keys, ","); got != want {
		t.Fatalf("standard key set is\n  %s\nwant\n  %s", got, want)
	}
}

// TestPagingABodyScrollSurfaceNeverStepsOverAnItem is the defect Project's
// migration found (#2425), pinned where it lives.
//
// ActionPageDown moved the offset by `itemViewport`, which is a ROW count, and
// the offset is an ITEM index. On a section whose window spends part of its
// allocation on the "▲ N above" / "▼ N below" hints, fewer items are SHOWN than
// the viewport has rows — so a page stepped clean over the items the hint rows
// displaced, and on a short section that stranded them permanently: paging is
// the only key a user reaches for on a body-scroll surface, and no page landed
// on them.
//
// It is the same defect this package exists to delete, in its own code: a
// second, hand-derived copy of a number the renderer already knew. Project's
// pre-migration screen even had the fix — `screenkit.ScrollDataRows(rows)`,
// with the comment "paging by the full budget would step clean over the lines
// those hints displaced" — and the migration would have dropped it.
//
// The window the arranger just painted reports how many items it showed, so
// there is nothing to derive.
func TestPagingABodyScrollSurfaceNeverStepsOverAnItem(t *testing.T) {
	// Four rows for the window, one-line items, no cursor: the section is a
	// body-scroll surface, so paging moves its offset. With both hints showing
	// only two of the four rows carry items.
	items := numbered("row", 40)
	sec := Func{
		Def:  Spec{ID: "body", MinWidth: 20, MinRows: 6, MaxRows: 6, Scroll: ScrollItems},
		Body: func(Canvas) Block { return Block{Items: items, Cursor: NoSelection()} },
	}
	kit := testKit(60, 20, 2)

	seen := map[string]bool{}
	state := NewState()
	for press := 0; press < 60; press++ {
		result := Arrange(kit, state, sec)
		placement, _ := result.Placement("body")
		for i := placement.First; i <= placement.Last && i < len(items); i++ {
			seen[items[i]] = true
		}
		next, handled := state.HandleKey(kit, "pgdown", sec)
		if !handled {
			t.Fatal("the arranger declined pgdown on a scrollable section; the measurement is vacuous")
		}
		if next.Offset("body") == state.Offset("body") {
			break
		}
		state = next
	}

	var missed []string
	for _, item := range items {
		if !seen[item] {
			missed = append(missed, item)
		}
	}
	if len(missed) > 0 {
		t.Errorf("paging to the end of a body-scroll surface never showed %d of %d items (first: %q); the page step is charging the hint rows to the content",
			len(missed), len(items), missed[0])
	}
}
