// Package knowledge renders the file-backed project knowledge catalog.
package knowledge

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"omakiten/internal/domain"
	"omakiten/internal/graph"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

const sectionBody = screenlayout.ID("project-knowledge-body")

// Screen presents resources and follows their file-backed relations.
type Screen struct {
	snapshot domain.KnowledgeSnapshot
	lines    []graph.KnowledgeLine
	grid     screengrid.State
	selected int
	detail   bool
	graph    bool
	md       *markdown.Renderer
	cache    *detailCache
}

type detailCache struct {
	lines []string
	width int
}

func New() Screen {
	return Screen{grid: screengrid.NewState(), md: markdown.New(screenkit.MarkdownTokens{}), cache: &detailCache{}}
}

func (s Screen) ID() screenhost.ID { return screenhost.ProjectKnowledge }

func (s Screen) Snapshot() domain.KnowledgeSnapshot { return s.snapshot }

func (s Screen) Apply(snapshot domain.KnowledgeSnapshot, lines []graph.KnowledgeLine) Screen {
	s.snapshot = snapshot
	s.lines = lines
	s.grid = screengrid.NewState()
	s.detail = false
	s.graph = true
	s.cache = &detailCache{}
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	switch key.String() {
	case "esc":
		if s.detail {
			s.detail = false
			s.cache = &detailCache{}
			s.grid = screengrid.NewState()
			s = s.resync(frame)
			return screenhost.Stay(s, nil)
		}
		return screenhost.Back(s, nil)
	case "enter":
		if !s.detail {
			if selected, ok := s.selectedResource(); ok {
				s.selected, s.detail = selected, true
				s.grid = screengrid.NewState()
				s.cache = &detailCache{}
				s = s.resync(frame)
			}
		}
		return screenhost.Stay(s, nil)
	case "g", "l":
		if !s.detail {
			s.graph = key.String() == "g"
			s.grid = screengrid.NewState()
			s = s.resync(frame)
		}
		return screenhost.Stay(s, nil)
	case "r":
		return screenhost.Reload(s, nil)
	}
	kit := frame.Kit()
	s.grid, _ = s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.root(kit))
	return screenhost.Stay(s, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		s.cache = &detailCache{}
		s = s.resync(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	return "\n" + screenkit.Indent(screengrid.Render(kit, s.grid, s.panelBox(kit), s.root(kit)).View, 2)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	for _, binding := range screenlayout.StandardBindings() {
		for _, owned := range binding.Keys {
			if key.String() == owned {
				return true
			}
		}
	}
	switch key.String() {
	case "enter", "esc", "r", "g", "l":
		return true
	}
	return false
}

func (s Screen) OwnsFooter() bool { return true }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{frame.FooterOpen(true), {Key: "g", Label: "graph"}, {Key: "l", Label: "list"}, frame.FooterMoveVim(false), frame.FooterScrollPage(false), frame.FooterRefresh(false), frame.FooterBack(false), frame.FooterHelp(false)}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "project_knowledge", Title: frame.Text("tui.knowledge.title"), Bindings: []screenhost.HelpBinding{
		{Key: "enter", Description: frame.Text("tui.knowledge.open")},
		{Key: "g", Description: "show nodes and relations"},
		{Key: "l", Description: "show resource list"},
		{Key: "j k · pgup · pgdn · g G", Description: frame.Text("tui.footer.scroll")},
		{Key: "r", Description: frame.Text("tui.footer.refresh")},
		{Key: "esc", Description: frame.Text("tui.footer.back")},
	}}}
}

func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().ViewportRows()
	if kit.Height <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	return screenlayout.Box{Width: kit.PanelContentWidth(), Rows: rows}
}

func (s Screen) root(kit screenkit.Kit) screengrid.Node {
	spec := screenlayout.Spec{ID: sectionBody, MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems, SelectFirst: !s.detail}
	return screengrid.Cell(spec, func(canvas screenlayout.Canvas) screenlayout.Block { return s.body(kit, canvas) })
}

