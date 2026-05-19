package tui

import (
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// stripStyle drops lipgloss SGR sequences from rendered output so
// tests can assert against the underlying glyph stream.
func stripStyle(s string) string { return stripANSI(s) }

// TestPlanNetworkCriticalPathPicksLongestChain proves the helper
// identifies the deepest blocker chain (A → B → C) and excludes
// isolated tasks (D) from the highlight set. Ties on depth break by
// lowest id for stable rendering across refreshes.
func TestPlanNetworkCriticalPathPicksLongestChain(t *testing.T) {
	tasks := []domain.PlanTaskRow{
		{TaskID: 1, Title: "A"},
		{TaskID: 2, Title: "B"},
		{TaskID: 3, Title: "C"},
		{TaskID: 4, Title: "D"},
	}
	deps := []domain.TaskDependency{
		{TaskID: 2, DependsOnTaskID: 1}, // B depends on A
		{TaskID: 3, DependsOnTaskID: 2}, // C depends on B
	}

	path := planNetworkCriticalPath(deps, tasks)
	if len(path) != 3 {
		t.Fatalf("path len = %d, want 3 (A→B→C): %+v", len(path), path)
	}
	for _, id := range []int64{1, 2, 3} {
		if !path[id] {
			t.Fatalf("task #%d missing from critical path: %+v", id, path)
		}
	}
	if path[4] {
		t.Fatal("isolated task #4 should not be on critical path")
	}
}

// TestPlanNetworkCriticalPathNilWhenNoDeps confirms zero-dep plans
// return a nil set so the renderer skips the accent border entirely.
func TestPlanNetworkCriticalPathNilWhenNoDeps(t *testing.T) {
	tasks := []domain.PlanTaskRow{{TaskID: 1}, {TaskID: 2}}
	if path := planNetworkCriticalPath(nil, tasks); path != nil {
		t.Fatalf("path = %+v, want nil on zero-dep plan", path)
	}
}

// TestPlanNetworkCriticalPathSurvivesCycle proves the cycle guard
// prevents infinite recursion when an accidentally-circular
// dependency slips through.
func TestPlanNetworkCriticalPathSurvivesCycle(t *testing.T) {
	tasks := []domain.PlanTaskRow{{TaskID: 1}, {TaskID: 2}}
	deps := []domain.TaskDependency{
		{TaskID: 1, DependsOnTaskID: 2},
		{TaskID: 2, DependsOnTaskID: 1},
	}
	_ = planNetworkCriticalPath(deps, tasks)
}

// TestPlanNetworkDepsFooterFormatsLine confirms the footer reads
// "Dependencies: #A→#B,#C  #D→#E" with stable ordering across
// refreshes.
func TestPlanNetworkDepsFooterFormatsLine(t *testing.T) {
	deps := []domain.TaskDependency{
		{TaskID: 3, DependsOnTaskID: 1},
		{TaskID: 3, DependsOnTaskID: 2},
		{TaskID: 4, DependsOnTaskID: 3},
	}
	got := planNetworkDepsFooter(deps)
	want := "Dependencies: #3→#1,#2  #4→#3"
	if got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

// TestPlanNetworkDepsFooterEmpty confirms zero-dep plans return an
// empty string so the renderer can skip writing the footer line.
func TestPlanNetworkDepsFooterEmpty(t *testing.T) {
	if got := planNetworkDepsFooter(nil); got != "" {
		t.Fatalf("footer = %q, want empty", got)
	}
}

// TestPlanNetworkBuildRailsDFSPreOrder proves the DFS pre-order rail
// builder:
//   - tasks render in input order (NOT readiness-sorted)
//   - intra-wave parent → child rails render as ├─/└─ INSIDE the row
//     body, with │ continuations on intervening sibling rows.
func TestPlanNetworkBuildRailsDFSPreOrder(t *testing.T) {
	wv := app.PlanWaveView{
		Tasks: []domain.PlanTaskRow{
			{TaskID: 1, Title: "root-a"},
			{TaskID: 2, Title: "child-of-1"},
			{TaskID: 3, Title: "child-of-1"},
			{TaskID: 4, Title: "grandchild-of-2"},
			{TaskID: 5, Title: "root-b"},
		},
	}
	intraBlockers := map[int64][]int64{
		2: {1},
		3: {1},
		4: {2},
	}
	layout := planNetworkBuildRails(wv, intraBlockers)
	if len(layout.OrderedIdx) != 5 {
		t.Fatalf("ordered = %d, want 5", len(layout.OrderedIdx))
	}
	// DFS pre-order: 1 → 2 → 4 → 3 → 5. The branch under #1 fully
	// expands before #5 (the sibling root) appears.
	want := []struct {
		taskID   int64
		parentID int64
		railHas  string
	}{
		{1, 0, ""},
		{2, 1, "├─"},
		{4, 2, "└─"},
		{3, 1, "└─"},
		{5, 0, ""},
	}
	for pos, w := range want {
		gotID := wv.Tasks[layout.OrderedIdx[pos]].TaskID
		gotParent := layout.ParentByPos[pos]
		if gotID != w.taskID || gotParent != w.parentID {
			t.Fatalf("layout[%d] = (task=%d parent=%d), want (task=%d parent=%d)",
				pos, gotID, gotParent, w.taskID, w.parentID)
		}
		if w.railHas != "" && !strings.Contains(layout.Rails[pos], w.railHas) {
			t.Fatalf("rail[#%d] = %q, want contains %q", gotID, layout.Rails[pos], w.railHas)
		}
	}
}

// TestPlanNetworkBuildRailsRejectsTopologicalReorder pins the design
// decision: even when a later root would be "more ready" than an
// earlier task with children, the rail builder keeps input order.
// The previously-attempted topological reorder is not allowed to
// return.
func TestPlanNetworkBuildRailsRejectsTopologicalReorder(t *testing.T) {
	wv := app.PlanWaveView{
		Tasks: []domain.PlanTaskRow{
			{TaskID: 10, Title: "blocked-by-nothing"},
			{TaskID: 20, Title: "blocks-30"},
			{TaskID: 30, Title: "blocked-by-20"},
			{TaskID: 40, Title: "blocked-by-nothing-too"},
		},
	}
	intraBlockers := map[int64][]int64{30: {20}}
	layout := planNetworkBuildRails(wv, intraBlockers)
	wantOrder := []int64{10, 20, 30, 40}
	for pos, want := range wantOrder {
		gotID := wv.Tasks[layout.OrderedIdx[pos]].TaskID
		if gotID != want {
			t.Fatalf("rails ordered[%d] = #%d, want #%d (input order, NOT readiness)", pos, gotID, want)
		}
	}
}

// TestPlanNetworkBuildFilamentsGreedyLanes proves overlapping
// cross-wave sources allocate distinct lanes. Two sources whose
// destination ranges overlap must land in lanes 0 and 1; a third
// source whose range starts after the first lane has freed
// collapses back to lane 0.
func TestPlanNetworkBuildFilamentsGreedyLanes(t *testing.T) {
	rows := []planNetworkRow{
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 1}}, // 0
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 2}}, // 1
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 3}}, // 2 — dst of #1
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 4}}, // 3 — dst of #2
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 5}}, // 4 — src
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 6}}, // 5 — dst of #5
	}
	cross := map[int64][]int64{
		3: {1},
		4: {2},
		6: {5},
	}
	filaments, laneCount := planNetworkBuildFilaments(rows, cross)
	if laneCount != 2 {
		t.Fatalf("laneCount = %d, want 2", laneCount)
	}
	bySrc := map[int]planNetworkFilament{}
	for _, f := range filaments {
		bySrc[f.SrcRow] = f
	}
	if bySrc[0].Lane != 0 {
		t.Fatalf("filament from #1 (row 0) lane = %d, want 0", bySrc[0].Lane)
	}
	if bySrc[1].Lane != 1 {
		t.Fatalf("filament from #2 (row 1) lane = %d, want 1 (overlaps lane 0)", bySrc[1].Lane)
	}
	if bySrc[4].Lane != 0 {
		t.Fatalf("filament from #5 (row 4) lane = %d, want 0 (lane 0 freed after row 2)", bySrc[4].Lane)
	}
}

