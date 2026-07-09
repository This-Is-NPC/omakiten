package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// insightsTestModel builds a bare, styled Model with the InsightsService
// port wired (to a nil repo — the render path reads m.insights directly,
// never the service) and the supplied payload pre-loaded, mirroring the
// state the refresh path leaves before View runs. The two-bucket workflow
// lets the stuck / WIP insights resolve bucket-id → name.
func insightsTestModel(loaded bool, ins domain.Insights) Model {
	return Model{
		styles:         newStyles(config.Theme{}),
		width:          120,
		insightsLoaded: loaded,
		insights:       ins,
		repos: Repositories{
			Insights: app.NewInsightsService(nil),
		},
		workflow: domain.Workflow{Buckets: []domain.Bucket{
			{ID: 2, Name: "Development"},
			{ID: 3, Name: "Review"},
		}},
	}
}

// TestRenderInsightsUnavailable: a nil Insights service shows the
// "unavailable" placeholder, never a blank panel.
func TestRenderInsightsUnavailable(t *testing.T) {
	t.Parallel()
	m := Model{styles: newStyles(config.Theme{}), width: 120}
	out := ansi.Strip(m.renderInsights())
	if !strings.Contains(out, "Insights service not available.") {
		t.Fatalf("expected unavailable placeholder, got:\n%s", out)
	}
}

// TestRenderInsightsComputing: before the first load the view shows the
// computing placeholder, not an all-empty board (which would misread as
// healthy).
func TestRenderInsightsComputing(t *testing.T) {
	t.Parallel()
	m := insightsTestModel(false, domain.Insights{})
	out := ansi.Strip(m.renderInsights())
	if !strings.Contains(out, "Computing insights") {
		t.Fatalf("expected computing placeholder, got:\n%s", out)
	}
}

// TestRenderInsightsEmptyStates: when every sub-insight has HasData=false
// the renderer paints the muted empty line under each of the six numbered
// sections and emits no misleading "0" figure.
func TestRenderInsightsEmptyStates(t *testing.T) {
	t.Parallel()
	m := insightsTestModel(true, domain.Insights{StuckDays: 7})
	out := ansi.Strip(m.renderInsights())

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
	m := insightsTestModel(true, ins)
	out := ansi.Strip(m.renderInsights())

	for _, want := range []string{
		"#42", "9d", "wire the thing", // stuck
		"Development",                // bucket-id 2 resolved
		"Review",                     // bucket-id 3 (WIP)
		"self-branch/branch", "3x",   // guard hotspot
		"open of 10 recorded",        // error loop summary
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
	m := insightsTestModel(true, ins)
	out := ansi.Strip(m.renderInsights())

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

	m := insightsTestModel(true, domain.Insights{
		StuckDays: 7,
		PerModel:  domain.PerModelInsight{HasData: true, Models: models},
	})
	m.height = 20
	m.top = topStats
	m.sub = subStatsInsights

	bodyLines := strings.Count(m.renderInsightsBody(), "\n") + 1
	viewport := m.insightsViewportRows()
	if viewport <= 0 {
		t.Fatalf("viewport budget = 0; fixture too small to scroll (bodyLines=%d, height=%d)", bodyLines, m.height)
	}
	if bodyLines <= viewport {
		t.Fatalf("fixture does not overflow: bodyLines=%d viewport=%d — add more per-model rows", bodyLines, viewport)
	}

	clipped := ansi.Strip(m.renderInsights())
	if strings.Contains(clipped, lastModel) {
		t.Fatalf("expected last per-model row to be clipped at top of scroll; rendered:\n%s", clipped)
	}
	if !strings.Contains(clipped, "below") {
		t.Fatalf("expected \"▼ N below\" hint on the clipped render; rendered:\n%s", clipped)
	}

	for i := 0; i < bodyLines+5; i++ {
		m.handleInsightsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	expanded := ansi.Strip(m.renderInsights())
	if !strings.Contains(expanded, lastModel) {
		t.Fatalf("expected last per-model row visible after scrolling to bottom; rendered:\n%s", expanded)
	}

	m.handleInsightsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.insightsLines.Scroll() != 0 {
		t.Fatalf("expected `g` to reset scroll to 0; got %d", m.insightsLines.Scroll())
	}

	m.handleInsightsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if out := ansi.Strip(m.renderInsights()); !strings.Contains(out, lastModel) {
		t.Fatalf("expected `G` to land on the last per-model row; rendered:\n%s", out)
	}
}

// TestRenderInsightsSanitizesUntrustedStrings pins the escape-injection
// guard: task titles, agent model ids, guard rule/tags, and payload-derived
// bucket keys flow from MCP input into the operator's terminal, and
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
		}},
	}
	m := insightsTestModel(true, ins)
	out := m.renderInsights()

	// C1 controls (U+009B 8-bit CSI, U+009D 8-bit OSC) are NOT removed by
	// ansi.Strip — the rune filter must drop them too.
	for _, forbidden := range []string{"\x07", "\x1b]0;", "\x1b[2J", "\x1b[31m", "\u009b", "\u009d"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("injected sequence %q survived into the render", forbidden)
		}
	}
	stripped := ansi.Strip(out)
	for _, want := range []string{"eviltitle", "c131mtitle", "rulename", "badmodel"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("expected sanitized text %q in render:\n%s", want, stripped)
		}
	}
}
