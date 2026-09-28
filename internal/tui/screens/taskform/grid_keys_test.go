package taskform

import (
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

// typedMotionSpellings are the four spellings that are BOTH ordinary characters
// and part of screenlayout's standard motion table. They are the whole exposure
// of routing this screen's keys through screengrid: on a lone-Cell root the
// grid declines tab, f, esc and the lane keys, and the only vocabulary left is
// screenlayout.StandardBindings — of which exactly these four are typeable.
var typedMotionSpellings = []string{"j", "k", "g", "G"}

// textSections are the four sections that own a bubbles input. Every spelling
// belongs to them while they hold focus, which is the guard.
type textSectionCase struct {
	tabs  int
	value func(Values) string
}

var textSections = map[string]textSectionCase{
	"title":       {tabs: 0, value: func(v Values) string { return v.Title }},
	"description": {tabs: 1, value: func(v Values) string { return v.Description }},
	"tags":        {tabs: 3, value: func(v Values) string { return v.TagsCSV }},
	"parent":      {tabs: 4, value: func(v Values) string { return v.Parent }},
}

// TestTypedMotionKeysReachTheFieldNotTheGrid is the guard the migration rests
// on: `j`, `k`, `g` and `G` typed into a focused text field insert those
// characters and move nothing the arranger owns.
//
// It asserts both halves. The value must gain exactly the four characters — a
// grid that swallowed one would drop it — and the grid's stored cursor and
// offset must not move across any of them, which is what "the arranger never
// saw the key" looks like from outside.
func TestTypedMotionKeysReachTheFieldNotTheGrid(t *testing.T) {
	t.Parallel()

	for name, section := range textSections {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertTypedMotionSection(t, section)
		})
	}
}

func assertTypedMotionSection(t *testing.T, section textSectionCase) {
	f := frame(120, 40)
	screen := openGridForm(t, f)
	for range section.tabs {
		screen = driveForm(t, screen, f, "tab")
	}
	before := section.value(screen.Values())
	cursor, offset := screen.grid.Layout().Cursor(sectionForm), screen.grid.Layout().Offset(sectionForm)
	for _, spelling := range typedMotionSpellings {
		screen = driveForm(t, screen, f, spelling)
		if got := screen.grid.Layout().Cursor(sectionForm); got != cursor {
			t.Fatalf("typing %q moved the arranger cursor from %d to %d; the grid consumed a character", spelling, cursor, got)
		}
		if got := screen.grid.Layout().Offset(sectionForm); got != offset {
			t.Fatalf("typing %q moved the arranger offset from %d to %d; the grid scrolled instead of the field taking the key", spelling, offset, got)
		}
	}
	if got, want := section.value(screen.Values()), before+strings.Join(typedMotionSpellings, ""); got != want {
		t.Fatalf("field holds %q after typing %v, want %q", got, typedMotionSpellings, want)
	}
}

// TestPriorityStripHandsMotionKeysToTheGrid is the other half of the same
// boundary. The priority strip takes no text, so the spellings the form's own
// table does not name are the grid's — and on this body they are INERT by
// construction, because formSection states its cursor with screenlayout.At and
// the reclamp resolves it straight back to the focused field.
//
// Inert is the assertion, not an accident: the selection on this screen IS the
// focused field, so a motion key that moved it would be the defect.
func TestPriorityStripHandsMotionKeysToTheGrid(t *testing.T) {
	t.Parallel()

	f := frame(120, 40)
	screen := openGridForm(t, f)
	screen = driveForm(t, screen, f, "tab")
	screen = driveForm(t, screen, f, "tab")
	if got := screen.form.activeSection(); got != SectionPriority {
		t.Fatalf("two tabs parked on section %d, want the priority strip", got)
	}
	values := screen.Values()
	cursor := screen.grid.Layout().Cursor(sectionForm)
	if cursor <= 0 {
		t.Fatalf("the priority strip resolved to item %d; this test needs a cursor off the first field", cursor)
	}
	for _, spelling := range append(append([]string{}, typedMotionSpellings...), "up", "down", "home", "end", "pgup", "pgdown", "ctrl+u", "ctrl+d") {
		screen = driveForm(t, screen, f, spelling)
		if got := screen.Values(); got != values {
			t.Fatalf("%q changed the form values to %#v; a key the form declined must not reach an input", spelling, got)
		}
		if got := screen.grid.Layout().Cursor(sectionForm); got != cursor {
			t.Fatalf("%q moved the cursor from %d to %d; the section states its cursor, so motion on this body is inert", spelling, cursor, got)
		}
	}
}