// TestPlanNetworkBuildFilamentsHubReusesLane proves ONE source with
// N dependents collapses to ONE lane, not N. A hub task #1 blocking
// #2, #3, #4 emits a single filament with three destination rows;
// the lane count is 1.
func TestPlanNetworkBuildFilamentsHubReusesLane(t *testing.T) {
	rows := []planNetworkRow{
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 1}}, // 0 — hub src
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 2}}, // 1 — dst
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 3}}, // 2 — dst
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 4}}, // 3 — dst
	}
	cross := map[int64][]int64{
		2: {1},
		3: {1},
		4: {2, 1}, // #4 depends on hub AND on #2
	}
	filaments, laneCount := planNetworkBuildFilaments(rows, cross)
	if laneCount != 2 {
		t.Fatalf("laneCount = %d, want 2 (hub reuse + #2's single edge to #4)", laneCount)
	}
	var hub planNetworkFilament
	found := false
	for _, f := range filaments {
		if f.SrcRow == 0 {
			hub = f
			found = true
		}
	}
	if !found {
		t.Fatalf("hub filament missing from %+v", filaments)
	}
	if len(hub.DstRows) != 3 || hub.DstRows[0] != 1 || hub.DstRows[1] != 2 || hub.DstRows[2] != 3 {
		t.Fatalf("hub DstRows = %v, want [1 2 3] (lane reused for all 3 dependents)", hub.DstRows)
	}
}

