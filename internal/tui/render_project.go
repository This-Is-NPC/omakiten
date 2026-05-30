package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/detailscreen"
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
// project tags and description, and a comment count, rendered through the
// same detailscreen grid the task-view form uses. The grid draws its own
// frame (gridtable.Render borders) — exactly like renderTaskDetailsBox —
// so there is NO outer m.styles.panel wrapper here; wrapping the already-
// framed grid produced a double border. focused flips the kicker to the
// focus accent so the user can see which panel owns navigation keys.
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
// — no outer panel wrap, matching renderTaskDetailsBox.
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
		Span(m.projectDescriptionInline(valueWidth))
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

// projectDescriptionInline renders the project description as full-width
// markdown (the way the task view renders task.Description), or a
// localized empty-state hint when the project has no description.
func (m Model) projectDescriptionInline(width int) string {
	body := strings.TrimSpace(m.projectDescription)
	if body == "" {
		return m.styles.hint.Render(m.t("tui.empty.project_description"))
	}
	return m.renderBodyMarkdown(body, width)
}

// renderProjectActivityPanel renders the project-scoped activity feed:
// the same comment/event cards the task feed uses, fed from
// m.projectActivity (project + universal scope, pinned-first). Read-only
// in v1 — no embedded comment input, no per-card cursor; long feeds
// scroll line-by-line via projectActivityScroll. focused flips the kicker
// accent to mark the active panel.
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
		cards := m.activityRowsForRender(events)
		body := flattenActivityCards(cards)
		heights := make([]int, len(body))
		for i := range heights {
			heights[i] = 1
		}
		lines = append(lines, m.renderScrollWindowSplit(body, heights, m.projectActivityScroll, m.projectActivityViewportLines())...)
	}

	return m.styles.panel.Width(m.activityPanelWidth() - 2).Render(strings.Join(lines, "\n"))
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

// renderProjectView composes the project-view screen: the metadata panel
// and the project-scoped activity feed, side-by-side when the terminal is
// wide enough and stacked otherwise. The focused panel (toggled by Tab)
// carries the focus-accent kicker.
func (m Model) renderProjectView() string {
	metaFocused := m.projectFocus == projectFocusMeta
	activityFocused := m.projectFocus == projectFocusActivity

	meta := m.renderProjectMetaPanel(metaFocused)
	activity := m.renderProjectActivityPanel(activityFocused)

	// Stack only when the terminal is too narrow to give the meta panel its
	// minimum width beside the activity rail (plus the 2-cell gutter). Gating
	// on the raw width keeps the intent explicit: the old
	// `availableWidth() >= projectMetaPanelWidth()+activityPanelWidth()+2`
	// check was a tautology (projectMetaPanelWidth is defined as
	// availableWidth()-activityPanelWidth()-2), so it only ever stacked via
	// the meta-width floor.
	var content string
	if m.availableWidth() >= m.activityPanelWidth()+projectMetaPanelMinWidth+2 {
		content = lipgloss.JoinHorizontal(lipgloss.Top, meta, "  ", activity)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, meta, "", activity)
	}
	return "\n" + indentBlock(content, 2)
}
