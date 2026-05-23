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

// ===========================================================================
// Plan network — collapsible rails + cross-wave filaments
//
// The view renders the plan as one vertical outline. Each wave is a header
// row (▼ expanded, ▶ collapsed). Expanded waves render their tasks as
// one-line cards, with intra-wave dep edges drawn as a left-side `git log`
// style rail (├─ └─ │). Cross-wave deps surface as muted vertical filaments
// in a fixed left margin lane, so the user can trace where blockers come
// from without leaving the outline.
//
// Navigation: a single linear cursor walks every visible row (headers + task
// cards); j/k step one row at a time, space toggles the wave under the
// cursor. The flat row list makes scroll trivial (one line per row), which
// is why the multi-column-per-wave view + per-bucket vertical scroll map +
// gutter/backplane routing the previous design carried were all dropped.
// ===========================================================================

// handlePlanNetworkKey drives the linear-cursor outline. The single
// cursor walks both wave headers and task cards so toggling a wave
// stays inside the same key model as opening a task.
func (m *Model) handlePlanNetworkKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "esc":
		m.closePlanNetwork()
		return
	case "down", "j":
		rows := m.planNetworkBuildRows()
		if m.planNetworkCursor < len(rows)-1 {
			m.planNetworkCursor++
			m.syncPlanNetworkScroll(rows)
		}
	case "up", "k":
		if m.planNetworkCursor > 0 {
			m.planNetworkCursor--
			m.syncPlanNetworkScroll(m.planNetworkBuildRows())
		}
	case " ":
		m.togglePlanWaveAtCursor()
	case "left", "h":
		// Collapse the wave the cursor sits in; jump cursor to its
		// header so subsequent j/k navigates between waves cleanly.
		rows := m.planNetworkBuildRows()
		if m.planNetworkCursor >= len(rows) {
			return
		}
		waveID := rows[m.planNetworkCursor].WaveID
		if m.planNetworkCollapsed == nil {
			m.planNetworkCollapsed = map[int64]bool{}
		}
		m.planNetworkCollapsed[waveID] = true
		rows = m.planNetworkBuildRows()
		for i := m.planNetworkCursor; i >= 0 && i < len(rows); i-- {
			if rows[i].Kind == planRowWaveHeader && rows[i].WaveID == waveID {
				m.planNetworkCursor = i
				break
			}
		}
		m.syncPlanNetworkScroll(rows)
	case "right", "l":
		// Expand the focused wave header. No-op when the cursor is on
		// a task row (already inside an expanded wave).
		rows := m.planNetworkBuildRows()
		if m.planNetworkCursor >= len(rows) {
			return
		}
		row := rows[m.planNetworkCursor]
		if row.Kind != planRowWaveHeader {
			return
		}
		if m.planNetworkCollapsed == nil {
			m.planNetworkCollapsed = map[int64]bool{}
		}
		m.planNetworkCollapsed[row.WaveID] = false
		m.syncPlanNetworkScroll(m.planNetworkBuildRows())
	case "o", "enter":
		rows := m.planNetworkBuildRows()
		if m.planNetworkCursor >= len(rows) {
			return
		}
		row := rows[m.planNetworkCursor]
		if row.Kind != planRowTaskCard {
			// Enter on a wave header is treated as a collapse toggle
			// so the row that already advertises ▼/▶ supports the
			// most common gesture without a modifier.
			m.togglePlanWaveAtCursor()
			return
		}
		if task, ok := m.taskByID(row.Task.TaskID); ok {
			m.openTaskView(task)
		}
	case "pgup", "ctrl+u":
		viewport := m.planNetworkViewportRows()
		step := viewport / 2
		if step < 1 {
			step = 1
		}
		m.planNetworkCursor -= step
		if m.planNetworkCursor < 0 {
			m.planNetworkCursor = 0
		}
		m.syncPlanNetworkScroll(m.planNetworkBuildRows())
	case "pgdown", "ctrl+d":
		rows := m.planNetworkBuildRows()
		viewport := m.planNetworkViewportRows()
		step := viewport / 2
		if step < 1 {
			step = 1
		}
		m.planNetworkCursor += step
		if m.planNetworkCursor >= len(rows) {
			m.planNetworkCursor = len(rows) - 1
		}
		m.syncPlanNetworkScroll(rows)
	case "home", "g":
		m.planNetworkCursor = 0
		m.syncPlanNetworkScroll(m.planNetworkBuildRows())
	case "end", "G":
		rows := m.planNetworkBuildRows()
		if len(rows) > 0 {
			m.planNetworkCursor = len(rows) - 1
		}
		m.syncPlanNetworkScroll(rows)
	case "r":
		if err := m.refreshCurrentView(); err != nil {
			m.status = err.Error()
		}
		m.reloadPlanNetwork()
	case "c":
		m.openPlanAssignEditor()
	case "e":
		m.openPlanGoalEditor()
	}
}

// togglePlanWaveAtCursor flips the collapse flag for the wave the
// cursor currently sits on (its own wave for tasks; the header for
// header rows). Cursor stays in place; if the toggle would push the
// cursor past the end of the visible list, it clamps to the last row.
func (m *Model) togglePlanWaveAtCursor() {
	rows := m.planNetworkBuildRows()
	if m.planNetworkCursor >= len(rows) {
		return
	}
	waveID := rows[m.planNetworkCursor].WaveID
	if m.planNetworkCollapsed == nil {
		m.planNetworkCollapsed = map[int64]bool{}
	}
	m.planNetworkCollapsed[waveID] = !m.planNetworkCollapsed[waveID]
	rows = m.planNetworkBuildRows()
	if m.planNetworkCursor >= len(rows) {
		m.planNetworkCursor = len(rows) - 1
	}
	// Re-snap cursor onto the wave's header when the wave just
	// collapsed and the cursor used to live on a task row inside it —
	// the row at planNetworkCursor would otherwise be the NEXT wave's
	// header (or a later task), which silently teleports the cursor.
	if m.planNetworkCollapsed[waveID] {
		for i := m.planNetworkCursor; i >= 0 && i < len(rows); i-- {
			if rows[i].Kind == planRowWaveHeader && rows[i].WaveID == waveID {
				m.planNetworkCursor = i
				break
			}
		}
	}
	m.syncPlanNetworkScroll(rows)
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
// happened since open. Reused by the `r` binding and by claimNext
// after a successful claim. Cursor clamps to the new row count;
// collapse state survives the reload because wave ids are stable.
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
	rows := m.planNetworkBuildRows()
	if m.planNetworkCursor >= len(rows) {
		m.planNetworkCursor = 0
	}
	m.syncPlanNetworkScroll(rows)
}