// TestPlanNetworkBuildFilamentsDropsCollapsedSource proves cross-wave
// edges whose source row is missing (e.g. its wave is collapsed) are
// dropped from the filament list. The destination still surfaces the
// blocker as `←W #N` text via the regular annotation path.
func TestPlanNetworkBuildFilamentsDropsCollapsedSource(t *testing.T) {
	rows := []planNetworkRow{
		{Kind: planRowWaveHeader, WaveID: 1},
		// no #1 task card — wave 1 is collapsed
		{Kind: planRowWaveHeader, WaveID: 2},
		{Kind: planRowTaskCard, Task: domain.PlanTaskRow{TaskID: 2}},
	}
	cross := map[int64][]int64{2: {1}}
	filaments, laneCount := planNetworkBuildFilaments(rows, cross)
	if laneCount != 0 || len(filaments) != 0 {
		t.Fatalf("filaments = %v laneCount = %d, want empty (source row missing)", filaments, laneCount)
	}
}

// TestRenderPlanNetworkLaneGlyphs proves the lane renderer paints
// ┌─ at source, │ on pass-through rows, ├─► at intermediate dsts,
// └─► at final dst, and pads other lanes / trailing slots with
// spaces.
func TestRenderPlanNetworkLaneGlyphs(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	filaments := []planNetworkFilament{{SrcRow: 0, DstRows: []int{2, 3}, Lane: 0}}
	laneCount := 1

	src := stripStyle(m.renderPlanNetworkLane(0, filaments, laneCount))
	if src != "┌─" {
		t.Fatalf("source lane = %q, want %q (arm extends to body)", src, "┌─")
	}
	mid := stripStyle(m.renderPlanNetworkLane(1, filaments, laneCount))
	if mid != "│ " {
		t.Fatalf("pass-through lane = %q, want %q", mid, "│ ")
	}
	tee := stripStyle(m.renderPlanNetworkLane(2, filaments, laneCount))
	if tee != "├►" {
		t.Fatalf("intermediate dst lane = %q, want %q", tee, "├►")
	}
	dst := stripStyle(m.renderPlanNetworkLane(3, filaments, laneCount))
	if dst != "└►" {
		t.Fatalf("final dst lane = %q, want %q", dst, "└►")
	}
	empty := stripStyle(m.renderPlanNetworkLane(5, filaments, laneCount))
	if empty != "  " {
		t.Fatalf("empty lane row = %q, want %q (2 spaces)", empty, "  ")
	}
	zero := m.renderPlanNetworkLane(0, nil, 0)
	if zero != "" {
		t.Fatalf("zero lanes = %q, want empty string", zero)
	}
}

