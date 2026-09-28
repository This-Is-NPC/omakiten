// Package description owns the route-stacked Task Description reader.
package description

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Screen struct {
	task     domain.Task
	grid     screengrid.State
	md       *markdown.Renderer
	body     *descriptionBodyCache
	width    int
	height   int
	rendered bool
	loading  bool
	err      error
}

type descriptionBodyCache struct {
	memo        screenlayout.BlockMemo[descriptionBodyKey]
	key         descriptionBodyKey
	inputs      []string
	inputsReady bool
	block       screenlayout.Block
	blockReady  bool
	root        screengrid.Node
	rootReady   bool
}

type descriptionBodyKey struct {
	width    int
	rendered bool
}

func New() Screen {
	return Screen{
		grid: screengrid.NewState(),
		body: &descriptionBodyCache{},

		md:       markdown.New(markdown.Tokens{}),
		rendered: true,
	}
}
func (s Screen) ID() screenhost.ID { return screenhost.TaskDescription }
func (s Screen) Scroll() int       { return s.grid.Layout().Offset(bodySpec.ID) }
func (s Screen) Width() int        { return s.width }
func (s Screen) Height() int       { return s.height }
func (s Screen) Task() domain.Task { return s.task }

func (s Screen) Open(task domain.Task) Screen {
	s.task, s.loading, s.err = task, false, nil
	s.grid = screengrid.NewState()
	s.body = &descriptionBodyCache{}
	return s
}

func (s Screen) Apply(task domain.Task, err error) Screen {
	s = s.Open(task)
	s.err = err
	return s
}

func (s Screen) Loading() Screen {
	s.loading, s.err = true, nil
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	s.width, s.height = frame.Width(), frame.Height()
	// Resolve f/esc before delegating declined keys to the markdown arranger.
	switch key.String() {
	case "ctrl+c", "q":
		return screenhost.Quit(s, nil)
	case "esc", "f":
		return screenhost.Back(s, nil)
	case "M":
		s.rendered = !s.rendered
		s = s.resync(frame)
		status := frame.Text("tui.status.markdown_raw")
		if s.rendered {
			status = frame.Text("tui.status.markdown_rendered")
		}
		return screenhost.SetStatus(s, status, nil)
	}
	kit := frame.Kit()
	s.grid, _ = s.grid.HandleKey(kit, screenlayout.HostBox(kit), key.String(), s.cachedRoot(frame))
	return screenhost.Stay(s, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	s.width, s.height = frame.Width(), frame.Height()
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	s = s.resync(frame)
	return screenhost.Stay(s, nil)
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	// The liveness guard is a budget constraint, not a precedence decision:
	// building the candidate set costs an allocation and two catalog lookups on
	// EVERY render, and the keystroke budget measured that at +2 allocations per
	// round trip. Which state wins is still screenstate's to rank — this only
	// asks whether any of them is live at all before paying for the set.
	// err is included here because compose skips the body on the same condition
	// (#126545): omitting it blanks the screen once Failed leaves the hand-painted
	// branch.
	if s.err != nil || s.loading || s.task.ID == 0 {
		if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
			return kit.Panel(body)
		}
	}
	return screenkit.Indent("\n"+s.gridView(frame), 2)
}

// states are the candidates screenstate ranks. The order they are listed in is
// not the precedence — screenstate ranks them — which is the point of handing
// it the whole set instead of writing the chain here.
func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(kit.T("tui.kicker.description"))
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.description")),
		id.Vazio(s.task.ID == 0, frame.Text("tui.empty.task_not_found_refresh"), ""),
	}
}

func (s Screen) section(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: bodySpec,
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.bodyBlock(frame, canvas)
		},
	}
}

func (s Screen) root(frame screenhost.Frame) screengrid.Node {
	section := s.section(frame)
	root := screengrid.Cell(section.Def, section.Body)
	if s.body != nil {
		s.body.root, s.body.rootReady = root, true
	}
	return root
}

func (s Screen) cachedRoot(frame screenhost.Frame) screengrid.Node {
	if s.body != nil && s.body.rootReady {
		return s.body.root
	}
	return s.root(frame)
}

func (s Screen) resync(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.Resync(kit, screenlayout.HostBox(kit), s.root(frame))
	return s
}

func (s Screen) bodyBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	if s.err != nil || s.loading || s.task.ID == 0 {
		return screenlayout.Block{}
	}
	width := canvas.Width()
	key := descriptionBodyKey{width: width, rendered: s.rendered}
	var inputs []string
	if s.body != nil && s.body.inputsReady && s.body.key == key {
		inputs = s.body.inputs
	} else {
		inputs = []string{
			s.task.Description,
			frame.Text("tui.kicker.description_fmt"),
			frame.Text("tui.row.title"),
			frame.Text("tui.row.bucket"),
			frame.Text("tui.kicker.description"),
			frame.Text("tui.empty.task_no_description"),
		}
		if s.body != nil {
			s.body.key, s.body.inputs, s.body.inputsReady = key, inputs, true
		}
	}
	if s.body != nil && s.body.blockReady && s.body.key == key {
		return s.body.block
	}
	build := func() screenlayout.Block {
		return screenlayout.Block{Items: s.composeRead(frame, width)}
	}
	if s.body == nil {
		return build()
	}
	s.body.block = s.body.memo.Block(key, inputs, build)
	s.body.blockReady = true
	return s.body.block
}

func (s Screen) gridView(frame screenhost.Frame) string {
	return s.gridResult(frame).View
}

// gridResult paints the body through screengrid, the single entry to the body.
//
// A one-Cell root paints exactly what arranging its mounted section painted:
// screengrid's arrangeLeaf rebuilds a screenlayout.Func from the Cell's Spec
// and body and calls the same screenlayout.ArrangeIn with the same kit, the
// same box and the same layout state. The only delta it applies is clearing
// Spec.Column/Spec.Group, and bodySpec declares neither.
//
// The Result is the grid's own, not a hand-forged stand-in: the placement it
// reports is the one the walker recorded while painting, so a caller reading
// it cannot drift from what the grid actually decided.
func (s Screen) gridResult(frame screenhost.Frame) screengrid.Result {
	kit := frame.Kit()
	return screengrid.Render(kit, s.grid, screenlayout.HostBox(kit), s.cachedRoot(frame))
}

func (s Screen) OwnsKey(tea.KeyMsg) bool { return true }
func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) BlocksHostInput() bool   { return true }
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{frame.FooterCloseFocus(true), frame.FooterScroll(false), frame.FooterPage(false), frame.FooterTopBottom(false), frame.FooterToggleMarkdown(false), frame.FooterHelp(false)}
}
func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "task_description", Title: frame.Text("tui.kicker.description"), Bindings: []screenhost.HelpBinding{{Key: "f · esc", Description: frame.Text("tui.footer.close_focus")}, {Key: "j k · pgup pgdn · g G", Description: frame.Text("tui.footer.scroll")}, {Key: "M", Description: frame.Text("tui.footer.toggle_markdown")}}}}
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
