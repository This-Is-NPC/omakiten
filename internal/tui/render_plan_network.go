package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/config"
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
// marker, the active-wave highlight, and inline blocker markers
// (← #N #M) all come straight from PlanShow — full edge routing across
// columns and the critical-path highlight stay deferred.
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

	blockers := planNetworkBlockerIndex(show.Dependencies)
	dependents := planNetworkDependentIndex(show.Dependencies)
	allTasks := make([]domain.PlanTaskRow, 0)
	for _, wv := range show.Waves {
		allTasks = append(allTasks, wv.Tasks...)
	}
	criticalPath := planNetworkCriticalPath(show.Dependencies, allTasks)
	nextClaimableID := planNetworkPeekNextClaimable(m.repos.Plans, m.ctx, m.project, show.Plan.ID, m.repos.activeSnapshot())

	contentWidth := m.availableWidth() - 4
	colCount := len(show.Waves)
	colInner := planNetworkColumnWidth(contentWidth, colCount)

	// Pre-compute card heights and per-task vertical extents per wave so
	// the inter-wave gutter router can anchor edges on the right
	// midline regardless of how many content lines each card carries.
	cardHeights := make(map[int64]int)
	for _, wv := range show.Waves {
		for _, t := range wv.Tasks {
			cardHeights[t.TaskID] = planNetworkCardHeight(t, blockers, dependents)
		}
	}
	waveExtents := make([]map[int64]planNetworkBoxExtent, len(show.Waves))
	for i, wv := range show.Waves {
		waveExtents[i] = planNetworkBoxRows(wv, cardHeights)
	}
	waveToIdx := make(map[int64]int, len(show.Waves))
	for i, wv := range show.Waves {
		waveToIdx[wv.Wave.ID] = i
	}
	taskToWave := make(map[int64]int64)
	for _, wv := range show.Waves {
		for _, t := range wv.Tasks {
			taskToWave[t.TaskID] = wv.Wave.ID
		}
	}

	cells := make([]string, 0, colCount)
	for i, wv := range show.Waves {
		focused := i == m.planNetworkWaveCursor
		cursorIdx := -1
		if focused {
			cursorIdx = m.planNetworkTaskCursor
		}
		cells = append(cells, m.renderPlanNetworkColumn(wv, focused, cursorIdx, colInner, wv.Wave.ID == activeWaveID, wv.Wave.ID, activeWaveID, finalBucket, blockers, dependents, criticalPath, nextClaimableID))
	}

	// Compute the joined block's row count so each gutter is sized to
	// match the tallest neighbouring column. lipgloss.JoinHorizontal
	// auto-pads shorter columns with blank rows, so picking max works.
	totalRows := 0
	for _, c := range cells {
		if h := strings.Count(c, "\n") + 1; h > totalRows {
			totalRows = h
		}
	}

	// Build inter-wave gutter strings carrying cross-wave edges.
	const gutterWidth = 6
	gutters := make([][]string, colCount-1)
	for gi := 0; gi < colCount-1; gi++ {
		edges := planNetworkCrossWaveEdges(show.Dependencies, gi, taskToWave, waveToIdx, waveExtents)
		gutters[gi] = renderPlanNetworkGutter(gutterWidth, totalRows, edges)
	}

	var parts []string
	for i, c := range cells {
		parts = append(parts, c)
		if i < len(cells)-1 {
			parts = append(parts, m.styles.hintAccent.Render(strings.Join(gutters[i], "\n")))
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
	if footer := planNetworkDepsFooter(show.Dependencies); footer != "" {
		sb.WriteString("\n\n")
		sb.WriteString(indentBlock(m.styles.muted.Render(footer), 2))
	}
	if nextClaimableID != 0 {
		sb.WriteString("\n  ")
		sb.WriteString(m.styles.hintAccent.Render(fmt.Sprintf("▶ next claimable: #%d", nextClaimableID)))
	}
	return sb.String()
}

// planNetworkCrossWaveEdges filters PlanShow.Dependencies down to the
// edges that cross the gutter between adjacent waves gutterIdx and
// gutterIdx+1. Each returned edge carries the rendered source row
// (in the left wave column) and destination row (in the right wave
// column) of the connected cards' midlines, so the gutter router
// can draw the arrow directly.
func planNetworkCrossWaveEdges(deps []domain.TaskDependency, gutterIdx int, taskToWave map[int64]int64, waveToIdx map[int64]int, extents []map[int64]planNetworkBoxExtent) []planNetworkGutterEdge {
	if len(deps) == 0 {
		return nil
	}
	var out []planNetworkGutterEdge
	for _, d := range deps {
		srcWave, ok := taskToWave[d.DependsOnTaskID]
		if !ok {
			continue
		}
		dstWave, ok := taskToWave[d.TaskID]
		if !ok {
			continue
		}
		srcIdx, sok := waveToIdx[srcWave]
		dstIdx, dok := waveToIdx[dstWave]
		if !sok || !dok {
			continue
		}
		// Only edges whose source sits in the left wave of THIS
		// gutter (srcIdx == gutterIdx) and destination sits in the
		// right wave (dstIdx == gutterIdx+1) belong here. Edges that
		// skip a wave column are tracked separately (out of scope for
		// the single-gutter MVP — they fall back to the inline
		// markers).
		if srcIdx != gutterIdx || dstIdx != gutterIdx+1 {
			continue
		}
		srcRow, ok := extents[srcIdx][d.DependsOnTaskID]
		if !ok {
			continue
		}
		dstRow, ok := extents[dstIdx][d.TaskID]
		if !ok {
			continue
		}
		out = append(out, planNetworkGutterEdge{SrcY: srcRow.Mid, DstY: dstRow.Mid})
	}
	return out
}

// planNetworkBoxRows computes per-task vertical extents inside a
// rendered wave column: returns each task's top row, mid row, and
// bottom row (0-indexed from the top of the column). Used by the
// edge-routing helper to anchor arrows on card midlines without
// re-measuring every renderer call. headerRows = 2 (kicker +
// separator) and each card is 1 (top border) + N content + 1
// (bottom border), with no inter-card spacer because the cards stack
// flush.
func planNetworkBoxRows(wv app.PlanWaveView, cardHeights map[int64]int) map[int64]planNetworkBoxExtent {
	out := make(map[int64]planNetworkBoxExtent, len(wv.Tasks))
	y := 2 // header + separator rows
	for _, t := range wv.Tasks {
		h := cardHeights[t.TaskID]
		if h < 3 {
			h = 3
		}
		out[t.TaskID] = planNetworkBoxExtent{Top: y, Mid: y + h/2, Bottom: y + h - 1}
		y += h
	}
	return out
}

// planNetworkBoxExtent records the rendered top / middle / bottom
// row of one task card so the gutter router can anchor edges on the
// card's midline (centred regardless of how many content lines the
// card carries).
type planNetworkBoxExtent struct {
	Top    int
	Mid    int
	Bottom int
}

// planNetworkCardHeight measures the rendered height of a task's
// card in rows: 2 border rows + the same content lines
// renderPlanNetworkCard produces (head + optional @assignee + optional
// ← blockers + optional → dependents). The helper stays decoupled
// from the renderer so the gutter router can call it without holding a
// Model receiver.
func planNetworkCardHeight(t domain.PlanTaskRow, blockers, dependents map[int64][]int64) int {
	content := 1 // head line
	if t.AssignedTo != "" {
		content++
	}
	if ids, ok := blockers[t.TaskID]; ok && len(ids) > 0 {
		content++
	}
	if ids, ok := dependents[t.TaskID]; ok && len(ids) > 0 {
		content++
	}
	return content + 2 // top + bottom borders
}

// renderPlanNetworkGutter draws the inter-wave gutter content as a
// height-row slice of strings where each cross-wave dependency
// surfaces as a horizontal line + bend + arrow. Source rows are the
// source card's midline; destination rows are the destination card's
// midline. The router writes into a small char grid sized
// (gutterWidth, totalRows) and folds box-drawing junctions on
// overlap.
func renderPlanNetworkGutter(gutterWidth, totalRows int, edges []planNetworkGutterEdge) []string {
	if gutterWidth <= 0 || totalRows <= 0 {
		return nil
	}
	cells := make([][]uint8, totalRows)
	for i := range cells {
		cells[i] = make([]uint8, gutterWidth)
	}

	setDir := func(x, y int, dir uint8) {
		if x < 0 || x >= gutterWidth || y < 0 || y >= totalRows {
			return
		}
		cells[y][x] |= dir
	}

	for _, e := range edges {
		// Route: horizontal across top half of gutter at srcY, vertical
		// turn at midX, horizontal across bottom half at dstY.
		midX := gutterWidth / 2
		// horizontal: srcY 0..midX
		for x := 0; x <= midX; x++ {
			setDir(x, e.SrcY, dirE)
			if x > 0 {
				setDir(x, e.SrcY, dirW)
			}
		}
		// vertical along midX between srcY and dstY (inclusive)
		lo, hi := e.SrcY, e.DstY
		if lo > hi {
			lo, hi = hi, lo
		}
		for y := lo; y <= hi; y++ {
			setDir(midX, y, dirN|dirS)
		}
		// fix endpoints of the vertical (no overshoot)
		if e.SrcY < e.DstY {
			cells[e.SrcY][midX] = cells[e.SrcY][midX] &^ dirN
			cells[e.DstY][midX] = cells[e.DstY][midX] &^ dirS
		} else if e.SrcY > e.DstY {
			cells[e.SrcY][midX] = cells[e.SrcY][midX] &^ dirS
			cells[e.DstY][midX] = cells[e.DstY][midX] &^ dirN
		}
		// horizontal: dstY midX..gutterWidth-1
		for x := midX; x < gutterWidth; x++ {
			setDir(x, e.DstY, dirE)
			if x > midX {
				setDir(x, e.DstY, dirW)
			}
		}
		// final arrow head at the last column
		cells[e.DstY][gutterWidth-1] |= dirArrow
	}

	out := make([]string, totalRows)
	for y := 0; y < totalRows; y++ {
		var sb strings.Builder
		for x := 0; x < gutterWidth; x++ {
			sb.WriteRune(planNetworkJunction(cells[y][x]))
		}
		out[y] = sb.String()
	}
	return out
}

// Direction flags for the char-grid cell. The grid stores a bitmask
// per cell so overlapping edges fold into the right junction glyph
// at render time.
const (
	dirN     uint8 = 1 << 0
	dirS     uint8 = 1 << 1
	dirE     uint8 = 1 << 2
	dirW     uint8 = 1 << 3
	dirArrow uint8 = 1 << 4
)

// planNetworkJunction picks the box-drawing glyph for a given
// direction-flag bitmask. The 16 NSEW cases plus the explicit
// arrowhead flag cover every junction the router emits.
func planNetworkJunction(bits uint8) rune {
	if bits&dirArrow != 0 {
		return '→'
	}
	switch bits & (dirN | dirS | dirE | dirW) {
	case 0:
		return ' '
	case dirN:
		return '╵'
	case dirS:
		return '╷'
	case dirN | dirS:
		return '│'
	case dirE:
		return '╶'
	case dirW:
		return '╴'
	case dirE | dirW:
		return '─'
	case dirN | dirE:
		return '└'
	case dirN | dirW:
		return '┘'
	case dirS | dirE:
		return '┌'
	case dirS | dirW:
		return '┐'
	case dirN | dirS | dirE:
		return '├'
	case dirN | dirS | dirW:
		return '┤'
	case dirN | dirE | dirW:
		return '┴'
	case dirS | dirE | dirW:
		return '┬'
	case dirN | dirS | dirE | dirW:
		return '┼'
	}
	return ' '
}

// planNetworkGutterEdge identifies one cross-wave edge by row indices
// inside the joined column block (already resolved to the actual
// rendered row of the source and destination card midlines).
type planNetworkGutterEdge struct {
	SrcY int
	DstY int
}

// planNetworkPeekNextClaimable wraps the PlanRepository peek so the
// renderer can highlight the candidate ClaimNext would reserve next.
// Returns 0 when no candidate is available (or any repo / snap error)
// so a failed peek never blocks the render.
func planNetworkPeekNextClaimable(repo app.PlanRepository, ctx context.Context, project domain.ProjectContext, planID int64, snap *config.Snapshot) int64 {
	if repo == nil || snap == nil || planID == 0 {
		return 0
	}
	row, ok, err := repo.PeekNextClaimable(ctx, project.ID, planID, snap)
	if err != nil || !ok {
		return 0
	}
	return row.TaskID
}

// planNetworkDepsFooter renders the cross-task edge summary shown
// under the diagram: "Dependencies: #A→#B,#C  #D→#E". Tasks are
// ordered by dependent id; blockers per dependent stay sorted asc so
// the line reads the same on every refresh.
func planNetworkDepsFooter(deps []domain.TaskDependency) string {
	if len(deps) == 0 {
		return ""
	}
	bytask := map[int64][]int64{}
	for _, d := range deps {
		bytask[d.TaskID] = append(bytask[d.TaskID], d.DependsOnTaskID)
	}
	keys := make([]int64, 0, len(bytask))
	for k := range bytask {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var sb strings.Builder
	sb.WriteString("Dependencies: ")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("  ")
		}
		sort.Slice(bytask[k], func(a, b int) bool { return bytask[k][a] < bytask[k][b] })
		parts := make([]string, len(bytask[k]))
		for j, b := range bytask[k] {
			parts[j] = fmt.Sprintf("#%d", b)
		}
		fmt.Fprintf(&sb, "#%d→%s", k, strings.Join(parts, ","))
	}
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
	// Cap at 56 columns so a very wide terminal does not stretch each
	// card to the point of looking sparse, but a moderately wide one
	// still has room for "○ #NN longer-task-title @assignee ← #N #M
	// → #M #N" without truncating mid-marker.
	if w > 56 {
		return 56
	}
	return w
}