// TestRenderPlanNetworkLaneHorizontalArmCrossesPassThrough proves
// the horizontal arm from a source/dst lane paints `┼` over an
// unrelated lane's pass-through vertical, and reaches the trailing
// slot with `─` (source) or `►` (dst).
func TestRenderPlanNetworkLaneHorizontalArmCrossesPassThrough(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	filaments := []planNetworkFilament{
		{SrcRow: 0, DstRows: []int{6}, Lane: 0}, // long lane 0
		{SrcRow: 2, DstRows: []int{4}, Lane: 1}, // shorter lane 1
	}
	laneCount := 2

	// Row 2 is source of filament 1 (lane 1); filament 0 (lane 0) is
	// mid-flight here. Source arm at lane 1 has no inner cells to
	// the right, so trailing carries `─`. Lane 0 stays `│`.
	srcRow := stripStyle(m.renderPlanNetworkLane(2, filaments, laneCount))
	if srcRow != "│┌─" {
		t.Fatalf("row 2 = %q, want %q (pass-through │ + source ┌ + arm)", srcRow, "│┌─")
	}

	// Row 4 is dst of filament 1 (lane 1); filament 0 still mid-flight.
	dstRow := stripStyle(m.renderPlanNetworkLane(4, filaments, laneCount))
	if dstRow != "│└►" {
		t.Fatalf("row 4 = %q, want %q (pass-through │ + └ + ►)", dstRow, "│└►")
	}

	// Row 6 is dst of filament 0 (lane 0). Arm crosses lane 1 — but
	// at row 6 lane 1 is finished (ended at row 4), so col 1 has been
	// freed. Arm paints `─` not `┼`.
	dstAcross := stripStyle(m.renderPlanNetworkLane(6, filaments, laneCount))
	if dstAcross != "└─►" {
		t.Fatalf("row 6 = %q, want %q (└ + arm + ►)", dstAcross, "└─►")
	}
}

// TestRenderPlanNetworkLaneArmCrossesActivePassThrough proves a
// horizontal arm crossing an active pass-through `│` from a
// different (longer) lane renders the junction glyph `┼`.
func TestRenderPlanNetworkLaneArmCrossesActivePassThrough(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	filaments := []planNetworkFilament{
		{SrcRow: 0, DstRows: []int{8}, Lane: 1}, // very long lane 1
		{SrcRow: 2, DstRows: []int{4}, Lane: 0}, // shorter lane 0
	}
	laneCount := 2

	// Row 2 = source of filament 1 at lane 0. Arm at lane 0+1=1
	// crosses filament 0's pass-through `│` — should render `┼`.
	out := stripStyle(m.renderPlanNetworkLane(2, filaments, laneCount))
	if out != "┌┼─" {
		t.Fatalf("row 2 = %q, want %q (┌ + ┼ crossing + arm)", out, "┌┼─")
	}

	// Row 4 = dst of filament 1 at lane 0. Same crossing pattern.
	dst := stripStyle(m.renderPlanNetworkLane(4, filaments, laneCount))
	if dst != "└┼►" {
		t.Fatalf("row 4 = %q, want %q (└ + ┼ crossing + ►)", dst, "└┼►")
	}
}

// TestRenderPlanNetworkWaveHeaderLaneAlignment proves wave header
// rows receive the same lane prefix as task rows. A filament passing
// through a wave header must paint │ at the header row at the same
// column as on intervening task rows — no wave nests under another.
func TestRenderPlanNetworkWaveHeaderLaneAlignment(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	filaments := []planNetworkFilament{{SrcRow: 0, DstRows: []int{4}, Lane: 0}}
	headerRow := planNetworkRow{
		Kind: planRowWaveHeader, WavePos: 2, WaveName: "phase",
		WaveDone: 0, WaveTotal: 3,
	}
	taskRow := planNetworkRow{
		Kind: planRowTaskCard,
		Task: domain.PlanTaskRow{TaskID: 99, Title: "t"},
	}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}

	headerPrimary := m.renderPlanNetworkLane(2, filaments, 1)
	taskPrimary := m.renderPlanNetworkLane(3, filaments, 1)

	headerOut := stripStyle(m.renderPlanNetworkRowBody(headerRow, false, headerPrimary, nil, layout))
	taskOut := stripStyle(m.renderPlanNetworkRowBody(taskRow, false, taskPrimary, nil, layout))

	if !strings.HasPrefix(headerOut, "  │ ") {
		t.Fatalf("wave header row = %q, want it to start with cursor pad + lane │ (no nesting)", headerOut)
	}
	if !strings.HasPrefix(taskOut, "  │ ") {
		t.Fatalf("task row = %q, want same lane prefix as wave header", taskOut)
	}
}

