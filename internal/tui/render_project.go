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
	if w < 32 {
		w = 32
	}
	return w
}

// renderProjectMetaPanel builds the project metadata panel: a kicker plus
// the projects-table identity fields (name / slug / root path / id) and a
// comment count, rendered through the same detailscreen grid the task-view
// form uses. focused flips the kicker to the focus accent so the user can
// see which panel owns navigation keys.
func (m Model) renderProjectMetaPanel(focused bool) string {
	panelWidth := m.projectMetaPanelWidth()
	valueWidth := panelWidth - detailscreen.LabelWidth - 1 - 2
	if valueWidth < 8 {
		valueWidth = 8
	}

	kickerLabel := fmt.Sprintf(m.t("tui.kicker.project_fmt"), m.project.Slug)
	kicker := m.styles.kicker(kickerLabel)
	if focused {
		kicker = m.styles.kickerFocused(kickerLabel)
	}

	detail := m.projectMetaDetail(valueWidth, kicker)
	return m.styles.panel.Width(panelWidth - 2).Render(detail)
}

// projectMetaDetail assembles the detailscreen rows for the metadata
// panel. Split out from renderProjectMetaPanel so the row set can be
// asserted directly in tests without measuring the surrounding box.
func (m Model) projectMetaDetail(valueWidth int, kicker string) string {
	rootPath := m.project.RootPath
	if strings.TrimSpace(rootPath) == "" {
		rootPath = "—"
	}
	return detailscreen.New(valueWidth).
		Custom(kicker).
		Row(m.t("tui.row.name"), m.project.Name).
		Row(m.t("tui.row.slug"), m.project.Slug).
		Row(m.t("tui.row.root_path"), rootPath).
		Row(m.t("tui.row.id"), fmt.Sprintf("%d", m.project.ID)).
		Row(m.t("tui.row.comments"), fmt.Sprintf("%d", len(m.projectActivity))).
		View(0, m.styles.border, m.styles.hint)
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

	var content string
	if m.availableWidth() >= m.projectMetaPanelWidth()+m.activityPanelWidth()+2 {
		content = lipgloss.JoinHorizontal(lipgloss.Top, meta, "  ", activity)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, meta, "", activity)
	}
	return "\n" + indentBlock(content, 2)
}
