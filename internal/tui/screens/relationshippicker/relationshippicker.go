// Package relationshippicker owns the local lifecycle and presentation of the
// Persona-skill and Template-default child screens. Persistence remains host-owned.
package relationshippicker

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/relationshipprojection"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

const (
	PersonaSkills   = relationshipprojection.PersonaSkills
	TemplateDefault = relationshipprojection.TemplateDefault
)

func kindID(k relationshipprojection.Kind) screenhost.ID {
	if k == relationshipprojection.PersonaSkills {
		return screenhost.PersonaSkills
	}
	return screenhost.TemplateDefault
}

type Payload struct {
	Kind        relationshipprojection.Kind
	EntitySlug  string
	ProjectSlug string
	Generation  uint64
	Options     []relationshipprojection.Option
}

type Screen struct {
	payload          Payload
	picker           list.Picker
	grid             screengrid.State
	original         map[string]bool
	originalCursor   int
	confirmingCancel bool
}

func New(kind relationshipprojection.Kind) Screen {
	return Screen{payload: Payload{Kind: kind}, picker: list.NewPicker(list.Single), grid: screengrid.NewState()}
}

func (s Screen) ID() screenhost.ID { return kindID(s.payload.Kind) }
func (s Screen) Cursor() int       { return s.picker.Cursor }

// Scroll is the grid arranger's window offset. picker.Scroll is kept in sync
// for any remaining picker readers, but the grid is authoritative.
func (s Screen) Scroll() int      { return s.grid.Layout().Offset(sectionOptions) }
func (s Screen) Payload() Payload { return s.payload }
func (s Screen) Options() []relationshipprojection.Option {
	return append([]relationshipprojection.Option(nil), s.payload.Options...)
}
func (s Screen) ConfirmingCancel() bool { return s.confirmingCancel }
func (s Screen) Selected() relationshipprojection.Option {
	if s.picker.Cursor < 0 || s.picker.Cursor >= len(s.payload.Options) {
		return relationshipprojection.Option{}
	}
	return s.payload.Options[s.picker.Cursor]
}

func (s Screen) SelectedValues() []string {
	values := make([]string, 0, len(s.payload.Options))
	for _, option := range s.payload.Options {
		if option.Selected && !option.None && !option.Create {
			values = append(values, option.Value)
		}
	}
	return values
}

func (s Screen) Dirty() bool {
	if s.payload.Kind == TemplateDefault {
		return s.picker.Cursor != s.originalCursor
	}
	selected := relationshipprojection.SelectedValues(s.payload.Options)
	if len(selected) != len(s.original) {
		return true
	}
	for _, value := range selected {
		if !s.original[value] {
			return true
		}
	}
	return false
}

func (s Screen) Open(payload Payload) Screen {
	payload.Options = relationshipprojection.NormalizeOptions(payload.Kind, payload.Options)
	s.payload = payload
	s.original = relationshipprojection.SelectedSet(payload.Options)
	s.originalCursor = relationshipprojection.SelectedIndex(payload.Options)
	s.confirmingCancel = false
	mode := list.Single
	if payload.Kind == PersonaSkills {
		mode = list.Multi
	}
	s.picker = list.NewPicker(mode).WithCursor(s.originalCursor, len(payload.Options), 0)
	s.grid = screengrid.NewState()
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	if outcome, handled := s.handleExitKey(key); handled {
		return outcome
	}
	if outcome, handled := s.handleCreateKey(key); handled {
		return outcome
	}
	if next, handled := s.handleNavKey(frame, msg, key); handled {
		return next
	}
	var cmd tea.Cmd
	s.picker, cmd = s.picker.Update(msg, len(s.payload.Options), 0)
	if outcome, handled := s.handlePickerEvent(cmd); handled {
		return outcome
	}
	if s.payload.Kind == PersonaSkills && key.String() == "ctrl+s" {
		return s.action(screenhost.ActionSavePersonaSkills, s.payload.EntitySlug, cmd)
	}
	return screenhost.Stay(s, cmd)
}

func (s Screen) handleExitKey(key tea.KeyMsg) (screenhost.Outcome, bool) {
	if key.String() == "q" || key.String() == "ctrl+c" {
		return screenhost.Quit(s, nil), true
	}
	if key.String() != "esc" {
		s.confirmingCancel = false
		return screenhost.Outcome{}, false
	}
	if s.Dirty() && !s.confirmingCancel {
		s.confirmingCancel = true
		return screenhost.Stay(s, nil), true
	}
	return s.action(screenhost.ActionCancelRelationshipPicker, s.payload.EntitySlug, nil), true
}

func (s Screen) handleCreateKey(key tea.KeyMsg) (screenhost.Outcome, bool) {
	if s.payload.Kind == PersonaSkills && key.String() == "enter" && s.Selected().Create {
		return s.action(screenhost.ActionCreateRelationship, s.payload.EntitySlug, nil), true
	}
	return screenhost.Outcome{}, false
}

