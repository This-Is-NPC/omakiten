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

	cells := make([]string, 0, colCount)
	for i, wv := range show.Waves {
		focused := i == m.planNetworkWaveCursor
		cursorIdx := -1
		if focused {
			cursorIdx = m.planNetworkTaskCursor
		}
		cells = append(cells, m.renderPlanNetworkColumn(wv, focused, cursorIdx, colInner, wv.Wave.ID == activeWaveID, wv.Wave.ID, activeWaveID, finalBucket, blockers, dependents, criticalPath, nextClaimableID))
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
// per-wave done/total), a separator rule, then one row per task. The
// focused-wave header uses the accent style so cursor focus is visible
// even when no individual task carries the cursor. blockers maps every
// dependent task id to its sorted blocker list so each task line can
// suffix a "← #N #M" marker without re-scanning PlanShow.Dependencies.
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
	for i, t := range wv.Tasks {
		badge := planNetworkStatusBadge(t, waveID, activeWaveID, finalBucket)
		marker := m.cursorMarker(focused && i == cursorIdx)
		assignee := ""
		if t.AssignedTo != "" {
			assignee = " @" + t.AssignedTo
		}
		blockerMarker := ""
		if ids, ok := blockers[t.TaskID]; ok && len(ids) > 0 {
			parts := make([]string, len(ids))
			for j, id := range ids {
				parts[j] = fmt.Sprintf("#%d", id)
			}
			blockerMarker = " ← " + strings.Join(parts, " ")
		}
		dependentMarker := ""
		if ids, ok := dependents[t.TaskID]; ok && len(ids) > 0 {
			parts := make([]string, len(ids))
			for j, id := range ids {
				parts[j] = fmt.Sprintf("#%d", id)
			}
			dependentMarker = " → " + strings.Join(parts, " ")
		}
		title := fmt.Sprintf("%s %s #%d %s%s%s%s",
			marker, badge, t.TaskID, t.Title, assignee, blockerMarker, dependentMarker,
		)
		rendered := truncateText(title, width)
		// Prefix decorations: ▶ on the next-claimable candidate, ║ on
		// every critical-path row. When both apply the next-claimable
		// glyph wins the leading slot so a reviewer sees the actionable
		// hint first; ║ still announces the chain via the accent style
		// on the rest of the line.
		switch {
		case t.TaskID == nextClaimableID && criticalPath[t.TaskID]:
			rendered = m.styles.hintAccent.Render("▶ ") + m.styles.hintAccent.Render(rendered)
		case t.TaskID == nextClaimableID:
			rendered = m.styles.hintAccent.Render("▶ ") + rendered
		case criticalPath[t.TaskID]:
			rendered = m.styles.hintAccent.Render("║ ") + rendered
		}
		rows = append(rows, rendered)
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
