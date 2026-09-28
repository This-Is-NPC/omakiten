package taskform

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

func TestScreenCreateAndEditSaveIntents(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{Create, Edit} {
		t.Run(mode.String(), func(t *testing.T) {
			screen := New().Open(Payload{
				Mode: mode, TaskID: 42, Generation: 7,
				Values:     Values{Title: "Task", Description: "Body", Priority: "2", TagsCSV: "tui", Parent: "9"},
				Priorities: priorities(),
			}, frame(100, 40))
			out := screen.Update(frame(100, 40), tea.KeyMsg{Type: tea.KeyCtrlS})
			if out.Action.Kind != screenhost.ActionSaveTaskForm {
				t.Fatalf("save action = %v, want task-form save", out.Action.Kind)
			}
			if out.Action.TaskFormMode != mode.String() || out.Action.TaskID != 42 || out.Action.Generation != 7 {
				t.Fatalf("save identity = %#v", out.Action)
			}
			if out.Action.TaskTitle != "Task" || out.Action.TaskDescription != "Body" || out.Action.TaskPriority != "2" || out.Action.TaskTagsCSV != "tui" || out.Action.TaskParent != "9" {
				t.Fatalf("save values = %#v", out.Action)
			}
		})
	}
}

func TestScreenInvalidSaveStaysOpenAndQIsText(t *testing.T) {
	t.Parallel()

	screen := New().Open(Payload{Mode: Create, Generation: 1, Values: Values{Priority: "2"}, Priorities: priorities()}, frame(90, 35))
	out := screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyCtrlS})
	screen = out.Screen.(Screen)
	if out.Action.Kind != screenhost.ActionNone || !strings.Contains(screen.Err(), "title") {
		t.Fatalf("invalid save action=%v err=%q", out.Action.Kind, screen.Err())
	}
	out = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	screen = out.Screen.(Screen)
	if out.Action.Kind == screenhost.ActionQuit || screen.Values().Title != "q" {
		t.Fatalf("q action=%v title=%q, want typed text", out.Action.Kind, screen.Values().Title)
	}
}

func TestScreenLookupInvalidAndStaleResults(t *testing.T) {
	t.Parallel()

	screen := New().Open(Payload{
		Mode: Edit, TaskID: 42, Generation: 4,
		Values:     Values{Title: "Task", Priority: "2", Parent: "nope"},
		Priorities: priorities(),
	}, frame(90, 35))
	for range 4 {
		screen = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyTab}).Screen.(Screen)
	}
	out := screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyTab})
	if out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("invalid parent emitted action %v", out.Action.Kind)
	}
	screen = out.Screen.(Screen)
	if !strings.Contains(screen.Err(), "valid task id") {
		t.Fatalf("invalid parent error = %q", screen.Err())
	}
	screen = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}}).Screen.(Screen)
	if !strings.Contains(screen.Err(), "valid task id") {
		t.Fatalf("unrelated title edit cleared parent error: %q", screen.Err())
	}

	screen = New().Open(Payload{
		Mode: Edit, TaskID: 42, Generation: 5,
		Values:     Values{Title: "Task", Priority: "2", Parent: "9"},
		Priorities: priorities(),
	}, frame(90, 35))
	for range 4 {
		screen = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyTab}).Screen.(Screen)
	}
	out = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyTab})
	if out.Action.Kind != screenhost.ActionLookupTaskParent || out.Action.TaskParent != "9" || out.Action.Generation != 5 {
		t.Fatalf("lookup action = %#v", out.Action)
	}
	screen = out.Screen.(Screen)
	screen = screen.ApplyLookup(LookupResult{Generation: 4, Parent: "9", Label: "stale"})
	if screen.LookupLabel() != "" || !screen.Loading() {
		t.Fatalf("stale lookup mutated screen: label=%q loading=%v", screen.LookupLabel(), screen.Loading())
	}
	screen = screen.ApplyLookup(LookupResult{Generation: 5, Parent: "8", Label: "wrong parent"})
	if screen.LookupLabel() != "" || !screen.Loading() {
		t.Fatalf("wrong-parent lookup mutated screen: label=%q loading=%v", screen.LookupLabel(), screen.Loading())
	}
	screen = screen.ApplyLookup(LookupResult{Generation: 5, Parent: "9", Label: "#9 Parent"})
	if screen.LookupLabel() != "#9 Parent" || screen.Loading() || screen.Err() != "" {
		t.Fatalf("current lookup = label %q loading %v err %q", screen.LookupLabel(), screen.Loading(), screen.Err())
	}
	screen = screen.ApplyLookup(LookupResult{Generation: 5, Parent: "9", Err: errors.New("gone")})
	if screen.Err() != "gone" {
		t.Fatalf("lookup error = %q, want gone", screen.Err())
	}
}

