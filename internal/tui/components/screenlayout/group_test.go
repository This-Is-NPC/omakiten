package screenlayout

import (
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenkit"
)

// ---------------------------------------------------------------------------
// Spec.Group (#2446, shape 2 of 3).
//
// Both screens this package was designed against compose their wide branch as
// TWO columns, the left one holding two stacked zones:
//
//	Project:     JoinHorizontal(JoinVertical(meta, "", dashboard), "  ", activity)
//	Task Detail: JoinHorizontal(JoinVertical(form, subtasks),      "  ", activity)
//
// `fitsSideBySide` required EVERY section to set Column and `Arrange` gave every
// undropped section its own column, so a flat set of three sections could only
// be three columns beside each other. Two levels of composition had no spelling.
//
// Folding the two stacked zones into a single section IS expressible, and it is
// the wrong answer: one section is one scroll surface and one focus zone, so the
// fold merges two of each. That is a behaviour change, not a migration — which
// is why the shape had to be added rather than worked around.
// ---------------------------------------------------------------------------

// groupedColumn is a member of the stacked column `group`.
func groupedColumn(id, group ID, minWidth, minRows, weight int) Spec {
	return Spec{
		ID: id, Group: group, MinWidth: minWidth, MinRows: minRows, Weight: weight,
		Column: true, Scroll: ScrollItems, ColumnGap: 2,
	}
}

// twoLevelBody is `[form over subtasks] | [activity]` — Task Detail's wide
// branch, and Project's with the names changed.
func twoLevelBody() []Section {
	activity := proportionalColumn("activity")
	activity.ColumnGap = 2
	return []Section{
		listSection("form", groupedColumn("form", "left", 32, 4, 1), numbered("f", 30)),
		listSection("subtasks", groupedColumn("subtasks", "left", 32, 4, 1), numbered("s", 30)),
		listSection("activity", activity, numbered("a", 40)),
	}
}

func TestTwoSectionsCanStackInsideOneColumnBesideAThird(t *testing.T) {
	kit := testKit(120, 40, 2)
	res := Arrange(kit, NewState(), twoLevelBody()...)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	form, _ := res.Placement("form")
	subtasks, _ := res.Placement("subtasks")
	activity, _ := res.Placement("activity")

	// The second level: subtasks starts BELOW the form, not beside it.
	if subtasks.TopLine <= form.TopLine {
		t.Errorf("the form starts on body line %d and the subtasks on %d; they are not stacked",
			form.TopLine, subtasks.TopLine)
	}
	// The first level: activity starts LEVEL with the form. Three flat columns
	// would put all three on line 1; a flat stack would put all three on lines
	// of their own. This body is neither.
	if activity.TopLine != form.TopLine {
		t.Errorf("the activity column starts on body line %d and the form on %d; they are not side by side",
			activity.TopLine, form.TopLine)
	}
	// One column means one width, and the two members share it.
	if form.Width != subtasks.Width {
		t.Errorf("the stacked members got %d and %d columns; a column has one width",
			form.Width, subtasks.Width)
	}
	if total := form.Width + activity.Width + 2; total != kit.AvailableWidth() {
		t.Errorf("the two columns and their declared 2-column gap spend %d of the %d available",
			total, kit.AvailableWidth())
	}
	t.Logf("form on body line %d width %d, subtasks stacked under it on %d, activity beside both on %d width %d",
		form.TopLine, form.Width, subtasks.TopLine, activity.TopLine, activity.Width)
}