// renderPlanNetworkColumn renders a single wave column: header (name +
// per-wave done/total), a separator rule, then one bordered card per
// task. Cards reuse the board's chrome (m.styles.card / cardSelected)
// so the network view inherits the project's design language without a
// new style token. Intra-plan blockers / dependents surface inline on
// the card body; cross-wave routing lives in the inter-wave gutters
// produced by renderPlanNetworkGutter, not here.
func (m Model) renderPlanNetworkColumn(wv app.PlanWaveView, focused bool, cursorIdx, width int, isActive bool, waveID, activeWaveID int64, finalBucket string, blockers, dependents map[int64][]int64, criticalPath map[int64]bool, nextClaimableID int64) string {
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
	// Card chrome (NormalBorder + Padding(0, 1)) reserves 2 cols for
	// borders and 2 for padding, so the content width is width-4.
	cardContent := width - 4
	if cardContent < 8 {
		cardContent = 8
	}
	for i, t := range wv.Tasks {
		card := m.renderPlanNetworkCard(t, waveID, activeWaveID, finalBucket, focused && i == cursorIdx, blockers, dependents, criticalPath, nextClaimableID, width, cardContent)
		rows = append(rows, card)
	}
	return strings.Join(rows, "\n")
}

// renderPlanNetworkCard renders a single task through the shared
// renderTaskCard helper so the plan network view inherits the same
// bordered-pill chrome the board uses. Plan-specific badges (status,
// blockers, dependents, next-claimable hint) and the @assigned
// extra line are built here and passed in as taskCardSpec fields —
// no surface-specific layout escapes into the helper.
func (m Model) renderPlanNetworkCard(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string, selected bool, blockers, dependents map[int64][]int64, criticalPath map[int64]bool, nextClaimableID int64, boxWidth, contentWidth int) string {
	var extras []string
	if t.AssignedTo != "" {
		extras = append(extras, "@"+t.AssignedTo)
	}
	if ids := blockers[t.TaskID]; len(ids) > 0 {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("#%d", id)
		}
		extras = append(extras, "← "+strings.Join(parts, " "))
	}
	if ids := dependents[t.TaskID]; len(ids) > 0 {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("#%d", id)
		}
		extras = append(extras, "→ "+strings.Join(parts, " "))
	}
	return m.renderTaskCard(taskCardSpec{
		ID:         t.TaskID,
		Title:      t.Title,
		ExtraLines: extras,
		Badges:     m.planCardBadges(t, waveID, activeWaveID, finalBucket, blockers, dependents, criticalPath, nextClaimableID),
		Selected:   selected,
		Archived:   t.State == domain.TaskStateArchived,
		Accent:     t.TaskID == nextClaimableID || criticalPath[t.TaskID],
		BoxWidth:   boxWidth,
		InnerWidth: contentWidth,
	})
}

