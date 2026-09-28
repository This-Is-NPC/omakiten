package insights

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/screens/screentest"
)

// testScreen builds a screen with the Insights port wired (to a stub — the
// render path reads the cached reading, never the service) and the supplied
// payload pre-loaded, mirroring the state the reload path leaves before View
// runs. The two-bucket workflow lets the stuck / WIP insights resolve
// bucket-id → name.
func testScreen(loaded bool, ins domain.Insights) Screen {
	screen := New().Bind(testDeps(true))
	if loaded {
		screen = screen.Apply(Payload{Insights: ins, BucketNames: []domain.Bucket{{ID: 2, Name: "Development"}, {ID: 3, Name: "Review"}}})
	}
	return screen
}

// testDeps is the standard host snapshot for a screen fixture.
func testDeps(available bool) Deps {
	return Deps{Available: available}
}

// TestRenderInsightsUnavailable: a nil Insights service shows the
// "unavailable" placeholder, never a blank panel.
func TestRenderInsightsUnavailable(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(New().View(screentest.FrameAt(t, 120, 40)))
	if !strings.Contains(out, "Insights service not available.") {
		t.Fatalf("expected unavailable placeholder, got:\n%s", out)
	}
	if !strings.Contains(out, "INSIGHTS") {
		t.Fatalf("unavailable state lost the screen identity:\n%s", out)
	}
}

// TestRenderInsightsComputing: before the first load the view shows the
// computing placeholder, not an all-empty board (which would misread as
// healthy).
func TestRenderInsightsComputing(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(testScreen(false, domain.Insights{}).View(screentest.FrameAt(t, 120, 40)))
	if !strings.Contains(out, "Computing insights") {
		t.Fatalf("expected computing placeholder, got:\n%s", out)
	}
	if !strings.Contains(out, "INSIGHTS") {
		t.Fatalf("computing state lost the screen identity:\n%s", out)
	}
}

// TestInsightsBodyCellUsesPanelGeometry pins the P5 ownership split: the panel
// classifies available width and chrome height, then the mounted cell receives
// exactly that box. Section rendering reads only its Canvas width.
func TestInsightsBodyCellUsesPanelGeometry(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 97, 31)
	kit := frame.Kit()
	screen := testScreen(true, domain.Insights{StuckDays: 7})

	result := screengrid.Render(kit, screen.grid, screen.panelBox(kit), screen.bodyRoot(kit))
	placement, ok := result.Placement(sectionBody)
	if !ok || !placement.Leaf || placement.Dropped {
		t.Fatalf("Insights body placement = %+v, found=%v; want a mounted visible cell", placement, ok)
	}
	want := screen.panelBox(kit)
	if placement.Box != want {
		t.Fatalf("Insights cell box = %+v, want classified panel box %+v", placement.Box, want)
	}
	if want.Width != kit.PanelContentWidth() || want.Rows != kit.Chrome().ViewportRows() {
		t.Fatalf("panel box = %+v, width/height classifiers disagree", want)
	}
}

// TestRenderInsightsEmptyStates: when every sub-insight has HasData=false
// the renderer paints the muted empty line under each of the six numbered
// sections and emits no misleading "0" figure.
func TestRenderInsightsEmptyStates(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(testScreen(true, domain.Insights{StuckDays: 7}).View(screentest.FrameAt(t, 120, 40)))

	for _, kicker := range []string{
		"STUCK TASKS", "CYCLE TIME", "WORK IN PROGRESS",
		"GUARD HOTSPOTS", "ERROR LOOP", "PER-MODEL CONTRAST",
	} {
		if !strings.Contains(out, kicker) {
			t.Fatalf("missing section %q in:\n%s", kicker, out)
		}
	}
	// One "No data yet." per empty sub-insight — six total.
	if n := strings.Count(out, "No data yet."); n != 6 {
		t.Fatalf("expected 6 empty-state lines, got %d in:\n%s", n, out)
	}
	for i := 1; i <= 6; i++ {
		if !strings.Contains(out, "#"+strconv.Itoa(i)) {
			t.Fatalf("missing numbered head #%d in:\n%s", i, out)
		}
	}
}