func TestScreenEditingParentInvalidatesPriorLookupProjection(t *testing.T) {
	t.Parallel()

	screen := New().Open(Payload{Mode: Edit, TaskID: 42, Generation: 5, Values: Values{Title: "Task", Priority: "2", Parent: "9"}, Priorities: priorities()}, frame(90, 35))
	for range 4 {
		screen = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyTab}).Screen.(Screen)
	}
	out := screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyTab})
	screen = out.Screen.(Screen).ApplyLookup(LookupResult{Generation: 5, Parent: "9", Label: "#9 Parent"})
	screen = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyShiftTab}).Screen.(Screen)
	screen = screen.Update(frame(90, 35), tea.KeyMsg{Type: tea.KeyBackspace}).Screen.(Screen)
	if screen.LookupLabel() != "" || screen.Loading() || strings.Contains(screen.View(frame(90, 35)), "#9 Parent") {
		t.Fatalf("edited parent retained stale lookup: label=%q loading=%v\n%s", screen.LookupLabel(), screen.Loading(), screen.View(frame(90, 35)))
	}
}

func TestScreenDirtyCancelAndResize(t *testing.T) {
	t.Parallel()

	screen := New().Open(Payload{Mode: Create, Generation: 1, Values: Values{Title: "Task", Priority: "2"}, Priorities: priorities()}, frame(100, 40))
	screen = screen.Update(frame(100, 40), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}}).Screen.(Screen)
	out := screen.Update(frame(100, 40), tea.KeyMsg{Type: tea.KeyEsc})
	if out.Action.Kind != screenhost.ActionNone || !out.Screen.(Screen).ConfirmingCancel() {
		t.Fatalf("first dirty esc = %#v", out)
	}
	out = out.Screen.(Screen).Update(frame(100, 40), tea.KeyMsg{Type: tea.KeyEsc})
	if out.Action.Kind != screenhost.ActionCancelTaskForm {
		t.Fatalf("second dirty esc action = %v", out.Action.Kind)
	}

	screen = screen.Lifecycle(frame(52, 24), screenhost.LifecycleResize).Screen.(Screen)
	if screen.FormWidth() != 40 {
		t.Fatalf("form width after resize = %d, want 40", screen.FormWidth())
	}
}

func TestScreenCellMountPreservesPrivateTabFieldWalk(t *testing.T) {
	t.Parallel()

	frame := frame(100, 40)
	screen := New().Open(Payload{
		Mode: Edit, TaskID: 42, Values: Values{Title: "Task", Priority: "2"}, Priorities: priorities(),
	}, frame)
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	if root := screen.formRoot(frame, screen.labels(frame)); !root.IsLeaf() || root.Spec.ID != sectionForm {
		t.Fatalf("form root = leaf %v id %q, want Cell %q", root.IsLeaf(), root.Spec.ID, sectionForm)
	}
	if got := screen.grid.Focus(); got != sectionForm {
		t.Fatalf("grid focus after enter = %q, want %q", got, sectionForm)
	}
	if got := screen.FormWidth(); got != 88 {
		t.Fatalf("form width at 100 columns = %d, want AvailableWidth()-8 = 88", got)
	}

	screen = screen.Update(frame, tea.KeyMsg{Type: tea.KeyTab}).Screen.(Screen)
	if got := screen.form.activeSection(); got != SectionDescription {
		t.Fatalf("form section after tab = %v, want description; Cell stole the private field-walk key", got)
	}
	if got := screen.grid.Focus(); got != sectionForm {
		t.Fatalf("grid focus after form-owned tab = %q, want stable Cell focus %q", got, sectionForm)
	}
}