// planCardBadges builds the plan-specific badge slice rendered into
// each card. Order is intentional: status (most informative), then
// next-claimable / critical-path flags (actionable hints), then the
// count badges (blockers, dependents, comments). wrapBadges inside
// renderTaskCard reflows them if the row overflows the inner width.
func (m Model) planCardBadges(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string, blockers, dependents map[int64][]int64, criticalPath map[int64]bool, nextClaimableID int64) []string {
	var badges []string
	badges = append(badges, m.planStatusBadgePill(t, waveID, activeWaveID, finalBucket))
	if t.TaskID == nextClaimableID {
		badges = append(badges, m.styles.badgeInfo.Render("next"))
	}
	if criticalPath[t.TaskID] {
		badges = append(badges, m.styles.badgeTokenYellow.Render("critical"))
	}
	if n := len(blockers[t.TaskID]); n > 0 {
		badges = append(badges, m.styles.badgeBlocker.Render(fmt.Sprintf("%d %s", n, plural(n, m.t("tui.badge.blocker"), m.t("tui.badge.blockers")))))
	}
	if n := len(dependents[t.TaskID]); n > 0 {
		badges = append(badges, m.styles.badgeScope.Render(fmt.Sprintf("%d %s", n, plural(n, "dependent", "dependents"))))
	}
	if cmts := m.commentCount(t.TaskID); cmts > 0 {
		badges = append(badges, m.styles.badgeComment.Render(fmt.Sprintf("%d %s", cmts, plural(cmts, m.t("tui.badge.comment"), m.t("tui.badge.comments")))))
	}
	return badges
}