// TestRenderPlanNetworkSeparatorJunctions proves the separator
// builder emits the correct junction characters at the four row
// transitions plus the top / bottom borders.
func TestRenderPlanNetworkSeparatorJunctions(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 4, Bucket: 4, Deps: 4}
	const noRow = -1

	cases := []struct {
		name        string
		above, below int
		wantSuffix  string
	}{
		{"top→wave", noRow, int(planRowWaveHeader), "────────────┐"},
		{"top→task", noRow, int(planRowTaskCard), "────┬────┬────┐"},
		{"wave→task", int(planRowWaveHeader), int(planRowTaskCard), "────┬────┬────┤"},
		{"task→wave", int(planRowTaskCard), int(planRowWaveHeader), "────┴────┴────┤"},
		{"task→task", int(planRowTaskCard), int(planRowTaskCard), "────┼────┼────┤"},
		{"task→bottom", int(planRowTaskCard), noRow, "────┴────┴────┘"},
		{"wave→bottom", int(planRowWaveHeader), noRow, "────────────┘"},
	}
	for _, c := range cases {
		got := stripStyle(m.renderPlanNetworkSeparator(c.above, c.below, layout, "", ""))
		if !strings.HasSuffix(got, c.wantSuffix) {
			t.Fatalf("%s sep = %q, want suffix %q", c.name, got, c.wantSuffix)
		}
	}
}

// TestRenderPlanNetworkTaskRowHasThreeCells proves a task row
// renders with exactly two inner `│` separators (Title │ Bucket │
// Deps │) and ends with a right border `│`.
func TestRenderPlanNetworkTaskRowHasThreeCells(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}
	row := planNetworkRow{
		Kind: planRowTaskCard,
		Task: domain.PlanTaskRow{TaskID: 99, Title: "hello", BucketKey: "dev"},
	}
	plain := stripStyle(m.renderPlanNetworkRowBody(row, false, "", nil, layout))
	if strings.Count(plain, "│") != 3 {
		t.Fatalf("task row = %q, want exactly 3 │ separators (2 inner + 1 right)", plain)
	}
	if !strings.HasSuffix(plain, "│") {
		t.Fatalf("task row missing right border: %q", plain)
	}
}

// TestRenderPlanNetworkWaveHeaderFullWidth proves a wave header
// row carries NO inner `│` separators — its single cell spans the
// full table interior — and still closes with the right border.
func TestRenderPlanNetworkWaveHeaderFullWidth(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}
	row := planNetworkRow{
		Kind: planRowWaveHeader, WavePos: 1, WaveName: "phase",
		WaveDone: 1, WaveTotal: 3,
	}
	plain := stripStyle(m.renderPlanNetworkRowBody(row, false, "", nil, layout))
	if strings.Count(plain, "│") != 1 {
		t.Fatalf("wave header = %q, want exactly 1 │ (right border only, no inner separators)", plain)
	}
	if !strings.HasSuffix(plain, "│") {
		t.Fatalf("wave header missing right border: %q", plain)
	}
}