// openPlanAssignEditor drives the `c` binding: opens the single-line
// assignee input pre-targeted at the task under the cursor. The
// previous implementation called PlanService.ClaimNext, which moved
// the task into the next bucket as part of the claim — that bypassed
// the preset's bucket guards (omakase requires a self-branch comment
// before backlog → dev). The new flow only writes assigned_to;
// bucket transitions stay manual via the board's move binding.
func (m *Model) openPlanAssignEditor() {
	if m.repos.Tasks == nil || m.planNetworkShow.Plan.ID == 0 {
		return
	}
	rows := m.planNetworkBuildRows()
	if m.planNetworkCursor < 0 || m.planNetworkCursor >= len(rows) {
		m.status = m.t("tui.plans.status.assign_no_task")
		return
	}
	row := rows[m.planNetworkCursor]
	if row.Kind != planRowTaskCard || row.Task.TaskID == 0 {
		m.status = m.t("tui.plans.status.assign_no_task")
		return
	}
	m.planAssignTaskID = row.Task.TaskID
	m.beginInput(modePlanAssign,
		fmt.Sprintf(m.t("tui.plans.assign.status_fmt"), row.Task.TaskID),
		row.Task.AssignedTo,
	)
}

// syncPlanNetworkScroll keeps planNetworkScroll aligned so the cursor
// stays inside the viewport. Each row is exactly one terminal line, so
// the math is the same as table/graph scrolling.
func (m *Model) syncPlanNetworkScroll(rows []planNetworkRow) {
	viewport := m.planNetworkBodyViewportRows()
	if viewport <= 0 || len(rows) == 0 {
		m.planNetworkScroll = 0
		return
	}
	heights := planNetworkRowHeights(rows)
	m.planNetworkScroll = followScrollWindowSplit(m.planNetworkScroll, m.planNetworkCursor, heights, viewport)
}

// planNetworkViewportRows returns the number of terminal rows the
// outline body gets after the screen chrome + the plan view's own
// header + Dependencies footer + next-claimable hint. The chrome
// estimate covers: header line + blank line + footer (Dependencies
// line) + blank + next-claimable line.
func (m Model) planNetworkViewportRows() int {
	return m.panelViewportRows(5)
}

// planNetworkBodyViewportRows returns the rows available for the
// scrollable data region of the bordered table — the full panel
// budget minus the table's static chrome (top border + header row
// + sep below header + bottom border).
func (m Model) planNetworkBodyViewportRows() int {
	const tableChrome = 4
	rows := m.planNetworkViewportRows() - tableChrome
	if rows < 1 {
		return 0
	}
	return rows
}

// ===========================================================================
// Row projection — flatten waves + tasks into a single linear list.
// ===========================================================================

// planNetworkRowKind tags each row as either a wave header (collapsible)
// or a task card body line.
type planNetworkRowKind uint8

const (
	planRowWaveHeader planNetworkRowKind = iota
	planRowTaskCard
	// planRowNone is a sentinel kind used by renderPlanNetworkSeparator
	// to mean "no row on this side" — i.e. the table's top border
	// (no row above) or bottom border (no row below).
	planRowNone
)

// planNetworkRow is one rendered line in the outline. The renderer
// converts each row to a single terminal-line string; navigation,
// scroll, and filament routing all operate on the index of the row
// inside the flat slice returned by planNetworkBuildRows.
type planNetworkRow struct {
	Kind planNetworkRowKind

	WaveID  int64
	WaveIdx int

	// Wave header fields (Kind == planRowWaveHeader).
	WaveName   string
	WavePos    int
	WaveDone   int
	WaveTotal  int
	WaveActive bool
	Collapsed  bool

	// Task card fields (Kind == planRowTaskCard).
	Task          domain.PlanTaskRow
	Rail          string  // pre-built intra-wave rail glyph prefix
	IsNext        bool
	IsCritical    bool
	BlockerCount  int
	IntraBlockers []int64 // intra-wave blockers that are NOT the rail parent
	CrossBlockers []int64 // cross-wave blockers (annotated inline)
	FinalBucket   bool    // task lives in the workflow's final bucket
	InProgress    bool    // task lives between first and final bucket (working pipeline)
	Gated         bool
}

// planNetworkBuild bundles the projected row list with the auxiliary
// indices the renderer needs (cross-wave blockers for filament
// routing + the current next-claimable id for the footer hint).
// Building everything in one pass means the renderer never has to
// re-walk the dependency slice or hit the repo a second time.
type planNetworkBuild struct {
	Rows            []planNetworkRow
	CrossBlockers   map[int64][]int64
	NextClaimableID int64
}

// planNetworkBuildOpts toggles the optional projections inside
// planNetworkBuildData. Key handlers only need the row skeleton
// (Kind / WaveID / len) for cursor + scroll math, so they skip the
// SQL peek + the critical-path DFS to keep navigation O(rows) instead
// of O(rows + 1 query). Render leaves both opts at their zero value
// so the full surface still renders the next-claimable hint and the
// critical-path accent.
type planNetworkBuildOpts struct {
	// SkipPeek drops the PeekNextClaimable SQL call. The resulting
	// build carries NextClaimableID == 0; IsNext on every row will be
	// false. Safe for handler-side row counting.
	SkipPeek bool
	// SkipCritical drops the critical-path DFS. IsCritical on every
	// row will be false. Safe for handler-side row counting.
	SkipCritical bool
}