func (s Screen) handlePickerEvent(cmd tea.Cmd) (screenhost.Outcome, bool) {
	switch s.picker.LastEvent() {
	case list.PickerToggle:
		if s.picker.Cursor >= 0 && s.picker.Cursor < len(s.payload.Options) && !s.payload.Options[s.picker.Cursor].Create {
			s.payload.Options[s.picker.Cursor].Selected = !s.payload.Options[s.picker.Cursor].Selected
			return s.action(screenhost.ActionSelectRelationship, s.payload.Options[s.picker.Cursor].Value, cmd), true
		}
	case list.PickerSelect:
		if s.payload.Kind == TemplateDefault {
			selected := s.Selected()
			return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionSelectTemplateDefault, Value: selected.Value, EntityKind: s.payload.EntitySlug, Generation: s.payload.Generation}, Command: cmd}, true
		}
	}
	return screenhost.Outcome{}, false
}

// handleNavKey routes scroll/cursor motion through the screengrid arranger.
// The picker mirrors the arranger's resolved cursor and offset for selection
// helpers, while the Cell owns geometry and the scroll ceiling.
func (s Screen) handleNavKey(frame screenhost.Frame, _ tea.Msg, key tea.KeyMsg) (screenhost.Outcome, bool) {
	kit := frame.Kit()
	if s.grid.Focus() != sectionOptions {
		s = s.syncOptionsWindow(frame)
	}
	next, handled := s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.root(frame))
	if !handled {
		return screenhost.Outcome{}, false
	}
	s.grid = next
	s = s.syncPickerFromGrid()
	return screenhost.Stay(s, nil), true
}

func (s Screen) action(kind screenhost.ActionKind, value string, cmd tea.Cmd) screenhost.Outcome {
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: kind, Value: value, EntityKind: string(s.payload.Kind), Generation: s.payload.Generation}, Command: cmd}
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		return screenhost.Stay(s.syncOptionsWindow(frame), nil)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "enter", " ", "space", "ctrl+s", "esc", "q", "ctrl+c", "up", "down", "j", "k", "pgup", "pgdown", "pgdn", "ctrl+u", "ctrl+d", "home", "end", "g", "G":
		return true
	}
	return false
}
func (s Screen) OwnsFooter() bool      { return true }
func (s Screen) BlocksHostInput() bool { return true }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if s.payload.Kind == PersonaSkills {
		return []screenhost.FooterBinding{
			{Key: "space", Label: frame.Text("tui.footer.toggle"), Primary: true},
			{Key: "enter", Label: frame.Text("tui.footer.new_skill_on_plus"), Primary: true},
			frame.FooterSave(true),
			frame.FooterMove(false),
			frame.FooterScrollPage(false),
			frame.FooterCancel(false),
		}
	}
	return []screenhost.FooterBinding{
		{Key: "enter", Label: frame.Text("tui.footer.assign_clears_prior_owner"), Primary: true},
		frame.FooterMove(false),
		frame.FooterScrollPage(false),
		frame.FooterCancel(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	bindings := []screenhost.HelpBinding{{Key: "j k · pgup pgdn · g G", Description: frame.Text("tui.help.entity_screen.scroll")}}
	if s.payload.Kind == PersonaSkills {
		bindings = append(bindings,
			screenhost.HelpBinding{Key: "space", Description: frame.Text("tui.footer.toggle")},
			screenhost.HelpBinding{Key: "ctrl+s", Description: frame.Text("tui.footer.save")})
	} else {
		bindings = append(bindings, screenhost.HelpBinding{Key: "enter", Description: frame.Text("tui.footer.assign_clears_prior_owner")})
	}
	bindings = append(bindings, screenhost.HelpBinding{Key: "esc", Description: frame.Text("tui.footer.cancel")})
	return []screenhost.HelpGroup{{ID: string(s.payload.Kind), Title: s.kicker(frame), Bindings: bindings}}
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	// Panel owns the leading blank + border; the root Cell fills the panel
	// content box and the grid applies the same clip and row budget on every
	// render path.
	return "\n" + screenkit.Indent(screengrid.Render(kit, s.grid, s.panelBox(kit), s.root(frame)).View, 2)
}

func (s Screen) kicker(frame screenhost.Frame) string {
	entity := screenkit.Sanitize(s.payload.EntitySlug)
	if s.payload.Kind == PersonaSkills {
		return fmt.Sprintf(frame.Text("tui.kicker.skills_for_persona_fmt"), entity)
	}
	return fmt.Sprintf(frame.Text("tui.kicker.default_kind_fmt"), entity, screenkit.Sanitize(s.payload.ProjectSlug))
}
func (s Screen) hint(frame screenhost.Frame) string {
	if s.payload.Kind == PersonaSkills {
		return frame.Text("tui.picker.hint.skills_persona")
	}
	return frame.Text("tui.picker.hint.template_default")
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
