package tui

import (
	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/header"
	"omakiten/internal/tui/components/overlay"
	"omakiten/internal/tui/screenhost"
)

type helpBinding struct{ key, desc string }

type helpGroup struct {
	key, title string
	bindings   []helpBinding
}

func (m Model) renderHelp() string {
	groups := m.helpGroups()
	if !m.helpAll {
		groups = m.filterHelpGroups(groups)
	}
	painted := m.paintHelpGroups(groups)
	title := m.t("tui.help.title_current")
	if m.helpAll {
		title = m.t("tui.help.title_all")
	}
	return overlay.Render(m.helpOverlayStyles(), overlay.Options{
		Title: title, ScopeHint: m.t("tui.help.toggle_scope"),
		Groups: painted, Scroll: m.help.Scroll, Viewport: m.helpViewportRows(),
		FormatScrollHint: m.viewportFooterHint,
	})
}

func (m Model) helpGroups() []helpGroup {
	commentKeys := newCommentInputBindings()
	commentInputHelpRows := []helpBinding{
		{commentKeys.Save.Help().Key, m.t("tui.keys.save_comment_desc")},
		{commentKeys.InsertNewline.Help().Key, m.t("tui.keys.insert_newline_desc")},
		{commentKeys.Cancel.Help().Key, m.t("tui.keys.cancel_desc")},
	}
	groups := []helpGroup{
		{"global", m.t("tui.help.global.title"), []helpBinding{
			{"?", m.t("tui.help.global.close_help")},
			{"a", m.t("tui.help.global.toggle_all")},
			{"q · ctrl+c", m.t("tui.help.global.quit")},
			{keynav.Default.Tops.Primary(), m.t("tui.help.global.cycle_tabs")},
			{"1 · 2 · 3", m.t("tui.help.global.jump_zone")},
			{", · /", m.t("tui.help.global.prev_next_sub")},
			{"0 · ctrl+h", m.t("tui.help.global.back_home")},
			{"r", m.t("tui.help.global.refresh")},
		}},
		{"task_view", m.t("tui.help.task_view.title"), []helpBinding{
			{keynav.Default.Zones.Primary(), m.t("tui.help.task_view.switch_focus")},
			{"↑ ↓ · j k", m.t("tui.help.task_view.scroll_or_navigate")},
			{"J · K", m.t("tui.help.task_view.navigate_activity")},
			{"enter", m.t("tui.help.task_view.open_focused")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.task_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.task_view.jump_top_bottom")},
			{"e", m.t("tui.help.task_view.edit_task")},
			{"a · n", m.t("tui.help.task_view.new_subtask")},
			{"s", m.t("tui.help.task_view.focus_subtasks")},
			{"space", m.t("tui.help.task_view.send_subtask_done")},
			{"f", m.t("tui.footer.focus")},
			{"b", m.t("tui.help.task_view.edit_blockers")},
			{"c", m.t("tui.help.task_view.add_comment")},
			{"m", m.t("tui.help.task_view.move")},
			{"M", m.t("tui.help.task_view.toggle_markdown")},
			{"d · d", m.t("tui.help.task_view.arm_delete")},
			{"esc", m.t("tui.help.task_view.back_or_parent")},
		}},
		{"description_view", m.t("tui.help.description_view.title"), []helpBinding{
			{"↑ ↓ · j k", m.t("tui.help.description_view.scroll_body")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.description_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.description_view.jump_top_bottom")},
			{"M", m.t("tui.help.description_view.toggle_markdown")},
			{"f · esc", m.t("tui.help.description_view.close")},
		}},
		{"comment_view", m.t("tui.help.comment_view.title"), []helpBinding{
			{"↑ ↓ · j k", m.t("tui.help.comment_view.scroll_body")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.comment_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.comment_view.jump_top_bottom")},
			{"e", m.t("tui.help.comment_view.edit_body")},
			{"M", m.t("tui.help.comment_view.toggle_markdown")},
			{"d · d", m.t("tui.help.comment_view.arm_delete")},
			{"esc", m.t("tui.help.comment_view.back_task")},
		}},
		{"comment_input", m.t("tui.help.comment_input.title"), commentInputHelpRows},
		{"comment_edit", m.t("tui.help.comment_edit.title"), []helpBinding{
			{"ctrl+s", m.t("tui.help.comment_edit.save")},
			{"alt+enter · shift+enter", m.t("tui.help.comment_edit.newline")},
			{"esc", m.t("tui.help.comment_edit.cancel")},
			{"arrows · home · end", m.t("tui.help.comment_edit.caret")},
		}},
		{"task_form", m.t("tui.help.task_form.title"), []helpBinding{
			{"tab", m.t("tui.help.task_form.switch_field")},
			{"← → · h l", m.t("tui.help.task_form.change_priority")},
			{"ctrl+b", m.t("tui.help.task_form.edit_blockers")},
			{"enter · alt+enter · shift+enter", m.t("tui.help.task_form.newline")},
			{"ctrl+s", m.t("tui.help.task_form.save")},
			{"esc", m.t("tui.help.task_form.cancel")},
		}},
		{"blocker_picker", m.t("tui.help.blocker_picker.title"), []helpBinding{
			{"↑ ↓ · j k", m.t("tui.help.blocker_picker.move")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.blocker_picker.scroll_halfpage")},
			{"g · G", m.t("tui.help.blocker_picker.first_last_candidate")},
			{"space", m.t("tui.help.blocker_picker.toggle")},
			{"ctrl+s", m.t("tui.help.blocker_picker.save")},
			{"esc", m.t("tui.help.blocker_picker.cancel")},
		}},
		{"settings_entity", m.t("tui.help.settings_entity.title"), []helpBinding{
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
		{"settings_tags", m.t("tui.help.settings_tags.title"), []helpBinding{
			{", · /", m.t("tui.help.settings_tags.prev_next_sub")},
			{"↑ ↓ · j k", m.t("tui.help.settings_tags.select_tag")},
			{"d", m.t("tui.help.settings_tags.arm_delete")},
			{"m · m", m.t("tui.help.settings_tags.arm_merge")},
			{"D", m.t("tui.help.settings_tags.delete_all")},
			{"t · c", m.t("tui.help.settings_tags.theme_or_config_picker")},
		}},
		{"entity_view", m.t("tui.help.entity_view.title"), []helpBinding{
			{"↑ ↓ · j k", m.t("tui.help.entity_view.scroll_body")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.entity_view.scroll_halfpage")},
			{"g · G", m.t("tui.help.entity_view.jump_top_bottom")},
			{"e", m.t("tui.help.entity_view.edit")},
			{"M", m.t("tui.help.entity_view.toggle_markdown")},
			{"d · d", m.t("tui.help.entity_view.arm_delete")},
			{"p", m.t("tui.help.entity_view.skill_picker")},
			{"esc", m.t("tui.help.entity_view.back_or_cancel")},
		}},
		{"skill_picker", m.t("tui.help.skill_picker.title"), []helpBinding{
			{"↑ ↓ · j k", m.t("tui.help.skill_picker.move")},
			{"pgup · pgdn · ctrl+u · ctrl+d", m.t("tui.help.skill_picker.scroll_halfpage")},
			{"g · G", m.t("tui.help.skill_picker.first_last_row")},
			{"space", m.t("tui.help.skill_picker.toggle")},
			{"enter on '+ create new'", m.t("tui.help.skill_picker.scaffold_new")},
			{"ctrl+s", m.t("tui.help.skill_picker.save")},
			{"esc", m.t("tui.help.skill_picker.cancel")},
		}},
	}

	// Extracted screens declare their own help groups; they are appended in
	// their historical positions so the overlay's group order stays stable
	// while cohorts migrate one screen at a time.
	for _, id := range []screenhost.ID{screenhost.Home, screenhost.TasksBoard, screenhost.TasksTable, screenhost.TasksGraph, screenhost.TasksPlans, screenhost.TaskForm, screenhost.TaskDescription, screenhost.CommentDetail, screenhost.PlanGoal, screenhost.PlanNetwork, screenhost.StatsLogs, screenhost.StatsGeneral, screenhost.StatsInsights, screenhost.SettingsGeneral, screenhost.SettingsLaws, screenhost.SettingsPersonas, screenhost.SettingsSkills, screenhost.SettingsGuards} {
		groups = mergeHelpGroups(groups, m.screenHelpGroups(id))
	}

	return groups
}

func mergeHelpGroups(groups []helpGroup, declared []screenhost.HelpGroup) []helpGroup {
	for _, group := range declared {
		bindings := make([]helpBinding, 0, len(group.Bindings))
		for _, binding := range group.Bindings {
			bindings = append(bindings, helpBinding{binding.Key, binding.Description})
		}
		replacement := helpGroup{group.ID, group.Title, bindings}
		for i := range groups {
			if groups[i].key == group.ID {
				groups[i] = replacement
				break
			}
		}
		if !helpGroupExists(groups, group.ID) {
			groups = append(groups, replacement)
		}
	}
	return groups
}

func helpGroupExists(groups []helpGroup, key string) bool {
	for _, group := range groups {
		if group.key == key {
			return true
		}
	}
	return false
}

func (m Model) filterHelpGroups(groups []helpGroup) []helpGroup {
	wanted := map[string]bool{"global": true}
	for _, key := range m.currentHelpTitles() {
		wanted[key] = true
	}
	filtered := make([]helpGroup, 0, len(wanted))
	for _, g := range groups {
		if wanted[g.key] {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

func (m Model) paintHelpGroups(groups []helpGroup) []overlay.Group {
	painted := make([]overlay.Group, 0, len(groups))
	for _, g := range groups {
		bindings := make([]overlay.Binding, 0, len(g.bindings))
		for _, b := range g.bindings {
			if m.helpTokenDenied(g.key, b.key) {
				continue
			}
			bindings = append(bindings, overlay.Binding{Key: b.key, Desc: b.desc})
		}
		painted = append(painted, overlay.Group{Title: g.title, Bindings: bindings})
	}
	return painted
}

func (m Model) helpOverlayStyles() overlay.Styles {
	return overlay.Styles{
		Info:      m.styles.info,
		Hint:      m.styles.hint,
		Key:       m.styles.hintAccent,
		Footer:    m.styles.footer,
		Separator: m.styles.separator,
	}
}

// helpViewportRows returns the line budget for the help screen content.
// Header and footer heights are measured from the same painters Stack uses,
// plus the leading blank overlay.Render emits.
func (m Model) helpViewportRows() int {
	headerRows := header.Height(m.headerStyles(), m.headerOptions())
	footerRows := overlay.FooterHeight(m.helpOverlayStyles(), m.t("tui.help.footer"))
	return overlay.ViewportRows(m.height, headerRows, footerRows)
}

func (m Model) renderHelpFooter() string {
	return overlay.Footer(m.helpOverlayStyles(), m.t("tui.help.footer"))
}

// currentHelpTitles returns the catalog-keyed group ids for the active
// surface. Group ids are stable identifiers used by the help filter, so
// localized title changes do not break the current-context filter.
func (m Model) currentHelpTitles() []string {
	switch {
	case len(m.screenStack) > 0:
		screen, ok := m.activeHostedScreen()
		if !ok {
			return []string{"tasks_board"}
		}
		groups := screen.Help(m.screenFrame())
		ids := make([]string, 0, len(groups))
		for _, group := range groups {
			ids = append(ids, group.ID)
		}
		return ids
	default:
		descriptor, ok := m.activeScreenDescriptor()
		if !ok || len(descriptor.HelpKeys) == 0 {
			return []string{"tasks_board"}
		}
		return descriptor.HelpKeys
	}
}
