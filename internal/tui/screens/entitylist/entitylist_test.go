package entitylist

import (
	"strings"
	"testing"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

type entityListCase struct {
	descriptor Descriptor
	persona    bool
	listOnly   bool
	template   bool
}

func TestParameterizedContractAndPerKindOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 92, 24)
	cases := map[string]entityListCase{
		"laws":      {descriptor: Laws()},
		"personas":  {descriptor: Personas(), persona: true},
		"skills":    {descriptor: Skills()},
		"templates": {descriptor: Templates(), template: true},
		"tags":      {descriptor: Tags(), listOnly: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assertParameterizedEntityList(t, frame, name, tc)
		})
	}
}

func assertParameterizedEntityList(t *testing.T, frame screenhost.Frame, name string, tc entityListCase) {
	screen := New(tc.descriptor).Bind(testItems(name), nil)
	if screen.ID() != tc.descriptor.ID || !screen.OwnsFooter() || !screen.ResetOnNavigation() {
		t.Fatalf("contract incomplete: id=%q footer=%v reset=%v", screen.ID(), screen.OwnsFooter(), screen.ResetOnNavigation())
	}
	for _, key := range []string{"j", "k", "enter", "n", "e", "d", "r", "t", "c"} {
		if !screen.OwnsKey(screentest.Key(key)) {
			t.Errorf("does not own %q", key)
		}
	}
	selected := screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if selected.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", selected.Cursor())
	}
	if tc.listOnly {
		assertEntityListActionsDisabled(t, frame, selected)
		return
	}
	assertEntityListActions(t, frame, selected, name, tc.template, tc.persona)
}

func assertEntityListActionsDisabled(t *testing.T, frame screenhost.Frame, screen Screen) {
	for _, key := range []string{"enter", "n", "e"} {
		if got := screen.Update(frame, screentest.Key(key)).Action.Kind; got != screenhost.ActionNone {
			t.Errorf("%s action = %v, want none", key, got)
		}
	}
}

func assertEntityListActions(t *testing.T, frame screenhost.Frame, screen Screen, name string, template, persona bool) {
	wants := map[string]screenhost.ActionKind{
		"enter": screenhost.ActionOpenEntity,
		"n":     screenhost.ActionCreateEntity,
		"e":     screenhost.ActionEditEntity,
		"r":     screenhost.ActionReload,
	}
	if template {
		wants["n"] = screenhost.ActionTemplateCreateHint
	}
	for key, want := range wants {
		out := screen.Update(frame, screentest.Key(key))
		if out.Action.Kind != want {
			t.Errorf("%s action = %v, want %v", key, out.Action.Kind, want)
		}
		if key != "n" && key != "r" && out.Action.Value != name+"-2" {
			t.Errorf("%s slug = %q", key, out.Action.Value)
		}
	}
	if got := screen.Update(frame, screentest.Key("p")).Action.Kind; (got == screenhost.ActionOpenPersonaSkills) != persona {
		t.Errorf("persona picker action = %v, persona=%v", got, persona)
	}
}

func TestIndependentCursorReloadResizeEmptyAndError(t *testing.T) {
	frame := screentest.FrameAt(t, 60, 24)
	laws := New(Laws()).Bind(testItems("laws"), nil)
	personas := New(Personas()).Bind(testItems("personas"), nil)
	laws = laws.Update(frame, screentest.Key("G")).Screen.(Screen)
	if laws.Cursor() != 2 || personas.Cursor() != 0 {
		t.Fatalf("independent cursors = laws:%d personas:%d", laws.Cursor(), personas.Cursor())
	}
	laws = laws.Bind([]Item{{Slug: "reloaded", Label: "RELOADED", Badges: []string{"CUSTOM"}}}, nil)
	if laws.Cursor() != 0 || !strings.Contains(screentest.StripANSI(laws.View(frame)), "RELOADED") {
		t.Fatalf("reload did not clamp/project: cursor=%d\n%s", laws.Cursor(), laws.View(frame))
	}
	laws = laws.Lifecycle(screentest.FrameAt(t, 160, 80), screenhost.LifecycleResize).Screen.(Screen)
	if laws.Scroll() != 0 {
		t.Fatalf("wide resize left scroll at %d", laws.Scroll())
	}
	if got := screentest.StripANSI(New(Skills()).View(frame)); !strings.Contains(got, "No items") {
		t.Fatalf("empty state missing:\n%s", got)
	}
	if got := screentest.StripANSI(New(Skills()).Bind(nil, assertError("bundle failed")).View(frame)); !strings.Contains(got, "bundle failed") {
		t.Fatalf("error state missing:\n%s", got)
	}
	if got := screentest.StripANSI(New(Skills()).Loading().View(frame)); !strings.Contains(got, "Loading") {
		t.Fatalf("loading state missing:\n%s", got)
	}
}

