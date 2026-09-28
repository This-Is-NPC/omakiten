// Package taskform owns Task create/edit screen state and presentation.
// Persistence and project-aware parent lookup remain host-owned.
package taskform

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/keynav"
	"omakiten/internal/taskvalidation"
	field "omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

type Mode string

const (
	Create Mode = "create"
	Edit   Mode = "edit"
)

func (m Mode) String() string { return string(m) }

type Payload struct {
	Mode           Mode
	TaskID         int64
	CreateParentID *int64
	Generation     uint64
	Kicker         string
	Values         Values
	Priorities     []PriorityOption
}

type LookupResult struct {
	Generation uint64
	Parent     string
	Label      string
	Err        error
}

type Deps struct {
	Theme  field.FormTheme
	Labels Labels
}

type errorField uint8

const (
	errorNone errorField = iota
	errorTitle
	errorParent
)

type Screen struct {
	deps        Deps
	payload     Payload
	form        form
	formWidth   int
	grid        screengrid.State
	loading     bool
	err         string
	errField    errorField
	lookupLabel string
}

func New() Screen { return Screen{grid: screengrid.NewState()} }

func (s Screen) ID() screenhost.ID      { return screenhost.TaskForm }
func (s Screen) Bind(deps Deps) Screen  { s.deps = deps; return s }
func (s Screen) Payload() Payload       { return s.payload }
func (s Screen) Values() Values         { return s.form.values() }
func (s Screen) FormWidth() int         { return s.formWidth }
func (s Screen) Loading() bool          { return s.loading }
func (s Screen) Err() string            { return s.err }
func (s Screen) LookupLabel() string    { return s.lookupLabel }
func (s Screen) ConfirmingCancel() bool { return s.form.confirmingDiscard() }

func (s Screen) Open(payload Payload, frame screenhost.Frame) Screen {
	payload.Priorities = append([]PriorityOption(nil), payload.Priorities...)
	s.payload = payload
	s.loading, s.err, s.errField, s.lookupLabel = false, "", errorNone, ""
	s = s.resize(frame)
	s.form = newForm(payload.Values, s.formWidth, s.deps.Theme).withPriorities(payload.Priorities)
	return s
}

func (s Screen) ApplyLookup(result LookupResult) Screen {
	if result.Generation != s.payload.Generation || result.Parent != s.form.values().Parent {
		return s
	}
	s.loading = false
	s.lookupLabel = result.Label
	s.err, s.errField = "", errorNone
	if result.Err != nil {
		s.err = result.Err.Error()
		s.errField = errorParent
		s.lookupLabel = ""
	}
	msg := s.lookupLabel
	if s.err != "" {
		msg = s.err
	}
	s.form = s.form.withParentError(msg)
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	// One spelling, read once: tea.KeyMsg.String() builds a fresh string per
	// call and this function asks four questions of it.
	spelling := key.String()
	if spelling == "ctrl+c" {
		return screenhost.Quit(s, nil)
	}
	if spelling == "ctrl+b" && s.payload.Mode == Edit {
		return s.action(screenhost.ActionOpenTaskBlockers, s.form.values(), nil)
	}
	s = s.resize(frame)
	if !s.form.ownsKey(spelling) {
		return s.routeGridKey(frame, key, spelling)
	}
	var event Event
	var cmd tea.Cmd
	previousTitle := s.form.values().Title
	previousParent := s.form.values().Parent
	s.form, cmd, event = s.form.update(key)
	if s.form.values().Parent != previousParent {
		s.loading, s.lookupLabel, s.err, s.errField = false, "", "", errorNone
		s.form = s.form.withParentError("")
	} else if s.form.values().Title != previousTitle && s.errField == errorTitle {
		s.err, s.errField = "", errorNone
	}
	s.form = s.form.withParentError(s.parentMessage(frame))
	s = s.syncFormWindow(frame)
	return s.handleEvent(frame, event, cmd)
}

func (s Screen) handleEvent(frame screenhost.Frame, event Event, cmd tea.Cmd) screenhost.Outcome {
	switch event.Kind {
	case EventSave:
		if !s.validate(frame, event.Values) {
			return screenhost.Stay(s, cmd)
		}
		return s.action(screenhost.ActionSaveTaskForm, event.Values, cmd)
	case EventCancel:
		return s.action(screenhost.ActionCancelTaskForm, s.form.values(), cmd)
	case EventParentBlur:
		if event.Parent == "" {
			s.loading, s.lookupLabel, s.err, s.errField = false, "", "", errorNone
			return screenhost.Stay(s, cmd)
		}
		if _, err := taskvalidation.PositiveID(event.Parent); err != nil {
			s.err = text(frame, "tui.taskedit.parent_lookup_invalid", "enter a valid task id")
			s.errField = errorParent
			s.form = s.form.withParentError(s.err)
			return screenhost.Stay(s, cmd)
		}
		s.loading, s.lookupLabel, s.err, s.errField = true, "", "", errorNone
		s.form = s.form.withParentError(text(frame, "tui.taskedit.looking_up_parent", "looking up parent..."))
		return s.action(screenhost.ActionLookupTaskParent, s.form.values(), cmd)
	}
	return screenhost.Stay(s, cmd)
}

