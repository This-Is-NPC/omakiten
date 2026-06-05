package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/gridtable"
)

// projectMetaPanelWidth is the on-screen width of the metadata panel. It
// takes the width the activity rail leaves behind, with a 2-cell gutter
// between the two columns, floored so the grid never collapses.
func (m Model) projectMetaPanelWidth() int {
	w := m.availableWidth() - m.activityPanelWidth() - 2
	if w < projectMetaPanelMinWidth {
		w = projectMetaPanelMinWidth
	}
	return w
}

// renderProjectMetaPanel builds the project metadata panel: a kicker plus
// the projects-table identity fields (name / slug / root path / id), the
// project tags and a CAPPED description, rendered through the same
// detailscreen grid the task-view form uses. The grid draws its own frame
// (gridtable.Render borders) — exactly like renderTaskDetailsBox — so
// there is NO outer m.styles.panel wrapper here; wrapping the already-
// framed grid produced a double border. focused flips the kicker to the
// focus accent so the user can see which zone owns navigation keys.
func (m Model) renderProjectMetaPanel(focused bool) string {
	panelWidth := m.projectMetaPanelWidth()
	// The grid renders at LabelWidth + valueWidth + 1 (inner divider) + 2
	// (outer │ borders). Solve valueWidth so the framed grid's total width
	// matches panelWidth and its horizontal rules span the full column.
	valueWidth := panelWidth - detailscreen.LabelWidth - 3
	if valueWidth < 8 {
		valueWidth = 8
	}

	kickerLabel := fmt.Sprintf(m.t("tui.kicker.project_fmt"), m.project.Slug)
	kicker := m.styles.kicker(kickerLabel)
	if focused {
		kicker = m.styles.kickerFocused(kickerLabel)
	}

	return m.projectMetaDetail(valueWidth, kicker)
}

// projectMetaDetail assembles the detailscreen rows for the metadata
// panel. Split out from renderProjectMetaPanel so the row set can be
// asserted directly in tests without measuring the surrounding box.
// Returns the self-framed grid string (gridtable draws the single border)
// — no outer panel wrap, matching renderTaskDetailsBox. The description is
// CAPPED via renderProjectDescriptionInline so a long body no longer
// overflows the zone; `f` opens the full body in the fullscreen overlay.
func (m Model) projectMetaDetail(valueWidth int, kicker string) string {
	rootPath := m.project.RootPath
	if strings.TrimSpace(rootPath) == "" {
		rootPath = "—"
	}

	tagLine := m.styles.hint.Render("—")
	if line := m.projectTagsLine(); line != "" {
		tagLine = line
	}

	detail := detailscreen.New(valueWidth).
		Custom(kicker).
		Row(m.t("tui.row.name"), m.project.Name).
		Row(m.t("tui.row.slug"), m.project.Slug).
		Row(m.t("tui.row.root_path"), rootPath).
		Row(m.t("tui.row.id"), fmt.Sprintf("%d", m.project.ID)).
		Row(m.t("tui.row.tags"), tagLine).
		Row(m.t("tui.row.comments"), fmt.Sprintf("%d", len(m.projectActivity))).
		Kicker(m.t("tui.kicker.description")).
		Span(m.renderProjectDescriptionInline(valueWidth))
	return detail.View(0, m.styles.border, m.styles.hint)
}

// projectTagsLine renders the project's tag attachments as a single
// `·`-joined chip line (the same separator the task-view form uses for
// task tags). Empty when the project has no tags so the caller can fall
// back to a dash placeholder.
func (m Model) projectTagsLine() string {
	if len(m.projectTags) == 0 {
		return ""
	}
	names := make([]string, len(m.projectTags))
	for i, tag := range m.projectTags {
		label := tag.Label
		if label == "" {
			label = tag.Name
		}
		names[i] = label
	}
	return strings.Join(names, " · ")
}

// renderProjectDescriptionInline returns the project description block as
// rendered inside the form zone: the empty-state hint, the full markdown
// when it fits within taskDescriptionInlineCap, or a truncated head with a
// `+N more · f to focus` cue pointing at the fullscreen overlay. Mirrors
// renderTaskDescriptionInline so the two forms elide identically; the cap
// (shared taskDescriptionInlineCap) is what removes the #390 overflow.
func (m Model) renderProjectDescriptionInline(width int) string {
	body := strings.TrimSpace(m.projectDescription)
	if body == "" {
		return m.styles.hint.Render(m.t("tui.empty.project_description"))
	}
	rendered := m.renderBodyMarkdown(body, width)
	lines := strings.Split(rendered, "\n")
	if len(lines) <= taskDescriptionInlineCap {
		return rendered
	}
	head := strings.Join(lines[:taskDescriptionInlineCap], "\n")
	cue := m.styles.hint.Render(fmt.Sprintf(m.t("tui.task.description_more_fmt"), len(lines)-taskDescriptionInlineCap))
	return head + "\n" + cue
}