func TestAllKeysDeleteChromeLookupAndScrolling(t *testing.T) {
	frame := screentest.FrameAt(t, 60, 18)
	items := make([]Item, 14)
	for i := range items {
		items[i] = Item{Slug: "item-" + string(rune('a'+i)), Label: strings.Repeat("wrapped ", 5)}
	}
	screen := New(Personas()).Bind(items, nil)
	if screen.Kind() != KindPersonas {
		t.Fatalf("kind = %q", screen.Kind())
	}
	assertEntityListDescriptorsAndChrome(t, frame, screen)
	screen = assertEntityListNavigation(t, frame, screen, len(items))
	assertEntityListDeleteFlow(t, frame, screen)
	assertEntityListEmptyInputs(t, frame, screen)
}

func assertEntityListDescriptorsAndChrome(t *testing.T, frame screenhost.Frame, screen Screen) {
	for _, descriptor := range []Descriptor{Laws(), Personas(), Skills(), Templates(), Tags()} {
		got, ok := ForID(descriptor.ID)
		if !ok || got != descriptor {
			t.Errorf("ForID(%q) = %+v, %v", descriptor.ID, got, ok)
		}
	}
	if _, ok := ForID(screenhost.ID("unknown")); ok {
		t.Fatal("unknown id resolved")
	}
	if screen.OwnsKey(screentest.Key("x")) {
		t.Fatal("unexpected key ownership")
	}
	if len(screen.Footer(frame)) != 12 || len(screen.Help(frame)[0].Bindings) != 10 {
		t.Fatalf("chrome mismatch: footer=%d help=%d", len(screen.Footer(frame)), len(screen.Help(frame)[0].Bindings))
	}
	for key, want := range map[string]screenhost.ActionKind{
		"t": screenhost.ActionOpenThemePicker,
		"c": screenhost.ActionOpenConfigPicker,
		"p": screenhost.ActionOpenPersonaSkills,
	} {
		if got := screen.Update(frame, screentest.Key(key)).Action.Kind; got != want {
			t.Errorf("%s action = %v, want %v", key, got, want)
		}
	}
}