// TestPlanRowStateBadgePrecedence pins the order in which the
// state badge selector resolves (done > gated > blocked >
// in-progress > next > ready).
func TestPlanRowStateBadgePrecedence(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	cases := []struct {
		name string
		row  planNetworkRow
		want string
	}{
		{"done beats all", planNetworkRow{Kind: planRowTaskCard, FinalBucket: true, Gated: true, BlockerCount: 3, IsNext: true}, "done"},
		{"gated beats blocked", planNetworkRow{Kind: planRowTaskCard, Gated: true, BlockerCount: 2}, "gated"},
		{"blocked beats in-progress", planNetworkRow{Kind: planRowTaskCard, BlockerCount: 1, Task: domain.PlanTaskRow{AssignedTo: "x"}}, "blocked"},
		{"in-progress beats next", planNetworkRow{Kind: planRowTaskCard, Task: domain.PlanTaskRow{AssignedTo: "x"}, IsNext: true}, "in-progress"},
		{"next beats ready", planNetworkRow{Kind: planRowTaskCard, IsNext: true}, "▶next"},
		{"ready default", planNetworkRow{Kind: planRowTaskCard}, "ready"},
	}
	for _, c := range cases {
		got, _ := m.planRowStateBadge(c.row)
		if got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestRenderPlanNetworkTaskRowShowsBucketCell proves the Bucket
// column carries the task's raw bucket key (no hardcoded mapping)
// and skips the value when the task is in the workflow's final
// bucket (done already implied by the state badge + glyph).
func TestRenderPlanNetworkTaskRowShowsBucketCell(t *testing.T) {
	m := Model{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}
	row := planNetworkRow{
		Kind: planRowTaskCard,
		Task: domain.PlanTaskRow{TaskID: 1, Title: "x", BucketKey: "review"},
	}
	plain := stripStyle(m.renderPlanNetworkRowBody(row, false, "", nil, layout))
	if !strings.Contains(plain, "review") {
		t.Fatalf("expected bucket cell to contain %q, got %q", "review", plain)
	}

	doneRow := planNetworkRow{
		Kind:        planRowTaskCard,
		FinalBucket: true,
		Task:        domain.PlanTaskRow{TaskID: 1, Title: "x", BucketKey: "done"},
	}
	donePlain := stripStyle(m.renderPlanNetworkRowBody(doneRow, false, "", nil, layout))
	if !strings.Contains(donePlain, "done") {
		t.Fatalf("done row must still show bucket value, got %q", donePlain)
	}
}

// TestPlanNetworkExcludeIDStripsRailParent proves the helper drops
// the rail-parent id from the inline-annotation list so the same
// blocker doesn't surface twice (rail glyph + "← #N").
func TestPlanNetworkExcludeIDStripsRailParent(t *testing.T) {
	got := planNetworkExcludeID([]int64{1, 2, 3}, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("excludeID = %v, want [1 3]", got)
	}
	if untouched := planNetworkExcludeID([]int64{1, 2, 3}, 0); len(untouched) != 3 {
		t.Fatalf("excludeID with zero parent should return slice unchanged, got %v", untouched)
	}
}

// TestPlanNetworkCrossWaveIndicesFiltersIntraEdges confirms the helper
// returns only edges whose source and destination tasks live in
// different waves. Intra-wave edges are excluded because the rail
// tree already surfaces them.
func TestPlanNetworkCrossWaveIndicesFiltersIntraEdges(t *testing.T) {
	waves := []app.PlanWaveView{
		{Wave: domain.PlanWave{ID: 100}, Tasks: []domain.PlanTaskRow{{TaskID: 10}, {TaskID: 11}}},
		{Wave: domain.PlanWave{ID: 200}, Tasks: []domain.PlanTaskRow{{TaskID: 20}}},
	}
	deps := []domain.TaskDependency{
		{TaskID: 11, DependsOnTaskID: 10}, // intra W1 — excluded
		{TaskID: 20, DependsOnTaskID: 10}, // cross W1→W2 — kept
	}
	blockers, dependents := planNetworkCrossWaveIndices(deps, waves)
	if len(blockers[20]) != 1 || blockers[20][0] != 10 {
		t.Fatalf("blockers[20] = %v, want [10]", blockers[20])
	}
	if _, ok := blockers[11]; ok {
		t.Fatalf("intra-wave edge leaked into cross-wave blockers: %+v", blockers)
	}
	if len(dependents[10]) != 1 || dependents[10][0] != 20 {
		t.Fatalf("dependents[10] = %v, want [20]", dependents[10])
	}
}
