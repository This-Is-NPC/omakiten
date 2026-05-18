package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handlePlansKey drives the Tasks › plans sub-tab list view. j/k move
// the cursor, page/home/end jump in larger steps, and `r` triggers a
// manual refresh — same shape as handleListKey so muscle memory carries
// over from the table sub-tab.
func (m *Model) handlePlansKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		if m.planCursor > 0 {
			m.planCursor--
			m.syncPlansScroll()
		}
	case "down", "j":
		if m.planCursor < len(m.plans)-1 {
			m.planCursor++
			m.syncPlansScroll()
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.plansViewportRows())
		m.planCursor -= step
		if m.planCursor < 0 {
			m.planCursor = 0
		}
		m.syncPlansScroll()
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.plansViewportRows())
		m.planCursor += step
		if m.planCursor > len(m.plans)-1 {
			m.planCursor = len(m.plans) - 1
		}
		if m.planCursor < 0 {
			m.planCursor = 0
		}
		m.syncPlansScroll()
	case "home", "g":
		m.planCursor = 0
		m.syncPlansScroll()
	case "end", "G":
		if len(m.plans) > 0 {
			m.planCursor = len(m.plans) - 1
			m.syncPlansScroll()
		}
	case "r":
		if err := m.refreshPreservingTaskSelection(); err != nil {
			m.status = err.Error()
		}
	}
}

// syncPlansScroll keeps planScroll aligned so the cursor stays in view —
// same follow-cursor pattern as syncTableScroll/syncGraphScroll.
func (m *Model) syncPlansScroll() {
	m.planScroll = followCursor(m.planScroll, m.planCursor, scrollDataRows(m.plansViewportRows()), len(m.plans))
}

// plansViewportRows returns how many plan rows fit in the panel. Chrome
// budget mirrors renderTable: 2 borders + 3 header rows (kicker / info
// header / rule) = 5.
func (m Model) plansViewportRows() int {
	return m.panelViewportRows(5)
}

// renderPlans draws the Tasks › plans list view: one row per plan with
// slug, name, done/total, percent, status, and the active wave name.
// Empty state mirrors the other sub-tabs — a single hint when the
// project has no plans yet.
func (m Model) renderPlans() string {
	if len(m.plans) == 0 {
		return m.renderPanel("// PLANS\n\nNo plans yet. Create one with `okt plan create <slug> --name \"...\"`.")
	}

	contentWidth := m.availableWidth() - 4
	const fixedWidth = 38 // done/total (8) + percent (6) + status (10) + active wave (~14)
	nameWidth := contentWidth - fixedWidth - 20
	if nameWidth < 12 {
		nameWidth = 12
	}

	dataRows := make([]string, 0, len(m.plans))
	for idx, r := range m.plans {
		marker := m.cursorMarker(idx == m.planCursor)
		pct := planPercent(r.DoneCount, r.TotalCount)
		active := r.ActiveWaveName
		if active == "" {
			active = "—"
		}
		dataRows = append(dataRows,
			fmt.Sprintf("%s %-16s %-7s %5d/%-3d %4d%%  %s",
				marker,
				truncateText(r.Plan.Slug, 16),
				truncateText(string(r.Plan.Status), 7),
				r.DoneCount, r.TotalCount,
				pct,
				truncateText(r.Plan.Name+"  ‹"+active+"›", nameWidth),
			),
		)
	}

	rows := []string{
		m.styles.kickerCount("// PLANS", len(m.plans)),
		m.styles.info.Render("  slug             status    done/total   %    name ‹active wave›"),
		m.hRule(contentWidth),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.planScroll, m.plansViewportRows())...)
	return m.renderPanel(strings.Join(rows, "\n"))
}

// planPercent returns the integer percentage of done tasks relative to
// the plan's total. Zero-total plans render as 0% (rather than divide-by-
// zero panic or NaN-flavoured float formatting).
func planPercent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return (done * 100) / total
}
