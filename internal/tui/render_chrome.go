package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/keyfooter"
)

// renderHeader renders the global title breadcrumb + the navigation
// kicker. The breadcrumb (`omakiten › project · local checkpoint`) is
// always visible; the nav rows are hidden when an overlay is open
// (help/task/entity) so the overlay's own kicker reads as the focused
// surface. The Home tile is folded into the same top strip as Tasks /
// Stats / Settings, separated by a faded `│` divider — it stays
// reachable from every per-project view without forcing a separate
// chrome layout. Falls back to a compact "active tab + hint" form when
// the full nav row would overflow.
//
// While on Home itself the per-project nav strip is suppressed (the
// rest of the app's tops have no meaning before a project is picked),
// and the breadcrumb swaps the project segment for an explicit
// "select a project" hint so the absent project slug never reads as a
// render bug.
func (m Model) renderHeader() string {
	var sb strings.Builder
	sb.WriteString("\n  ")
	sb.WriteString(m.styles.title.Render("omakiten"))
	sb.WriteString(m.styles.hint.Render(" › "))
	if m.onHome() {
		sb.WriteString(m.styles.nav.Render("home"))
		sb.WriteString(m.styles.hint.Render(" · select a project"))
		sb.WriteString("\n\n  ")
		sb.WriteString(m.homeHeaderTitle())
		return sb.String()
	}
	sb.WriteString(m.styles.nav.Render(truncateText(m.project.Slug, 40)))
	sb.WriteString(m.styles.hint.Render(" · local checkpoint"))
	if m.helpOpen || m.taskScreen != taskScreenClosed || m.entityScreen != entityScreenClosed {
		return sb.String()
	}
	sb.WriteString("\n\n  ")

	const navGap = "   "
	homeLabel := "00 // HOME"
	homeItem := m.styles.nav.Render(homeLabel)
	homeRule := strings.Repeat(" ", lipgloss.Width(homeLabel))
	divider := m.styles.hint.Render("│")

	topItems := make([]string, 0, len(topOrder))
	topRules := make([]string, 0, len(topOrder))
	for i, t := range topOrder {
		label := fmt.Sprintf("%02d // %s", i+1, topLabels[t])
		width := lipgloss.Width(label)
		if t == m.top {
			topItems = append(topItems, m.styles.activeNav.Render(label))
			topRules = append(topRules, m.styles.activeNav.Render(strings.Repeat("─", width)))
		} else {
			topItems = append(topItems, m.styles.nav.Render(label))
			topRules = append(topRules, strings.Repeat(" ", width))
		}
	}
	stripItems := homeItem + navGap + divider + navGap + strings.Join(topItems, navGap)
	stripRules := homeRule + navGap + " " + navGap + strings.Join(topRules, navGap)
	if lipgloss.Width(stripItems) > m.availableWidth() {
		active := fmt.Sprintf("%02d // %s", topIndex(m.top)+1, topLabels[m.top])
		sb.WriteString(m.styles.activeNav.Render(active))
		sb.WriteString(m.styles.hint.Render("  tab/1-3 switch zones · ,// switch sub · 0 home · ctrl+o back"))
		return sb.String()
	}
	sb.WriteString(stripItems)
	sb.WriteString("\n  ")
	sb.WriteString(stripRules)

	subs := subsByTop[m.top]
	if len(subs) > 1 {
		subItems := make([]string, 0, len(subs))
		subRules := make([]string, 0, len(subs))
		for _, s := range subs {
			label := fmt.Sprintf("// %s", subLabels[s])
			width := lipgloss.Width(label)
			if s == m.sub {
				subItems = append(subItems, m.styles.activeNav.Render(label))
				subRules = append(subRules, m.styles.activeNav.Render(strings.Repeat("─", width)))
			} else {
				subItems = append(subItems, m.styles.nav.Render(label))
				subRules = append(subRules, strings.Repeat(" ", width))
			}
		}
		sb.WriteString("\n  ")
		sb.WriteString(strings.Join(subItems, navGap))
		sb.WriteString("\n  ")
		sb.WriteString(strings.Join(subRules, navGap))
	}
	return sb.String()
}