func TestScreenViewGolden(t *testing.T) {
	t.Parallel()

	screen := New().Open(Payload{
		Mode: Edit, TaskID: 42, Generation: 3, Kicker: "Edit task · #42",
		Values:     Values{Title: "Ship screen", Description: "First line\nSecond line", Priority: "2", TagsCSV: "tui, forms", Parent: "9"},
		Priorities: priorities(),
	}, frame(72, 32))
	got := normalizeGolden(screen.View(frame(72, 32)))
	want := strings.TrimSpace(`// EDIT TASK · #42
ctrl+s saves · tab switches field · esc cancels

> // TITLE
Ship screen

  // DESCRIPTION
First line
Second line







  // PRIORITY
low  [normal]  high

  // TAGS
tui, forms

  // PARENT
9`)
	if got != want {
		t.Fatalf("golden mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestScreenContractChromeAndEditBlockerIntent(t *testing.T) {
	t.Parallel()

	deps := Deps{Labels: Labels{Title: "Title", Description: "Description", Priority: "Priority", Tags: "Tags", Parent: "Parent"}}
	screen := New().Bind(deps).Open(Payload{Mode: Edit, TaskID: 8, Generation: 2, Values: Values{Title: "Task", Priority: "2"}, Priorities: priorities()}, frame(42, 20))
	if screen.ID() != screenhost.TaskForm || screen.Payload().TaskID != 8 || !screen.OwnsKey(tea.KeyMsg{}) || !screen.OwnsFooter() || !screen.BlocksHostInput() {
		t.Fatal("screen contract metadata mismatch")
	}
	if out := screen.Update(frame(42, 20), struct{}{}); out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("non-key action = %v", out.Action.Kind)
	}
	out := screen.Update(frame(42, 20), tea.KeyMsg{Type: tea.KeyCtrlB})
	if out.Action.Kind != screenhost.ActionOpenTaskBlockers || out.Action.TaskID != 8 {
		t.Fatalf("blocker action = %#v", out.Action)
	}
	if got := screen.Footer(frame(42, 20)); len(got) != 5 || got[1].Key != "ctrl+b" {
		t.Fatalf("edit footer = %#v", got)
	}
	if got := screen.Help(frame(42, 20)); len(got) != 1 || got[0].ID != "task_form" {
		t.Fatalf("help = %#v", got)
	}
	if got := screen.Lifecycle(frame(160, 50), screenhost.LifecycleEnter).Screen.(Screen).FormWidth(); got != 120 {
		t.Fatalf("wide form width = %d, want capped 120", got)
	}
	if got := screen.Lifecycle(frame(20, 10), screenhost.LifecycleResize).Screen.(Screen).FormWidth(); got != 32 {
		t.Fatalf("narrow form width = %d, want floor 32", got)
	}
}

func TestScreenCleanCancelAndValidationView(t *testing.T) {
	t.Parallel()

	screen := New().Open(Payload{Mode: Create, Generation: 1, Values: Values{Priority: "2"}, Priorities: priorities()}, frame(80, 30))
	out := screen.Update(frame(80, 30), tea.KeyMsg{Type: tea.KeyCtrlS})
	screen = out.Screen.(Screen)
	if !strings.Contains(screen.View(frame(80, 30)), "task title is required") {
		t.Fatalf("validation view missing title error\n%s", screen.View(frame(80, 30)))
	}
	clean := New().Open(Payload{Mode: Create, Generation: 2, Values: Values{Title: "Task", Priority: "2"}, Priorities: priorities()}, frame(80, 30))
	out = clean.Update(frame(80, 30), tea.KeyMsg{Type: tea.KeyEsc})
	if out.Action.Kind != screenhost.ActionCancelTaskForm {
		t.Fatalf("clean cancel action = %v", out.Action.Kind)
	}
	if got := clean.Footer(frame(80, 30)); len(got) != 4 || got[0].Label == "" {
		t.Fatalf("create footer = %#v", got)
	}
}

func priorities() []PriorityOption {
	return []PriorityOption{{Value: "1", Label: "low"}, {Value: "2", Label: "normal"}, {Value: "3", Label: "high"}}
}

func frame(width, height int) screenhost.Frame {
	return screenhost.NewFrame(screenhost.FrameOptions{
		Width: width, Height: height, Focused: true,
		Styles: screenkit.Styles{
			Panel: lipgloss.NewStyle(), Hint: lipgloss.NewStyle(), HintAccent: lipgloss.NewStyle(),
			Info: lipgloss.NewStyle(), Error: lipgloss.NewStyle(), Border: lipgloss.NewStyle(),
		},
		Text: func(key string) string { return key },
	})
}

func normalizeGolden(value string) string {
	lines := strings.Split(strings.Trim(value, "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(strings.TrimPrefix(lines[i], "  "), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
