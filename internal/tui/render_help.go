package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderHelp() string {
	type binding struct{ key, desc string }
	type group struct {
		key, title string
		bindings   []binding
	}
	commentKeys := newCommentInputBindings()
	commentInputHelpRows := []binding{
		{commentKeys.Save.Help().Key, m.t("tui.keys.save_comment_desc")},
		{commentKeys.InsertNewline.Help().Key, m.t("tui.keys.insert_newline_desc")},
		{commentKeys.Cancel.Help().Key, m.t("tui.keys.cancel_desc")},
	}
	groups := []group{
		{"global", m.t("tui.help.global.title"), []binding{
			{"?", m.t("tui.help.global.close_help")},
			{"a", m.t("tui.help.global.toggle_all")},
			{"q · ctrl+c", m.t("tui.help.global.quit")},
			{"tab · shift+tab", m.t("tui.help.global.cycle_zones")},
			{"1 · 2 · 3", m.t("tui.help.global.jump_zone")},
			{", · /", m.t("tui.help.global.prev_next_sub")},
			{"0 · ctrl+h", m.t("tui.help.global.back_home")},
			{"r", m.t("tui.help.global.refresh")},
		}},
		{"home", m.t("tui.help.home.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.home.move_project")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.home.scroll_halfpage")},
			{"g · G", m.t("tui.help.home.first_last_project")},
			{"enter", m.t("tui.help.home.open_project")},
			{"ctrl+h", m.t("tui.help.home.reload")},
			{"q · ctrl+c", m.t("tui.help.home.quit")},
		}},
		{"tasks_board", m.t("tui.help.tasks_board.title"), []binding{
			{"← ↑ ↓ → · h j k l", m.t("tui.help.tasks_board.navigate")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.tasks_board.scroll_column")},
			{"g · G", m.t("tui.help.tasks_board.first_last_card")},
			{"enter", m.t("tui.help.tasks_board.open_task")},
			{"n", m.t("tui.help.tasks_board.new_task")},
			{"e", m.t("tui.help.tasks_board.edit_task")},
			{"c", m.t("tui.help.tasks_board.add_comment")},
			{"m", m.t("tui.help.tasks_board.move_task")},
			{"A", m.t("tui.help.tasks_board.toggle_archived")},
		}},
		{"tasks_table", m.t("tui.help.tasks_table.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.tasks_table.select_task")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.tasks_table.scroll_halfpage")},
			{"g · G", m.t("tui.help.tasks_table.first_last_task")},
			{"enter", m.t("tui.help.tasks_table.open_task")},
			{"n", m.t("tui.help.tasks_table.new_task")},
			{"e", m.t("tui.help.tasks_table.edit_task")},
			{"m", m.t("tui.help.tasks_table.move_bucket")},
			{"A", m.t("tui.help.tasks_table.toggle_archived")},
		}},
		{"tasks_graph", m.t("tui.help.tasks_graph.title"), []binding{
			{"← →", m.t("tui.help.tasks_graph.switch_view")},
			{"↑ ↓ · j k", m.t("tui.help.tasks_graph.move_cursor")},
			{"enter", m.t("tui.help.tasks_graph.open_task")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.tasks_graph.scroll_halfpage")},
			{"g · G", m.t("tui.help.tasks_graph.jump_top_bottom")},
		}},
		{"task_view", m.t("tui.help.task_view.title"), []binding{
			{"tab · shift+tab", m.t("tui.help.task_view.switch_focus")},
			{"↑ ↓ · j k", m.t("tui.help.task_view.scroll_or_navigate")},
			{"J · K", m.t("tui.help.task_view.navigate_activity")},
			{"enter", m.t("tui.help.task_view.open_focused")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.task_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.task_view.jump_top_bottom")},
			{"e", m.t("tui.help.task_view.edit_task")},
			{"a · n", m.t("tui.help.task_view.new_subtask")},
			{"s", m.t("tui.help.task_view.focus_subtasks")},
			{"space", m.t("tui.help.task_view.send_subtask_done")},
			{"f", m.t("tui.help.task_view.focus_description")},
			{"b", m.t("tui.help.task_view.edit_blockers")},
			{"c", m.t("tui.help.task_view.add_comment")},
			{"m", m.t("tui.help.task_view.move")},
			{"M", m.t("tui.help.task_view.toggle_markdown")},
			{"d · d", m.t("tui.help.task_view.arm_delete")},
			{"esc", m.t("tui.help.task_view.back_or_parent")},
		}},
		{"description_view", m.t("tui.help.description_view.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.description_view.scroll_body")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.description_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.description_view.jump_top_bottom")},
			{"M", m.t("tui.help.description_view.toggle_markdown")},
			{"f · esc", m.t("tui.help.description_view.close")},
		}},
		{"comment_view", m.t("tui.help.comment_view.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.comment_view.scroll_body")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.comment_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.comment_view.jump_top_bottom")},
			{"e", m.t("tui.help.comment_view.edit_body")},
			{"M", m.t("tui.help.comment_view.toggle_markdown")},
			{"d · d", m.t("tui.help.comment_view.arm_delete")},
			{"esc", m.t("tui.help.comment_view.back_task")},
		}},
		{"comment_input", m.t("tui.help.comment_input.title"), commentInputHelpRows},
		{"comment_edit", m.t("tui.help.comment_edit.title"), []binding{
			{"ctrl+s", m.t("tui.help.comment_edit.save")},
			{"alt+enter · shift+enter", m.t("tui.help.comment_edit.newline")},
			{"esc", m.t("tui.help.comment_edit.cancel")},
			{"arrows · home · end", m.t("tui.help.comment_edit.caret")},
		}},
		{"task_form", m.t("tui.help.task_form.title"), []binding{
			{"tab", m.t("tui.help.task_form.switch_field")},
			{"← → · h l", m.t("tui.help.task_form.change_priority")},
			{"ctrl+b", m.t("tui.help.task_form.edit_blockers")},
			{"enter · alt+enter · shift+enter", m.t("tui.help.task_form.newline")},
			{"ctrl+s", m.t("tui.help.task_form.save")},
			{"esc", m.t("tui.help.task_form.cancel")},
		}},
		{"blocker_picker", m.t("tui.help.blocker_picker.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.blocker_picker.move")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.blocker_picker.scroll_halfpage")},
			{"g · G", m.t("tui.help.blocker_picker.first_last_candidate")},
			{"space", m.t("tui.help.blocker_picker.toggle")},
			{"ctrl+s", m.t("tui.help.blocker_picker.save")},
			{"esc", m.t("tui.help.blocker_picker.cancel")},
		}},
		{"settings_general", m.t("tui.help.settings_general.title"), []binding{
			{", · /", m.t("tui.help.settings_general.prev_next_sub")},
			{"t", m.t("tui.help.settings_general.theme_picker")},
			{"c", m.t("tui.help.settings_general.config_picker")},
			{"e", m.t("tui.help.settings_general.edit_config")},
			{"r", m.t("tui.help.settings_general.refresh")},
		}},
		{"settings_entity", m.t("tui.help.settings_entity.title"), []binding{
			{", · /", m.t("tui.help.settings_entity.prev_next_sub")},
			{"↑ ↓ · j k", m.t("tui.help.settings_entity.select")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.settings_entity.scroll_halfpage")},
			{"g · G", m.t("tui.help.settings_entity.first_last")},
			{"enter", m.t("tui.help.settings_entity.open_detail")},
			{"n", m.t("tui.help.settings_entity.new_entity")},
			{"e", m.t("tui.help.settings_entity.edit_in_editor")},
			{"d · d", m.t("tui.help.settings_entity.arm_delete")},
			{"p", m.t("tui.help.settings_entity.skill_picker")},
			{"a", m.t("tui.help.settings_entity.set_default")},
			{"t · c", m.t("tui.help.settings_entity.theme_or_config_picker")},
		}},
		{"settings_tags", m.t("tui.help.settings_tags.title"), []binding{
			{", · /", m.t("tui.help.settings_tags.prev_next_sub")},
			{"↑ ↓ · j k", m.t("tui.help.settings_tags.select_tag")},
			{"d", m.t("tui.help.settings_tags.arm_delete")},
			{"D", m.t("tui.help.settings_tags.delete_all")},
			{"t · c", m.t("tui.help.settings_tags.theme_or_config_picker")},
		}},
		{"entity_view", m.t("tui.help.entity_view.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.entity_view.scroll_body")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.entity_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.entity_view.jump_top_bottom")},
			{"e", m.t("tui.help.entity_view.edit")},
			{"M", m.t("tui.help.entity_view.toggle_markdown")},
			{"d · d", m.t("tui.help.entity_view.arm_delete")},
			{"p", m.t("tui.help.entity_view.skill_picker")},
			{"esc", m.t("tui.help.entity_view.back_or_cancel")},
		}},
		{"skill_picker", m.t("tui.help.skill_picker.title"), []binding{
			{"↑ ↓ · j k", m.t("tui.help.skill_picker.move")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.skill_picker.scroll_halfpage")},
			{"g · G", m.t("tui.help.skill_picker.first_last_row")},
			{"space", m.t("tui.help.skill_picker.toggle")},
			{"enter on '+ create new'", m.t("tui.help.skill_picker.scaffold_new")},
			{"ctrl+s", m.t("tui.help.skill_picker.save")},
			{"esc", m.t("tui.help.skill_picker.cancel")},
		}},
		{"stats_logs", m.t("tui.help.stats_logs.title"), []binding{
			{"← →", m.t("tui.help.stats_logs.switch_view")},
			{"↑ ↓ · j k", m.t("tui.help.stats_logs.select_row")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.stats_logs.scroll_halfpage")},
			{"g · G", m.t("tui.help.stats_logs.first_last_row")},
			{"f", m.t("tui.help.stats_logs.filter_cycle")},
			{"shift+F", m.t("tui.help.stats_logs.filter_cycle_back")},
			{"r", m.t("tui.help.stats_logs.refresh")},
		}},
		{"stats_general", m.t("tui.help.stats_general.title"), []binding{
			{"← →", m.t("tui.help.stats_general.cycle_period")},
			{"r", m.t("tui.help.stats_general.refresh")},
		}},
		{"stats_insights", m.t("tui.help.stats_insights.title"), []binding{
			{"r", m.t("tui.help.stats_insights.refresh")},
			// Reuses the footer's translated "scroll" label — the binding is
			// the shared read-only scroll vocabulary, no bespoke copy needed.
			{"j k · pgup · pgdn · g G", m.t("tui.footer.scroll")},
		}},
	}

	if !m.helpAll {
		wanted := map[string]bool{"global": true}
		for _, key := range m.currentHelpTitles() {
			wanted[key] = true
		}
		filtered := make([]group, 0, len(wanted))
		for _, g := range groups {
			if wanted[g.key] {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}

	const keyW = 34
	var lines []string
	title := m.t("tui.help.title_current")
	if m.helpAll {
		title = m.t("tui.help.title_all")
	}
	lines = append(lines, m.styles.kicker(title), m.styles.hint.Render(m.t("tui.help.toggle_scope")), "")
	for _, g := range groups {
		lines = append(lines, m.styles.kicker(g.title))
		lines = append(lines, m.hRule(keyW+24))
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
	return indentBlock(m.styles.footer.Render(m.t("tui.help.footer")), 2)
}

// currentHelpTitles returns the catalog-keyed group ids for the active
// surface. Group ids are stable identifiers used by the help filter, so
// localized title changes do not break the current-context filter.
func (m Model) currentHelpTitles() []string {
	switch {
	case m.isEmbeddedCommentInput():
		return []string{"comment_input"}
	case m.blockerPickerOpen:
		return []string{"blocker_picker"}
	case m.commentScreenOpen && m.commentScreenEditing:
		return []string{"comment_edit"}
	case m.commentScreenOpen:
		return []string{"comment_view"}
	case m.taskScreen == taskScreenCreate || m.taskScreen == taskScreenEdit:
		return []string{"task_form"}
	case m.taskScreen == taskScreenView:
		return []string{"task_view"}
	case m.entityScreen == entityScreenSkillPicker:
		return []string{"skill_picker"}
	case m.entityScreen == entityScreenView:
		return []string{"entity_view"}
	case m.onHome():
		return []string{"home"}
	case m.sub == subBoard:
		return []string{"tasks_board"}
	case m.sub == subTable:
		return []string{"tasks_table"}
	case m.sub == subGraph:
		return []string{"tasks_graph"}
	case m.sub == subSettingsGeneral:
		return []string{"settings_general"}
	case m.sub == subSettingsTags:
		return []string{"settings_tags"}
	case m.sub == subSettingsLaws || m.sub == subSettingsPersonas || m.sub == subSettingsSkills || m.sub == subSettingsTemplates:
		return []string{"settings_entity"}
	case m.sub == subStatsLogs:
		return []string{"stats_logs"}
	case m.sub == subStatsGeneral:
		return []string{"stats_general"}
	case m.sub == subStatsInsights:
		return []string{"stats_insights"}
	default:
		return []string{"tasks_board"}
	}
}