// renderInput renders the modal text-input bar shown when the user is
// typing a move target (modeMove). The comment modal lives inside the
// task-view chrome and is rendered by renderCommentInput; this top-bar
// surface is only active for modeMove. Lives in the chrome layer because
// it's a screen-level surface, not a per-view widget.
func (m Model) renderInput() string {
	// Modal input is always the active surface while it's on screen, so
	// override the neutral default border with the accent — the user is
	// typing here and the focus cue should match the form's focused-field
	// treatment.
	input := m.moveInput
	input.Cursor.Style = m.styles.cursor
	style := m.styles.input.BorderForeground(m.styles.hintAccent.GetForeground())
	return indentBlock(style.Render(fmt.Sprintf("%s: %s", m.status, input.View())), 2)
}

// renderCurrentView is the screen dispatcher: which renderer fires for the
// current state. Order matters — the comment screen overlays the task
// screen, which overlays the entity screen, which overlays the view tabs.
func (m Model) renderCurrentView() string {
	if m.commentScreenOpen {
		return m.renderCommentScreen()
	}
	if m.descriptionScreenOpen {
		return m.renderDescriptionScreen()
	}
	if m.taskScreen != taskScreenClosed {
		return m.renderTaskScreen()
	}
	if m.entityScreen != entityScreenClosed {
		return m.renderEntityScreen()
	}
	if m.onHome() {
		return m.renderHome()
	}
	switch m.sub {
	case subBoard:
		return m.renderBoard()
	case subTable:
		return m.renderTable()
	case subGraph:
		return m.renderGraph()
	case subPlans:
		if m.planNetworkOpen {
			return m.renderPlanNetwork()
		}
		return m.renderPlans()
	case subStatsGeneral:
		return m.renderStats()
	case subStatsLogs:
		return m.renderLogs()
	case subSettingsGeneral:
		return m.renderSettingsGeneral()
	case subSettingsLaws:
		return m.renderSettingsEntity(entityKindLaw)
	case subSettingsPersonas:
		return m.renderSettingsEntity(entityKindPersona)
	case subSettingsSkills:
		return m.renderSettingsEntity(entityKindSkill)
	case subSettingsTemplates:
		return m.renderSettingsEntity(entityKindTemplate)
	case subSettingsTags:
		return m.renderSettingsEntity(entityKindTag)
	case subSettingsGuards:
		return m.renderSettingsGuards()
	default:
		return ""
	}
}

// footerToken is one keybinding entry on the footer hint row. `Primary`
// signals that the action is the focal verb for the current surface
// (`enter` / `n` / `e` etc.) — `renderFooter` highlights up to three
// primaries with `hintAccent` so the eye lands on them first; the rest
// stay in the muted `hint` style.
type footerToken struct {
	key     string
	label   string
	primary bool
}

// renderFooter ladders through every modal/overlay/view in priority
// order — the most-specific surface wins — and emits its keybinding
// hint as a list of structured tokens. The renderer applies the
// primary / secondary styling, joins with double-space separators,
// guarantees `?` is the trailing token whenever help is reachable,
// and indents the row to match the rest of the chrome.
func (m Model) renderFooter() string {
	tokens := m.footerTokens()
	return "\n" + indentBlock(m.formatFooterTokens(tokens), 2)
}

// formatFooterTokens lays the token list out for the footer row. Up to
// three primary tokens get `hintAccent` (more than that and the visual
// hierarchy stops landing); the rest get `hint`. Tokens are joined
// with double-space separators so the eye can chunk them cleanly.
func (m Model) formatFooterTokens(tokens []footerToken) string {
	return keyfooter.Render(toKeyFooterTokens(tokens), keyfooter.Styles{
		Primary:   m.styles.hintAccent,
		Secondary: m.styles.footer,
	})
}