func (s Screen) resync(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	if s.detail {
		s.prepareDetail(kit, max(1, s.panelBox(kit).Width-panel.Borders))
	}
	s.grid = s.grid.Resync(kit, s.panelBox(kit), s.root(kit))
	return s
}

func (s Screen) body(kit screenkit.Kit, canvas screenlayout.Canvas) screenlayout.Block {
	width := max(1, canvas.Width())
	inner := max(1, width-panel.Borders)
	if s.detail {
		block := framed.List(kit.Styles.Border, width, kit.Styles.FocusKicker(kit.T("tui.knowledge.title")), nil, s.cache.lines)
		block.Cursor = screenlayout.NoSelection()
		return block
	}
	var items []string
	var selectable []int
	if s.graph {
		items, selectable = s.graphRows(inner)
	} else {
		items, selectable = s.listRows(inner)
	}
	if cursor := canvas.Cursor(); cursor >= 0 && cursor < len(items) {
		items[cursor] = kit.Styles.HintAccent.Render(items[cursor])
	}
	for _, diagnostic := range s.snapshot.Diagnostics {
		items = append(items, screenkit.Truncate(screenkit.Sanitize("! "+diagnostic), inner))
	}
	if len(items) == 0 {
		items = []string{kit.T("tui.knowledge.empty")}
	}
	block := framed.List(kit.Styles.Border, width, kit.Styles.FocusKickerCount(kit.T("tui.knowledge.title"), len(s.snapshot.Resources)), nil, items)
	block.Selectable = selectable
	return block
}

func (s Screen) listRows(width int) ([]string, []int) {
	items := make([]string, 0, len(s.snapshot.Resources))
	selectable := make([]int, len(s.snapshot.Resources))
	for i, item := range s.snapshot.Resources {
		items = append(items, screenkit.Truncate(screenkit.Sanitize(item.Project+":"+item.ID+"  "+item.Title), width))
		selectable[i] = i
	}
	return items, selectable
}

func (s Screen) graphRows(width int) ([]string, []int) {
	items := make([]string, 0, len(s.lines))
	selectable := make([]int, 0, len(s.lines))
	for i, line := range s.lines {
		items = append(items, screenkit.Truncate(screenkit.Sanitize(line.Text), width))
		if line.ResourceID != "" {
			selectable = append(selectable, i)
		}
	}
	return items, selectable
}

func (s Screen) selectedResource() (int, bool) {
	cursor := s.grid.Layout().Cursor(sectionBody)
	if !s.graph {
		return cursor, cursor >= 0 && cursor < len(s.snapshot.Resources)
	}
	if cursor < 0 || cursor >= len(s.lines) || s.lines[cursor].ResourceID == "" {
		return 0, false
	}
	for i, item := range s.snapshot.Resources {
		if item.Project+":"+item.ID == s.lines[cursor].ResourceID {
			return i, true
		}
	}
	return 0, false
}

func (s Screen) prepareDetail(kit screenkit.Kit, width int) {
	if s.cache == nil || s.cache.width == width && s.cache.lines != nil {
		return
	}
	s.cache.width = width
	s.cache.lines = s.detailLines(kit, width)
}

func (s Screen) detailLines(kit screenkit.Kit, width int) []string {
	item := s.snapshot.Resources[s.selected]
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n`%s:%s` · `%s`\n\n", item.Title, item.Project, item.ID, item.Path)
	if item.Description != "" {
		b.WriteString(item.Description + "\n\n")
	}
	b.WriteString(item.Body)
	for _, relation := range s.snapshot.Relations {
		if relation.From == item.Project+":"+item.ID {
			fmt.Fprintf(&b, "\n- %s → `%s`", relation.Kind, relation.To)
		}
	}
	s.md.Reload(kit.Markdown)
	return screenkit.CapRows(strings.Split(markdown.Body(s.md, b.String(), width, true), "\n"), width)
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