func TestStackedMembersKeepSeparateFocusZonesAndSeparateScrollSurfaces(t *testing.T) {
	// The reason the fold-into-one-section workaround was rejected. Grouping is
	// a layout statement and nothing else: each member keeps its own offset, its
	// own cursor and its own turn in the focus cycle.
	kit := testKit(120, 40, 2)
	sections := twoLevelBody()

	// Every member is still its own focus zone, reachable by tab.
	seen := map[ID]bool{}
	state := NewState().WithFocus("form")
	for press := 0; press < 3; press++ {
		seen[state.Focus()] = true
		next, handled := state.HandleKey(kit, "tab", sections...)
		if !handled {
			t.Fatal("tab was not handled; the focus cycle is vacuous")
		}
		state = next
	}
	for _, id := range []ID{"form", "subtasks", "activity"} {
		if !seen[id] {
			t.Errorf("tab never reached %q; the stacked members do not each hold focus", id)
		}
	}

	// And its own scroll surface: paging one member moves that member alone.
	moved := NewState().WithFocus("subtasks")
	for press := 0; press < 6; press++ {
		moved, _ = moved.HandleKey(kit, "pgdown", sections...)
	}
	if moved.Cursor("subtasks") == 0 {
		t.Fatal("paging the focused member moved nothing; the measurement is vacuous")
	}
	if moved.Offset("form") != 0 || moved.Offset("activity") != 0 {
		t.Errorf("paging the subtasks also moved the form to offset %d and the activity to %d; the members share a scroll surface",
			moved.Offset("form"), moved.Offset("activity"))
	}
}

func TestAStackedColumnSplitsItsRowsBetweenItsMembers(t *testing.T) {
	kit := testKit(120, 40, 2)
	res := Arrange(kit, NewState(), twoLevelBody()...)
	form, _ := res.Placement("form")
	subtasks, _ := res.Placement("subtasks")
	activity, _ := res.Placement("activity")

	height := BodyRows(kit) - leadingBlankRows
	if activity.Rows != height {
		t.Errorf("the ungrouped column got %d rows, want the whole body height %d", activity.Rows, height)
	}
	if got := form.Rows + subtasks.Rows; got != height {
		t.Errorf("the two stacked members got %d + %d = %d rows, want the column's whole height %d",
			form.Rows, subtasks.Rows, got, height)
	}
	if form.Rows <= 0 || subtasks.Rows <= 0 {
		t.Errorf("a stacked member got no rows: form %d, subtasks %d", form.Rows, subtasks.Rows)
	}
}

func TestAStackedColumnSpendsItsWholeHeightAtEveryContentShape(t *testing.T) {
	// The row-budget equality, restated for the shape grouping introduces. A
	// column of two members is the first place side by side has slack to move:
	// a member with less content than rows sits ABOVE a member hiding content,
	// which is exactly the transfer reclaimSlack was written for and exactly
	// what "columns are not fungible" said could never happen here.
	kit := testKit(140, 30, 2)
	for top := 2; top <= 8; top++ {
		for bottom := 2; bottom <= 8; bottom++ {
			assertStackedShape(t, kit, top, bottom)
		}
	}
}

func assertStackedShape(t *testing.T, kit screenkit.Kit, top, bottom int) {
	res := Arrange(kit, NewState(),
		Func{Def: groupedColumn("top", "left", 40, 4, 1), Body: constantBody(cards("t", 40, top))},
		Func{Def: groupedColumn("bottom", "left", 40, 4, 1), Body: constantBody(cards("b", 40, bottom))},
		Func{Def: groupedColumn("side", "", 40, 4, 1), Body: constantBody(cards("s", 40, 3))},
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("cards %d+%d: arrangement = %v, want SideBySide", top, bottom, res.Arrangement)
	}
	if got := res.Rows(); got != BodyRows(kit) {
		t.Fatalf("cards %d+%d: the body paints %d rows against a %d-row budget",
			top, bottom, got, BodyRows(kit))
	}
	for i, line := range lines(res.View) {
		if got := len([]rune(line)); got > kit.AvailableWidth() {
			t.Fatalf("cards %d+%d: body line %d is %d cells wide, past the %d available",
				top, bottom, i, got, kit.AvailableWidth())
		}
	}
}

