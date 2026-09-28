package plannetwork

import (
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	networkprojection "omakiten/internal/plannetwork"
)

// stripStyle drops lipgloss SGR sequences from rendered output so
// tests can assert against the underlying glyph stream.
func stripStyle(s string) string { return stripANSI(s) }

// TestPlanNetworkDepsFooterFormatsLine confirms the footer reads
// "Dependencies: #A→#B,#C  #D→#E" with stable ordering across
// refreshes. The prefix comes from the i18n catalog so the test
// uses the bundled en baseline (no Repositories.Catalog set on the
// zero-value Screen).
func TestPlanNetworkDepsFooterFormatsLine(t *testing.T) {
	m := Screen{}
	deps := []domain.TaskDependency{
		{TaskID: 3, DependsOnTaskID: 1},
		{TaskID: 3, DependsOnTaskID: 2},
		{TaskID: 4, DependsOnTaskID: 3},
	}
	got := m.planNetworkDepsFooter(deps)
	want := "Dependencies: #3→#1,#2  #4→#3"
	if got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

// TestPlanNetworkDepsFooterEmpty confirms zero-dep plans return an
// empty string so the renderer can skip writing the footer line.
func TestPlanNetworkDepsFooterEmpty(t *testing.T) {
	m := Screen{}
	if got := m.planNetworkDepsFooter(nil); got != "" {
		t.Fatalf("footer = %q, want empty", got)
	}
}

// TestPlanNetworkBuildFilamentsDropsCollapsedSource proves cross-wave
// edges whose source row is missing (e.g. its wave is collapsed) are
// dropped from the filament list. The destination still surfaces the
// blocker as `←W #N` text via the regular annotation path.
func TestPlanNetworkBuildFilamentsDropsCollapsedSource(t *testing.T) {
	build := networkprojection.Build(networkprojection.Input{
		Show: domain.PlanShow{
			Waves: []domain.PlanWaveView{
				{Wave: domain.PlanWave{ID: 1, Position: 1}, Tasks: []domain.PlanTaskRow{{TaskID: 1}}},
				{Wave: domain.PlanWave{ID: 2, Position: 2}, Tasks: []domain.PlanTaskRow{{TaskID: 2}}},
			},
			Dependencies: []domain.TaskDependency{{TaskID: 2, DependsOnTaskID: 1}},
		},
		Collapsed: map[int64]bool{1: true},
	})
	filaments, laneCount := build.Filaments, build.LaneCount
	if laneCount != 0 || len(filaments) != 0 {
		t.Fatalf("filaments = %v laneCount = %d, want empty (source row missing)", filaments, laneCount)
	}
}

// TestRenderPlanNetworkLaneGlyphs proves the lane renderer paints
// ┌─ at source, │ on pass-through rows, ├─► at intermediate dsts,
// └─► at final dst, and pads other lanes / trailing slots with
// spaces.
func TestRenderPlanNetworkLaneGlyphs(t *testing.T) {
	m := Screen{styles: newStyles(config.Theme{})}
	filaments := []networkprojection.Filament{{SrcRow: 0, DstRows: []int{2, 3}, Lane: 0}}
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
	m := Screen{styles: newStyles(config.Theme{})}
	filaments := []networkprojection.Filament{
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
	m := Screen{styles: newStyles(config.Theme{})}
	filaments := []networkprojection.Filament{
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

// TestRenderPlanNetworkLaneSourceAndDestinationSameRow proves a row
// that is BOTH a source for one filament AND a destination for
// another paints both glyphs at their respective lanes and resolves
// the trailing slot to `►` (destination wins over source — the
// arrowhead is the more informative marker when both apply).
func TestRenderPlanNetworkLaneSourceAndDestinationSameRow(t *testing.T) {
	m := Screen{styles: newStyles(config.Theme{})}
	filaments := []networkprojection.Filament{
		{SrcRow: 0, DstRows: []int{2}, Lane: 0}, // arrives at row 2
		{SrcRow: 2, DstRows: []int{4}, Lane: 1}, // departs at row 2
	}
	laneCount := 2

	out := stripStyle(m.renderPlanNetworkLane(2, filaments, laneCount))
	if out != "└┌►" {
		t.Fatalf("row 2 = %q, want %q (└ at lane 0 + ┌ at lane 1 + ► trailing)", out, "└┌►")
	}
}

// TestRenderPlanNetworkWaveHeaderLaneAlignment proves wave header
// rows receive the same lane prefix as task rows. A filament passing
// through a wave header must paint │ at the header row at the same
// column as on intervening task rows — no wave nests under another.
func TestRenderPlanNetworkWaveHeaderLaneAlignment(t *testing.T) {
	m := Screen{styles: newStyles(config.Theme{})}
	filaments := []networkprojection.Filament{{SrcRow: 0, DstRows: []int{4}, Lane: 0}}
	headerRow := networkprojection.Row{
		Kind: planRowWaveHeader, WavePos: 2, WaveName: "phase",
		WaveDone: 0, WaveTotal: 3,
	}
	taskRow := networkprojection.Row{
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
	m := Screen{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 4, Bucket: 4, Deps: 4}

	cases := []struct {
		name         string
		above, below networkprojection.RowKind
		wantSuffix   string
	}{
		{"top→wave", planRowNone, planRowWaveHeader, "────────────┐"},
		{"top→task", planRowNone, planRowTaskCard, "────┬────┬────┐"},
		{"wave→task", planRowWaveHeader, planRowTaskCard, "────┬────┬────┤"},
		{"task→wave", planRowTaskCard, planRowWaveHeader, "────┴────┴────┤"},
		{"task→task", planRowTaskCard, planRowTaskCard, "────┼────┼────┤"},
		{"task→bottom", planRowTaskCard, planRowNone, "────┴────┴────┘"},
		{"wave→bottom", planRowWaveHeader, planRowNone, "────────────┘"},
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
	m := Screen{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}
	row := networkprojection.Row{
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
	m := Screen{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}
	row := networkprojection.Row{
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

// TestPlanNetworkRowStateBadgePrecedence pins the order in which the
// state badge selector resolves:
//
//	done > gated > in-progress > blocked > assigned > next > ready
//
// The split between in-progress / assigned exists because claim only
// stamps assigned_to nowadays — it never moves the bucket. An
// "assigned" task may still sit in backlog waiting for its preset
// guards (e.g. omakase's self-branch comment); an "in-progress" task
// already left the first bucket and is in the working pipeline.
func TestPlanNetworkRowStateBadgePrecedence(t *testing.T) {
	m := Screen{styles: newStyles(config.Theme{})}
	cases := []struct {
		name string
		row  networkprojection.Row
		want string
	}{
		{"done", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusDone}, "done"},
		{"gated", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusGated}, "gated"},
		{"in-progress", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusInProgress}, "in-progress"},
		{"blocked", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusBlocked}, "blocked"},
		{"assigned", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusAssigned}, "assigned"},
		{"next", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusNext}, "▶next"},
		{"ready", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusReady}, "ready"},
	}
	for _, c := range cases {
		got, _ := m.planNetworkRowStateBadge(c.row)
		if got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestPlanNetworkRowStatusGlyphSharesFlags pins the status glyph to
// the same FinalBucket / Gated flags that drive the state badge —
// no hardcoded bucket-key lookups (the previous "dev → ●" path was
// dropped). Every non-done / non-gated row collapses to ○ and the
// inline badge disambiguates downstream.
func TestPlanNetworkRowStatusGlyphSharesFlags(t *testing.T) {
	m := Screen{styles: newStyles(config.Theme{})}
	cases := []struct {
		name string
		row  networkprojection.Row
		want string
	}{
		{"done", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusDone}, "✓"},
		{"gated", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusGated}, "⊘"},
		{"in-progress falls through to ○", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusInProgress}, "○"},
		{"blocked falls through to ○", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusBlocked}, "○"},
		{"ready default", networkprojection.Row{Kind: planRowTaskCard, Status: networkprojection.StatusReady}, "○"},
	}
	for _, c := range cases {
		got, _ := m.planNetworkRowStatusGlyph(c.row)
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
	m := Screen{styles: newStyles(config.Theme{})}
	layout := planNetworkTableLayout{Title: 30, Bucket: 8, Deps: 10}
	row := networkprojection.Row{
		Kind: planRowTaskCard,
		Task: domain.PlanTaskRow{TaskID: 1, Title: "x", BucketKey: "review"},
	}
	plain := stripStyle(m.renderPlanNetworkRowBody(row, false, "", nil, layout))
	if !strings.Contains(plain, "review") {
		t.Fatalf("expected bucket cell to contain %q, got %q", "review", plain)
	}

	doneRow := networkprojection.Row{
		Kind:        planRowTaskCard,
		FinalBucket: true,
		Status:      networkprojection.StatusDone,
		Task:        domain.PlanTaskRow{TaskID: 1, Title: "x", BucketKey: "done"},
	}
	donePlain := stripStyle(m.renderPlanNetworkRowBody(doneRow, false, "", nil, layout))
	if !strings.Contains(donePlain, "done") {
		t.Fatalf("done row must still show bucket value, got %q", donePlain)
	}
}