// planNetworkBuildData projects PlanShow into the flat row list the
// renderer walks AND the auxiliary indices it consumes. Collapsed
// waves emit only a header row. Expanded waves emit their header
// followed by one row per task, ordered by the intra-wave rail tree
// (roots in input order; children DFS pre-order under their first-id
// parent). Pass a non-zero opts to skip projections handlers don't
// need; render passes the zero value to get the full surface.
func (m Model) planNetworkBuildData(opts planNetworkBuildOpts) planNetworkBuild {
	show := m.planNetworkShow
	if len(show.Waves) == 0 {
		return planNetworkBuild{}
	}
	finalBucket := ""
	firstBucket := ""
	if snap := m.repos.activeSnapshot(); snap != nil {
		wf := snap.Workflow()
		finalBucket = wf.FinalBucketKey()
		// First bucket = lowest-position bucket in the workflow. Tasks
		// living in this bucket are "not yet started" — used by the
		// state-badge precedence to distinguish `assigned` (claimed
		// but still in first bucket) from `in-progress` (already in
		// a working bucket downstream).
		firstPos := 0
		for _, b := range wf.Buckets {
			if firstBucket == "" || b.Position < firstPos {
				firstBucket = b.Key
				firstPos = b.Position
			}
		}
	}
	activeWaveID := show.ActiveWaveID

	intraBlockers := planNetworkIntraWaveIndices(show.Dependencies, show.Waves)
	crossBlockers := planNetworkCrossWaveIndices(show.Dependencies, show.Waves)

	var criticalPath map[int64]bool
	if !opts.SkipCritical {
		allTasks := make([]domain.PlanTaskRow, 0)
		for _, wv := range show.Waves {
			allTasks = append(allTasks, wv.Tasks...)
		}
		criticalPath = planNetworkCriticalPath(show.Dependencies, allTasks)
	}
	var nextClaimableID int64
	if !opts.SkipPeek {
		nextClaimableID = planNetworkPeekNextClaimable(m.repos.Plans, m.ctx, m.project, show.Plan.ID, m.repos.activeSnapshot())
	}

	var rows []planNetworkRow
	for waveIdx, wv := range show.Waves {
		collapsed := m.planNetworkCollapsed[wv.Wave.ID]
		// Wave headers carry only a header row in the flat slice; the
		// lane prefix that crosses through them is layered on at
		// render time from the filament list, not encoded in the row.
		// That keeps every header row visually flush at the same
		// screen column regardless of which filaments pass through.
		rows = append(rows, planNetworkRow{
			Kind:       planRowWaveHeader,
			WaveID:     wv.Wave.ID,
			WaveIdx:    waveIdx,
			WaveName:   wv.Wave.Name,
			WavePos:    wv.Wave.Position,
			WaveDone:   wv.DoneCount,
			WaveTotal:  wv.TotalCount,
			WaveActive: wv.Wave.ID == activeWaveID,
			Collapsed:  collapsed,
		})
		if collapsed {
			continue
		}
		// DFS pre-order over intra-wave parents. Topological reorder
		// by readiness is explicitly rejected; the rail tree mirrors
		// the order tasks were authored in.
		layout := planNetworkBuildRails(wv, intraBlockers)
		for pos, idx := range layout.OrderedIdx {
			t := wv.Tasks[idx]
			parentID := layout.ParentByPos[pos]
			rows = append(rows, planNetworkRow{
				Kind:          planRowTaskCard,
				WaveID:        wv.Wave.ID,
				WaveIdx:       waveIdx,
				Task:          t,
				Rail:          layout.Rails[pos],
				IsNext:        t.TaskID == nextClaimableID,
				IsCritical:    criticalPath[t.TaskID],
				BlockerCount:  len(intraBlockers[t.TaskID]) + len(crossBlockers[t.TaskID]),
				IntraBlockers: planNetworkExcludeID(intraBlockers[t.TaskID], parentID),
				CrossBlockers: crossBlockers[t.TaskID],
				FinalBucket:   finalBucket != "" && t.BucketKey == finalBucket,
				InProgress:    finalBucket != "" && firstBucket != "" && t.BucketKey != finalBucket && t.BucketKey != firstBucket,
				Gated:         activeWaveID != 0 && wv.Wave.ID != activeWaveID,
			})
		}
	}
	return planNetworkBuild{
		Rows:            rows,
		CrossBlockers:   crossBlockers,
		NextClaimableID: nextClaimableID,
	}
}

// planNetworkBuildRows is the row-only shim used by the input
// handlers — they never need the auxiliary indices, only the row
// slice for cursor clamps and kind checks. Skips the SQL peek + the
// critical-path DFS so the projection stays cheap on every keystroke
// (the full projection runs once per render in renderPlanNetwork).
func (m Model) planNetworkBuildRows() []planNetworkRow {
	return m.planNetworkBuildData(planNetworkBuildOpts{SkipPeek: true, SkipCritical: true}).Rows
}

// planNetworkWaveRails records the rendered shape of one wave: tasks
// in DFS pre-order over intra-wave parents, the rail glyph prefix per
// ordered position, and the intra-wave rail parent so the renderer
// can exclude that edge from the inline `← #N` annotation.
type planNetworkWaveRails struct {
	OrderedIdx  []int    // wv.Tasks indexes in render order (DFS pre-order)
	Rails       []string // rail prefix per ordered position (├─ / └─ / │ / etc.)
	ParentByPos []int64  // intra-wave rail parent id per ordered position (0 = root)
}

// planNetworkBuildRails projects a wave into DFS pre-order over its
// intra-wave parent tree. The rail parent of each task is the
// lowest-id intra-wave blocker; tasks with no intra-wave blocker (or
// whose blocker appears after them in input order) surface as roots.
// Each row's rail prefix uses standard tree glyphs:
//   - "├─" for a non-last child of its parent,
//   - "└─" for the last child,
//   - "│ " or "  " per ancestor lane (continuing vs. closed).
//
// Topological reorder is explicitly NOT performed — the user
// rejected it. Input order survives within sibling groups.
func planNetworkBuildRails(wv app.PlanWaveView, intraBlockers map[int64][]int64) planNetworkWaveRails {
	tasks := wv.Tasks
	n := len(tasks)
	if n == 0 {
		return planNetworkWaveRails{}
	}
	idxByID := make(map[int64]int, n)
	for i, t := range tasks {
		idxByID[t.TaskID] = i
	}

	// Intra-wave rail parent = lowest-id intra-wave blocker present in
	// the wave. The DFS only descends parent→child where the parent
	// appears BEFORE the child in input order; back-edges revert the
	// child to a root so the rail never tries to draw upward.
	parentOf := make(map[int64]int64, n)
	for i, t := range tasks {
		var pid int64
		for _, b := range intraBlockers[t.TaskID] {
			pi, ok := idxByID[b]
			if !ok || pi >= i {
				continue
			}
			if pid == 0 || b < pid {
				pid = b
			}
		}
		if pid != 0 {
			parentOf[t.TaskID] = pid
		}
	}

	// Children-by-parent retain input order so siblings render in the
	// order the user typed them.
	children := map[int64][]int64{}
	var roots []int64
	for _, t := range tasks {
		if pid, ok := parentOf[t.TaskID]; ok {
			children[pid] = append(children[pid], t.TaskID)
		} else {
			roots = append(roots, t.TaskID)
		}
	}

	orderedIdx := make([]int, 0, n)
	parentByPos := make([]int64, 0, n)
	rails := make([]string, 0, n)

	// Iterative DFS pre-order. The stack stores the ancestor prefix
	// ("│ " for still-open lanes, "  " for closed) plus the current
	// node's last-sibling flag — that lets each emitted row paint its
	// glyph (├─ / └─) and the next descent extend the prefix.
	type frame struct {
		id     int64
		parent int64
		prefix string
		isLast bool
	}
	stack := make([]frame, 0, n)
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{id: roots[i], parent: 0, prefix: "", isLast: i == len(roots)-1})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		glyph := ""
		if f.parent != 0 {
			if f.isLast {
				glyph = "└─"
			} else {
				glyph = "├─"
			}
		}
		rails = append(rails, f.prefix+glyph)
		orderedIdx = append(orderedIdx, idxByID[f.id])
		parentByPos = append(parentByPos, f.parent)

		kids := children[f.id]
		if len(kids) == 0 {
			continue
		}
		var childPrefix string
		if f.parent == 0 {
			childPrefix = f.prefix
		} else if f.isLast {
			childPrefix = f.prefix + "  "
		} else {
			childPrefix = f.prefix + "│ "
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, frame{
				id:     kids[i],
				parent: f.id,
				prefix: childPrefix,
				isLast: i == len(kids)-1,
			})
		}
	}

	return planNetworkWaveRails{
		OrderedIdx:  orderedIdx,
		Rails:       rails,
		ParentByPos: parentByPos,
	}
}