// planStatusBadgePill maps the four task states (done / dev / ready
// / gated) onto coloured pills so the card surfaces status as a
// real badge instead of a glyph buried in the title row.
//
// - done   → badgeNormal (success / green)
// - dev    → badgeInfo   (secondary / accent)
// - gated  → badgeBlocker (warning / yellow)
// - ready  → badgeComment (border / neutral)
func (m Model) planStatusBadgePill(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string) string {
	switch {
	case finalBucket != "" && t.BucketKey == finalBucket:
		return m.styles.badgeNormal.Render("✓ done")
	case t.BucketKey == "dev":
		return m.styles.badgeInfo.Render("● dev")
	case activeWaveID != 0 && waveID != activeWaveID:
		return m.styles.badgeBlocker.Render("⊘ gated")
	default:
		return m.styles.badgeComment.Render("○ ready")
	}
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

// planNetworkCriticalPath returns the set of task ids on the longest
// blocker chain inside the plan. Tasks are scored by the depth of
// their longest blocker chain (memoised DFS over the dependency
// graph); the deepest task and every blocker reachable from it form
// the critical path. Returns nil when the plan has fewer than two
// chained tasks (no chain → no path worth highlighting). Ties on the
// deepest endpoint break by lowest task id for stable rendering.
func planNetworkCriticalPath(deps []domain.TaskDependency, tasks []domain.PlanTaskRow) map[int64]bool {
	if len(deps) == 0 || len(tasks) == 0 {
		return nil
	}
	blockers := map[int64][]int64{}
	for _, d := range deps {
		blockers[d.TaskID] = append(blockers[d.TaskID], d.DependsOnTaskID)
	}

	memoDepth := map[int64]int{}
	var depth func(id int64, seen map[int64]bool) int
	depth = func(id int64, seen map[int64]bool) int {
		if d, ok := memoDepth[id]; ok {
			return d
		}
		if seen[id] {
			// Cycle guard: dependency cycles are invalid by design, but
			// the helper must not infinite-loop if one slips through.
			return 0
		}
		seen[id] = true
		best := 0
		for _, b := range blockers[id] {
			if d := depth(b, seen); d+1 > best {
				best = d + 1
			}
		}
		delete(seen, id)
		memoDepth[id] = best
		return best
	}

	bestID := int64(0)
	bestDepth := 0
	for _, t := range tasks {
		d := depth(t.TaskID, map[int64]bool{})
		if d > bestDepth || (d == bestDepth && bestID == 0) || (d == bestDepth && t.TaskID < bestID) {
			bestID = t.TaskID
			bestDepth = d
		}
	}
	if bestDepth == 0 {
		return nil
	}

	path := map[int64]bool{bestID: true}
	current := bestID
	for {
		next := int64(0)
		bestSubDepth := -1
		for _, b := range blockers[current] {
			d := memoDepth[b]
			if d > bestSubDepth || (d == bestSubDepth && (next == 0 || b < next)) {
				next = b
				bestSubDepth = d
			}
		}
		// Stop on no-blocker leaf OR when the walk revisits a node
		// (cycle in the dep graph would otherwise loop forever).
		if next == 0 || path[next] {
			break
		}
		path[next] = true
		current = next
	}
	return path
}

// planNetworkBlockerIndex folds the in-plan dependency slice into a
// dependent→[blocker ids] lookup so each task row can render its
// blockers in O(1). Each value slice is sorted ascending for stable
// markers across refreshes.
func planNetworkBlockerIndex(deps []domain.TaskDependency) map[int64][]int64 {
	if len(deps) == 0 {
		return nil
	}
	out := make(map[int64][]int64, len(deps))
	for _, d := range deps {
		out[d.TaskID] = append(out[d.TaskID], d.DependsOnTaskID)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i] < out[k][j] })
	}
	return out
}

// planNetworkDependentIndex inverts the dependency slice: blocker→
// [dependent ids]. The renderer uses this to suffix "→ #N #M" on
// every task that BLOCKS something else, complementing the
// "← #N" blocker marker so a reviewer sees both sides of every edge
// from one line.
func planNetworkDependentIndex(deps []domain.TaskDependency) map[int64][]int64 {
	if len(deps) == 0 {
		return nil
	}
	out := make(map[int64][]int64, len(deps))
	for _, d := range deps {
		out[d.DependsOnTaskID] = append(out[d.DependsOnTaskID], d.TaskID)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i] < out[k][j] })
	}
	return out
}