func TestAStackedColumnReclaimsRowsAMemberDidNotUse(t *testing.T) {
	// The transfer itself, isolated: a stub member with two rows of content
	// beside — above — a feed that is hiding items. Before grouping this could
	// not arise side by side, so reclaimSlack ran only on the stacked body.
	kit := testKit(140, 30, 2)
	res := Arrange(kit, NewState(),
		Func{Def: groupedColumn("stub", "left", 40, 3, 1), Body: constantBody(numbered("s", 2))},
		Func{Def: groupedColumn("feed", "left", 40, 3, 1), Body: constantBody(cards("f", 60, 4))},
		Func{Def: groupedColumn("side", "", 40, 3, 1), Body: constantBody(numbered("o", 10))},
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	stub, _ := res.Placement("stub")
	feed, _ := res.Placement("feed")
	if feed.Below == 0 {
		t.Fatal("the feed is not hiding anything, so there is no recipient and the test proves nothing")
	}
	// The stub paints two rows of content. Everything else in the column is the
	// feed's, so the feed's allocation is the column height less those two —
	// which is only true if the transfer happened. Its own even share of a
	// 25-row column is 12.
	const stubPaints = 2
	height := BodyRows(kit) - leadingBlankRows
	if want := height - stubPaints; feed.Rows != want {
		t.Fatalf("the feed was assigned %d rows of a %d-row column holding a %d-row stub, want %d; the rows the stub could not use went nowhere",
			feed.Rows, height, stubPaints, want)
	}
	if feed.Rows <= height/2 {
		t.Fatalf("the feed got %d rows, no more than its even share of %d; nothing was transferred", feed.Rows, height/2)
	}
	// The donor keeps its assignment on paper — reclaimSlack moves the rows the
	// donor could not USE, it does not renegotiate the split — and the column
	// still paints exactly its height, which is checked next door across every
	// content shape.
	if stub.Rows == 0 {
		t.Fatal("the stub was assigned no rows at all; the fixture is not a transfer")
	}
}

func TestAStackedColumnTakesTheTightestWidthDeclarationOfItsMembers(t *testing.T) {
	// A column has one width and its members declare it jointly. The rule is
	// one sentence: the tightest floor, the tightest ceiling, and the strongest
	// claim on the surplus — so no member is ever placed narrower than it asked
	// or wider than it allowed.
	narrow := groupedColumn("narrow", "left", 30, 3, 1)
	wide := groupedColumn("wide", "left", 48, 3, 4)
	wide.MaxWidth = 70
	narrow.MaxWidth = 60
	got := groupColumnSpec([]Spec{narrow.normalize(), wide.normalize()})
	if got.MinWidth != 48 {
		t.Errorf("the column's minimum is %d, want the widest member's 48", got.MinWidth)
	}
	if got.MaxWidth != 60 {
		t.Errorf("the column's maximum is %d, want the tightest member's 60", got.MaxWidth)
	}
	if got.Weight != 4 {
		t.Errorf("the column's weight is %d, want the strongest member's 4", got.Weight)
	}
	// A member that declared no ceiling does not remove one a sibling declared.
	open := groupedColumn("open", "left", 30, 3, 1)
	if capped := groupColumnSpec([]Spec{open.normalize(), wide.normalize()}); capped.MaxWidth != 70 {
		t.Errorf("a member declaring no maximum left the column at %d, want the sibling's 70", capped.MaxWidth)
	}
	// And a proportion declared by any member is the column's.
	proportional := groupedColumn("proportional", "left", 30, 3, 1)
	proportional.WidthPercent = 45
	if share := groupColumnSpec([]Spec{open.normalize(), proportional.normalize()}); share.WidthPercent != 45 {
		t.Errorf("the column's proportion is %d%%, want the declared 45%%", share.WidthPercent)
	}
	// The two tightest rules can contradict each other — one member's ceiling
	// below another's floor — and the floor wins, the same way Spec.normalize
	// settles it for a single section. A column narrower than a member is
	// unpaintable; a column wider than a member's comfortable measure is merely
	// less comfortable.
	capped := groupedColumn("capped", "left", 20, 3, 1)
	capped.MaxWidth = 24
	if contradictory := groupColumnSpec([]Spec{capped.normalize(), wide.normalize()}); contradictory.MaxWidth != 48 {
		t.Errorf("a ceiling of 24 below a sibling's floor of 48 resolved to %d, want the floor 48",
			contradictory.MaxWidth)
	}
}

func TestAStackedColumnWhoseMemberDidNotOptInKeepsTheBodyStacked(t *testing.T) {
	// Column is the breakpoint opt-in and grouping does not launder it: a
	// member that never asked to be placed beside anything keeps its whole
	// column — and therefore the whole body — stacked.
	reluctant := groupedColumn("reluctant", "left", 40, 3, 1)
	reluctant.Column = false
	res := Arrange(testKit(200, 40, 2), NewState(),
		listSection("willing", groupedColumn("willing", "left", 40, 3, 1), numbered("w", 20)),
		listSection("reluctant", reluctant, numbered("r", 20)),
		listSection("side", groupedColumn("side", "", 40, 3, 1), numbered("s", 20)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked — one member of the grouped column did not opt in", res.Arrangement)
	}
}

func TestAStackedColumnIsSizedByItsProportionLikeAnyOther(t *testing.T) {
	// Grouping and proportion compose: the column takes 45% of the width and
	// its two members share it.
	kit := testKit(200, 40, 2) // available 196
	top := groupedColumn("top", "feed", 44, 3, 1)
	top.WidthPercent, top.MaxWidth = 45, 96
	bottom := groupedColumn("bottom", "feed", 44, 3, 1)
	res := Arrange(kit, NewState(),
		listSection("top", top, numbered("t", 20)),
		listSection("bottom", bottom, numbered("b", 20)),
		listSection("meta", groupedColumn("meta", "", 32, 3, 1), numbered("m", 20)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	for _, id := range []ID{"top", "bottom"} {
		p, _ := res.Placement(id)
		if p.Width != 88 {
			t.Errorf("member %s got %d columns, want the column's 45%% of 196", id, p.Width)
		}
	}
	meta, _ := res.Placement("meta")
	if want := 196 - 88 - 2; meta.Width != want {
		t.Errorf("the ungrouped column got %d, want the rest %d", meta.Width, want)
	}
}

func TestSectionsInOneGroupAreOneColumnAndThereforeStack(t *testing.T) {
	// Side by side needs two columns. Putting every section in one group leaves
	// one column, and one column is a stack — the same answer a single section
	// has always got.
	res := Arrange(testKit(200, 30, 2), NewState(),
		listSection("a", groupedColumn("a", "only", 40, 3, 1), numbered("a", 20)),
		listSection("b", groupedColumn("b", "only", 40, 3, 1), numbered("b", 20)),
	)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked — two members of one group are one column", res.Arrangement)
	}
}

func TestAGroupIsIgnoredWhenTheBodyStacks(t *testing.T) {
	// Stacked there is one column already, so a group says nothing new. Every
	// section takes the full width and its own share of the rows, exactly as it
	// would with no group declared.
	kit := testKit(70, 30, 2) // too narrow for the columns to fit
	sections := twoLevelBody()
	res := Arrange(kit, NewState(), sections...)
	if res.Arrangement != Stacked {
		t.Fatalf("arrangement = %v, want Stacked", res.Arrangement)
	}
	previous := -1
	for _, p := range res.Placements {
		if p.Width != kit.AvailableWidth() && p.Spec.MaxWidth == 0 {
			t.Errorf("stacked section %s got %d columns, want the whole %d available",
				p.ID, p.Width, kit.AvailableWidth())
		}
		if p.TopLine <= previous {
			t.Errorf("stacked section %s starts on body line %d, not below its predecessor's %d",
				p.ID, p.TopLine, previous)
		}
		previous = p.TopLine
	}
}

func TestUngroupedSectionsAreStillOneColumnEach(t *testing.T) {
	// Criterion 6's grouping half: a body that declares no group is arranged
	// exactly as it was — every section its own column, every column on body
	// line 1.
	res := Arrange(testKit(200, 40, 2), NewState(),
		listSection("a", columnSpec("a", 40), numbered("a", 40)),
		listSection("b", columnSpec("b", 40), numbered("b", 40)),
		listSection("c", columnSpec("c", 40), numbered("c", 40)),
	)
	if res.Arrangement != SideBySide {
		t.Fatalf("arrangement = %v, want SideBySide", res.Arrangement)
	}
	for _, p := range res.Placements {
		if p.TopLine != 1 {
			t.Errorf("column %s starts on body line %d, want 1", p.ID, p.TopLine)
		}
	}
}

func TestAStackedMemberTheColumnCannotAffordIsDroppedNotOverdrawn(t *testing.T) {
	// A column too short for every member's minimum drops from the bottom up,
	// the same rule the stacked body has always applied — and the dropped
	// member's siblings still spend the column exactly.
	for height := 4; height <= 14; height++ {
		kit := testKit(140, height, 2)
		res := Arrange(kit, NewState(),
			Func{Def: groupedColumn("one", "left", 40, 5, 1), Body: constantBody(cards("1", 20, 3))},
			Func{Def: groupedColumn("two", "left", 40, 5, 1), Body: constantBody(cards("2", 20, 3))},
			Func{Def: groupedColumn("three", "left", 40, 5, 1), Body: constantBody(cards("3", 20, 3))},
			Func{Def: groupedColumn("side", "", 40, 3, 1), Body: constantBody(cards("s", 20, 3))},
		)
		if got, budget := res.Rows(), BodyRows(kit); got > budget {
			t.Fatalf("height %d: the body paints %d rows into a %d-row budget", height, got, budget)
		}
		for _, p := range res.Placements {
			if p.Dropped && p.Rows != 0 {
				t.Fatalf("height %d: dropped member %s was still given %d rows", height, p.ID, p.Rows)
			}
		}
	}
}

func TestAStackedColumnsMembersPaintUnderOneAnotherInDeclarationOrder(t *testing.T) {
	// The placements say where each member starts; this reads the same answer
	// off the rendered string, so the two cannot disagree about the shape the
	// user sees.
	kit := testKit(140, 30, 2)
	res := Arrange(kit, NewState(),
		Func{Def: groupedColumn("top", "left", 40, 4, 1), Body: constantBody([]string{"TOPMARK"})},
		Func{Def: groupedColumn("bottom", "left", 40, 4, 1), Body: constantBody([]string{"BOTMARK"})},
		Func{Def: groupedColumn("side", "", 40, 4, 1), Body: constantBody([]string{"SIDEMARK"})},
	)
	body := lines(res.View)
	at := func(needle string) int {
		for i, line := range body {
			if strings.Contains(line, needle) {
				return i
			}
		}
		return -1
	}
	top, bottom, side := at("TOPMARK"), at("BOTMARK"), at("SIDEMARK")
	if top < 0 || bottom < 0 || side < 0 {
		t.Fatalf("could not find all three markers (top %d, bottom %d, side %d) in:\n%s", top, bottom, side, res.View)
	}
	if bottom <= top {
		t.Errorf("the bottom member paints on line %d and the top on %d", bottom, top)
	}
	if side != top {
		t.Errorf("the side column paints on line %d and the top member on %d", side, top)
	}
	for _, id := range []ID{"top", "bottom", "side"} {
		p, _ := res.Placement(id)
		marker := map[ID]string{"top": "TOPMARK", "bottom": "BOTMARK", "side": "SIDEMARK"}[id]
		if got := at(marker); got != p.TopLine {
			t.Errorf("%s reports TopLine %d and paints its first line on %d", id, p.TopLine, got)
		}
	}
	if t.Failed() {
		t.Logf("body:\n%s", res.View)
	}
}