// renderProjectDashboardPanel renders the project status dashboard — the
// zone that replaces the task view's sub-tasks slot. It surfaces three
// grouped stat tables (tasks per bucket + total, the root/sub split, and
// plan progress) via the same gridtable cell style the Stats screen uses
// (renderSummaryTables / summaryRows). width is the outer column width;
// focused flips the kicker to the focus accent. Read-only — the dashboard
// owns no cursor, only the shared zone scroll.
func (m Model) renderProjectDashboardPanel(focused bool, width int) string {
	d := m.projectDashboard

	// Tasks table: one row per workflow bucket + a total row.
	taskFields := make([][2]string, 0, len(d.bucketCounts)+1)
	for _, bc := range d.bucketCounts {
		taskFields = append(taskFields, [2]string{bc.name, fmt.Sprintf("%d", bc.count)})
	}
	taskFields = append(taskFields, [2]string{m.t("tui.dashboard.total"), fmt.Sprintf("%d", d.totalTasks)})
	tasksRows := m.summaryRows(m.t("tui.dashboard.tasks"), taskFields...)

	// Sub-tasks table: roots vs children split.
	subRows := m.summaryRows(m.t("tui.dashboard.subtasks"),
		[2]string{m.t("tui.dashboard.roots"), fmt.Sprintf("%d", d.rootTasks)},
		[2]string{m.t("tui.dashboard.children"), fmt.Sprintf("%d", d.subTasks)},
	)

	// Plans table: plan count + aggregate done/total + percent.
	planPct := "—"
	if d.planTotal > 0 {
		planPct = fmt.Sprintf("%.0f%%", float64(d.planDone)/float64(d.planTotal)*100)
	}
	planRows := m.summaryRows(m.t("tui.dashboard.plans"),
		[2]string{m.t("tui.dashboard.plan_count"), fmt.Sprintf("%d", d.planCount)},
		[2]string{m.t("tui.dashboard.plan_progress"), fmt.Sprintf("%d/%d", d.planDone, d.planTotal)},
		[2]string{m.t("tui.dashboard.plan_percent"), planPct},
	)

	// Size the two-column grid to the zone width: label column fits the
	// widest kicker, the value column fills the rest. gridtable adds 3
	// border columns ("│"+"│"+"│") around the two cells.
	labelW := maxLabelVisibleWidth([][][]string{tasksRows, subRows, planRows})
	if labelW < 10 {
		labelW = 10
	}
	valueW := width - labelW - 3
	if valueW < 8 {
		valueW = 8
	}
	widths := []int{labelW, valueW}

	tables := []string{
		gridtable.Render(tasksRows, widths, m.styles.border),
		gridtable.Render(subRows, widths, m.styles.border),
		gridtable.Render(planRows, widths, m.styles.border),
	}

	header := m.styles.kicker(m.t("tui.kicker.dashboard"))
	if focused {
		header = m.styles.kickerFocused(m.t("tui.kicker.dashboard"))
	}
	return strings.Join(append([]string{header, ""}, tables...), "\n")
}

// renderProjectActivityPanel renders the project-scoped activity feed:
// the same comment/event cards the task feed uses, fed from
// m.projectActivity (project + universal scope, pinned-first). Read-only
// for mutation keys — no embedded comment input — but navigable by card:
// projectActivityCursor selects the focused card (accent border) and Enter
// opens its full comment detail. Long feeds scroll by card via
// projectActivityScroll. focused flips the kicker accent to mark the active
// zone.
//
// The cards are rendered through the SAME helpers the task feed uses
// (renderCommentCardSelected / renderSystemEventCard) at m.commentCardWidth().
//
// Border containment: the prior panel wrapped the cards in m.styles.panel,
// which adds Padding(0,2) ON TOP of its border. A card's visible width is
// commentCardWidth()+2 == activityPanelWidth-4, but the padded panel only
// offered activityPanelWidth-6 of inner content area, so lipgloss clipped
// each card's right border edge — the "broken borders" the user reported.
// The panel here drops the horizontal padding and sizes its content area to
// the exact card visible width, so every card (long bodies, pinned, tag
// chips) sits inside the rail with its single border intact. The border row
// is kept (not swapped for a bare indentBlock) so the activity rail stays
// row-aligned with the framed metadata panel in the side-by-side layout.
func (m Model) renderProjectActivityPanel(focused bool) string {
	events := m.projectActivity
	header := m.styles.kickerCount(m.t("tui.kicker.activity"), len(events))
	if focused {
		header = m.styles.kickerCountFocused(m.t("tui.kicker.activity"), len(events))
	}
	lines := []string{header}

	if len(events) == 0 {
		lines = append(lines, "", m.styles.hint.Render(m.t("tui.empty.project_activity")))
	} else {
		cards := m.activityRowsForRenderWithCursor(events, m.projectActivityCursor)
		body := flattenActivityCards(cards)
		heights := make([]int, len(body))
		for i := range heights {
			heights[i] = 1
		}
		lines = append(lines, m.renderScrollWindowSplit(body, heights, m.projectActivityScroll, m.projectActivityViewportLines())...)
	}

	// Content area == card visible width (commentCardWidth()+2) with zero
	// horizontal padding so the card border never tips past the panel's
	// inner edge. Vertical padding stays 0; the kicker already leads.
	panel := m.styles.panel.Padding(0, 0).Width(m.commentCardWidth() + 2)
	return panel.Render(strings.Join(lines, "\n"))
}