func toKeyFooterTokens(tokens []footerToken) []keyfooter.Token {
	out := make([]keyfooter.Token, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, keyfooter.Token{Key: t.key, Label: t.label, Primary: t.primary})
	}
	return out
}

// helpToken returns the `?` token used at the trailing edge of every
// surface that has the help overlay reachable. Centralised so the
// `?` always appears last and never accidentally becomes a primary.
func (m Model) helpToken() footerToken {
	return footerToken{key: "?", label: m.t("tui.footer.help")}
}

// escToken standardises the verbal of the Esc binding across overlays.
// Every "go back" / "close overlay" / "cancel modal" footer renders it
// the same way — `esc back` — so users do not have to retrain the
// shortcut between surfaces. Pickers that explicitly *cancel* (vs.
// step back) keep their own `esc cancel` token because the action is
// destructive on save state.
func (m Model) escBack() footerToken {
	return footerToken{key: "esc", label: m.t("tui.footer.back")}
}

// footerTokens returns the keybinding hint for the active surface as
// a structured list. Order encodes priority: the most relevant action
// for the surface comes first (and is usually marked `primary`).
func (m Model) footerTokens() []footerToken {
	switch {
	case m.isEmbeddedCommentInput():
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.save_comment"), primary: true},
			{key: "alt+enter/shift+enter", label: m.t("tui.footer.newline")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.blockerPickerOpen:
		return []footerToken{
			{key: "space", label: m.t("tui.footer.toggle_blocker"), primary: true},
			{key: "ctrl+s", label: m.t("tui.footer.save"), primary: true},
			{key: "up/down", label: m.t("tui.footer.move")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.mode != modeNormal:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.save"), primary: true},
			{key: "esc", label: m.t("tui.footer.cancel")},
			{key: "ctrl+c", label: m.t("tui.footer.quit")},
		}
	case m.commentScreenOpen && m.commentScreenEditing:
		return []footerToken{
			{key: "ctrl+s", label: m.t("tui.footer.save"), primary: true},
			{key: "alt+enter", label: m.t("tui.footer.newline")},
			{key: "esc", label: m.t("tui.footer.cancel")},
			m.helpToken(),
		}
	case m.descriptionScreenOpen:
		return []footerToken{
			{key: "f/esc", label: m.t("tui.footer.close_focus"), primary: true},
			{key: "j/k", label: m.t("tui.footer.scroll")},
			{key: "pgup/pgdn", label: m.t("tui.footer.page")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: "M", label: m.t("tui.footer.toggle_markdown")},
			m.helpToken(),
		}
	case m.commentScreenOpen:
		deleteLabel := m.t("tui.footer.arm_delete")
		if m.commentDeletePendingID != 0 {
			deleteLabel = m.t("tui.footer.confirm_delete")
		}
		return []footerToken{
			{key: "e", label: m.t("tui.footer.edit"), primary: true},
			{key: "d", label: deleteLabel, primary: m.commentDeletePendingID != 0},
			{key: "j/k", label: m.t("tui.footer.scroll")},
			{key: "pgup/pgdn", label: m.t("tui.footer.page")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			m.escBack(),
			m.helpToken(),
		}
	case m.taskScreen == taskScreenView:
		deleteLabel := m.t("tui.footer.arm_delete")
		if m.taskDeletePendingID != 0 {
			deleteLabel = m.t("tui.footer.confirm_delete")
		}
		enterLabel := m.t("tui.footer.enter_zone_action")
		switch m.taskFocus {
		case taskFocusActivity:
			enterLabel = m.t("tui.footer.open_comment_activity")
		case taskFocusSubtasks:
			enterLabel = m.t("tui.footer.open_subtask")
		}
		escLabel := m.escBack()
		if len(m.taskViewStack) > 0 {
			escLabel = footerToken{key: "esc", label: m.t("tui.footer.back_parent")}
		}
		return []footerToken{
			{key: "e", label: m.t("tui.footer.edit"), primary: true},
			{key: "n", label: m.t("tui.footer.sub_task"), primary: true},
			{key: "f", label: m.t("tui.footer.focus_description"), primary: true},
			{key: "c", label: m.t("tui.footer.comment"), primary: true},
			{key: "m", label: m.t("tui.footer.move"), primary: true},
			{key: "tab", label: m.t("tui.footer.zone")},
			{key: "j/k", label: m.t("tui.footer.scroll")},
			{key: "b", label: m.t("tui.footer.blockers")},
			{key: "d", label: deleteLabel, primary: m.taskDeletePendingID != 0},
			{key: "enter", label: enterLabel},
			{key: "r", label: m.t("tui.footer.refresh")},
			escLabel,
			m.helpToken(),
		}
	case m.taskScreen == taskScreenCreate:
		return []footerToken{
			{key: "ctrl+s", label: m.t("tui.footer.create"), primary: true},
			{key: "tab", label: m.t("tui.footer.field")},
			{key: "←/→", label: m.t("tui.footer.priority")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.taskScreen == taskScreenEdit:
		return []footerToken{
			{key: "ctrl+s", label: m.t("tui.footer.save"), primary: true},
			{key: "ctrl+b", label: m.t("tui.footer.blockers"), primary: true},
			{key: "tab", label: m.t("tui.footer.field")},
			{key: "←/→", label: m.t("tui.footer.priority")},
			m.escBack(),
		}
	case m.entityScreen == entityScreenView && m.deletePending:
		return []footerToken{
			{key: "d", label: m.t("tui.footer.confirm_delete"), primary: true},
			{key: "esc", label: m.t("tui.footer.cancel")},
			{key: "q", label: m.t("tui.footer.quit")},
		}
	case m.entityScreen == entityScreenView:
		return []footerToken{
			{key: "e", label: m.t("tui.footer.edit_in_editor"), primary: true},
			{key: "d", label: m.t("tui.footer.arm_delete")},
			{key: "p", label: m.t("tui.footer.skills_persona")},
			{key: "j/k", label: m.t("tui.footer.scroll")},
			{key: "r", label: m.t("tui.footer.refresh")},
			m.escBack(),
		}
	case m.entityScreen == entityScreenSkillPicker:
		return []footerToken{
			{key: "space", label: m.t("tui.footer.toggle"), primary: true},
			{key: "enter", label: m.t("tui.footer.new_skill_on_plus"), primary: true},
			{key: "ctrl+s", label: m.t("tui.footer.save"), primary: true},
			{key: "up/down", label: m.t("tui.footer.move")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.entityScreen == entityScreenThemePicker:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.apply_hot_reload"), primary: true},
			{key: "up/down", label: m.t("tui.footer.move")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.entityScreen == entityScreenConfigPicker:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.select_restart_required"), primary: true},
			{key: "up/down", label: m.t("tui.footer.move")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.entityScreen == entityScreenDefaultPicker:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.assign_clears_prior_owner"), primary: true},
			{key: "up/down", label: m.t("tui.footer.move")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.moveMode:
		return []footerToken{
			{key: "left/right", label: m.t("tui.footer.move_task_to_lane"), primary: true},
			{key: "esc", label: m.t("tui.footer.cancel")},
			{key: "q", label: m.t("tui.footer.quit")},
		}
	case m.onHome():
		return m.homeFooterTokens()
	case m.sub == subPlans && m.planNetworkOpen:
		return []footerToken{
			{key: "c", label: m.t("tui.footer.assign"), primary: true},
			{key: "e", label: m.t("tui.footer.edit_goal"), primary: true},
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "j/k", label: m.t("tui.footer.move")},
			{key: "space", label: m.t("tui.footer.toggle_wave")},
			{key: "h/l", label: m.t("tui.footer.collapse_expand")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: "pgup/pgdn", label: m.t("tui.footer.page")},
			{key: "r", label: m.t("tui.footer.refresh")},
			m.escBack(),
			m.helpToken(),
		}
	case m.sub == subBoard:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "n", label: m.t("tui.footer.new"), primary: true},
			{key: "m", label: m.t("tui.footer.move"), primary: true},
			{key: "left/right", label: m.t("tui.footer.lanes")},
			{key: "up/down", label: m.t("tui.footer.tasks")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "e", label: m.t("tui.footer.edit")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			{key: "ctrl+o", label: m.t("tui.footer.back")},
			m.helpToken(),
		}
	case m.sub == subSettingsGeneral:
		return []footerToken{
			{key: "t", label: m.t("tui.footer.theme"), primary: true},
			{key: "c", label: m.t("tui.footer.config"), primary: true},
			{key: "e", label: m.t("tui.footer.edit"), primary: true},
			{key: "j/k", label: m.t("tui.footer.scroll")},
			{key: "pgup/pgdn", label: m.t("tui.footer.page")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: "r", label: m.t("tui.footer.refresh")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			{key: "ctrl+o", label: m.t("tui.footer.back")},
			m.helpToken(),
		}
	case m.sub == subSettingsTags && m.deletePending:
		return []footerToken{
			{key: "d", label: m.t("tui.footer.confirm_delete"), primary: true},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.sub == subSettingsTags:
		return []footerToken{
			{key: "d", label: m.t("tui.footer.arm_delete_orphan"), primary: true},
			{key: "D", label: m.t("tui.footer.delete_all_orphans"), primary: true},
			{key: "up/down", label: m.t("tui.footer.select")},
			{key: "t", label: m.t("tui.footer.theme")},
			{key: "c", label: m.t("tui.footer.config")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	case m.sub == subSettingsTemplates:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "a", label: m.t("tui.footer.default"), primary: true},
			{key: "up/down", label: m.t("tui.footer.select")},
			{key: "t", label: m.t("tui.footer.theme")},
			{key: "c", label: m.t("tui.footer.config")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	case (m.sub == subSettingsLaws || m.sub == subSettingsPersonas || m.sub == subSettingsSkills) && m.deletePending:
		return []footerToken{
			{key: "d", label: m.t("tui.footer.confirm_delete"), primary: true},
			{key: "esc", label: m.t("tui.footer.cancel")},
		}
	case m.sub == subSettingsLaws || m.sub == subSettingsPersonas || m.sub == subSettingsSkills:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "n", label: m.t("tui.footer.new"), primary: true},
			{key: "e", label: m.t("tui.footer.edit"), primary: true},
			{key: "d", label: m.t("tui.footer.arm_delete")},
			{key: "p", label: m.t("tui.footer.skills_persona")},
			{key: "up/down", label: m.t("tui.footer.select")},
			{key: "t", label: m.t("tui.footer.theme")},
			{key: "c", label: m.t("tui.footer.config")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	case m.sub == subStatsLogs:
		return []footerToken{
			{key: "up/down", label: m.t("tui.footer.select_row"), primary: true},
			{key: "r", label: m.t("tui.footer.refresh"), primary: true},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	case m.sub == subStatsGeneral:
		return []footerToken{
			{key: "←/→", label: m.t("tui.footer.period_7d_30d_all"), primary: true},
			{key: "r", label: m.t("tui.footer.refresh")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	case m.sub == subGraph:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "j/k", label: m.t("tui.footer.move")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	default:
		return []footerToken{
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "n", label: m.t("tui.footer.new"), primary: true},
			{key: "m", label: m.t("tui.footer.move"), primary: true},
			{key: "up/down", label: m.t("tui.footer.select")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: "tab", label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		}
	}
}
