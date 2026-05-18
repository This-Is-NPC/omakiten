package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/multilineform"
)

// handlePlanNetworkKey drives navigation inside the column-per-wave
// network diagram. j/k move the task cursor inside the focused wave,
// h/l swap focus to the adjacent wave, o opens the focused task's
// detail screen, r reloads the projection, and esc / q returns to the
// list view. Bindings stay terse on purpose — the wider plan view
// surface (claim, edit goal_body, critical-path highlight) lands in
// follow-up slices.
func (m *Model) handlePlanNetworkKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "esc":
		m.closePlanNetwork()
		return
	case "left", "h":
		if m.planNetworkWaveCursor > 0 {
			m.planNetworkWaveCursor--
			m.planNetworkTaskCursor = 0
		}
	case "right", "l":
		if m.planNetworkWaveCursor < len(m.planNetworkShow.Waves)-1 {
			m.planNetworkWaveCursor++
			m.planNetworkTaskCursor = 0
		}
	case "up", "k":
		if tasks := m.planNetworkCurrentTasks(); m.planNetworkTaskCursor > 0 && len(tasks) > 0 {
			m.planNetworkTaskCursor--
		}
	case "down", "j":
		tasks := m.planNetworkCurrentTasks()
		if m.planNetworkTaskCursor < len(tasks)-1 {
			m.planNetworkTaskCursor++
		}
	case "o", "enter":
		tasks := m.planNetworkCurrentTasks()
		if m.planNetworkTaskCursor < 0 || m.planNetworkTaskCursor >= len(tasks) {
			return
		}
		row := tasks[m.planNetworkTaskCursor]
		if task, ok := m.taskByID(row.TaskID); ok {
			m.openTaskView(task)
		}
	case "r":
		if err := m.refreshCurrentView(); err != nil {
			m.status = err.Error()
		}
		m.reloadPlanNetwork()
	case "c":
		m.claimNextInPlanNetwork()
	case "e":
		m.openPlanGoalEditor()
	}
}

// openPlanGoalEditor flips the model into modePlanGoal, pre-filling
// the bubbles textarea with the focused plan's current goal_body. The
// editor is sqlite-backed end-to-end (no tempfile / no $EDITOR shell-
// out) — submit hits PlanService.UpdateGoalBody and reloadPlanNetwork
// refreshes the projection so the next render reflects the new body.
func (m *Model) openPlanGoalEditor() {
	if m.planNetworkShow.Plan.ID == 0 {
		return
	}
	m.planGoalEditingID = m.planNetworkShow.Plan.ID
	m.beginInput(modePlanGoal,
		fmt.Sprintf(m.t("tui.plans.goal.status_fmt"), m.planNetworkShow.Plan.Slug),
		m.planNetworkShow.Plan.GoalBody,
	)
}

// reloadPlanNetwork re-fetches the focused plan's projection so
// per-task buckets and done/total counters reflect any moves that
// happened since open. Reused by the `r` binding and by claimNext after
// a successful claim.
func (m *Model) reloadPlanNetwork() {
	if m.repos.Plans == nil || m.planNetworkShow.Plan.Slug == "" {
		return
	}
	planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
	show, err := planSvc.Show(m.ctx, m.project, m.planNetworkShow.Plan.Slug)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.planNetworkShow = show
	if m.planNetworkWaveCursor >= len(show.Waves) {
		m.planNetworkWaveCursor = 0
	}
	if m.planNetworkTaskCursor >= len(m.planNetworkCurrentTasks()) {
		m.planNetworkTaskCursor = 0
	}
}

// claimNextInPlanNetwork drives the `c` binding: invoke
// PlanService.ClaimNext against the focused plan, then reflect the
// outcome in the status line and reload the projection on success so
// the freshly-claimed task moves from ○ to ● and picks up its
// @assigned_to marker without the user needing to press `r`.
func (m *Model) claimNextInPlanNetwork() {
	if m.repos.Plans == nil || m.planNetworkShow.Plan.ID == 0 {
		return
	}
	planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
	task, claimed, err := planSvc.ClaimNext(m.ctx, m.project, m.planNetworkShow.Plan.ID)
	if err != nil {
		m.status = err.Error()
		return
	}
	if !claimed {
		m.status = fmt.Sprintf(m.t("tui.plans.status.claim_empty_fmt"), m.planNetworkShow.Plan.Slug)
		return
	}
	m.status = fmt.Sprintf(m.t("tui.plans.status.claim_success_fmt"), task.ID, task.Title)
	m.reloadPlanNetwork()
}

// planNetworkCurrentTasks returns the active task slice for the focused
// wave; nil when the wave cursor is out of range or the wave has no
// tasks. Archived rows stay in the slice so the network view mirrors the
// audit trail PlanShow already carries.
func (m Model) planNetworkCurrentTasks() []domain.PlanTaskRow {
	if m.planNetworkWaveCursor < 0 || m.planNetworkWaveCursor >= len(m.planNetworkShow.Waves) {
		return nil
	}
	return m.planNetworkShow.Waves[m.planNetworkWaveCursor].Tasks
}

