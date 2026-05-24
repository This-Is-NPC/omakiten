package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
)

// handlePlansKey drives the Tasks › plans sub-tab list view. j/k move
// the cursor, page/home/end jump in larger steps, and `r` triggers a
// manual refresh — same shape as handleListKey so muscle memory carries
// over from the table sub-tab. Every navigation goes through the
// cursorwindow.Model mutators (MoveCursor / JumpFirst / JumpLast)
// which re-run scrollwindow.Resync internally so the scroll follow
// is one implementation deep instead of being inlined per case.
func (m *Model) handlePlansKey(msg tea.KeyMsg) {
	m.plansCursor = m.plansCursor.
		WithItemCount(len(m.plans)).
		WithViewport(m.plansViewportRows())
	switch msg.String() {
	case "up", "k":
		m.plansCursor = m.plansCursor.MoveCursor(-1)
	case "down", "j":
		m.plansCursor = m.plansCursor.MoveCursor(1)
	case "pgup", "ctrl+u":
		m.plansCursor = m.plansCursor.MoveCursor(-taskViewPageStep(m.plansViewportRows()))
	case "pgdown", "ctrl+d":
		m.plansCursor = m.plansCursor.MoveCursor(taskViewPageStep(m.plansViewportRows()))
	case "home", "g":
		m.plansCursor = m.plansCursor.JumpFirst()
	case "end", "G":
		m.plansCursor = m.plansCursor.JumpLast()
	case "enter":
		m.openPlanNetwork()
	case "r":
		if err := m.refreshPreservingTaskSelection(); err != nil {
			m.status = err.Error()
		}
	}
}

// openPlanNetwork loads the cursored plan's full PlanShow projection and
// flips planNetworkOpen. Failure leaves the list view in place and surfaces
// the error in the status line; renderPlans stays responsible for the
// fallback render in that case. No-op when the plan list is empty so a
// stray enter from an unpopulated project does not erase the empty-state
// hint.
func (m *Model) openPlanNetwork() {
	cursor := m.plansCursor.Cursor()
	if len(m.plans) == 0 || cursor < 0 || cursor >= len(m.plans) {
		return
	}
	if m.repos.Plans == nil {
		return
	}
	planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
	slug := m.plans[cursor].Plan.Slug
	show, err := planSvc.Show(m.ctx, m.project, slug)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.planNetworkShow = show
	m.planNetworkOpen = true
	m.planNetwork = m.planNetwork.WithItems(nil)
	m.planNetworkCollapsed = map[int64]bool{}
	rows := m.planNetworkBuildRows()
	m.planNetworkCursor = m.planNetworkCursor.WithItemCount(len(rows)).SetCursor(0)
	// Land the cursor on the active wave's header when one exists so
	// the user sees the live frontier first. Falls back to row 0
	// (first wave header) on plans without an active wave.
	if show.ActiveWaveID > 0 {
		for i, row := range rows {
			if row.Kind == planRowWaveHeader && row.WaveID == show.ActiveWaveID {
				m.planNetworkCursor = m.planNetworkCursor.SetCursor(i)
				break
			}
		}
	}
}

// closePlanNetwork flips the renderer back to the plans list view and
// drops the cached PlanShow projection so the next open re-fetches fresh
// counts (avoid stale done/total after a claim or move). Collapsed
// state resets too so the next open starts with every wave expanded.
func (m *Model) closePlanNetwork() {
	m.planNetworkOpen = false
	m.planNetworkShow = app.PlanShow{}
	m.planNetworkCursor = m.planNetworkCursor.WithItemCount(0)
	m.planNetwork = m.planNetwork.WithItems(nil)
	m.planNetworkCollapsed = nil
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
		return m.renderPanel(m.styles.kicker(m.t("tui.plans.kicker")) + "\n\n" + m.t("tui.plans.list.empty"))
	}

	contentWidth := m.availableWidth() - 4
	const fixedWidth = 38 // done/total (8) + percent (6) + status (10) + active wave (~14)
	nameWidth := contentWidth - fixedWidth - 20
	if nameWidth < 12 {
		nameWidth = 12
	}

	missingActive := m.t("tui.plans.list.active_wave_missing")
	dataRows := make([]string, 0, len(m.plans))
	plansCursorIdx := m.plansCursor.Cursor()
	for idx, r := range m.plans {
		marker := m.cursorMarker(idx == plansCursorIdx)
		pct := planPercent(r.DoneCount, r.TotalCount)
		active := r.ActiveWaveName
		if active == "" {
			active = missingActive
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
		m.styles.kickerCount(m.t("tui.plans.kicker"), len(m.plans)),
		m.styles.info.Render(m.t("tui.plans.list.header")),
		m.hRule(contentWidth),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.plansCursor.Scroll(), m.plansViewportRows())...)
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
