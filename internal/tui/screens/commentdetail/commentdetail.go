// Package commentdetail owns the route-stacked Comment detail and edit modes.
package commentdetail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Mode uint8

const (
	ModeRead Mode = iota
	ModeEdit
)

type Payload struct {
	Comment      domain.Comment
	Editable     bool
	Deletable    bool
	EditDenied   string
	DeleteDenied string
}

type Result struct {
	CommentID int64
	Comment   domain.Comment
	Saved     bool
	Err       error
}

type Deps struct {
	EditTheme field.Theme
}

type Screen struct {
	deps        Deps
	payload     Payload
	input       textarea.Model
	grid        screengrid.State
	md          *markdown.Renderer
	mode        Mode
	width       int
	height      int
	rendered    bool
	deleteArmed bool
	loading     bool
	pending     bool
	err         error
}

func New() Screen {
	input := textarea.New()
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"))
	return Screen{input: input, grid: screengrid.NewState(), md: markdown.New(screenkit.MarkdownTokens{}), rendered: true}
}

func (s Screen) ID() screenhost.ID     { return screenhost.CommentDetail }
func (s Screen) Bind(deps Deps) Screen { s.deps = deps; return s }
func (s Screen) Payload() Payload      { return s.payload }
func (s Screen) Mode() Mode            { return s.mode }
func (s Screen) Value() string         { return s.input.Value() }
func (s Screen) Input() textarea.Model { return s.input }
func (s Screen) Dirty() bool           { return s.mode == ModeEdit && s.input.Value() != s.payload.Comment.Body }
func (s Screen) DeleteArmed() bool     { return s.deleteArmed }

// Scroll is the one section's offset in both modes now that read mode no longer
// keeps a second window of its own.
func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionBody) }
func (s Screen) Width() int  { return s.width }
func (s Screen) Height() int { return s.height }

func (s Screen) Open(payload Payload) Screen {
	s.payload, s.mode = payload, ModeRead
	s.grid = screengrid.NewState()
	s.deleteArmed, s.loading, s.pending, s.err = false, false, false, nil
	s.resetInput()
	return s
}

func (s Screen) Loading() Screen { s.loading, s.err = true, nil; return s }

func (s Screen) Apply(result Result) Screen {
	if result.CommentID != s.payload.Comment.ID {
		return s
	}
	s.pending = false
	s.err = result.Err
	if result.Err != nil {
		return s
	}
	if result.Saved {
		s.payload.Comment = result.Comment
		s.mode, s.deleteArmed = ModeRead, false
		s.resetInput()
	}
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	s.width, s.height = frame.Width(), frame.Height()
	if s.mode == ModeEdit {
		return s.updateEdit(frame, keyMsg)
	}
	if keyMsg.String() != "d" {
		s.deleteArmed = false
	}
	switch keyMsg.String() {
	case "ctrl+c", "q":
		return screenhost.Quit(s, nil)
	case "esc":
		return screenhost.Back(s, nil)
	case "e":
		if !s.payload.Editable {
			return screenhost.SetStatus(s, s.payload.EditDenied, nil)
		}
		s.mode, s.err = ModeEdit, nil
		s.resetInput()
		s.resizeInput(frame)
		s = s.syncBodyWindow(frame)
		s.input.CursorEnd()
		s.input.Focus()
		return screenhost.Stay(s, nil)
	case "d":
		if !s.payload.Deletable {
			return screenhost.SetStatus(s, s.payload.DeleteDenied, nil)
		}
		if s.deleteArmed {
			s.pending, s.deleteArmed = true, false
			return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionDeleteComment, CommentID: s.payload.Comment.ID, TaskID: s.payload.Comment.TaskID}}
		}
		s.deleteArmed = true
		return screenhost.SetStatus(s, fmt.Sprintf(frame.Text("tui.confirm.comment_delete_fmt"), s.payload.Comment.ID), nil)
	case "M":
		s.rendered = !s.rendered
		status := frame.Text("tui.status.markdown_raw")
		if s.rendered {
			status = frame.Text("tui.status.markdown_rendered")
		}
		return screenhost.SetStatus(s, status, nil)
	}
	// Every motion spelling is the grid's; on a single-Cell root it declines
	// everything the switch above already claimed.
	kit := frame.Kit()
	s.grid, _ = s.grid.HandleKey(kit, screenlayout.HostBox(kit), keyMsg.String(), s.root(frame))
	return screenhost.Stay(s, nil)
}

