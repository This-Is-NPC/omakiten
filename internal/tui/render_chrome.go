package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHeader renders the global title bar + view navigation row. The
// title (`omakiten · project · local checkpoint`) is always visible; the
// view tabs are hidden when an overlay is open (help/task/entity) so the
// overlay's own kicker reads as the focused surface. Falls back to a
// compact "active tab + hint" form when the full nav row would overflow.
//
// On the Home view the per-project tab bar is suppressed (Home is outside
// the tab cycle and only reachable via ctrl+h), and the title swaps the
// project segment for an explicit "select a project" hint so the absent
// project slug never reads as a render bug.
func (m Model) renderHeader() string {
	var sb strings.Builder
	sb.WriteString("\n  ")
	sb.WriteString(m.styles.title.Render("omakiten"))
	sb.WriteString(m.styles.hint.Render(" › "))
	if m.view == viewHome {
		sb.WriteString(m.styles.nav.Render("home"))
		sb.WriteString(m.styles.hint.Render(" · select a project"))
		sb.WriteString("\n\n  ")
		sb.WriteString(m.homeHeaderTitle())
		return sb.String()
	}
	sb.WriteString(m.styles.nav.Render(m.project.Slug))
	sb.WriteString(m.styles.hint.Render(" · local checkpoint"))
	if m.helpOpen || m.taskScreen != taskScreenClosed || m.entityScreen != entityScreenClosed {
		return sb.String()
	}
	sb.WriteString("\n\n  ")

	const navGap = "   "
	items := make([]string, 0, len(viewNames))
	rules := make([]string, 0, len(viewNames))
	for i, name := range viewNames {
		label := fmt.Sprintf("%02d // %s", i+1, name)
		width := lipgloss.Width(label)
		if i == m.view {
			items = append(items, m.styles.activeNav.Render(label))
			rules = append(rules, m.styles.activeNav.Render(strings.Repeat("─", width)))
		} else {
			items = append(items, m.styles.nav.Render(label))
			rules = append(rules, strings.Repeat(" ", width))
		}
	}
	if lipgloss.Width(strings.Join(items, navGap)) > m.availableWidth() {
		active := fmt.Sprintf("%02d // %s", m.view+1, viewNames[m.view])
		sb.WriteString(m.styles.activeNav.Render(active))
		sb.WriteString(m.styles.hint.Render("  tab/1-6 switch views"))
		return sb.String()
	}
	sb.WriteString(strings.Join(items, navGap))
	sb.WriteString("\n  ")
	sb.WriteString(strings.Join(rules, navGap))
	return sb.String()
}

// renderInput renders the modal text-input bar shown when the user is
// adding a comment or typing a move target. Lives in the chrome layer
// because it's a screen-level surface, not a per-view widget.
func (m Model) renderInput() string {
	return indentBlock(m.styles.input.Render(fmt.Sprintf("%s: %s", m.status, m.input)), 2)
}

// renderCurrentView is the screen dispatcher: which renderer fires for the
// current state. Order matters — the comment screen overlays the task
// screen, which overlays the entity screen, which overlays the view tabs.
func (m Model) renderCurrentView() string {
	if m.commentScreenOpen {
		return m.renderCommentScreen()
	}
	if m.taskScreen != taskScreenClosed {
		return m.renderTaskScreen()
	}
	if m.entityScreen != entityScreenClosed {
		return m.renderEntityScreen()
	}
	if m.view == viewHome {
		return m.renderHome()
	}
	switch m.view {
	case 0:
		return m.renderBoard()
	case 1:
		return m.renderTable()
	case 2:
		return m.renderGraph()
	case 3:
		return m.renderConfig()
	case 4:
		return m.renderLogs()
	case 5:
		return m.renderStats()
	default:
		return ""
	}
}

// renderFooter renders the keybindings hint at the bottom of the screen.
// The big switch ladders through every modal/overlay/view in priority
// order so the most-specific footer wins (e.g. an open picker shows its
// keys, not the underlying screen's).
func (m Model) renderFooter() string {
	var text string
	switch {
	case m.isEmbeddedCommentInput():
		text = "enter save comment  alt+enter/shift+enter newline  esc cancel"
	case m.blockerPickerOpen:
		text = "up/down move  pgup/pgdn scroll  space toggle blocker  ctrl+s save  esc cancel"
	case m.mode != modeNormal:
		text = "enter save  esc cancel  ctrl+c quit"
	case m.commentScreenOpen:
		text = "j/k scroll  pgup/pgdn page  g/G top/bottom  esc back to task  ? help"
	case m.taskScreen == taskScreenView:
		text = "tab focus  j/k scroll  e edit  b blockers  c comment  m move  r refresh  esc board  ? help"
	case m.taskScreen == taskScreenCreate:
		text = "tab field  ←/→ priority  ctrl+s create  esc cancel"
	case m.taskScreen == taskScreenEdit:
		text = "tab field  ←/→ priority  ctrl+b blockers  ctrl+s save  esc view"
	case m.entityScreen == entityScreenView && m.deletePending:
		text = "d confirm delete  esc cancel  q quit"
	case m.entityScreen == entityScreenView:
		text = "j/k scroll  e edit in $EDITOR  d arm delete  p skills (persona)  r refresh  esc config"
	case m.entityScreen == entityScreenSkillPicker:
		text = "up/down move  pgup/pgdn scroll  space toggle  enter on '+': new skill  ctrl+s save  esc cancel"
	case m.entityScreen == entityScreenThemePicker:
		text = "up/down move  pgup/pgdn scroll  enter apply (hot-reload)  esc cancel"
	case m.entityScreen == entityScreenConfigPicker:
		text = "up/down move  pgup/pgdn scroll  enter select (restart required)  esc cancel"
	case m.entityScreen == entityScreenDefaultPicker:
		text = "up/down move  pgup/pgdn scroll  enter assign (clears prior owner)  esc cancel"
	case m.moveMode:
		text = "left/right move task to lane  esc cancel  q quit"
	case m.view == viewHome:
		text = m.homeFooterHint()
	case m.view == 0:
		text = "left/right lanes  up/down tasks  pgup/pgdn scroll  enter open  n new  e edit  m move  ? help"
	case m.view == 3 && m.deletePending:
		text = "d confirm delete  esc cancel  left/right changes target"
	case m.view == 3 && m.entityKind == entityKindTag:
		text = "left/right section  up/down select  d arm delete (orphan)  D delete all orphans  ? help"
	case m.view == 3:
		text = "left/right section  up/down select  enter open  n new  e edit  d arm delete  a default(template)  t theme  c config  ? help"
	case m.view == 4:
		text = "left/right switch view  up/down select row  pgup/pgdn scroll  g/G top/bottom  r refresh  ? help"
	case m.view == 5:
		text = "← → period (7d / 30d / all)  r refresh  ? help"
	case m.view == 2:
		text = "left/right switch view  j/k move  pgup/pgdn scroll  g/G top/bottom  enter open  ? help"
	default:
		text = "tab switch view  up/down select  pgup/pgdn scroll  g/G top/bottom  enter open  n new  m move  ? help"
	}
	return "\n" + indentBlock(m.styles.footer.Render(text), 2)
}
