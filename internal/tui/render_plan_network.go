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
			m.syncPlanNetworkColScroll()
		}
	case "right", "l":
		if m.planNetworkWaveCursor < len(m.planNetworkShow.Waves)-1 {
			m.planNetworkWaveCursor++
			m.planNetworkTaskCursor = 0
			m.syncPlanNetworkColScroll()
		}
	case "up", "k":
		if tasks := m.planNetworkCurrentTasks(); m.planNetworkTaskCursor > 0 && len(tasks) > 0 {
			m.planNetworkTaskCursor--
			m.syncPlanNetworkVScroll()
		}
	case "down", "j":
		tasks := m.planNetworkCurrentTasks()
		if m.planNetworkTaskCursor < len(tasks)-1 {
			m.planNetworkTaskCursor++
			m.syncPlanNetworkVScroll()
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

// syncPlanNetworkColScroll mirrors syncBoardColScroll: keeps the
// horizontal slide aligned so the focused wave stays inside the
// per-screen capacity window. Called from h/l so a narrow terminal
// scrolls automatically as the user navigates the wave cursor.
func (m *Model) syncPlanNetworkColScroll() {
	n := len(m.planNetworkShow.Waves)
	if n == 0 {
		m.planNetworkColScroll = 0
		return
	}
	layout := m.computeBoardLayout(n)
	cap := m.boardColumnCapacity(layout)
	focused := clampInt(m.planNetworkWaveCursor, 0, n-1)
	m.planNetworkColScroll = scrollIntoView(m.planNetworkColScroll, focused, n, cap)
}

// syncPlanNetworkVScroll mirrors syncBoardScroll: per-wave vertical
// scroll so the focused task stays inside the panel's row budget
// when a wave carries more cards than fit.
func (m *Model) syncPlanNetworkVScroll() {
	tasks := m.planNetworkCurrentTasks()
	if len(tasks) == 0 || m.planNetworkWaveCursor < 0 || m.planNetworkWaveCursor >= len(m.planNetworkShow.Waves) {
		return
	}
	viewport := m.planNetworkViewportRows()
	if viewport <= 0 {
		return
	}
	if m.planNetworkScroll == nil {
		m.planNetworkScroll = map[int64]int{}
	}
	intraBlockers, intraDependents := planNetworkIntraWaveIndices(m.planNetworkShow.Dependencies, m.planNetworkShow.Waves)
	heights := make([]int, len(tasks))
	for i, t := range tasks {
		// 2 border rows + content lines (mirrors renderPlanNetworkCard
		// extras: title + optional @assignee + intra-wave ← / → lines
		// + badge row). Cross-wave edges are routed through the gutter
		// / backplane instead of producing card extras, so they no
		// longer inflate per-card height.
		c := 1
		if t.AssignedTo != "" {
			c++
		}
		if len(intraBlockers[t.TaskID]) > 0 {
			c++
		}
		if len(intraDependents[t.TaskID]) > 0 {
			c++
		}
		c++ // badge row
		heights[i] = c + 2
	}
	waveID := m.planNetworkShow.Waves[m.planNetworkWaveCursor].Wave.ID
	m.planNetworkScroll[waveID] = followScrollWindowSplit(m.planNetworkScroll[waveID], m.planNetworkTaskCursor, heights, viewport)
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
	// Intra-wave subset of the same indices. The renderer surfaces
	// these as inline "← #N" / "→ #M" markers on the card body
	// because no gutter exists between same-wave cards (they stack
	// flush). Cross-wave edges are deliberately filtered out so
	// the gutter arrows + backplane band remain the authoritative
	// surface for those.
	intraBlockers, intraDependents := planNetworkIntraWaveIndices(show.Dependencies, show.Waves)
	criticalPath := planNetworkCriticalPath(show.Dependencies, allTasks)
	nextClaimableID := planNetworkPeekNextClaimable(m.repos.Plans, m.ctx, m.project, show.Plan.ID, m.repos.activeSnapshot())

	colCount := len(show.Waves)
	// Reuse the board's layout helper so plan cards inherit the same
	// min/max card-width clamp + per-column geometry as the kanban
	// surface. Two surfaces, one width-discipline → no drift between
	// `// board` and `// plans` when the user resizes the terminal.
	layout := m.computeBoardLayout(colCount)
	colInner := layout.columnInner

	// Pre-compute card heights and per-task vertical extents per wave so
	// the inter-wave gutter router can anchor edges on the right
	// midline regardless of how many content lines each card carries.
	cardHeights := make(map[int64]int)
	for _, wv := range show.Waves {
		for _, t := range wv.Tasks {
			cardHeights[t.TaskID] = planNetworkCardHeight(t, intraBlockers, intraDependents)
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

	// Horizontal slide: reuse the board's column-capacity helper +
	// scrollIntoView so a narrow terminal carrying 5 waves behaves
	// the same as a narrow terminal carrying 5 buckets.
	cap := m.boardColumnCapacity(layout)
	if cap > colCount {
		cap = colCount
	}
	focused := clampInt(m.planNetworkWaveCursor, 0, colCount-1)
	start := scrollIntoView(m.planNetworkColScroll, focused, colCount, cap)
	end := start + cap
	if end > colCount {
		end = colCount
	}

	cells := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		wv := show.Waves[i]
		isFocused := i == m.planNetworkWaveCursor
		cursorIdx := -1
		if isFocused {
			cursorIdx = m.planNetworkTaskCursor
		}
		cells = append(cells, m.renderPlanNetworkColumn(wv, isFocused, cursorIdx, colInner, wv.Wave.ID == activeWaveID, wv.Wave.ID, activeWaveID, finalBucket, blockers, dependents, intraBlockers, intraDependents, criticalPath, nextClaimableID, layout))
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

	// Build inter-wave gutter strings carrying cross-wave edges. Only
	// the gutters between waves currently in the visible window are
	// drawn; off-screen waves contribute their edges to the off-screen
	// hint instead.
	const gutterWidth = 6
	gutters := make([][]string, 0, end-start-1)
	for gi := start; gi < end-1; gi++ {
		edges := planNetworkCrossWaveEdges(show.Dependencies, gi, taskToWave, waveToIdx, waveExtents)
		gutters = append(gutters, renderPlanNetworkGutter(gutterWidth, totalRows, edges))
	}

	// Style gutter strings per-glyph so the network "lines" stay muted
	// (border-grey) while the arrowheads themselves pop in accent
	// colour. The prior all-green gutter painted both, which made the
	// route fight the cards for visual weight.
	var parts []string
	for i, c := range cells {
		parts = append(parts, c)
		if i < len(cells)-1 {
			parts = append(parts, m.styleGutterRows(gutters[i]))
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	// Skip-edge backplane: cross-2+-wave edges route via a band rendered
	// ABOVE the wave columns. Adjacent-wave edges already live inside
	// the inter-wave gutters; the backplane only carries edges that
	// would otherwise be invisible to the diagram. Routes sweep
	// horizontally across the intermediate wave columns and terminate
	// with a `▼` glyph above the destination column header — the
	// signal "incoming dep from another wave" — without writing into
	// the cards themselves.
	skipEdges := planNetworkSkipEdges(show.Dependencies, taskToWave, waveToIdx)
	backplane := m.renderPlanNetworkBackplane(skipEdges, layout.cardWidth+2, gutterWidth, start, end)

	pct := planPercent(show.DoneCount, show.TotalCount)
	header := fmt.Sprintf(m.t("tui.plans.network.header_fmt"), show.Plan.Slug, show.DoneCount, show.TotalCount, pct) + "   " + m.t("tui.plans.network.keymap")

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(m.styles.hintAccent.Render(header), 2))
	sb.WriteString("\n\n")
	if backplane != "" {
		sb.WriteString(indentBlock(backplane, 2))
		sb.WriteString("\n")
	}
	sb.WriteString(indentBlock(board, 2))
	if cap < colCount {
		// Mirror renderBoard's lanes_hint: show user the visible window
		// vs total so the hidden waves are not silently dropped.
		hint := fmt.Sprintf(m.t("tui.board.lanes_hint_fmt"), start+1, end, colCount)
		sb.WriteString("\n  " + m.styles.hint.Render(hint))
	}
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
// intra-wave ← blockers + optional intra-wave → dependents). The
// helper takes the INTRA-wave maps because cross-wave edges no longer
// inflate the card — they live in the gutter / backplane instead.
func planNetworkCardHeight(t domain.PlanTaskRow, intraBlockers, intraDependents map[int64][]int64) int {
	content := 1 // head line
	if t.AssignedTo != "" {
		content++
	}
	if ids, ok := intraBlockers[t.TaskID]; ok && len(ids) > 0 {
		content++
	}
	if ids, ok := intraDependents[t.TaskID]; ok && len(ids) > 0 {
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
// arrowhead flag cover every junction the router emits. The
// `►` glyph (heavier than `→`) is used as the terminator so the
// arrow head reads as a clear "lands here" marker against the
// adjacent card's vertical border.
func planNetworkJunction(bits uint8) rune {
	if bits&dirArrow != 0 {
		return '►'
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

// renderPlanNetworkColumn renders a single wave column: header (name +
// per-wave done/total), a separator rule, then one bordered card per
// task. Cards reuse the board's chrome (m.styles.card / cardSelected)
// so the network view inherits the project's design language without a
// new style token. Intra-wave blockers / dependents surface inline on
// the card body; cross-wave routing lives in the inter-wave gutters
// (adjacent edges) and the backplane band (skip edges) instead.
func (m Model) renderPlanNetworkColumn(wv app.PlanWaveView, focused bool, cursorIdx, width int, isActive bool, waveID, activeWaveID int64, finalBucket string, blockers, dependents, intraBlockers, intraDependents map[int64][]int64, criticalPath map[int64]bool, nextClaimableID int64, layout boardLayout) string {
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
	// Clip the wave rule to the card-stack width (cardWidth) instead
	// of the full column-inner width. The cards underneath are
	// cardWidth chars wide, so a colInner-wide rule trailed 2 chars
	// past the rightmost card border — a visible drift between
	// header chrome and content. Aligning rule = cardWidth keeps the
	// editorial kicker visually anchored to the cards it describes.
	rows := []string{
		headerStyle.Render(truncateText(header, layout.cardWidth)),
		m.styles.separator.Render(strings.Repeat("─", layout.cardWidth)),
	}

	if len(wv.Tasks) == 0 {
		rows = append(rows, m.styles.empty.Width(width).Render(m.t("tui.plans.network.empty_wave")))
	}
	// Render every card up front so the (optional) vertical scroll
	// helper sees real heights — mirrors renderKanbanCell.
	rendered := make([]string, len(wv.Tasks))
	heights := make([]int, len(wv.Tasks))
	for i, t := range wv.Tasks {
		rendered[i] = m.renderPlanNetworkCard(t, waveID, activeWaveID, finalBucket, focused && i == cursorIdx, blockers, dependents, intraBlockers, intraDependents, criticalPath, nextClaimableID, layout.cardWidth, layout.cardContentWidth)
		heights[i] = strings.Count(rendered[i], "\n") + 1
	}
	viewport := m.planNetworkViewportRows()
	if viewport <= 0 {
		rows = append(rows, rendered...)
		return strings.Join(rows, "\n")
	}
	offset := m.planNetworkScroll[wv.Wave.ID]
	rows = append(rows, m.renderScrollWindowSplit(rendered, heights, offset, viewport)...)
	return strings.Join(rows, "\n")
}

// planNetworkViewportRows mirrors boardViewportRows for the plan
// network surface. Per-column chrome inside renderPlanNetworkColumn
// is 2 lines (wave header + rule), and the screen chrome around the
// board (kicker + blank + footer hints) eats roughly the same budget
// as the kanban board, so reuse the same chrome=4 constant for
// consistency between the two views.
func (m Model) planNetworkViewportRows() int {
	return m.panelViewportRows(6)
}

// renderPlanNetworkCard renders a single task through the shared
// renderTaskCard helper so the plan network view inherits the same
// bordered-pill chrome the board uses. Plan-specific badges (status,
// blockers, dependents, next-claimable hint) and the @assigned
// extra line are built here and passed in as taskCardSpec fields —
// no surface-specific layout escapes into the helper.
//
// Cross-wave dependencies (adjacent-gutter routed or backplane-
// routed) deliberately do NOT surface as inline "← #N" / "→ #M"
// rows because the gutter arrows + backplane band already carry
// the signal — duplicating them here turned the card into a
// metadata wall. Intra-wave dependencies (source and dest in the
// same wave) DO surface inline because the column-stack layout
// has no gutter to route them through; without the marker the
// only signal would be the muted footer Dependencies line.
func (m Model) renderPlanNetworkCard(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string, selected bool, blockers, dependents map[int64][]int64, intraBlockers, intraDependents map[int64][]int64, criticalPath map[int64]bool, nextClaimableID int64, boxWidth, contentWidth int) string {
	var extras []string
	if t.AssignedTo != "" {
		extras = append(extras, "@"+truncateAgentHandle(t.AssignedTo, contentWidth-1))
	}
	if ids := intraBlockers[t.TaskID]; len(ids) > 0 {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("#%d", id)
		}
		extras = append(extras, "← "+strings.Join(parts, " "))
	}
	if ids := intraDependents[t.TaskID]; len(ids) > 0 {
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
		Badges:     m.planCardBadges(t, waveID, activeWaveID, finalBucket, blockers, nextClaimableID),
		Selected:   selected,
		Archived:   t.State == domain.TaskStateArchived,
		Accent:     t.TaskID == nextClaimableID || criticalPath[t.TaskID],
		BoxWidth:   boxWidth,
		InnerWidth: contentWidth,
	})
}

// planCardBadges builds the plan-specific badge slice rendered into
// each card. The palette is intentionally narrow: status (most
// informative), next-claimable (actionable hint), blocker count
// (action required), and comments (when noisy). Two pills that
// used to live here moved out:
//   - `critical` pill: the critical-path signal is now carried by
//     the card border accent (taskCardSpec.Accent), so painting it
//     as another orange pill duplicated the signal AND collided
//     with the BLOCKER warning colour.
//   - `N dependent` pill: dependent counts are visible from the
//     gutter arrows + backplane band, so the pill became noise.
//     Inline "→ #M" markers remain for intra-wave dependents.
func (m Model) planCardBadges(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string, blockers map[int64][]int64, nextClaimableID int64) []string {
	var badges []string
	badges = append(badges, m.planStatusBadgePill(t, waveID, activeWaveID, finalBucket))
	if t.TaskID == nextClaimableID {
		badges = append(badges, m.styles.badgeInfo.Render("next"))
	}
	if n := len(blockers[t.TaskID]); n > 0 {
		badges = append(badges, m.styles.badgeBlocker.Render(fmt.Sprintf("%d %s", n, plural(n, m.t("tui.badge.blocker"), m.t("tui.badge.blockers")))))
	}
	if cmts := m.commentCount(t.TaskID); cmts >= 2 {
		badges = append(badges, m.styles.badgeComment.Render(fmt.Sprintf("%d %s", cmts, plural(cmts, m.t("tui.badge.comment"), m.t("tui.badge.comments")))))
	}
	return badges
}

// planStatusBadgePill maps the four task states (done / dev / ready
// / gated) onto coloured pills so the card surfaces status as a
// real badge instead of a glyph buried in the title row.
//
// - done   → badgeNormal  (success / green)
// - dev    → badgeInfo    (secondary / accent)
// - gated  → badgeScope   (border / neutral) — gating is a STATE,
//   not a warning; painting it warning-orange collided with the
//   BLOCKER pill and gave the eye nothing to act on.
// - ready  → badgeComment (border / neutral with slightly brighter fg)
func (m Model) planStatusBadgePill(t domain.PlanTaskRow, waveID, activeWaveID int64, finalBucket string) string {
	switch {
	case finalBucket != "" && t.BucketKey == finalBucket:
		return m.styles.badgeNormal.Render("✓ done")
	case t.BucketKey == "dev":
		return m.styles.badgeInfo.Render("● dev")
	case activeWaveID != 0 && waveID != activeWaveID:
		return m.styles.badgeScope.Render("⊘ gated")
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

// planNetworkSkipEdge identifies a dependency that crosses more than
// one wave boundary (e.g., a task in W3 blocked by a task in W1).
// The renderer routes these edges through a backplane band ABOVE the
// wave columns because the inter-wave gutters only span one column
// boundary each, so a multi-column edge has no inline route.
type planNetworkSkipEdge struct {
	SrcIdx    int   // wave index of the blocker
	DstIdx    int   // wave index of the dependent
	SrcTaskID int64 // for footer / backplane labelling
	DstTaskID int64
}

// planNetworkSkipEdges returns the subset of plan dependencies whose
// source and destination wave indices differ by 2 or more. Adjacent
// (gutter-routed) edges and intra-wave edges are filtered out — they
// have their own surfaces (gutter arrows + inline card markers).
func planNetworkSkipEdges(deps []domain.TaskDependency, taskToWave map[int64]int64, waveToIdx map[int64]int) []planNetworkSkipEdge {
	if len(deps) == 0 {
		return nil
	}
	var out []planNetworkSkipEdge
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
		diff := dstIdx - srcIdx
		if diff < 0 {
			diff = -diff
		}
		if diff < 2 {
			continue
		}
		out = append(out, planNetworkSkipEdge{
			SrcIdx:    srcIdx,
			DstIdx:    dstIdx,
			SrcTaskID: d.DependsOnTaskID,
			DstTaskID: d.TaskID,
		})
	}
	return out
}

// renderPlanNetworkBackplane builds the skip-edge band rendered above
// the wave columns. Each visible skip edge gets its own row in the
// band; the route is a horizontal sweep from the source column's
// right edge to a `►` marker landed at the destination column's left
// edge. The terminating `▼` glyph sits one column-cell below the band
// at the dst column's right-edge x, signalling "incoming edge from
// above" without writing into the card surface itself. Returns empty
// string when no skip edge is visible — the band is laid out only
// when it carries real signal.
func (m Model) renderPlanNetworkBackplane(edges []planNetworkSkipEdge, colWidth, gutterWidth, visibleStart, visibleEnd int) string {
	if len(edges) == 0 || colWidth <= 0 || visibleEnd <= visibleStart {
		return ""
	}
	visible := make([]planNetworkSkipEdge, 0, len(edges))
	for _, e := range edges {
		// Both endpoints must be inside the visible window — partial
		// edges (one endpoint off-screen) would render as orphan
		// arrows. They fall back to the muted footer summary instead.
		if e.SrcIdx >= visibleStart && e.SrcIdx < visibleEnd && e.DstIdx >= visibleStart && e.DstIdx < visibleEnd {
			visible = append(visible, e)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	n := visibleEnd - visibleStart
	totalWidth := n*colWidth + (n-1)*gutterWidth
	// One row per edge keeps overlapping routes from colliding without
	// requiring a full lane-allocation pass. Plans tend to have few
	// skip edges in practice (most deps are adjacent), so the band
	// stays compact.
	rows := len(visible)
	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = make([]rune, totalWidth)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}
	colRightX := func(idx int) int {
		return (idx-visibleStart)*(colWidth+gutterWidth) + colWidth - 1
	}
	colLeftX := func(idx int) int {
		return (idx - visibleStart) * (colWidth + gutterWidth)
	}
	for r, e := range visible {
		srcX := colRightX(e.SrcIdx)
		dstX := colLeftX(e.DstIdx)
		lo, hi := srcX, dstX
		reverse := false
		if lo > hi {
			lo, hi = hi, lo
			reverse = true
		}
		for x := lo; x <= hi; x++ {
			grid[r][x] = '─'
		}
		if reverse {
			grid[r][srcX] = '┐'
			grid[r][dstX] = '◄'
		} else {
			grid[r][srcX] = '┌'
			grid[r][dstX] = '►'
		}
	}
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		out[r] = m.styleGutterRow(string(grid[r]))
	}
	return strings.Join(out, "\n")
}

// styleGutterRows applies per-glyph styling across a slice of gutter
// rows, joining them with newlines. The lines themselves render in
// the muted hint colour while arrowheads (`►` / `◄`) pop in the
// accent colour so the eye finds the termination quickly without
// the entire route screaming for attention.
func (m Model) styleGutterRows(rows []string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = m.styleGutterRow(r)
	}
	return strings.Join(out, "\n")
}

// styleGutterRow walks one row's runes and wraps each in the
// appropriate lipgloss style: arrowheads in hintAccent (primary
// green), every other box-drawing glyph in hint (border grey).
// Spaces pass through unstyled so width math stays consistent with
// what JoinHorizontal expects.
func (m Model) styleGutterRow(row string) string {
	if row == "" {
		return ""
	}
	var sb strings.Builder
	for _, r := range row {
		switch r {
		case '►', '◄', '▲', '▼':
			sb.WriteString(m.styles.hintAccent.Render(string(r)))
		case ' ':
			sb.WriteRune(r)
		default:
			sb.WriteString(m.styles.hint.Render(string(r)))
		}
	}
	return sb.String()
}

// planNetworkIntraWaveIndices returns the blocker/dependent maps
// restricted to dependencies whose source AND destination tasks
// live in the same wave. These are the only edges that surface as
// inline "← #N" / "→ #M" markers on the card body — cross-wave
// edges are routed through the gutter / backplane instead, so
// listing them on the card too would duplicate signal and bloat
// the card height.
func planNetworkIntraWaveIndices(deps []domain.TaskDependency, waves []app.PlanWaveView) (map[int64][]int64, map[int64][]int64) {
	if len(deps) == 0 || len(waves) == 0 {
		return nil, nil
	}
	taskToWave := make(map[int64]int64, len(deps))
	for _, wv := range waves {
		for _, t := range wv.Tasks {
			taskToWave[t.TaskID] = wv.Wave.ID
		}
	}
	blockers := map[int64][]int64{}
	dependents := map[int64][]int64{}
	for _, d := range deps {
		srcWave, sok := taskToWave[d.DependsOnTaskID]
		dstWave, dok := taskToWave[d.TaskID]
		if !sok || !dok || srcWave != dstWave {
			continue
		}
		blockers[d.TaskID] = append(blockers[d.TaskID], d.DependsOnTaskID)
		dependents[d.DependsOnTaskID] = append(dependents[d.DependsOnTaskID], d.TaskID)
	}
	for k := range blockers {
		sort.Slice(blockers[k], func(i, j int) bool { return blockers[k][i] < blockers[k][j] })
	}
	for k := range dependents {
		sort.Slice(dependents[k], func(i, j int) bool { return dependents[k][i] < dependents[k][j] })
	}
	return blockers, dependents
}

// truncateAgentHandle clamps long @assigned handles to the card's
// inner width so a verbose model id ("@claude-opus-4-7-20251215")
// doesn't consume the full card line. Returns the original string
// when it already fits. The "…" terminator stays an ASCII safe
// alternative to U+2026 so width math survives the lipgloss render.
func truncateAgentHandle(handle string, max int) string {
	if max < 3 {
		return handle
	}
	if len([]rune(handle)) <= max {
		return handle
	}
	rs := []rune(handle)
	return string(rs[:max-1]) + "…"
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