// routeGridKey hands screengrid a spelling the focused field does not want.
//
// The form still sees it first: "any key but esc disarms the discard
// confirmation" is [form.update]'s rule and does not stop being true for a key
// the grid goes on to consume. For a key [form.ownsKey] declined that is the
// only thing update does, so no Event and no command can be dropped here.
//
// On this lone-Cell root the grid's whole vocabulary is the motion spellings
// screenlayout.StandardBindings owns: tab, f, esc and the lane keys all
// decline on a single-member ring, which is why ownsKey keeps them rather than
// handing them on to be refused. What the motion keys DO is nothing, and
// provably so: the section
// states its cursor with screenlayout.At, so HandleKey's reclamp resolves it
// straight back to the focused field and re-clamps the offset onto it. The
// selection on this screen IS the focused field, and routing the keys through
// the grid rather than a private table is what the arranger being the single
// entry to the body means.
func (s Screen) routeGridKey(frame screenhost.Frame, key tea.KeyMsg, spelling string) screenhost.Outcome {
	s.form, _, _ = s.form.update(key)
	kit := frame.Kit()
	s.grid, _ = s.grid.HandleKey(kit, screenlayout.HostBox(kit), spelling, s.formRoot(frame, s.labels(frame)))
	return screenhost.Stay(s, nil)
}

func (s *Screen) validate(frame screenhost.Frame, values Values) bool {
	if err := taskvalidation.ValidateTitle(values.Title); err != nil {
		s.err = text(frame, "tui.status.task_title_required", "task title is required")
		if !errors.Is(err, taskvalidation.ErrTitleRequired) {
			s.err = err.Error()
		}
		s.errField = errorTitle
		return false
	}
	if values.Parent != "" {
		if _, err := taskvalidation.PositiveID(values.Parent); err != nil {
			s.err = text(frame, "tui.taskedit.parent_lookup_invalid", "enter a valid task id")
			s.errField = errorParent
			s.form = s.form.withParentError(s.err)
			return false
		}
	}
	return true
}

func text(frame screenhost.Frame, key, fallback string) string {
	value := frame.Text(key)
	if value == key {
		return fallback
	}
	return value
}

func (s Screen) action(kind screenhost.ActionKind, values Values, cmd tea.Cmd) screenhost.Outcome {
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{
		Kind: kind, TaskID: s.payload.TaskID, Generation: s.payload.Generation,
		TaskFormMode: s.payload.Mode.String(), TaskTitle: values.Title,
		TaskDescription: values.Description, TaskPriority: values.Priority,
		TaskTagsCSV: values.TagsCSV, TaskParent: values.Parent,
	}, Command: cmd}
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		s = s.resize(frame)
		s = s.syncFormWindow(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) resize(frame screenhost.Frame) Screen {
	width := taskFormWidth(screenlayout.HostBox(frame.Kit()).Width)
	s.formWidth = width
	if s.form.isActive() {
		s.form = s.form.resize(width, s.deps.Theme)
	}
	return s
}

func (s Screen) parentMessage(frame screenhost.Frame) string {
	if s.err != "" {
		return s.err
	}
	if s.loading {
		return text(frame, "tui.taskedit.looking_up_parent", "looking up parent...")
	}
	return s.lookupLabel
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	labels := s.labels(frame)
	// The root Cell gives the form the HostBox budget while its private field
	// walk keeps ownership of tab and shift+tab.
	view := screengrid.Render(kit, s.grid, screenlayout.HostBox(kit), s.formRoot(frame, labels)).View
	return screenkit.Indent("\n"+view, 2)
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	label := frame.Text("tui.footer.save")
	if s.payload.Mode == Create {
		label = frame.Text("tui.footer.create")
	}
	bindings := []screenhost.FooterBinding{
		{Key: "ctrl+s", Label: label, Primary: true},
	}
	if s.payload.Mode == Edit {
		bindings = append(bindings, screenhost.FooterBinding{Key: "ctrl+b", Label: frame.Text("tui.footer.blockers"), Primary: true})
	}
	return append(bindings,
		screenhost.FooterBinding{Key: keynav.Default.Zones.Primary(), Label: frame.Text("tui.footer.field")},
		screenhost.FooterBinding{Key: "←/→", Label: frame.Text("tui.footer.priority")},
		frame.FooterCancel(false),
	)
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "task_form", Title: frame.Text("tui.help.task_form.title"), Bindings: []screenhost.HelpBinding{
		{Key: keynav.Default.Zones.Primary(), Description: frame.Text("tui.help.task_form.switch_field")},
		{Key: "← → · h l", Description: frame.Text("tui.help.task_form.change_priority")},
		{Key: "ctrl+b", Description: frame.Text("tui.help.task_form.edit_blockers")},
		{Key: "enter · alt+enter · shift+enter", Description: frame.Text("tui.help.task_form.newline")},
		{Key: "ctrl+s", Description: frame.Text("tui.help.task_form.save")},
		{Key: "esc", Description: frame.Text("tui.help.task_form.cancel")},
	}}}
}

func (s Screen) OwnsKey(tea.KeyMsg) bool { return true }
func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) BlocksHostInput() bool   { return true }

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
