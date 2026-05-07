package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderHelp() string {
	type binding struct{ key, desc string }
	type group struct {
		title    string
		bindings []binding
	}
	groups := []group{
		{"Global", []binding{
			{"?", "close this overlay"},
			{"a", "toggle all bindings"},
			{"q · ctrl+c", "quit"},
			{"tab · shift+tab", "cycle zones (Tasks · Stats · Settings)"},
			{"1 · 2 · 3", "jump to zone (Tasks · Stats · Settings)"},
			{", · /", "previous · next sub-menu inside the current zone"},
			{"0 · ctrl+h", "back to multi-project home"},
			{"r", "refresh"},
		}},
		{"Home", []binding{
			{"↑ ↓ · j k", "move project selection"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last project"},
			{"enter", "open project (loads board)"},
			{"ctrl+h", "reload home (refresh tags / counts)"},
			{"q · ctrl+c", "quit"},
		}},
		{"Tasks · board lens", []binding{
			{"← ↑ ↓ → · h j k l", "navigate lanes and tasks (auto-scrolls column)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll focused column by page"},
			{"g · G", "first / last card in column"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"c", "add comment"},
			{"m", "move task between lanes"},
		}},
		{"Tasks · table lens", []binding{
			{"↑ ↓ · j k", "select task (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last task"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"m", "move by bucket key"},
		}},
		{"Tasks · graph lens", []binding{
			{"← →", "switch view"},
			{"↑ ↓ · j k", "move cursor"},
			{"enter", "open task"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
		}},
		{"Task view", []binding{
			{"tab · shift+tab", "switch focus (form ⇄ activity)"},
			{"↑ ↓ · j k", "scroll description (form) · navigate cards (activity)"},
			{"J · K", "navigate activity cards (any focus)"},
			{"enter", "open focused comment in detail view"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
			{"e", "edit"},
			{"b", "edit blockers"},
			{"c", "add comment"},
			{"m", "move"},
			{"esc", "back to board"},
		}},
		{"Comment view", []binding{
			{"↑ ↓ · j k", "scroll body"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
			{"esc", "back to task view"},
		}},
		{"Comment input", []binding{
			{"enter", "save comment"},
			{"alt+enter · shift+enter", "insert newline"},
			{"esc", "cancel"},
		}},
		{"Task form", []binding{
			{"tab", "switch field"},
			{"← → · h l", "change priority"},
			{"ctrl+b", "edit blockers when editing an existing task"},
			{"enter · alt+enter · shift+enter", "newline in description"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Blocker picker", []binding{
			{"↑ ↓ · j k", "move (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last candidate"},
			{"space", "toggle blocker"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Settings · config", []binding{
			{"← →", "switch entity kind"},
			{"↑ ↓", "select entity"},
			{"enter", "open detail"},
			{"n", "new entity"},
			{"e", "edit in $EDITOR"},
			{"d · d", "arm delete, then confirm"},
			{"p", "skill picker (persona)"},
		}},
		{"Entity view", []binding{
			{"↑ ↓ · j k", "scroll body"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
			{"e", "edit (opens $EDITOR)"},
			{"d · d", "arm delete, then confirm"},
			{"p", "skill picker (persona)"},
			{"esc", "back, or cancel pending delete"},
		}},
		{"Skill picker", []binding{
			{"↑ ↓ · j k", "move (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last row"},
			{"space", "toggle"},
			{"enter on '+ create new'", "scaffold new skill"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Stats · logs", []binding{
			{"← →", "switch view"},
			{"↑ ↓ · j k", "select row (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last row"},
			{"r", "refresh"},
		}},
		{"Stats · general", []binding{
			{"← →", "cycle period (7d / 30d / all)"},
			{"r", "refresh"},
		}},
	}

	if !m.helpAll {
		wanted := map[string]bool{"Global": true}
		for _, title := range m.currentHelpTitles() {
			wanted[title] = true
		}
		filtered := make([]group, 0, len(wanted))
		for _, g := range groups {
			if wanted[g.title] {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}

	const keyW = 34
	var lines []string
	title := "Keybindings · current context"
	if m.helpAll {
		title = "Keybindings · all contexts"
	}
	lines = append(lines, m.styles.kicker(title), m.styles.hint.Render("press a to toggle scope"), "")
	for _, g := range groups {
		lines = append(lines, m.styles.kicker(g.title))
		lines = append(lines, m.styles.separator.Render(strings.Repeat("─", keyW+24)))
		for _, b := range g.bindings {
			pad := keyW - lipgloss.Width(b.key)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, m.styles.hintAccent.Render(b.key)+strings.Repeat(" ", pad)+b.desc)
		}
		lines = append(lines, "")
	}

	viewport := m.helpViewportRows()
	if viewport > 0 && len(lines) > viewport {
		visible, above, below := sliceViewport(lines, m.help.Scroll, viewport-1)
		return "\n" + indentBlock(strings.Join(visible, "\n")+"\n"+m.viewportFooterHint(above, below), 2)
	}
	return "\n" + indentBlock(strings.Join(lines, "\n"), 2)
}

// helpViewportRows returns the line budget for the help screen content. Help
// view chrome is small: header (2) + leading blank from renderHelp (1) + help
// footer (1).
func (m Model) helpViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 4
	rows := m.height - chrome
	if rows < 8 {
		return 0
	}
	return rows
}

func (m Model) renderHelpFooter() string {
	return indentBlock(m.styles.footer.Render("j/k pgup/pgdn g/G scroll · a all/current · ?/esc/q close help"), 2)
}

func (m Model) currentHelpTitles() []string {
	switch {
	case m.isEmbeddedCommentInput():
		return []string{"Comment input"}
	case m.blockerPickerOpen:
		return []string{"Blocker picker"}
	case m.commentScreenOpen:
		return []string{"Comment view"}
	case m.taskScreen == taskScreenCreate || m.taskScreen == taskScreenEdit:
		return []string{"Task form"}
	case m.taskScreen == taskScreenView:
		return []string{"Task view"}
	case m.entityScreen == entityScreenSkillPicker:
		return []string{"Skill picker"}
	case m.entityScreen == entityScreenView:
		return []string{"Entity view"}
	case m.onHome():
		return []string{"Home"}
	case m.sub == subBoard:
		return []string{"Tasks · board lens"}
	case m.sub == subTable:
		return []string{"Tasks · table lens"}
	case m.sub == subGraph:
		return []string{"Tasks · graph lens"}
	case m.sub == subSettingsConfig:
		return []string{"Settings · config"}
	case m.sub == subStatsLogs:
		return []string{"Stats · logs"}
	case m.sub == subStatsGeneral:
		return []string{"Stats · general"}
	default:
		return []string{"Tasks · board lens"}
	}
}
