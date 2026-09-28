// Package entitydetail owns the route-stacked settings entity detail surface.
package entitydetail

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Kind string

const (
	KindLaw      Kind = "laws"
	KindPersona  Kind = "personas"
	KindSkill    Kind = "skills"
	KindTemplate Kind = "templates"
)

type Row struct {
	Label string
	Value string
}

type Payload struct {
	Kind      Kind
	Slug      string
	Header    string
	Rows      []Row
	BodyLabel string
	Body      string
	Extra     string
	NotFound  string
}

type Screen struct {
	payload  Payload
	grid     screengrid.State
	md       *markdown.Renderer
	body     *entityDetailBodyCache
	delete   bool
	loading  bool
	err      error
	rendered bool
}

type entityDetailBodyCache struct {
	memo screenlayout.BlockMemo[entityDetailBodyKey]
}

type entityDetailBodyKey struct {
	width    int
	rendered bool
}

func New() Screen {
	return Screen{grid: screengrid.NewState(), md: markdown.New(screenkit.MarkdownTokens{}), body: &entityDetailBodyCache{}, rendered: true}
}
func (s Screen) ID() screenhost.ID { return screenhost.EntityDetail }
func (s Screen) Payload() Payload  { return s.payload }
func (s Screen) Scroll() int       { return s.grid.Layout().Offset(sectionBody) }
func (s Screen) Open(payload Payload) Screen {
	s.payload, s.grid, s.delete, s.loading, s.err = payload, screengrid.NewState(), false, false, nil
	s.body = &entityDetailBodyCache{}
	return s
}
func (s Screen) Apply(payload Payload, err error) Screen {
	s = s.Open(payload)
	s.err = err
	return s
}
func (s Screen) Loading() Screen { s.loading, s.err = true, nil; return s }

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	action := func(kind screenhost.ActionKind) screenhost.Outcome {
		return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: kind, EntityKind: string(s.payload.Kind), Value: s.payload.Slug}}
	}
	switch key.String() {
	case "esc":
		if s.delete {
			s.delete = false
			return action(screenhost.ActionCancelEntityDelete)
		}
		return screenhost.Back(s, nil)
	case "e":
		s.delete = false
		return action(screenhost.ActionEditEntity)
	case "d":
		if s.payload.Kind == KindTemplate {
			s.delete = false
			return action(screenhost.ActionTemplateDeleteHint)
		}
		if s.delete {
			s.delete = false
			return action(screenhost.ActionDeleteEntity)
		}
		s.delete = true
		return action(screenhost.ActionPrepareEntityDelete)
	case "p":
		s.delete = false
		if s.payload.Kind == KindPersona {
			return action(screenhost.ActionOpenPersonaSkills)
		}
	case "a":
		s.delete = false
		if s.payload.Kind == KindTemplate {
			return action(screenhost.ActionOpenTemplateDefault)
		}
	case "r":
		s.delete = false
		return screenhost.Reload(s, nil)
	case "M":
		s.delete = false
		s.rendered = !s.rendered
	}
	// Every motion spelling is the grid's; on a single-Cell root it declines
	// everything the switch above did not already claim.
	kit := frame.Kit()
	s.grid, _ = s.grid.HandleKey(kit, screenlayout.HostBox(kit), key.String(), s.bodyRoot(frame))
	return screenhost.Stay(s, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.delete = false
		s.grid = screengrid.NewState()
	}
	// A resize needs no offset reset of its own: syncBodyWindow re-clamps the
	// section's offset against the geometry that now exists, so a window that no
	// longer has a document under it settles onto one that does.
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.syncBodyWindow(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "esc", "e", "d", "p", "a", "r", "M", "up", "down", "j", "k", "pgup", "pgdn", "pgdown", "ctrl+u", "ctrl+d", "g", "G", "home", "end":
		return true
	}
	return false
}
func (s Screen) OwnsFooter() bool      { return true }
func (s Screen) BlocksHostInput() bool { return true }
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if s.delete {
		return []screenhost.FooterBinding{{Key: "d", Label: frame.Text("tui.footer.confirm_delete"), Primary: true}, frame.FooterCancel(false)}
	}
	bindings := []screenhost.FooterBinding{frame.FooterBack(true), frame.FooterEdit(false)}
	if s.payload.Kind != KindTemplate {
		bindings = append(bindings, screenhost.FooterBinding{Key: "d", Label: frame.Text("tui.footer.arm_delete")})
	}
	if s.payload.Kind == KindPersona {
		bindings = append(bindings, screenhost.FooterBinding{Key: "p", Label: frame.Text("tui.footer.skills_persona")})
	}
	if s.payload.Kind == KindTemplate {
		bindings = append(bindings, screenhost.FooterBinding{Key: "a", Label: frame.Text("tui.footer.set_default")})
	}
	return append(bindings, frame.FooterRefresh(false), frame.FooterToggleMarkdown(false), frame.FooterHelp(false))
}
func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "entity_detail", Title: frame.Text("tui.help.entity_screen.title"), Bindings: []screenhost.HelpBinding{
		{Key: "j k · pgup pgdn · g G", Description: frame.Text("tui.help.entity_screen.scroll")},
		{Key: "e · d d", Description: frame.Text("tui.help.entity_screen.edit_delete")},
		{Key: "r · M", Description: frame.Text("tui.help.entity_screen.refresh_markdown")},
		{Key: "esc", Description: frame.Text("tui.help.entity_screen.back")},
	}}}
}
func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if s.err != nil || s.loading || s.payload.NotFound != "" {
		if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
			return kit.Panel(body)
		}
	}
	// A root Cell resolves inside an explicit box, so it does not paint the
	// screen-opening row that the old host-level Arrange call owned. Keep that
	// one row at the mount while every nested Cell remains box-local.
	return screenkit.Indent("\n"+screengrid.Render(kit, s.grid, screenlayout.HostBox(kit), s.bodyRoot(frame)).View, 2)
}

func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	kicker := screenkit.Sanitize(s.payload.Header)
	if kicker == "" {
		kicker = frame.Text("tui.help.entity_screen.title")
	}
	id := screenstate.For(kicker)
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.generic")),
		id.Vazio(s.payload.NotFound != "", screenkit.Sanitize(s.payload.NotFound), ""),
	}
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