// planNetworkFilament is one cross-wave source's fan-out routed in
// the fixed-width left margin. ONE filament per source task, NOT
// per edge — a source with N dependents reuses the same lane and
// branches off at each destination row via ├─► (intermediate) /
// └─► (last). DstRows is the ascending list of destination rows;
// Lane is the column slot inside the lane block.
type planNetworkFilament struct {
	SrcRow  int
	DstRows []int
	Lane    int
}

// EndRow returns the row at which the filament's lane closes (the
// last destination). Callers use it as the lane-busy boundary.
func (f planNetworkFilament) EndRow() int {
	if len(f.DstRows) == 0 {
		return f.SrcRow
	}
	return f.DstRows[len(f.DstRows)-1]
}

// planNetworkBuildFilaments maps cross-wave dependencies onto lane
// columns, ONE LANE PER SOURCE TASK. A source with multiple
// dependents reuses its lane and branches at each destination row,
// so a hub task with 7 dependents collapses to a single lane (not
// 7). Returns the filament list and the number of distinct lanes
// used (= max overlap across the row list).
//
// Greedy allocation: pending filaments sort by (srcRow, endRow,
// srcTaskID) and pick the leftmost lane whose previous filament
// ends strictly before the new one starts. Stable across re-renders.
//
// Cross-wave blockers whose source row is not visible (collapsed
// wave) are dropped — the renderer can't draw a filament whose
// source has no row. Those edges still surface as `←W #N` text
// annotation via the regular cross-blocker path.
func planNetworkBuildFilaments(rows []planNetworkRow, crossBlockers map[int64][]int64) ([]planNetworkFilament, int) {
	if len(rows) == 0 || len(crossBlockers) == 0 {
		return nil, 0
	}
	rowByID := make(map[int64]int, len(rows))
	for i, r := range rows {
		if r.Kind == planRowTaskCard {
			rowByID[r.Task.TaskID] = i
		}
	}

	// Group destinations under each source. dstSet uses a map to
	// dedupe in case a destination appears more than once.
	srcRow := map[int64]int{}
	dstSet := map[int64]map[int]struct{}{}
	for dstID, blockers := range crossBlockers {
		dRow, dok := rowByID[dstID]
		if !dok {
			continue
		}
		for _, srcID := range blockers {
			sRow, sok := rowByID[srcID]
			if !sok || sRow >= dRow {
				continue
			}
			if dstSet[srcID] == nil {
				dstSet[srcID] = map[int]struct{}{}
			}
			dstSet[srcID][dRow] = struct{}{}
			srcRow[srcID] = sRow
		}
	}
	if len(dstSet) == 0 {
		return nil, 0
	}

	type pending struct {
		src   int
		dsts  []int
		srcID int64
	}
	queue := make([]pending, 0, len(dstSet))
	for srcID, dsts := range dstSet {
		dlist := make([]int, 0, len(dsts))
		for d := range dsts {
			dlist = append(dlist, d)
		}
		sort.Ints(dlist)
		queue = append(queue, pending{src: srcRow[srcID], dsts: dlist, srcID: srcID})
	}
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].src != queue[j].src {
			return queue[i].src < queue[j].src
		}
		li := queue[i].dsts[len(queue[i].dsts)-1]
		lj := queue[j].dsts[len(queue[j].dsts)-1]
		if li != lj {
			return li < lj
		}
		return queue[i].srcID < queue[j].srcID
	})

	var laneEnd []int // last row a lane is busy through (inclusive)
	out := make([]planNetworkFilament, 0, len(queue))
	for _, p := range queue {
		slot := -1
		for li, end := range laneEnd {
			if end < p.src {
				slot = li
				break
			}
		}
		if slot == -1 {
			slot = len(laneEnd)
			laneEnd = append(laneEnd, 0)
		}
		laneEnd[slot] = p.dsts[len(p.dsts)-1]
		out = append(out, planNetworkFilament{SrcRow: p.src, DstRows: p.dsts, Lane: slot})
	}
	return out, len(laneEnd)
}

