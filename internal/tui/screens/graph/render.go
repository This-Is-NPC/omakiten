package graph

import (
	"strings"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if len(s.deps.Dependencies) == 0 {
		content := kit.Styles.HintBox.Width(screenkit.Clamp(kit.AvailableWidth()-8, 32, 60)).Render(strings.Join([]string{
			kit.Styles.FocusKickerCount(kit.T("tui.kicker.dependency_graph"), 0),
			"",
			kit.Styles.Hint.Render(kit.T("tui.empty.graph_no_deps")),
			kit.Styles.Hint.Render(kit.T("tui.graph.use_okt_depend")) + kit.Styles.HintAccent.Render(kit.T("tui.graph.depend_cmd")) + kit.Styles.Hint.Render(kit.T("tui.graph.depend_suffix")),
		}, "\n"))
		return "\n" + screenkit.Indent(content, 2)
	}

	// Panel owns the leading blank + border; the root Cell fills the panel
	// content box with zero overdraw (panelBox.Rows == PanelChrome viewport).
	return "\n" + screenkit.Indent(screengrid.Render(kit, s.grid, s.panelBox(kit), s.bodyRoot(kit)).View, 2)
}