// renderPlanNetwork draws the column-per-wave layout. Each wave is its
// own vertical stack of cards laid out via lipgloss.JoinHorizontal so
// horizontal order matches wave position. Status badges, the @assigned
// marker, and the active-wave highlight are derived from PlanShow alone
// — dependency arrows and the critical-path highlight stay deferred.
func (m Model) renderPlanNetwork() string {
	if m.mode == modePlanGoal {
		return m.renderPlanGoalEditor()
	}
	show := m.planNetworkShow
	if len(show.Waves) == 0 {
		header := fmt.Sprintf(m.t("tui.plans.network.header_fmt"), show.Plan.Slug, 0, 0, 0)
		body := fmt.Sprintf(m.t("tui.plans.network.no_waves_fmt"), show.Plan.Slug)
		return m.renderPanel(header + "\n\n" + body)
	}

	finalBucket := ""
	if snap := m.repos.activeSnapshot(); snap != nil {
		finalBucket = snap.Workflow().FinalBucketKey()
	}
	activeWaveID := show.ActiveWaveID

	contentWidth := m.availableWidth() - 4
	colCount := len(show.Waves)
	colInner := planNetworkColumnWidth(contentWidth, colCount)

	cells := make([]string, 0, colCount)
	for i, wv := range show.Waves {
		focused := i == m.planNetworkWaveCursor
		cursorIdx := -1
		if focused {
			cursorIdx = m.planNetworkTaskCursor
		}
		cells = append(cells, m.renderPlanNetworkColumn(wv, focused, cursorIdx, colInner, wv.Wave.ID == activeWaveID, wv.Wave.ID, activeWaveID, finalBucket))
	}

	var parts []string
	for i, c := range cells {
		parts = append(parts, c)
		if i < len(cells)-1 {
			parts = append(parts, " ")
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	pct := planPercent(show.DoneCount, show.TotalCount)
	header := fmt.Sprintf(m.t("tui.plans.network.header_fmt"), show.Plan.Slug, show.DoneCount, show.TotalCount, pct) + "   " + m.t("tui.plans.network.keymap")

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(m.styles.hintAccent.Render(header), 2))
	sb.WriteString("\n\n")
	sb.WriteString(indentBlock(board, 2))
	return sb.String()
}

// planNetworkColumnWidth divides the available row width across the
// wave columns with a 12-char floor so badges + slug + assignee still
// fit on the narrowest viable terminal. Each column also reserves one
// column of horizontal gutter when more than one wave renders, matching
// the kanban board's spacer convention.
func planNetworkColumnWidth(contentWidth, colCount int) int {
	if colCount <= 0 {
		return 12
	}
	gutters := colCount - 1
	if gutters < 0 {
		gutters = 0
	}
	usable := contentWidth - gutters
	w := usable / colCount
	if w < 12 {
		return 12
	}
	if w > 36 {
		return 36
	}
	return w
}

// renderPlanNetworkColumn renders a single wave column: header (name +
// per-wave done/total), a separator rule, then one row per task. The
// focused-wave header uses the accent style so cursor focus is visible
// even when no individual task carries the cursor.
func (m Model) renderPlanNetworkColumn(wv app.PlanWaveView, focused bool, cursorIdx, width int, isActive bool, waveID, activeWaveID int64, finalBucket string) string {
	headerStyle := m.styles.muted
	if focused {
		headerStyle = m.styles.hintAccent
	}
	tag := ""
	if isActive {
		tag = m.t("tui.plans.network.active_tag")
	}
	header := fmt.Sprintf(m.t("tui.plans.network.wave_header_fmt"),
		wv.Wave.Position,
		truncateText(wv.Wave.Name, width-12),
		wv.DoneCount, wv.TotalCount,
	) + tag
	rows := []string{
		headerStyle.Render(truncateText(header, width)),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}

	if len(wv.Tasks) == 0 {
		rows = append(rows, m.styles.empty.Width(width).Render(m.t("tui.plans.network.empty_wave")))
	}
	for i, t := range wv.Tasks {
		badge := planNetworkStatusBadge(t, waveID, activeWaveID, finalBucket)
		marker := m.cursorMarker(focused && i == cursorIdx)
		assignee := ""
		if t.AssignedTo != "" {
			assignee = " @" + t.AssignedTo
		}
		title := fmt.Sprintf("%s %s #%d %s%s",
			marker, badge, t.TaskID, t.Title, assignee,
		)
		rows = append(rows, truncateText(title, width))
	}
	return strings.Join(rows, "\n")
}

// renderPlanGoalEditor draws the modePlanGoal overlay: a full-panel
// textarea pre-filled with the focused plan's goal_body, plus a kicker
// + key hint above it. The textarea content is sqlite-backed via
// PlanService.UpdateGoalBody; no tempfile / no $EDITOR shell-out.
func (m Model) renderPlanGoalEditor() string {
	width := m.availableWidth() - 6
	if width < 32 {
		width = 32
	}
	innerHeight := m.height - 12
	if innerHeight < 6 {
		innerHeight = 6
	}

	field := multilineform.Render(
		m.commentInput,
		width,
		innerHeight,
		true,
		m.styles.multilineFormTheme(),
	)
	lines := []string{
		m.styles.hintAccent.Render(fmt.Sprintf(m.t("tui.plans.goal.kicker_fmt"), m.planNetworkShow.Plan.Slug)),
		m.formHint(m.t("tui.plans.goal.hint.ctrl_s"), m.t("tui.plans.goal.hint.alt_newline"), m.t("tui.plans.goal.hint.esc_cancel")),
		"",
		field,
	}
	return m.renderPanel(strings.Join(lines, "\n"))
}

// planNetworkStatusBadge maps a task row onto the four-state badge the
// network view advertises: ✓ done · ● dev · ○ ready · ⊘ gated. Gated
// fires only when the task's wave sits past the plan's active wave —
// the wave_gate guard's exact semantics.
func planNetworkStatusBadge(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string) string {
	if finalBucket != "" && t.BucketKey == finalBucket {
		return "✓"
	}
	if t.BucketKey == "dev" {
		return "●"
	}
	if activeWaveID != 0 && waveID != activeWaveID {
		// Heuristic: any non-done task whose wave is not the active wave
		// must be in a later wave (earlier waves are fully done, by the
		// definition of active wave in PlanService.composeShow).
		return "⊘"
	}
	return "○"
}
