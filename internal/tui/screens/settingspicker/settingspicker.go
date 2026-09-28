// Package settingspicker owns the local lifecycle and presentation of the
// Theme, Config, and Subtask-kit child screens. Filesystem and persistence
// operations are deliberately represented only as semantic host outcomes.
package settingspicker

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Kind string

const (
	Theme      Kind = "theme"
	Config     Kind = "config"
	SubtaskKit Kind = "subtask-kit"
)

func (k Kind) ID() screenhost.ID {
	switch k {
	case Theme:
		return screenhost.ThemePicker
	case Config:
		return screenhost.ConfigPicker
	default:
		return screenhost.SubtaskKitPicker
	}
}

type Option struct {
	Value  string
	Label  string
	Detail string
	Custom bool
	None   bool
	Active bool
}

type Payload struct {
	Kind    Kind
	Current string
	Options []Option
}

type Screen struct {
	kind     Kind
	payload  Payload
	picker   list.Picker
	grid     screengrid.State
	original int
	err      error
}

func New(kind Kind) Screen {
	return Screen{kind: kind, payload: Payload{Kind: kind}, picker: list.NewPicker(list.Single), grid: newOptionsGrid(0)}
}
func (s Screen) ID() screenhost.ID { return s.kind.ID() }
func (s Screen) Cursor() int       { return s.picker.Cursor }

// Scroll is the grid arranger's window offset. picker.Scroll is kept in sync
// for any remaining picker readers, but the grid is authoritative.
func (s Screen) Scroll() int      { return s.grid.Layout().Offset(sectionOptions) }
func (s Screen) Payload() Payload { return s.payload }
func (s Screen) Selected() Option {
	if s.picker.Cursor < 0 || s.picker.Cursor >= len(s.payload.Options) {
		return Option{}
	}
	return s.payload.Options[s.picker.Cursor]
}
func (s Screen) Dirty() bool { return len(s.payload.Options) > 0 && s.picker.Cursor != s.original }
func (s Screen) Select(value string) Screen {
	if cursor := optionIndex(s.payload.Options, value); cursor >= 0 {
		s.picker = s.picker.WithCursor(cursor, len(s.payload.Options), 0)
		s.grid = s.grid.WithFocus(sectionOptions).WithCursor(sectionOptions, cursor)
	}
	return s
}

func (s Screen) Open(payload Payload) Screen {
	s.kind, s.payload, s.err = payload.Kind, payload, nil
	s.original = activeIndex(payload)
	s.picker = list.NewPicker(list.Single).WithCursor(s.original, len(payload.Options), 0)
	s.grid = newOptionsGrid(s.picker.Cursor)
	return s
}

// Refresh replaces host-discovered candidates while preserving the selected
// identity when possible. The screen never discovers or validates files.
func (s Screen) Refresh(payload Payload, err error) Screen {
	selected := s.Selected().Value
	oldCursor := s.picker.Cursor
	s.kind, s.payload, s.err = payload.Kind, payload, err
	cursor := optionIndex(payload.Options, selected)
	if cursor < 0 {
		cursor = oldCursor
	}
	s.original = activeIndex(payload)
	s.picker = s.picker.WithCursor(cursor, len(payload.Options), 0)
	s.grid = newOptionsGrid(s.picker.Cursor)
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	if key.String() == "q" || key.String() == "ctrl+c" {
		return screenhost.Quit(s, nil)
	}
	if key.String() == "r" {
		return screenhost.Reload(s, nil)
	}

	kit := frame.Kit()
	if next, handled := s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.root(frame)); handled {
		s.grid = next
		return screenhost.Stay(s.syncPickerFromGrid(), nil)
	}

	// Selection keys only — nav already consumed above so picker never
	// double-scrolls against the arranger.
	var cmd tea.Cmd
	s.picker, cmd = s.picker.Update(msg, len(s.payload.Options), 0)
	switch s.picker.LastEvent() {
	case list.PickerCancel:
		return screenhost.Back(s, cmd)
	case list.PickerSelect:
		selected := s.Selected()
		if selected.Value != "" || selected.None {
			return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionApplySettingsPicker, EntityKind: string(s.kind), Value: selected.Value}, Command: cmd}
		}
	}
	return screenhost.Stay(s, cmd)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		return screenhost.Stay(s.syncOptionsWindow(frame), nil)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "enter", "esc", "r", "q", "ctrl+c", "up", "down", "j", "k", "pgup", "pgdown", "pgdn", "ctrl+u", "ctrl+d", "home", "end", "g", "G":
		return true
	}
	return false
}
func (s Screen) OwnsFooter() bool      { return true }
func (s Screen) BlocksHostInput() bool { return true }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	apply := "tui.footer.apply_hot_reload"
	if s.kind == Config {
		apply = "tui.footer.select_restart_required"
	}
	return []screenhost.FooterBinding{
		{Key: "enter", Label: frame.Text(apply), Primary: true},
		frame.FooterMove(false),
		frame.FooterScrollPage(false),
		frame.FooterRefresh(false),
		frame.FooterCancel(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "settings_picker", Title: frame.Text("tui.help.settings_general.title"), Bindings: []screenhost.HelpBinding{
		{Key: "j k · pgup pgdn · g G", Description: frame.Text("tui.help.entity_screen.scroll")},
		{Key: "enter", Description: frame.Text("tui.footer.apply_hot_reload")},
		{Key: "r", Description: frame.Text("tui.footer.refresh")},
		{Key: "esc", Description: frame.Text("tui.footer.cancel")},
	}}}
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
		return kit.Panel(body)
	}

	// Panel owns the leading blank + border; the root Cell fills the panel
	// content box and the grid applies the same clip and row budget on every
	// render path.
	return "\n" + screenkit.Indent(screengrid.Render(kit, s.grid, s.panelBox(kit), s.root(frame)).View, 2)
}

func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	id := screenstate.For(s.kicker(frame))
	return []screenstate.State{
		id.Failed(s.err, frame.Text("tui.stat.error_badge")),
		id.Vazio(len(s.payload.Options) == 0, s.emptyText(frame), ""),
	}
}

func (s Screen) kicker(frame screenhost.Frame) string {
	current := screenkit.Sanitize(s.payload.Current)
	switch s.kind {
	case Theme:
		return fmt.Sprintf(frame.Text("tui.kicker.theme_current_fmt"), current)
	case Config:
		return fmt.Sprintf(frame.Text("tui.kicker.config_active_fmt"), current)
	default:
		if current == "" {
			current = frame.Text("tui.picker.subtask_kit_none")
		}
		return fmt.Sprintf(frame.Text("tui.kicker.subtask_kit_active_fmt"), current)
	}
}
func (s Screen) hint(frame screenhost.Frame) string {
	if s.kind == SubtaskKit {
		return frame.Text("tui.picker.hint.subtask_kit")
	}
	return frame.Text("tui.picker.hint.theme")
}
func (s Screen) emptyText(frame screenhost.Frame) string {
	switch s.kind {
	case Theme:
		return frame.Text("tui.status.no_themes_found")
	case Config:
		return frame.Text("tui.status.no_config_profiles")
	default:
		return frame.Text("tui.status.no_subtask_kit_profiles")
	}
}

func activeIndex(payload Payload) int {
	for i, option := range payload.Options {
		if option.Active || option.Value == payload.Current {
			return i
		}
	}
	return 0
}
func optionIndex(options []Option, value string) int {
	for i := range options {
		if options[i].Value == value {
			return i
		}
	}
	return -1
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