func assertEntityListNavigation(t *testing.T, frame screenhost.Frame, screen Screen, itemCount int) Screen {
	for _, key := range []string{"down", "pgdown", "pgdn", "ctrl+d", "up", "pgup", "ctrl+u", "end", "home", "G", "g"} {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if screen.Cursor() != itemCount-1 || screen.Scroll() == 0 {
		t.Fatalf("end navigation did not scroll: cursor=%d scroll=%d", screen.Cursor(), screen.Scroll())
	}
	return screen
}

func assertEntityListDeleteFlow(t *testing.T, frame screenhost.Frame, screen Screen) {
	first := screen.Update(frame, screentest.Key("d"))
	if first.Action.Kind != screenhost.ActionPrepareEntityDelete || len(first.Screen.(Screen).Footer(frame)) != 2 {
		t.Fatalf("first delete = %v footer=%v", first.Action.Kind, first.Screen.(Screen).Footer(frame))
	}
	second := first.Screen.(Screen).Update(frame, screentest.Key("d"))
	if second.Action.Kind != screenhost.ActionDeleteEntity {
		t.Fatalf("second delete = %v", second.Action.Kind)
	}
	cancel := first.Screen.(Screen).Update(frame, screentest.Key("esc"))
	if cancel.Action.Kind != screenhost.ActionCancelEntityDelete {
		t.Fatalf("cancel = %v", cancel.Action.Kind)
	}
}

func assertEntityListEmptyInputs(t *testing.T, frame screenhost.Frame, screen Screen) {
	if got := New(Laws()).Update(frame, screentest.Key("enter")).Action.Value; got != "" {
		t.Fatalf("empty selection slug = %q", got)
	}
	if got := New(Laws()).Update(frame, screentest.Key("esc")).Action.Kind; got != screenhost.ActionNone {
		t.Fatalf("unarmed esc action = %v", got)
	}
	if got := screen.Update(frame, struct{}{}).Screen.(Screen).Cursor(); got != screen.Cursor() {
		t.Fatalf("non-key changed cursor to %d", got)
	}
}

func TestTemplateAndTagOutcomesPreserveKindSpecificPolicyBoundary(t *testing.T) {
	frame := screentest.FrameAt(t, 92, 24)
	templates := New(Templates()).Bind(testItems("templates"), nil)
	if got := templates.Update(frame, screentest.Key("a")).Action.Kind; got != screenhost.ActionOpenTemplateDefault {
		t.Fatalf("template default action = %v", got)
	}
	if got := templates.Update(frame, screentest.Key("n")).Action.Kind; got != screenhost.ActionTemplateCreateHint {
		t.Fatalf("template create action = %v", got)
	}
	if got := templates.Update(frame, screentest.Key("d")).Action.Kind; got != screenhost.ActionTemplateDeleteHint {
		t.Fatalf("template delete action = %v", got)
	}

	tags := New(Tags()).Bind(testItems("tags"), nil)
	for _, key := range []string{"enter", "n", "e"} {
		if got := tags.Update(frame, screentest.Key(key)).Action.Kind; got != screenhost.ActionNone {
			t.Errorf("tag %s action = %v", key, got)
		}
	}
	first := tags.Update(frame, screentest.Key("d"))
	if first.Action.Kind != screenhost.ActionPrepareTagDelete {
		t.Fatalf("tag prepare action = %v", first.Action.Kind)
	}
	if got := first.Screen.(Screen).Update(frame, screentest.Key("d")).Action.Kind; got != screenhost.ActionDeleteTag {
		t.Fatalf("tag delete action = %v", got)
	}
	if got := tags.Update(frame, screentest.Key("D")).Action.Kind; got != screenhost.ActionDeleteOrphanTags {
		t.Fatalf("orphan delete action = %v", got)
	}

	merge := New(Tags()).Bind(testItems("tags"), nil)
	armed := merge.Update(frame, screentest.Key("m"))
	if armed.Action.Kind != screenhost.ActionPrepareTagMerge {
		t.Fatalf("tag merge prepare = %v", armed.Action.Kind)
	}
	armedScreen := armed.Screen.(Screen)
	if armedScreen.MergeSlug() == "" {
		t.Fatal("expected merge source armed")
	}
	// move to second item then confirm
	next := armedScreen.Update(frame, screentest.Key("j"))
	confirm := next.Screen.(Screen).Update(frame, screentest.Key("m"))
	if confirm.Action.Kind != screenhost.ActionMergeTags {
		t.Fatalf("tag merge confirm = %v", confirm.Action.Kind)
	}
	if confirm.Action.SourceValue == "" || confirm.Action.Value == "" || confirm.Action.SourceValue == confirm.Action.Value {
		t.Fatalf("merge action source/target = %q -> %q", confirm.Action.SourceValue, confirm.Action.Value)
	}
}

// TestResizePreservesTheCardCursorAndArmedActions holds the two things a
// resize must not disturb. The cursor is a CARD index and the resize changes
// how many cards go on a line, so it is the moment a fold could be re-read in
// the wrong unit; the armed delete and the armed merge are the screen's own
// state and no part of the layout.
func TestResizePreservesTheCardCursorAndArmedActions(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	tags := New(Tags()).Bind(testItems("tags"), nil)
	tags = tags.Update(frame, screentest.Key("j")).Screen.(Screen)
	if tags.Cursor() != 1 {
		t.Fatalf("card cursor = %d, want 1 before the resize", tags.Cursor())
	}

	deleteArmed := tags.Update(frame, screentest.Key("d")).Screen.(Screen)
	deleteArmed = deleteArmed.Lifecycle(screentest.FrameAt(t, 120, 40), screenhost.LifecycleResize).Screen.(Screen)
	if deleteArmed.Cursor() != 1 || len(deleteArmed.Footer(frame)) != 2 {
		t.Fatalf("delete state lost across the resize: cursor=%d footer=%v", deleteArmed.Cursor(), deleteArmed.Footer(frame))
	}

	mergeArmed := tags.Update(frame, screentest.Key("m")).Screen.(Screen)
	wantMerge := mergeArmed.MergeSlug()
	mergeArmed = mergeArmed.Lifecycle(screentest.FrameAt(t, 120, 40), screenhost.LifecycleResize).Screen.(Screen)
	if mergeArmed.Cursor() != 1 || wantMerge == "" || mergeArmed.MergeSlug() != wantMerge {
		t.Fatalf("merge state lost across the resize: cursor=%d merge=%q, want %q", mergeArmed.Cursor(), mergeArmed.MergeSlug(), wantMerge)
	}
}

func TestEntityListCardsSitInsideTheJoiningFrame(t *testing.T) {
	frame := screentest.FrameAt(t, 120, 40)
	view := screentest.StripANSI(New(Laws()).Bind(entityListGoldenItems(42), nil).View(frame))
	if !strings.Contains(view, "├") {
		t.Fatalf("kicker rule did not join the sides:\n%s", view)
	}
	cardInside, hintInside := false, false
	for _, line := range strings.Split(view, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "│─") {
			t.Fatalf("kicker rule was wrapped as │────│:\n%s", view)
		}
		if strings.HasPrefix(trimmed, "│") && strings.Contains(trimmed, "┌") {
			cardInside = true
		}
		if strings.Contains(trimmed, "▼") && strings.Contains(trimmed, "│") {
			hintInside = true
		}
	}
	if !cardInside {
		t.Fatalf("cards sat outside the frame:\n%s", view)
	}
	if strings.Contains(view, "▼") && !hintInside {
		t.Fatalf("scroll hint was not inside the column:\n%s", view)
	}
}

func TestRootIsCellAndColumnWidthsOwnTheThirtyColumnFloor(t *testing.T) {
	screen := New(Laws()).Bind(testItems("laws"), nil)
	frame := screentest.FrameAt(t, 20, 24)
	root := screen.root(frame)
	if !root.IsLeaf() || root.Spec.ID != sectionGrid {
		t.Fatalf("root = leaf %v id %q, want Cell %q", root.IsLeaf(), root.Spec.ID, sectionGrid)
	}
	inner, content := screen.columnWidths(frame.Kit())
	if inner != 28 || content != 26 {
		t.Fatalf("columnWidths below floor = (%d, %d), want (28, 26)", inner, content)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