// projectActivityViewportLines is the row budget the project-view
// activity feed gets. Derived from the terminal height minus a fixed
// chrome reserve (header + nav strip + footer); floored so the panel
// stays navigable on short terminals.
func (m Model) projectActivityViewportLines() int {
	const chromeReserve = 10
	rows := m.height - chromeReserve
	if rows < activityViewportMinLines {
		rows = activityViewportMinLines
	}
	return rows
}

// renderProjectView composes the project-view screen: the form (metadata +
// capped description), the status dashboard, and the project-scoped
// activity feed. It mirrors the task view's responsive packing — form +
// dashboard stack in a left column with the activity feed as a right rail
// when the terminal is wide enough, all three stacked vertically
// otherwise. The focused zone (cycled by Tab) carries the focus-accent
// kicker.
func (m Model) renderProjectView() string {
	formFocused := m.projectFocus == projectFocusForm
	dashFocused := m.projectFocus == projectFocusDashboard
	activityFocused := m.projectFocus == projectFocusActivity

	form := m.renderProjectMetaPanel(formFocused)

	// Stack only when the terminal is too narrow to give the form column
	// its minimum width beside the activity rail (plus the 2-cell gutter).
	// Same threshold the prior 2-pane view used; the dashboard rides in
	// the left column so it does not change the wrap decision.
	if m.availableWidth() >= m.activityPanelWidth()+projectMetaPanelMinWidth+2 {
		dashboard := m.renderProjectDashboardPanel(dashFocused, m.projectMetaPanelWidth())
		activity := m.renderProjectActivityPanel(activityFocused)
		left := lipgloss.JoinVertical(lipgloss.Left, form, "", dashboard)
		content := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", activity)
		return "\n" + indentBlock(content, 2)
	}

	dashboard := m.renderProjectDashboardPanel(dashFocused, m.availableWidth())
	activity := m.renderProjectActivityPanel(activityFocused)
	content := lipgloss.JoinVertical(lipgloss.Left, form, "", dashboard, "", activity)
	return "\n" + indentBlock(content, 2)
}

// renderProjectFormScreen renders the focused, full-width project form
// overlay opened with `f` from the project view. Long descriptions overflow
// the form zone when capped inline, so this surface gives the full
// metadata + the uncapped, scrollable description the entire available
// width. Mirrors renderDescriptionScreen / renderPlanGoalScreen so the
// read-only overlays read as the same component with a different content
// slot.
func (m Model) renderProjectFormScreen() string {
	available := m.availableWidth()
	// Drop the column cap so `f` gives the description every column the
	// terminal offers — same rationale as the task description overlay.
	valueWidth := available - detailscreen.LabelWidth - 1 - 2
	if valueWidth < 24 {
		valueWidth = 24
	}

	rootPath := m.project.RootPath
	if strings.TrimSpace(rootPath) == "" {
		rootPath = "—"
	}
	tagLine := m.styles.hint.Render("—")
	if line := m.projectTagsLine(); line != "" {
		tagLine = line
	}

	screen := m.projectFormScreen.Reset(valueWidth).
		Custom(m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.project_fmt"), m.project.Slug))).
		Row(m.t("tui.row.name"), m.project.Name).
		Row(m.t("tui.row.slug"), m.project.Slug).
		Row(m.t("tui.row.root_path"), rootPath).
		Row(m.t("tui.row.id"), fmt.Sprintf("%d", m.project.ID)).
		Row(m.t("tui.row.tags"), tagLine).
		Kicker(m.t("tui.kicker.description"))
	body := strings.TrimSpace(m.projectDescription)
	if body == "" {
		screen = screen.Span(m.styles.hint.Render(m.t("tui.empty.project_description")))
	} else {
		// One spanned row so the gridtable wraps the body inline rather
		// than drawing a horizontal border between every wrapped line —
		// same fix the description / goal overlays use.
		screen = screen.Span(m.renderBodyMarkdown(body, valueWidth))
	}

	return "\n" + indentBlock(screen.View(m.taskViewportHeight(), m.styles.border, m.styles.hint), 2)
}