// TestGridRoutedKeyStillDisarmsTheDiscardPrompt pins the one side effect a key
// handed on to the grid still owes the form: "any key but esc disarms the
// discard confirmation" is the form's rule and does not stop applying because
// the arranger ends up consuming the keystroke.
func TestGridRoutedKeyStillDisarmsTheDiscardPrompt(t *testing.T) {
	t.Parallel()

	f := frame(120, 40)
	screen := openGridForm(t, f)
	screen = driveForm(t, screen, f, "x")
	screen = driveForm(t, screen, f, "tab")
	screen = driveForm(t, screen, f, "tab")
	if !screen.form.dirty() {
		t.Fatal("the form is clean, so esc cannot arm a discard confirmation")
	}
	screen = driveForm(t, screen, f, "esc")
	if !screen.ConfirmingCancel() {
		t.Fatal("esc on a dirty form did not arm the discard confirmation")
	}
	screen = driveForm(t, screen, f, "j")
	if screen.ConfirmingCancel() {
		t.Fatal("a grid-routed key left the discard confirmation armed; the next esc would discard without a prompt")
	}
}

// TestFormOwnsKeyBoundary states the predicate itself, because it is what
// decides which owner sees a keystroke and the two tests above only sample it.
func TestFormOwnsKeyBoundary(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		section Section
		key     string
		want    bool
	}{
		"title takes j":            {SectionTitle, "j", true},
		"title takes G":            {SectionTitle, "G", true},
		"title takes ctrl+u":       {SectionTitle, "ctrl+u", true},
		"description takes down":   {SectionDescription, "down", true},
		"tags takes home":          {SectionTags, "home", true},
		"parent takes end":         {SectionParent, "end", true},
		"priority keeps ctrl+s":    {SectionPriority, "ctrl+s", true},
		"priority keeps esc":       {SectionPriority, "esc", true},
		"priority keeps tab":       {SectionPriority, "tab", true},
		"priority keeps shift+tab": {SectionPriority, "shift+tab", true},
		"priority keeps left":      {SectionPriority, "left", true},
		"priority keeps l":         {SectionPriority, "l", true},
		"priority yields j":        {SectionPriority, "j", false},
		"priority yields G":        {SectionPriority, "G", false},
		"priority yields pgdown":   {SectionPriority, "pgdown", false},
		"priority yields ctrl+d":   {SectionPriority, "ctrl+d", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := form{section: tc.section}
			if got := m.ownsKey(tc.key); got != tc.want {
				t.Fatalf("ownsKey(%q) on section %d = %v, want %v", tc.key, tc.section, got, tc.want)
			}
		})
	}
}

// TestEveryStandardSpellingIsAccountedFor is the completeness half: whatever
// screenlayout's standard table grows to, every spelling in it is either owned
// by the form on a text section or explicitly listed by ownsKey on the strip.
// A new motion key added upstream cannot silently start eating characters.
func TestEveryStandardSpellingIsAccountedFor(t *testing.T) {
	t.Parallel()

	typing := form{section: SectionTitle}
	strip := form{section: SectionPriority}
	seen := 0
	for _, binding := range screenlayout.StandardBindings() {
		for _, spelling := range binding.Keys {
			seen++
			if !typing.ownsKey(spelling) {
				t.Errorf("a focused text field yields %q to the arranger; every spelling is a character while a field takes text", spelling)
			}
			_ = strip.ownsKey(spelling)
		}
	}
	if seen == 0 {
		t.Fatal("the standard table reported no spellings; this test asserted nothing")
	}
}

func openGridForm(t *testing.T, f screenhost.Frame) Screen {
	t.Helper()
	screen := New().Bind(taskformGoldenDeps()).Open(Payload{
		Mode: Edit, TaskID: 7, Generation: 1,
		Values:     Values{Title: "T", Description: "D", Priority: "2", TagsCSV: "a", Parent: "3"},
		Priorities: priorities(),
	}, f)
	next, ok := screen.Lifecycle(f, screenhost.LifecycleEnter).Screen.(Screen)
	if !ok {
		t.Fatal("LifecycleEnter carried a foreign screen")
	}
	return next
}

func driveForm(t *testing.T, screen Screen, f screenhost.Frame, spelling string) Screen {
	t.Helper()
	next, ok := screen.Update(f, screentest.Key(spelling)).Screen.(Screen)
	if !ok {
		t.Fatalf("Update(%q) carried a foreign screen", spelling)
	}
	return next
}