// planNetworkFilamentSrcIDsAtRow returns the set of source task IDs
// whose filament terminates (or branches) at rowIdx. The annotation
// builder uses this to suppress the redundant `←W #N` text for
// blockers already represented by a ├─► / └─► glyph on this row.
func planNetworkFilamentSrcIDsAtRow(filaments []planNetworkFilament, rows []planNetworkRow, rowIdx int) map[int64]bool {
	if rowIdx < 0 || rowIdx >= len(rows) {
		return nil
	}
	out := map[int64]bool{}
	for _, f := range filaments {
		hit := false
		for _, dr := range f.DstRows {
			if dr == rowIdx {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		src := rows[f.SrcRow]
		if src.Kind == planRowTaskCard {
			out[src.Task.TaskID] = true
		}
	}
	return out
}

// planNetworkExcludeID drops `exclude` from `ids`. Returns the slice
// unchanged when `exclude` is zero or not in the slice.
func planNetworkExcludeID(ids []int64, exclude int64) []int64 {
	if exclude == 0 || len(ids) == 0 {
		return ids
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}

// ===========================================================================
// Renderer — assemble filament lanes + row bodies, apply scroll window.
// ===========================================================================

// renderPlanNetwork builds the outline view. Cross-wave deps route in
// a left-margin filament lane sized to the number of overlapping
// edges; intra-wave deps route through the per-row rail prefix.
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

	build := m.planNetworkBuildData(planNetworkBuildOpts{})
	rows := build.Rows
	nextClaimableID := build.NextClaimableID

	filaments, laneCount := planNetworkBuildFilaments(rows, build.CrossBlockers)

	cursorPad := "  "
	emptyLane := ""
	if laneCount > 0 {
		emptyLane = m.styles.muted.Render(strings.Repeat(" ", laneCount+1))
	}

	// availableWidth - panel chrome (6) - body indent (2) - cursor pad (2)
	budget := m.availableWidth() - 10
	if laneCount > 0 {
		budget -= laneCount + 1
	}
	if budget < 32 {
		budget = 32
	}
	measured := m.planNetworkMeasureTitle(rows, 10, 14)
	layout := planNetworkBuildTable(budget, measured)

	// Static top chrome — top border, column header, sep below
	// header. Never scrolls, so column labels stay anchored.
	var topChrome []string
	topChrome = append(topChrome, m.renderPlanNetworkSeparator(planRowNone, planRowTaskCard, layout, cursorPad, emptyLane))
	topChrome = append(topChrome, m.renderPlanNetworkHeaderRow(layout, cursorPad, emptyLane))
	if len(rows) > 0 {
		topChrome = append(topChrome, m.renderPlanNetworkSeparator(planRowTaskCard, rows[0].Kind, layout, cursorPad, emptyLane))
	}

	// Data rows — one content line, plus a transition separator
	// below when the next row is a different kind. Heights track
	// physical line counts so heights-aware scrolling can land the
	// cursor anywhere.
	rendered := make([]string, len(rows))
	heights := planNetworkRowHeights(rows)
	for i, row := range rows {
		primaryLane := m.renderPlanNetworkLane(i, filaments, laneCount)
		suppress := planNetworkFilamentSrcIDsAtRow(filaments, rows, i)
		line := m.renderPlanNetworkRowBody(row, i == m.planNetworkCursor, primaryLane, suppress, layout)
		if i+1 < len(rows) && rows[i+1].Kind != row.Kind {
			// Compact layout: only wave↔task transitions carry a
			// separator. Consecutive same-kind rows stack tight (tasks
			// separated visually by the dashed title fill).
			contLane := m.renderPlanNetworkLaneContinuation(i, filaments, laneCount)
			sep := m.renderPlanNetworkSeparator(row.Kind, rows[i+1].Kind, layout, cursorPad, contLane)
			line = line + "\n" + sep
		}
		rendered[i] = line
	}

	var bottomChrome string
	if len(rows) > 0 {
		bottomChrome = m.renderPlanNetworkSeparator(rows[len(rows)-1].Kind, planRowNone, layout, cursorPad, emptyLane)
	} else {
		bottomChrome = m.renderPlanNetworkSeparator(planRowNone, planRowNone, layout, cursorPad, emptyLane)
	}

	viewport := m.planNetworkBodyViewportRows()
	var bodyContent string
	if viewport <= 0 || len(rendered) == 0 {
		bodyContent = strings.Join(rendered, "\n")
	} else {
		sliced := m.renderScrollWindowSplit(rendered, heights, m.planNetworkScroll, viewport)
		bodyContent = strings.Join(sliced, "\n")
	}

	chromeParts := append([]string{}, topChrome...)
	chromeParts = append(chromeParts, bodyContent)
	chromeParts = append(chromeParts, bottomChrome)
	tableBlock := strings.Join(chromeParts, "\n")

	pct := planPercent(show.DoneCount, show.TotalCount)
	header := fmt.Sprintf(m.t("tui.plans.network.header_fmt"), show.Plan.Slug, show.DoneCount, show.TotalCount, pct) + "   " + m.t("tui.plans.network.keymap")

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(m.styles.hintAccent.Render(header), 2))
	sb.WriteString("\n\n")
	sb.WriteString(indentBlock(tableBlock, 2))

	if footer := m.planNetworkDepsFooter(show.Dependencies); footer != "" {
		sb.WriteString("\n\n")
		sb.WriteString(indentBlock(m.styles.muted.Render(footer), 2))
	}
	if nextClaimableID != 0 {
		sb.WriteString("\n  ")
		sb.WriteString(m.styles.hintAccent.Render(fmt.Sprintf(m.t("tui.plans.network.next_claimable_fmt"), nextClaimableID)))
	}
	return sb.String()
}

// planNetworkTableLayout records the column widths of the bordered
// task table. Title is flex (consumes whatever remains after fixed
// columns); Bucket / Deps are fixed widths chosen so the most
// common values (`backlog`/`dev`/`review` and `#NNN  #MMM`) fit
// without truncation in standard terminals.
type planNetworkTableLayout struct {
	Title  int
	Bucket int
	Deps   int
}

// Total returns the table's interior width (excluding the right
// border character). Narrow terminals can collapse the Deps column
// to zero width, in which case only one inner separator survives;
// the wave-header row uses this to span the full table.
func (l planNetworkTableLayout) Total() int {
	innerSeps := 2
	if l.Deps <= 0 {
		innerSeps = 1
	}
	return l.Title + l.Bucket + l.Deps + innerSeps
}

// planNetworkBuildTable allocates column widths given the available
// width budget AND the measured longest row content. Title hugs the
// longest task/wave text plus a small right padding so Bucket /
// Deps sit close to the title column instead of floating at the
// terminal's right edge. The budget caps Title so the table never
// overflows; the minimum keeps short titles readable.
func planNetworkBuildTable(budget int, measuredTitle int) planNetworkTableLayout {
	bucket := 10
	deps := 14
	minTitle := 24
	const padding = 4
	if budget < minTitle+bucket+deps+3 {
		// Narrow terminal — shrink fixed cols first, let Title own
		// whatever remains.
		if budget < minTitle+bucket+3 {
			bucket = budget - minTitle - 3
			if bucket < 6 {
				bucket = 6
			}
			deps = 0
		} else {
			deps = budget - minTitle - bucket - 3
			if deps < 6 {
				deps = 6
			}
		}
		title := budget - bucket - deps - 2
		if title < minTitle {
			title = minTitle
		}
		return planNetworkTableLayout{Title: title, Bucket: bucket, Deps: deps}
	}
	title := measuredTitle + padding
	if title < minTitle {
		title = minTitle
	}
	if maxTitle := budget - bucket - deps - 2; title > maxTitle {
		title = maxTitle
	}
	return planNetworkTableLayout{Title: title, Bucket: bucket, Deps: deps}
}

// planNetworkMeasureTitle walks every row and returns the largest
// Title-cell content width (rail + glyph + state badge + #id +
// title text + @assignee). Wave header text influences the result
// indirectly by demanding `title >= waveText - bucket - deps - 2`
// so the wave header doesn't truncate under the chosen layout.
func (m Model) planNetworkMeasureTitle(rows []planNetworkRow, bucketW, depsW int) int {
	const innerSeparators = 2
	maxW := 0
	for _, r := range rows {
		switch r.Kind {
		case planRowTaskCard:
			railW := lipgloss.Width(r.Rail)
			glyphW := 2 // glyph + space
			badge, _ := m.planNetworkRowStateBadge(r)
			badgeW := 0
			if badge != "" {
				badgeW = lipgloss.Width(badge) + 1
			}
			titleText := fmt.Sprintf("#%d %s", r.Task.TaskID, r.Task.Title)
			if r.Task.AssignedTo != "" {
				titleText += " @" + truncateAgentHandle(r.Task.AssignedTo, 18)
			}
			w := railW + glyphW + badgeW + lipgloss.Width(titleText)
			if w > maxW {
				maxW = w
			}
		case planRowWaveHeader:
			glyph := "▼"
			if r.Collapsed {
				glyph = "▶"
			}
			text := fmt.Sprintf(m.t("tui.plans.network.wave_glyph_header_fmt"), glyph, r.WavePos, strings.ToUpper(r.WaveName), r.WaveDone, r.WaveTotal)
			if r.WaveActive {
				text += " " + m.t("tui.plans.network.active_tag")
			}
			w := lipgloss.Width(text) - bucketW - depsW - innerSeparators
			if w > maxW {
				maxW = w
			}
		}
	}
	return maxW
}

// planNetworkRowStateBadge returns the inline state badge for a task
// row, chosen by precedence:
//
//   done > gated > in-progress > blocked > assigned > next > ready
//
// Rationale per slot:
//   - done           — bucket is the workflow's final position.
//   - gated          — wave is not the plan's active wave.
//   - in-progress    — bucket is BETWEEN first and final (e.g. dev,
//                      review). State of fact: work is in flight.
//                      Wins over blocked so a blocker added mid-flight
//                      does not visually erase the in-flight status.
//   - blocked        — task still in the first bucket with an
//                      unfinished blocker chain.
//   - assigned       — task still in the first bucket but has a
//                      named owner. Differentiates "claimed,
//                      waiting to start" from in-flight work.
//   - next           — next-claimable hint, no owner yet.
//   - ready          — default.
//
// The badge sits just after the status glyph in the Title cell so a
// horizontal scan reveals the state without reading the title text.
// Wave header rows return ("", _). Badge text comes from the i18n
// catalog (tui.plans.network.badge.*).
func (m Model) planNetworkRowStateBadge(row planNetworkRow) (string, lipgloss.Style) {
	if row.Kind != planRowTaskCard {
		return "", lipgloss.Style{}
	}
	switch {
	case row.FinalBucket:
		return m.t("tui.plans.network.badge.done"), m.styles.success
	case row.Gated:
		return m.t("tui.plans.network.badge.gated"), m.styles.muted
	case row.InProgress:
		return m.t("tui.plans.network.badge.in_progress"), m.styles.badgeInfo
	case row.BlockerCount > 0:
		return m.t("tui.plans.network.badge.blocked"), m.styles.badgeBlocker
	case row.Task.AssignedTo != "":
		return m.t("tui.plans.network.badge.assigned"), m.styles.badgeInfo
	case row.IsNext:
		return m.t("tui.plans.network.badge.next"), m.styles.hintAccent
	default:
		return m.t("tui.plans.network.badge.ready"), m.styles.success
	}
}

// planNetworkRowDepsCell collects the dependency ids surfaced for
// this row. Intra-wave non-rail blockers prefix with `←`; cross-wave
// blockers not covered by a filament prefix with `←W`. Returns the
// empty string when there are no deps to surface.
func planNetworkRowDepsCell(row planNetworkRow, suppressedCrossIDs map[int64]bool) string {
	var parts []string
	for _, id := range row.IntraBlockers {
		parts = append(parts, fmt.Sprintf("←#%d", id))
	}
	for _, id := range row.CrossBlockers {
		if suppressedCrossIDs[id] {
			continue
		}
		parts = append(parts, fmt.Sprintf("←W#%d", id))
	}
	return strings.Join(parts, " ")
}

// padCell pads a one-line cell content to the given width. lipgloss
// width awareness keeps SGR sequences from skewing the math.
func padCell(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// padCellDashed pads a one-line cell to the given width using a
// `- - - -` pattern (alternating space + dash, starting with a
// space) styled muted. Used for task title cells so the eye can
// trace from the title text to the next column without losing
// alignment. lipgloss width awareness keeps SGR escape sequences
// out of the math.
func (m Model) padCellDashed(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	remaining := width - w
	pad := strings.Repeat(" -", (remaining+1)/2)[:remaining]
	return s + m.styles.muted.Render(pad)
}

// renderPlanNetworkRowBody renders one row of the bordered task
// table as a single content line. Title / Bucket / Deps are
// truncated to their column widths — multi-line wrapping was
// removed because variable row heights broke cursor / scroll math
// and the user accepted tighter rows over wrapped titles. The
// caller appends a transition separator below this row only when
// the next row is a different kind.
func (m Model) renderPlanNetworkRowBody(row planNetworkRow, selected bool, lanePrimary string, suppressedCrossIDs map[int64]bool, layout planNetworkTableLayout) string {
	cursor := m.cursorChevron(selected)
	if cursor == "" {
		cursor = "  "
	}
	border := m.styles.hint.Render("│")

	switch row.Kind {
	case planRowWaveHeader:
		glyph := "▼"
		if row.Collapsed {
			glyph = "▶"
		}
		text := fmt.Sprintf(m.t("tui.plans.network.wave_glyph_header_fmt"), glyph, row.WavePos, strings.ToUpper(row.WaveName), row.WaveDone, row.WaveTotal)
		if row.WaveActive {
			text += " " + m.t("tui.plans.network.active_tag")
		}
		style := m.styles.hintAccent
		if row.Collapsed && !row.WaveActive {
			style = m.styles.muted
		}
		text = truncateText(text, layout.Total())
		return cursor + lanePrimary + style.Render(padCell(text, layout.Total())) + border

	case planRowTaskCard:
		statusGlyph, statusStyle := m.planNetworkRowStatusGlyph(row)
		badgeText, badgeStyle := m.planNetworkRowStateBadge(row)
		rail := ""
		if row.Rail != "" {
			rail = m.styles.hint.Render(row.Rail)
		}
		prefix := statusStyle.Render(statusGlyph) + " "
		if badgeText != "" {
			prefix += badgeStyle.Render(badgeText) + " "
		}
		titleStyle := lipgloss.NewStyle()
		if row.IsCritical {
			titleStyle = m.styles.info
		}
		// Sub-task rows get an indent glyph + a parent reference so
		// the WBS hierarchy reads at a glance — including the
		// cross-wave case where a sub-task lands in a different wave
		// than its parent. The parent lookup is in-memory against the
		// model snapshot (taskByID), so it does not require ParentID
		// to be carried on PlanTaskRow.
		subPrefix := ""
		parentSuffix := ""
		if task, ok := m.taskByID(row.Task.TaskID); ok && task.ParentID != nil {
			subPrefix = m.styles.hint.Render("↳ ")
			parentSuffix = "  " + m.styles.hint.Render(fmt.Sprintf("↳#%d", *task.ParentID))
		}
		titleText := fmt.Sprintf("#%d %s", row.Task.TaskID, row.Task.Title)
		if row.Task.AssignedTo != "" {
			titleText += " @" + truncateAgentHandle(row.Task.AssignedTo, 18)
		}
		prefixWidth := lipgloss.Width(rail) + lipgloss.Width(prefix) + lipgloss.Width(subPrefix)
		suffixWidth := lipgloss.Width(parentSuffix)
		titleBudget := layout.Title - prefixWidth - suffixWidth
		if titleBudget < 1 {
			titleBudget = 1
		}
		titleText = truncateText(titleText, titleBudget)
		titleCell := m.padCellDashed(rail+prefix+subPrefix+titleStyle.Render(titleText)+parentSuffix, layout.Title)

		bucketCell := truncateText(row.Task.BucketKey, layout.Bucket)
		bucketStyled := m.styles.muted.Render(padCell(bucketCell, layout.Bucket))

		if layout.Deps <= 0 {
			return cursor + lanePrimary + titleCell + border + bucketStyled + border
		}
		depsCell := truncateText(planNetworkRowDepsCell(row, suppressedCrossIDs), layout.Deps)
		depsStyled := m.styles.hint.Render(padCell(depsCell, layout.Deps))

		return cursor + lanePrimary + titleCell + border + bucketStyled + border + depsStyled + border
	}
	return ""
}

// planNetworkRowHeights returns the physical line count of each
// row. With the compact layout every row is one content line, plus
// one transition separator below when the next row's kind differs
// (wave→task or task→wave). Heights drive the heights-aware scroll
// helpers without re-rendering each row.
func planNetworkRowHeights(rows []planNetworkRow) []int {
	h := make([]int, len(rows))
	for i, r := range rows {
		h[i] = 1
		if i+1 < len(rows) && rows[i+1].Kind != r.Kind {
			h[i]++ // wave↔task transition sep
		}
	}
	return h
}

// renderPlanNetworkHeaderRow draws the static column-header line at
// the top of the bordered table. The header sits above the first
// data row and is fixed chrome — it never scrolls, so the column
// labels stay visible regardless of cursor position. Narrow
// terminals may zero the Deps column; the helper drops its label
// and trailing border in that case.
func (m Model) renderPlanNetworkHeaderRow(layout planNetworkTableLayout, cursorPad, lane string) string {
	border := m.styles.hint.Render("│")
	title := m.styles.hintAccent.Render(padCell(m.t("tui.plans.network.column.title"), layout.Title))
	bucket := m.styles.hintAccent.Render(padCell(m.t("tui.plans.network.column.bucket"), layout.Bucket))
	if layout.Deps <= 0 {
		return cursorPad + lane + title + border + bucket + border
	}
	deps := m.styles.hintAccent.Render(padCell(m.t("tui.plans.network.column.deps"), layout.Deps))
	return cursorPad + lane + title + border + bucket + border + deps + border
}

// renderPlanNetworkLaneContinuation paints the lane block for
// continuation / separator sub-lines. Only lanes that are still
// "flowing" through to the next row (source through row before
// final destination) render their vertical `│`; lanes closing at
// this row or beyond render empty cells. Trailing slot is always
// a space (no arrowhead on continuation lines).
func (m Model) renderPlanNetworkLaneContinuation(rowIdx int, filaments []planNetworkFilament, laneCount int) string {
	if laneCount <= 0 {
		return ""
	}
	cells := make([]string, laneCount)
	for i := range cells {
		cells[i] = " "
	}
	for _, f := range filaments {
		if f.SrcRow <= rowIdx && rowIdx < f.EndRow() {
			cells[f.Lane] = "│"
		}
	}
	return m.styles.muted.Render(strings.Join(cells, "") + " ")
}

// renderPlanNetworkSeparator emits a row-separator line for the
// bordered table. The junction characters depend on the kinds of
// the rows above and below the separator: a wave-header row has no
// inner column separators, a task row does, so the separator must
// open / close / cross the inner verticals as the layout changes.
// Pass planRowNone for the side that has no row (top / bottom of
// the table).
func (m Model) renderPlanNetworkSeparator(above, below planNetworkRowKind, layout planNetworkTableLayout, cursorPad, lane string) string {
	aboveHasCols := above == planRowTaskCard
	belowHasCols := below == planRowTaskCard

	mid := "─"
	leftSeg := strings.Repeat(mid, layout.Title)
	bucketSeg := strings.Repeat(mid, layout.Bucket)
	depsSeg := strings.Repeat(mid, layout.Deps)

	var firstJunction, secondJunction, rightCorner string
	switch {
	case above == planRowNone:
		// Top border.
		rightCorner = "┐"
		if belowHasCols {
			firstJunction = "┬"
			secondJunction = "┬"
		} else {
			firstJunction = mid
			secondJunction = mid
		}
	case below == planRowNone:
		// Bottom border.
		rightCorner = "┘"
		if aboveHasCols {
			firstJunction = "┴"
			secondJunction = "┴"
		} else {
			firstJunction = mid
			secondJunction = mid
		}
	default:
		rightCorner = "┤"
		switch {
		case aboveHasCols && belowHasCols:
			firstJunction = "┼"
			secondJunction = "┼"
		case aboveHasCols && !belowHasCols:
			firstJunction = "┴"
			secondJunction = "┴"
		case !aboveHasCols && belowHasCols:
			firstJunction = "┬"
			secondJunction = "┬"
		default:
			firstJunction = mid
			secondJunction = mid
		}
	}
	// Narrow-terminal fallback collapses the Deps column to zero
	// width; skip its segment + junction so the right border doesn't
	// double up with the bucket separator.
	var sep string
	if layout.Deps > 0 {
		sep = leftSeg + firstJunction + bucketSeg + secondJunction + depsSeg + rightCorner
	} else {
		sep = leftSeg + firstJunction + bucketSeg + rightCorner
	}
	return cursorPad + lane + m.styles.hint.Render(sep)
}

// renderPlanNetworkLane paints the fixed-width lane block for one
// row of the outline. Each lane is 1 char wide; a final trailing
// slot carries the horizontal arm head:
//   - "►" on rows where a filament branches or terminates,
//   - "─" on source rows (arm continues into body, no arrowhead),
//   - " " on rows with no filament touch.
//
// Source rows paint ┌ at the source's lane and extend ─ to the
// right edge so the eye traces straight into the task body.
// Intermediate destinations paint ├──►; final destinations paint
// └──►. Crossings between a horizontal arm and an unrelated lane's
// pass-through vertical become ┼ junctions.
func (m Model) renderPlanNetworkLane(rowIdx int, filaments []planNetworkFilament, laneCount int) string {
	if laneCount <= 0 {
		return ""
	}
	cells := make([]string, laneCount)
	for i := range cells {
		cells[i] = " "
	}
	// Pass 1: mark every lane mid-flight at this row with a vertical
	// so the horizontal-arm pass can detect crossings.
	for _, f := range filaments {
		end := f.EndRow()
		if rowIdx > f.SrcRow && rowIdx < end {
			cells[f.Lane] = "│"
		}
	}
	// Pass 2: paint source/destination glyphs and extend the
	// horizontal arm from each touched lane to the right edge.
	hasSrc := false
	hasDst := false
	for _, f := range filaments {
		var glyph string
		switch {
		case rowIdx == f.SrcRow:
			glyph = "┌"
			hasSrc = true
		case rowIdx > f.SrcRow && rowIdx <= f.EndRow():
			isDst := false
			isLast := false
			for i, dr := range f.DstRows {
				if dr == rowIdx {
					isDst = true
					isLast = i == len(f.DstRows)-1
					break
				}
			}
			if !isDst {
				continue
			}
			hasDst = true
			if isLast {
				glyph = "└"
			} else {
				glyph = "├"
			}
		default:
			continue
		}
		cells[f.Lane] = glyph
		for k := f.Lane + 1; k < laneCount; k++ {
			switch cells[k] {
			case "│":
				cells[k] = "┼"
			case " ":
				cells[k] = "─"
			}
		}
	}
	trailing := " "
	switch {
	case hasDst:
		trailing = "►"
	case hasSrc:
		trailing = "─"
	}
	return m.styles.muted.Render(strings.Join(cells, "") + trailing)
}

// planNetworkRowStatusGlyph maps the row's semantic flags onto a
// status glyph + style. Done and gated rows are distinct (✓ / ⊘);
// every other state collapses to ○ — the parallel inline state badge
// disambiguates blocked / in-progress / next / ready in text. Sharing
// FinalBucket / Gated with planNetworkRowStateBadge keeps both surfaces
// driven by the same flags (no hardcoded bucket-key lookups).
func (m Model) planNetworkRowStatusGlyph(row planNetworkRow) (string, lipgloss.Style) {
	switch {
	case row.FinalBucket:
		return "✓", m.styles.success
	case row.Gated:
		return "⊘", m.styles.muted
	default:
		return "○", m.styles.hintAccent
	}
}

// ===========================================================================
// Cross-wave + intra-wave dep indices (shared with renderer + filaments).
// ===========================================================================

// planNetworkIntraWaveIndices returns the blocker map for dependency
// edges whose source AND destination tasks live in the same wave.
// The renderer's rail tree consumes it.
func planNetworkIntraWaveIndices(deps []domain.TaskDependency, waves []app.PlanWaveView) map[int64][]int64 {
	return planNetworkWaveScopedBlockers(deps, waves, true)
}

// planNetworkCrossWaveIndices returns the blocker map for dependency
// edges that CROSS wave boundaries. The renderer surfaces these as
// `←W` inline annotations; the filament router uses them to draw
// left-margin vertical traces.
func planNetworkCrossWaveIndices(deps []domain.TaskDependency, waves []app.PlanWaveView) map[int64][]int64 {
	return planNetworkWaveScopedBlockers(deps, waves, false)
}

// planNetworkWaveScopedBlockers shares the wave-membership walk
// between the intra- and cross-wave index helpers. sameWave=true
// keeps edges within a wave; sameWave=false keeps the ones that
// cross. Blocker lists sort ascending so renders read identically
// across refreshes.
func planNetworkWaveScopedBlockers(deps []domain.TaskDependency, waves []app.PlanWaveView, sameWave bool) map[int64][]int64 {
	if len(deps) == 0 || len(waves) == 0 {
		return nil
	}
	taskToWave := make(map[int64]int64, len(deps))
	for _, wv := range waves {
		for _, t := range wv.Tasks {
			taskToWave[t.TaskID] = wv.Wave.ID
		}
	}
	blockers := map[int64][]int64{}
	for _, d := range deps {
		srcWave, sok := taskToWave[d.DependsOnTaskID]
		dstWave, dok := taskToWave[d.TaskID]
		if !sok || !dok {
			continue
		}
		if (srcWave == dstWave) != sameWave {
			continue
		}
		blockers[d.TaskID] = append(blockers[d.TaskID], d.DependsOnTaskID)
	}
	for k := range blockers {
		sort.Slice(blockers[k], func(i, j int) bool { return blockers[k][i] < blockers[k][j] })
	}
	return blockers
}

// ===========================================================================
// Critical-path / next-claimable / deps footer / agent handle helper.
// (Same contract as before — only the call sites moved.)
// ===========================================================================

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
// under the outline: "Dependencies: #A→#B,#C  #D→#E". Tasks are
// ordered by dependent id; blockers per dependent stay sorted asc so
// the line reads the same on every refresh. Attached to Model so the
// localized prefix flows through the same catalog as the rest of the
// surface.
func (m Model) planNetworkDepsFooter(deps []domain.TaskDependency) string {
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
	sb.WriteString(m.t("tui.plans.network.deps_footer_prefix"))
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
		if next == 0 || path[next] {
			break
		}
		path[next] = true
		current = next
	}
	return path
}

// truncateAgentHandle clamps long @assigned handles to the row
// budget so a verbose model id does not consume the full title
// line. Returns the original string when it already fits. The "…"
// terminator stays an ASCII safe alternative to U+2026 so width math
// survives the lipgloss render.
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