func (s Screen) updateEdit(frame screenhost.Frame, msg tea.KeyMsg) screenhost.Outcome {
	kit := frame.Kit()
	if next, handled := s.grid.HandleKey(kit, screenlayout.HostBox(kit), msg.String(), s.root(frame)); handled {
		s.grid = next
		return screenhost.Stay(s, nil)
	}
	switch msg.String() {
	case "ctrl+c":
		return screenhost.Quit(s, nil)
	case "esc":
		s.mode, s.pending, s.err = ModeRead, false, nil
		s.resetInput()
		s = s.syncBodyWindow(frame)
		return screenhost.SetStatus(s, frame.Text("tui.status.cancelled"), nil)
	case "ctrl+s":
		value := strings.TrimSpace(s.input.Value())
		if value == "" {
			return screenhost.SetStatus(s, frame.Text("tui.status.input_required"), nil)
		}
		s.pending, s.err = true, nil
		return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionSaveComment, CommentID: s.payload.Comment.ID, TaskID: s.payload.Comment.TaskID, Value: value}}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return screenhost.Stay(s, cmd)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	s.width, s.height = frame.Width(), frame.Height()
	if event == screenhost.LifecycleEnter {
		s.deleteArmed = false
		s.grid = screengrid.NewState()
	}
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s.resizeInput(frame)
		s = s.syncBodyWindow(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	box := screenlayout.HostBox(kit)
	if s.stateActive(frame) {
		box.Width = frame.Width()
		box.Rows = frame.Height()
	}
	view := screengrid.Render(kit, s.grid, box, s.root(frame)).View
	if s.stateActive(frame) {
		return view
	}
	// screengrid is the single entry to the body. A one-Cell root paints exactly
	// what arranging its section painted: arrangeLeaf rebuilds a
	// screenlayout.Func from the Cell's Spec and body and calls the same
	// screenlayout.ArrangeIn with the same kit, box and layout state. The only
	// delta is clearing Spec.Column/Spec.Group, and sectionBody declares neither.
	return screenkit.Indent("\n"+view, 2)
}
func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(fmt.Sprintf(frame.Text("tui.kicker.comment_fmt"), s.payload.Comment.ID))
	failed := s.err
	if s.mode == ModeEdit {
		failed = nil
	}
	return []screenstate.State{
		id.Failed(failed, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.comment")),
		id.Vazio(s.mode != ModeEdit && s.payload.Comment.ID == 0, frame.Text("tui.empty.comment_not_found"), ""),
	}
}

func (s *Screen) resetInput() {
	s.input.SetValue(s.payload.Comment.Body)
	s.input.Blur()
}
func (s *Screen) resizeInput(frame screenhost.Frame) {
	field.Resize(&s.input, s.EditWidth(frame), s.EditHeight(frame), s.deps.EditTheme)
}

// EditWidth sizes the comment editor to the panel that frames it.
//
// It used to be `AvailableWidth() - 4`, which is the width of a panel's CONTENT
// but not the width of a form allowed to sit in it: field.RenderArea adds
// its own border on top of the number it is given, so the field painted two
// cells wider than the panel's content area and the panel wrapped its border
// mid-glyph. Both numbers now come from the components that own them —
// PanelContentWidth from the panel, the border allowance from the form.
func (s Screen) EditWidth(frame screenhost.Frame) int {
	width := field.WidthFor(frame.Kit().PanelContentWidth(), s.deps.EditTheme)
	if width < 12 {
		return 12
	}
	return width
}
func (s Screen) EditHeight(frame screenhost.Frame) int {
	// Same budget editBlock uses: HostBox minus the growing header the
	// arranger charges before the textarea item (#2444).
	rows := s.viewport(frame)
	header := 3 // kicker + hint + blank
	if s.err != nil {
		header++
	}
	height := rows - header
	if height < 1 {
		return 1
	}
	return height
}

// viewport budgets the detail grid to the same HostBox the arranger uses.
func (s Screen) viewport(frame screenhost.Frame) int {
	return screenlayout.HostBox(frame.Kit()).Rows
}
func (s Screen) OwnsKey(tea.KeyMsg) bool { return true }
func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) BlocksHostInput() bool   { return true }
func (s Screen) BlocksHelp() bool        { return s.mode == ModeEdit }
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if s.mode == ModeEdit {
		return []screenhost.FooterBinding{frame.FooterSave(true), {Key: "alt+enter", Label: frame.Text("tui.footer.newline")}, frame.FooterCancel(false)}
	}
	bindings := []screenhost.FooterBinding{}
	if s.payload.Editable {
		bindings = append(bindings, frame.FooterEdit(true))
	}
	if s.payload.Deletable {
		label := frame.Text("tui.footer.arm_delete")
		if s.deleteArmed {
			label = frame.Text("tui.footer.confirm_delete")
		}
		bindings = append(bindings, screenhost.FooterBinding{Key: "d", Label: label, Primary: s.deleteArmed})
	}
	return append(bindings, frame.FooterScroll(false), frame.FooterPage(false), frame.FooterTopBottom(false), frame.FooterBack(false), frame.FooterHelp(false))
}
func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	if s.mode == ModeEdit {
		return []screenhost.HelpGroup{{ID: "comment_edit", Title: frame.Text("tui.help.comment_edit.title"), Bindings: []screenhost.HelpBinding{
			{Key: "ctrl+s", Description: frame.Text("tui.help.comment_edit.save")},
			{Key: "alt+enter · shift+enter", Description: frame.Text("tui.help.comment_edit.newline")},
			{Key: "esc", Description: frame.Text("tui.help.comment_edit.cancel")},
			{Key: "arrows · home · end", Description: frame.Text("tui.help.comment_edit.caret")},
		}}}
	}
	bindings := []screenhost.HelpBinding{{Key: "↑ ↓ · j k · pgup pgdn · g G", Description: frame.Text("tui.help.comment_view.scroll_body")}}
	if s.payload.Editable {
		bindings = append(bindings, screenhost.HelpBinding{Key: "e", Description: frame.Text("tui.help.comment_view.edit_body")})
	}
	bindings = append(bindings, screenhost.HelpBinding{Key: "M", Description: frame.Text("tui.help.comment_view.toggle_markdown")})
	if s.payload.Deletable {
		bindings = append(bindings, screenhost.HelpBinding{Key: "d · d", Description: frame.Text("tui.help.comment_view.arm_delete")})
	}
	bindings = append(bindings, screenhost.HelpBinding{Key: "esc", Description: frame.Text("tui.help.comment_view.back_task")})
	return []screenhost.HelpGroup{{ID: "comment_view", Title: frame.Text("tui.help.comment_view.title"), Bindings: bindings}}
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
var _ screenhost.HelpBlocker = Screen{}