// TestRenderInsightsPopulated: populated sub-insights render their data
// rows (ids, figures, bucket names) rather than the empty placeholder, and
// only the still-empty insights keep the placeholder.
func TestRenderInsightsPopulated(t *testing.T) {
	t.Parallel()
	ins := domain.Insights{
		StuckDays: 7,
		Stuck: domain.StuckInsight{HasData: true, Tasks: []domain.StuckTask{
			{TaskID: 42, BucketID: 2, DaysStuck: 9, Title: "wire the thing"},
		}},
		WIP: domain.WIPInsight{HasData: true, Buckets: []domain.BucketWIP{
			{BucketID: 3, Count: 4},
		}},
		ErrorLoop: domain.ErrorLoopInsight{HasData: true, Total: 10, Resolved: 6, Open: 4},
		Guards: domain.GuardInsight{HasData: true, Hotspots: []domain.GuardHotspot{
			{Rule: "self-branch", Tag: "branch", Hits: 3, Recent7d: 2},
		}},
	}
	out := ansi.Strip(testScreen(true, ins).View(screentest.FrameAt(t, 120, 40)))

	for _, want := range []string{
		"#42", "9d", "wire the thing", // stuck
		"Development",              // bucket-id 2 resolved
		"Review",                   // bucket-id 3 (WIP)
		"self-branch/branch", "3x", // guard hotspot
		"open of 10 recorded", // error loop summary
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in populated render:\n%s", want, out)
		}
	}
	// Stuck/WIP/Guards/Errors are populated → only the two still-empty
	// insights (cycle time, per-model) carry the placeholder.
	if n := strings.Count(out, "No data yet."); n != 2 {
		t.Fatalf("expected 2 empty-state lines (cycle, per-model), got %d in:\n%s", n, out)
	}
}

// TestRenderInsightsPerModelPartialState: a below-gate per-model row renders
// the "sample since <date>, N rows" partial label and NEVER a confident dwell
// average, while an above-gate row shows its averaged figure with a guards/task
// rate. This is the surface half of the partial-state gate (task 1353).
func TestRenderInsightsPerModelPartialState(t *testing.T) {
	t.Parallel()
	ins := domain.Insights{
		StuckDays: 7,
		PerModel: domain.PerModelInsight{HasData: true, Models: []domain.ModelContrast{
			// Above gate: a confident reading with a guards/task rate.
			{
				AgentModel: "claude-opus-4-8", AvgDwellDays: 1.4, DwellSamples: 6,
				GuardViolations: 3, GuardsPerTask: 1.5,
				SampleSize: 9, FirstStampedAt: "2026-05-01 10:00:00", Partial: false,
			},
			// Below gate (n=2): partial — must show the sample-since label, not
			// a dwell figure.
			{
				AgentModel: "claude-sonnet-4-6", AvgDwellDays: 0, DwellSamples: 0,
				GuardViolations: 1, GuardsPerTask: 1.0,
				SampleSize: 2, FirstStampedAt: "2026-06-15 08:30:00", Partial: true,
			},
		}},
	}
	out := ansi.Strip(testScreen(true, ins).View(screentest.FrameAt(t, 120, 40)))

	// Above-gate row: averaged dwell + per-task rate present.
	for _, want := range []string{"claude-opus-4-8", "1.4d", "3 guard hits", "1.5/task"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected above-gate marker %q in:\n%s", want, out)
		}
	}
	// Below-gate row: partial label with date + row count; NO dwell figure.
	for _, want := range []string{"sample since 2026-06-15, 2 rows"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected partial-state label %q in:\n%s", want, out)
		}
	}
	// The partial row must not leak a confident "0.0d"/"no dwell" average — it
	// is gated out entirely.
	if strings.Contains(out, "2026-06-15 08:30:00") {
		t.Fatalf("partial label must show date only, not full timestamp:\n%s", out)
	}
}

// TestRenderInsightsScrollsWhenBodyOverflowsViewport locks the fix for the
// "Insights dead-end" bug: at a terminal height shorter than the six-section
// body, the bottom sections used to be silently clipped because the renderer
// emitted a static block and handleInsightsKey was a no-op. The fix wraps the
// body in the shared scroll window (mirroring Settings › General). This test
// mounts the view at an overflow-inducing height, asserts the last per-model
// row is clipped initially, scrolls to the bottom with `j`, and asserts the
// row is now visible; `g` resets to the top and `G` jumps back down in one
// press.
func TestRenderInsightsScrollsWhenBodyOverflowsViewport(t *testing.T) {
	const lastModel = "marker-last-model"

	models := make([]domain.ModelContrast, 0, 30)
	for i := 0; i < 29; i++ {
		models = append(models, domain.ModelContrast{
			AgentModel: fmt.Sprintf("model-%02d", i), AvgDwellDays: 1.5,
			DwellSamples: 3, SampleSize: 9,
		})
	}
	models = append(models, domain.ModelContrast{
		AgentModel: lastModel, AvgDwellDays: 2.0, DwellSamples: 4, SampleSize: 9,
	})

	screen := testScreen(true, domain.Insights{
		StuckDays: 7,
		PerModel:  domain.PerModelInsight{HasData: true, Models: models},
	})
	frame := screentest.FrameAt(t, 120, 20)
	kit := frame.Kit()

	bodyLines := strings.Count(screen.renderBody(kit, kit.PanelContentWidth()), "\n") + 1
	viewport := screen.viewportRows(kit)
	if viewport <= 0 {
		t.Fatalf("viewport budget = 0; fixture too small to scroll (bodyLines=%d)", bodyLines)
	}
	if bodyLines <= viewport {
		t.Fatalf("fixture does not overflow: bodyLines=%d viewport=%d — add more per-model rows", bodyLines, viewport)
	}

	clipped := ansi.Strip(screen.View(frame))
	if strings.Contains(clipped, lastModel) {
		t.Fatalf("expected last per-model row to be clipped at top of scroll; rendered:\n%s", clipped)
	}
	if !strings.Contains(clipped, "below") {
		t.Fatalf("expected \"▼ N below\" hint on the clipped render; rendered:\n%s", clipped)
	}

	for i := 0; i < bodyLines+5; i++ {
		screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	}
	expanded := ansi.Strip(screen.View(frame))
	if !strings.Contains(expanded, lastModel) {
		t.Fatalf("expected last per-model row visible after scrolling to bottom; rendered:\n%s", expanded)
	}

	screen = screen.Update(frame, screentest.Key("g")).Screen.(Screen)
	if screen.Scroll() != 0 {
		t.Fatalf("expected `g` to reset scroll to 0; got %d", screen.Scroll())
	}

	screen = screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if out := ansi.Strip(screen.View(frame)); !strings.Contains(out, lastModel) {
		t.Fatalf("expected `G` to land on the last per-model row; rendered:\n%s", out)
	}
}

