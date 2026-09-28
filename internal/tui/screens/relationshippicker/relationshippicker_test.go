package relationshippicker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	relationshipprojection "omakiten/internal/relationshipprojection"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestPersonaSkillsTypedSelectSaveAndCreateOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 90, 24)
	screen := New(PersonaSkills).Open(Payload{
		Kind: PersonaSkills, EntitySlug: "agent", Generation: 7,
		Options: []relationshipprojection.Option{{Value: "sqlite", Label: "SQLite"}, {Value: "go", Label: "Go", Selected: true}},
	})

	if got := screen.Options(); len(got) != 3 || got[0].Value != "go" || !got[2].Create {
		t.Fatalf("normalized options = %+v", got)
	}
	toggled := screen.Update(frame, tea.KeyMsg{Type: tea.KeySpace})
	screen = toggled.Screen.(Screen)
	if got := screen.SelectedValues(); toggled.Action.Kind != screenhost.ActionSelectRelationship || len(got) != 0 {
		t.Fatalf("toggle outcome = %+v, selected = %v", toggled.Action, got)
	}

	saved := screen.Update(frame, tea.KeyMsg{Type: tea.KeyCtrlS})
	if saved.Action.Kind != screenhost.ActionSavePersonaSkills || saved.Action.Value != "agent" || saved.Action.Generation != 7 {
		t.Fatalf("save action = %+v", saved.Action)
	}

	for screen.Cursor() < len(screen.Options())-1 {
		screen = screen.Update(frame, tea.KeyMsg{Type: tea.KeyDown}).Screen.(Screen)
	}
	created := screen.Update(frame, tea.KeyMsg{Type: tea.KeyEnter})
	if created.Action.Kind != screenhost.ActionCreateRelationship || created.Action.Value != "agent" || created.Action.Generation != 7 {
		t.Fatalf("create action = %+v", created.Action)
	}
}

func TestTemplateDefaultTypedSelectOutcomeAndOrdering(t *testing.T) {
	frame := screentest.FrameAt(t, 90, 24)
	screen := New(TemplateDefault).Open(Payload{
		Kind: TemplateDefault, EntitySlug: "task-mine", ProjectSlug: "omakiten", Generation: 11,
		Options: []relationshipprojection.Option{{Value: "pr", Label: "pr"}, {Value: "task", Label: "task", Selected: true}, {None: true}},
	})
	if screen.Selected().Value != "task" || screen.Dirty() {
		t.Fatalf("initial selected = %+v dirty=%v", screen.Selected(), screen.Dirty())
	}
	screen = screen.Update(frame, tea.KeyMsg{Type: tea.KeyDown}).Screen.(Screen)
	out := screen.Update(frame, tea.KeyMsg{Type: tea.KeyEnter})
	if out.Action.Kind != screenhost.ActionSelectTemplateDefault || out.Action.Value != "" || out.Action.EntityKind != "task-mine" || out.Action.Generation != 11 {
		t.Fatalf("select action = %+v", out.Action)
	}
}

func TestDirtyCancelRequiresConfirmationAndCleanCancelDoesNot(t *testing.T) {
	frame := screentest.FrameAt(t, 90, 24)
	clean := New(PersonaSkills).Open(testPayload(PersonaSkills))
	if got := clean.Update(frame, tea.KeyMsg{Type: tea.KeyEsc}).Action.Kind; got != screenhost.ActionCancelRelationshipPicker {
		t.Fatalf("clean cancel action = %v", got)
	}

	dirty := clean.Update(frame, tea.KeyMsg{Type: tea.KeySpace}).Screen.(Screen)
	first := dirty.Update(frame, tea.KeyMsg{Type: tea.KeyEsc})
	dirty = first.Screen.(Screen)
	if first.Action.Kind != screenhost.ActionNone || !dirty.ConfirmingCancel() {
		t.Fatalf("first dirty cancel = action:%v confirming:%v", first.Action.Kind, dirty.ConfirmingCancel())
	}
	second := dirty.Update(frame, tea.KeyMsg{Type: tea.KeyEsc})
	if second.Action.Kind != screenhost.ActionCancelRelationshipPicker || second.Action.Generation != testPayload(PersonaSkills).Generation {
		t.Fatalf("confirmed cancel = %+v", second.Action)
	}
}

func TestTemplateCursorChangeIsDirtyAndRequiresCancelConfirmation(t *testing.T) {
	frame := screentest.FrameAt(t, 90, 24)
	screen := New(TemplateDefault).Open(testPayload(TemplateDefault))
	screen = screen.Update(frame, tea.KeyMsg{Type: tea.KeyDown}).Screen.(Screen)
	if !screen.Dirty() {
		t.Fatal("template selection change is not dirty")
	}
	first := screen.Update(frame, tea.KeyMsg{Type: tea.KeyEsc})
	if first.Action.Kind != screenhost.ActionNone || !first.Screen.(Screen).ConfirmingCancel() {
		t.Fatalf("dirty template cancel = %+v", first)
	}
}

func TestResizeClampsScrollAndChromeIsOwned(t *testing.T) {
	screen := New(PersonaSkills).Open(Payload{Kind: PersonaSkills, Options: manyOptions(30)})
	narrow := screentest.FrameAt(t, 70, 20)
	for range 25 {
		screen = screen.Update(narrow, tea.KeyMsg{Type: tea.KeyDown}).Screen.(Screen)
	}
	if screen.Scroll() == 0 {
		t.Fatal("selection did not follow scroll")
	}
	wide := screen.Lifecycle(screentest.FrameAt(t, 140, 60), screenhost.LifecycleResize).Screen.(Screen)
	if wide.Scroll() != 0 || !wide.OwnsFooter() || !wide.BlocksHostInput() || len(wide.Footer(narrow)) < 5 || len(wide.Help(narrow)) != 1 {
		t.Fatalf("resize/chrome = scroll:%d footer:%v help:%v", wide.Scroll(), wide.Footer(narrow), wide.Help(narrow))
	}
}

func manyOptions(count int) []relationshipprojection.Option {
	options := make([]relationshipprojection.Option, count)
	for i := range options {
		options[i] = relationshipprojection.Option{Value: strings.Repeat("x", i+1), Label: strings.Repeat("row", i+1), Selected: i == 0}
	}
	return options
}