// TestRenderInsightsSanitizesUntrustedStrings pins the escape-injection
// guard: task titles, agent model ids, guard rule/tags, and payload-derived
// bucket keys flow from agent input into the operator's terminal, and
// truncateText's width math treats escape sequences as zero-width — so the
// renderer must strip ANSI/control sequences before drawing. The output is
// checked UN-stripped: the styles' own ANSI is expected, but the injected
// OSC/BEL/CSI payloads must be gone while the printable text survives.
func TestRenderInsightsSanitizesUntrustedStrings(t *testing.T) {
	t.Parallel()
	ins := domain.Insights{
		StuckDays: 7,
		Stuck: domain.StuckInsight{HasData: true, Tasks: []domain.StuckTask{
			{TaskID: 1, BucketID: 2, DaysStuck: 9, Title: "evil\x1b]0;pwn\x07title"},
			{TaskID: 2, BucketID: 3, DaysStuck: 8, Title: "c1\u009b31mtitle"},
		}},
		Guards: domain.GuardInsight{HasData: true, Hotspots: []domain.GuardHotspot{
			{Rule: "rule\x1b[2Jname", Tag: "tag\x07", Hits: 1},
		}},
		PerModel: domain.PerModelInsight{HasData: true, Models: []domain.ModelContrast{
			{AgentModel: "bad\x1b[31mmodel", SampleSize: 9, DwellSamples: 1, AvgDwellDays: 1.0},
			{AgentModel: "partial", Partial: true, FirstStampedAt: "2026-\x1b[31m06-20 08:00:00", SampleSize: 2},
		}},
	}
	out := testScreen(true, ins).View(screentest.FrameAt(t, 120, 44))

	// C1 controls (U+009B 8-bit CSI, U+009D 8-bit OSC) need sequence-aware
	// stripping too; dropping only the introducer leaves terminal payload text.
	for _, forbidden := range []string{"\x07", "\x1b]0;", "\x1b[2J", "\x1b[31m", "\u009b", "\u009d"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("injected sequence %q survived into the render", forbidden)
		}
	}
	stripped := ansi.Strip(out)
	for _, want := range []string{"eviltitle", "c1title", "rulename", "badmodel", "partial", "2026-06-20"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("expected sanitized text %q in render:\n%s", want, stripped)
		}
	}
}

func TestInsightsRouteSanitizesWorkflowBucketLabelsBeforeHintRender(t *testing.T) {
	t.Parallel()
	const hostile = "Development\x1b[8m\x1b[31mred\x1b[0m\x1b]0;owned\a\u009b8m"
	screen := New().Bind(testDeps(true)).Apply(Payload{
		Insights: domain.Insights{
			StuckDays: 7,
			Stuck:     domain.StuckInsight{HasData: true, Tasks: []domain.StuckTask{{TaskID: 1, BucketID: 2, DaysStuck: 1, Title: "task"}}},
			WIP:       domain.WIPInsight{HasData: true, Buckets: []domain.BucketWIP{{BucketID: 2, Count: 1}}},
		},
		BucketNames: []domain.Bucket{{ID: 2, Name: hostile}},
	})
	for _, width := range []int{40, 120} {
		out := screen.View(screentest.FrameAt(t, width, 40))
		for _, forbidden := range []string{"\x1b[8m", "\x1b]0;", "owned", "\u009b"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("width %d retained hostile bucket sequence %q: %q", width, forbidden, out)
			}
		}
		if width == 120 && !strings.Contains(ansi.Strip(out), "Developmentred") {
			t.Fatalf("width %d lost printable bucket text: %q", width, ansi.Strip(out))
		}
	}
}
