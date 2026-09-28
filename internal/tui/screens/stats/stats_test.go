package stats

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func sampleSummary() domain.MetricsSummary {
	return domain.MetricsSummary{
		Period: "30d",
		Since:  "2026-06-01",
		ByModel: []domain.AgentMetrics{
			{
				AgentModel:              "claude-opus-5",
				SessionCorrelatedSample: 8,
				SearchBeforeRecordRatio: 0.75,
				LikeRate:                0.5,
				Buckets: map[domain.EventMetricBucket]int{
					domain.MetricBucketErrorRecorded:    12,
					domain.MetricBucketErrorsResearched: 9,
					domain.MetricBucketSolutionAdded:    4,
				},
			},
			{
				AgentModel: "gpt-5",
				Buckets: map[domain.EventMetricBucket]int{
					domain.MetricBucketErrorRecorded: 3,
				},
			},
		},
		Total: domain.AgentMetrics{
			AgentModel:              "total",
			SessionCorrelatedSample: 8,
			SearchBeforeRecordRatio: 0.6,
			LikeRate:                0.5,
			Buckets: map[domain.EventMetricBucket]int{
				domain.MetricBucketErrorRecorded:    15,
				domain.MetricBucketErrorsResearched: 9,
				domain.MetricBucketSolutionAdded:    4,
			},
		},
	}
}

type testMetricsService interface {
	Summary(context.Context, domain.ProjectContext, string, int64) (domain.MetricsSummary, error)
}

func testDeps(service testMetricsService) Deps {
	return Deps{
		Available: service != nil,
		Totals: Totals{
			Tasks:    12,
			Comments: 5,
			Tags:     3,
			Tokens:   domain.TokenMetrics{EstimatedTotal: 128000, MaxTokens: 200000},
		},
	}
}

func loadedScreen() Screen {
	return New().Bind(testDeps(fixedMetricsService{summary: sampleSummary()})).Apply(Payload{Summary: sampleSummary()})
}

// TestScreenIdentityAndChrome pins the contract surface: the stable id, the
// footer declarations (screen-owned keys only) and the single help group.
func TestScreenIdentityAndChrome(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	screen := loadedScreen()

	if screen.ID() != screenhost.StatsGeneral {
		t.Fatalf("ID() = %q, want %q", screen.ID(), screenhost.StatsGeneral)
	}
	assertStatsFooter(t, screen, frame)
	assertStatsHelp(t, screen, frame)
}

func assertStatsFooter(t *testing.T, screen Screen, frame screenhost.Frame) {
	t.Helper()
	footer := screen.Footer(frame)
	wantKeys := []string{"←/→", "up/down", "r", "g/G"}
	if len(footer) != len(wantKeys) {
		t.Fatalf("footer has %d bindings, want %d", len(footer), len(wantKeys))
	}
	for i, want := range wantKeys {
		if footer[i].Key != want {
			t.Fatalf("footer[%d].Key = %q, want %q", i, footer[i].Key, want)
		}
	}
	if !footer[0].Primary || !footer[1].Primary || footer[2].Primary || footer[3].Primary {
		t.Fatalf("footer priorities = %+v, want the period picker and the scroll verb as the primaries", footer)
	}
	for _, binding := range footer {
		if binding.Label == "" {
			t.Fatalf("footer binding %q has no label", binding.Key)
		}
		if binding.Key == "tab" || binding.Key == ",//" || binding.Key == "?" {
			t.Fatalf("screen advertises host-owned key %q", binding.Key)
		}
	}
}

func assertStatsHelp(t *testing.T, screen Screen, frame screenhost.Frame) {
	t.Helper()
	help := screen.Help(frame)
	if len(help) != 1 || help[0].ID != "stats_general" {
		t.Fatalf("help groups = %+v, want a single stats_general group", help)
	}
	if help[0].Title == "" || len(help[0].Bindings) != 5 {
		t.Fatalf("help group = %+v, want a title and 5 bindings", help[0])
	}
}

// TestPeriodDefaultsTo30d proves an unset screen reports and renders the
// documented default rather than an empty selection.
func TestPeriodDefaultsTo30d(t *testing.T) {
	t.Parallel()
	if got := New().Period(); got != DefaultPeriod {
		t.Fatalf("Period() = %q, want %q", got, DefaultPeriod)
	}
}

// TestUpdateCyclesThePeriodInBothDirections pins the picker rotation and its
// wraparound; the cycle order is also the render order of the inline picker.
func TestUpdateCyclesThePeriodInBothDirections(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)

	cases := []struct {
		name  string
		start string
		keys  []string
		want  string
	}{
		{"right advances from the default", "", []string{"right"}, "all"},
		{"l advances from the default", "", []string{"l"}, "all"},
		{"left retreats from the default", "", []string{"left"}, "7d"},
		{"h retreats from the default", "", []string{"h"}, "7d"},
		{"right wraps all → 7d", "all", []string{"right"}, "7d"},
		{"left wraps 7d → all", "7d", []string{"left"}, "all"},
		{"a full forward cycle returns home", "7d", []string{"right", "right", "right"}, "7d"},
		{"an unknown stored period recovers to the default neighbourhood", "90d", []string{"right"}, "all"},
		{"unbound keys leave the period alone", "7d", []string{"x"}, "7d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			screen := loadedScreen()
			screen.period = tc.start
			for _, key := range tc.keys {
				outcome := screen.Update(frame, screentest.Key(key))
				if key != "x" && outcome.Action.Kind != screenhost.ActionReload {
					t.Fatalf("a period cycle emitted action %v, want reload", outcome.Action.Kind)
				}
				screen = outcome.Screen.(Screen)
			}
			if screen.Period() != tc.want {
				t.Fatalf("period = %q, want %q", screen.Period(), tc.want)
			}
		})
	}
}

// TestCycleRequestsReloadWithTheNewPeriod proves the picker and host reload
// remain aligned.
func TestCycleReloadsWithTheNewPeriod(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(testDeps(&recordingMetricsService{}))

	outcome := screen.Update(frame, screentest.Key("right"))
	if outcome.Action.Kind != screenhost.ActionReload {
		t.Fatalf("period cycle action = %v, want reload", outcome.Action.Kind)
	}
	got := outcome.Screen.(Screen)
	if got.Period() != "all" {
		t.Fatalf("period = %q, want all", got.Period())
	}
}

// TestCycleWithoutPortStillRotates keeps headless hosts usable.
func TestCycleWithoutPortStillRotates(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	got := New().Update(frame, screentest.Key("right")).Screen.(Screen)
	if got.Period() != "all" {
		t.Fatalf("period = %q, want all without a metrics port", got.Period())
	}
}

// TestUpdateIgnoresNonKeyMessages proves an unrelated message is inert.
func TestUpdateIgnoresNonKeyMessages(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	screen := loadedScreen()
	got := screen.Update(frame, tea.WindowSizeMsg{Width: 1, Height: 1}).Screen.(Screen)
	if got.Period() != screen.Period() {
		t.Fatalf("a non-key message changed the period to %q", got.Period())
	}
}

// TestLifecycleIsInert pins the lifecycle contract for a stateless screen.
func TestLifecycleIsInert(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 120, 40)
	screen := loadedScreen()
	for _, event := range []screenhost.LifecycleEvent{
		screenhost.LifecycleEnter,
		screenhost.LifecycleLeave,
		screenhost.LifecycleFocus,
		screenhost.LifecycleBlur,
		screenhost.LifecycleResize,
	} {
		outcome := screen.Lifecycle(frame, event)
		if outcome.Action.Kind != screenhost.ActionNone {
			t.Fatalf("lifecycle %v emitted action %v, want none", event, outcome.Action.Kind)
		}
		if outcome.Screen.(Screen).Period() != screen.Period() {
			t.Fatalf("lifecycle %v changed the period", event)
		}
	}
}

// TestViewRendersTheBreakdownAndTotals is the populated render: every column
// header, both model rows, the totals row and the since note must appear.
func TestViewRendersTheBreakdownAndTotals(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(loadedScreen().View(screentest.FrameAt(t, 160, 40)))
	for _, want := range []string{
		"MODEL", "ERRORS", "SEARCHES", "SEARCH%", "SOL", "LIKE%",
		"claude-opus-5", "gpt-5", "total",
		"12", "9", "75%", "4", "50%",
		"since 2026-06-01",
		"// TOTALS", "// TASKS", "// COMMENTS", "// TAGS",
		"// TOKENS", "// ESTIMATED", "128000", "// MAX", "200000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats view missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "│  MODEL") {
		t.Fatalf("model column header lost the selection-column indent:\n%s", out)
	}
	headerAt, dataAt := -1, -1
	for _, line := range strings.Split(out, "\n") {
		if headerAt < 0 && strings.Contains(line, "MODEL") && strings.Contains(line, "ERRORS") {
			headerAt = strings.Index(line, "MODEL")
		}
		if dataAt < 0 && strings.Contains(line, "claude-opus-5") {
			dataAt = strings.Index(line, "claude-opus-5")
		}
	}
	if headerAt < 0 || dataAt < 0 || headerAt != dataAt {
		t.Fatalf("MODEL header column %d does not match data column %d:\n%s", headerAt, dataAt, out)
	}
}

// TestUnmeasurableRatiosRenderAsEmDash proves a model with no correlated sample
// (or no solutions) shows an em-dash rather than a misleading 0%.
func TestUnmeasurableRatiosRenderAsEmDash(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(loadedScreen().View(screentest.FrameAt(t, 160, 40)))
	if !strings.Contains(out, "—") {
		t.Fatalf("a model with no sample must render an em-dash, not 0%%\n%s", out)
	}
}

// TestEmptySummaryRendersThePlaceholder proves a wired-but-empty dataset shows
// the empty-state hint and no totals row.
func TestEmptySummaryRendersThePlaceholder(t *testing.T) {
	t.Parallel()
	screen := New().Bind(testDeps(fixedMetricsService{}))
	out := ansi.Strip(screen.View(screentest.FrameAt(t, 160, 40)))
	if !strings.Contains(out, "No agent activity recorded yet") {
		t.Fatalf("expected the empty-state hint, got:\n%s", out)
	}
	if strings.Contains(out, "since ") {
		t.Fatalf("an empty summary must not render a since note:\n%s", out)
	}
}

// TestUnavailableWithoutMetricsPort proves the placeholder wins over an empty
// panel when no service is wired.
func TestUnavailableWithoutMetricsPort(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(New().View(screentest.FrameAt(t, 120, 40)))
	if !strings.Contains(out, "Metrics repository not available") {
		t.Fatalf("expected the unavailable placeholder, got:\n%s", out)
	}
}

// TestTokenBudgetRowsFollowTheCeiling proves the max row is omitted when no
// budget is configured (so the panel never advertises a misleading "max: 0")
// and that exceeding the ceiling raises the badge.
func TestTokenBudgetRowsFollowTheCeiling(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 160, 40)

	deps := testDeps(fixedMetricsService{summary: sampleSummary()})
	deps.Totals.Tokens = domain.TokenMetrics{EstimatedTotal: 900}
	noCeiling := ansi.Strip(New().Bind(deps).Apply(Payload{Summary: sampleSummary()}).View(frame))
	if strings.Contains(noCeiling, "// MAX") {
		t.Fatalf("an unset budget must omit the max row:\n%s", noCeiling)
	}
	if strings.Contains(noCeiling, "budget exceeded") {
		t.Fatalf("an unset budget must not raise the exceeded badge:\n%s", noCeiling)
	}

	deps.Totals.Tokens = domain.TokenMetrics{EstimatedTotal: 210000, MaxTokens: 200000, Truncated: true}
	exceeded := ansi.Strip(New().Bind(deps).Apply(Payload{Summary: sampleSummary()}).View(frame))
	if !strings.Contains(exceeded, "// MAX") || !strings.Contains(exceeded, "budget exceeded") {
		t.Fatalf("an exceeded budget must show the max row and the badge:\n%s", exceeded)
	}
}

// TestPeriodPickerMarksTheActiveSelection proves every period is offered and the
// active one is visually distinguished from the rest.
func TestPeriodPickerMarksTheActiveSelection(t *testing.T) {
	t.Parallel()
	frame := screentest.FrameAt(t, 160, 40)
	for _, period := range Periods {
		screen := loadedScreen()
		screen.period = period
		kit := frame.Kit()
		panel := screen.modelPanelView(kit)
		for _, offered := range Periods {
			if !strings.Contains(ansi.Strip(panel), offered) {
				t.Fatalf("picker for %q does not offer %q", period, offered)
			}
		}
		if !strings.Contains(panel, kit.Styles.ActiveNav.Render(period)) {
			t.Fatalf("picker does not mark %q as active", period)
		}
	}
}

// TestNarrowTerminalStillRenders proves the responsive fallbacks hold at a
// terminal too narrow for the side-by-side summary tables.
//
// The terminal is TALL as well as narrow on purpose. Stacking the two tables
// costs 17 rows, and since #2441 the summary block only renders when the model
// panel beneath it still gets a data window — so a short terminal would drop the
// block and this would stop testing the width fallback it exists for.
func TestNarrowTerminalStillRenders(t *testing.T) {
	t.Parallel()
	out := ansi.Strip(loadedScreen().View(screentest.FrameAt(t, 50, 50)))
	if strings.TrimSpace(out) == "" {
		t.Fatal("the narrow layout rendered nothing")
	}
	if !strings.Contains(out, "TASKS") {
		t.Fatalf("the narrow layout dropped the totals table:\n%s", out)
	}
}

// TestModelPanelFitsBelow80Columns pins #2442: the per-model table used fixed
// column widths that summed past a terminal under 80 columns. Every painted
// line must now fit the frame width — FitWidths shrinks the cells instead of
// letting the panel run off the edge.
func TestModelPanelFitsBelow80Columns(t *testing.T) {
	t.Parallel()
	for _, width := range []int{72, 79} {
		frame := screentest.FrameAt(t, width, 40)
		out := loadedScreen().View(frame)
		for i, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line %d is %d cells wide:\n%s", width, i, got, ansi.Strip(line))
			}
		}
	}
}

// TestViewSanitizesUntrustedModelNames pins the escape-injection guard: agent
// model ids arrive from MCP input and must not be able to repaint the operator's
// terminal.
func TestViewSanitizesUntrustedModelNames(t *testing.T) {
	t.Parallel()
	summary := domain.MetricsSummary{
		ByModel: []domain.AgentMetrics{{
			AgentModel: "evil\x1b]0;pwn\x07model\u009b31m",
			Buckets:    map[domain.EventMetricBucket]int{domain.MetricBucketErrorRecorded: 1},
		}},
	}
	screen := New().Bind(testDeps(fixedMetricsService{})).Apply(Payload{Summary: summary})
	out := screen.View(screentest.FrameAt(t, 160, 40))
	for _, forbidden := range []string{"\x07", "\x1b]0;", "\u009b"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("injected sequence %q survived into the render", forbidden)
		}
	}
	// The OSC payload (including its "pwn" body) is stripped wholesale; the
	// surrounding printable text survives.
	if !strings.Contains(ansi.Strip(out), "evilmodel") {
		t.Fatalf("the printable text must survive sanitisation:\n%s", ansi.Strip(out))
	}
}

// TestViewWithoutCatalogDoesNotPanic proves the screen renders on a host with no
// catalog wired: labels degrade to keys instead of crashing.
func TestViewWithoutCatalogDoesNotPanic(t *testing.T) {
	t.Parallel()
	frame := screentest.Frame(t, screentest.Options{NoCatalog: true})
	out := ansi.Strip(loadedScreen().View(frame))
	if !strings.Contains(out, "tui.stat") {
		t.Fatalf("expected key-literal degradation, got:\n%s", out)
	}
}

// fixedMetricsService returns a canned summary.
type fixedMetricsService struct{ summary domain.MetricsSummary }

func (f fixedMetricsService) Summary(context.Context, domain.ProjectContext, string, int64) (domain.MetricsSummary, error) {
	return f.summary, nil
}

// recordingMetricsService captures what it was asked for and echoes the period
// back so the caller can prove the result was applied.
type recordingMetricsService struct {
	period    string
	projectID int64
}

func (r *recordingMetricsService) Summary(_ context.Context, _ domain.ProjectContext, period string, projectID int64) (domain.MetricsSummary, error) {
	r.period = period
	r.projectID = projectID
	return domain.MetricsSummary{Period: period}, nil
}

// statsChromeReadings are the four shapes the model panel's chrome takes: with
// and without model rows, crossed with a dated and an undated reading. The
// footer is the half that moves — the total row is earned by the rows above it
// and the since note by the date on the reading — so a count that ignores
// either branch fails here.
func statsChromeReadings() map[string]domain.MetricsSummary {
	undated := sampleSummary()
	undated.Since = ""
	return map[string]domain.MetricsSummary{
		"rows-and-date": sampleSummary(),
		"rows-no-date":  undated,
		"empty-dated":   {Period: "30d", Since: "2026-06-01"},
		"empty":         {Period: "30d"},
	}
}

func statsScreenWith(summary domain.MetricsSummary) Screen {
	return New().Bind(testDeps(fixedMetricsService{summary: summary})).Apply(Payload{Summary: summary})
}

// statsArrange is one geometry's model-section placement and the rows it
// painted, read off the same panelBox the screen renders through.
func statsArrange(t *testing.T, screen Screen, width, height int) (screenlayout.Placement, string, bool) {
	t.Helper()
	kit := screentest.FrameAt(t, width, height).Kit()
	res := screenlayout.ArrangeIn(kit, screen.panelBox(kit), screen.grid.Layout(), screen.modelsSection(kit))
	p, ok := res.Placement(sectionModels)
	return p, screentest.StripANSI(res.View), ok && !p.Dropped
}

// TestModelChromeRowsCountsWhatTheBuildersPin is the charge on
// [Screen.modelChromeRows]. The floors the model section declares are read on
// every arrange, so the chrome is COUNTED rather than composed — and a counted
// number is only safe while something fails when it drifts from the rows the
// builders actually pin.
//
// Three readings of the same number are compared: the count, the builders' own
// line counts, and the rows the ARRANGER charges for that chrome. The third is
// the one a count could not otherwise see — the arranger soft-wraps header and
// footer blocks, so a chrome line that wrapped at a narrow width would cost a
// row the count never knew about, and the floors would be under-declared
// exactly where the panel has the least room to spare.
func TestModelChromeRowsCountsWhatTheBuildersPin(t *testing.T) {
	t.Parallel()
	for name, summary := range statsChromeReadings() {
		name, summary := name, summary
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			screen := statsScreenWith(summary)
			placed := 0
			for _, width := range []int{200, 120, 80, 60, 40, 24, 12, 6} {
				for _, height := range []int{60, 30, 20} {
					placed += assertStatsChromeCharge(t, screen, width, height)
				}
			}
			if placed == 0 {
				t.Fatal("the sweep never placed the section, so the arranger half of the comparison never ran")
			}
		})
	}
}

// assertStatsChromeCharge compares the count against the builders and against
// the arranger at one geometry, and reports whether the section was placed.
func assertStatsChromeCharge(t *testing.T, screen Screen, width, height int) int {
	t.Helper()
	kit := screentest.FrameAt(t, width, height).Kit()
	counted := screen.modelChromeRows()
	inner := max(1, kit.PanelContentWidth()-panel.Borders)
	if built := len(screen.modelPanelHeader(kit, inner)) + len(screen.modelPanelFooter(kit, inner)); built != counted {
		t.Fatalf("%dx%d: modelChromeRows counts %d rows, the builders pin %d", width, height, counted, built)
	}
	p, _, placed := statsArrange(t, screen, width, height)
	if !placed {
		return 0
	}
	// Below the whole-shape floor the block still pins Bottom so the section
	// paints its own box; at or above it, everything the count knows about.
	want := modelHeaderRows + 1
	if p.Rows >= screen.modelsWholeShapeRows() {
		want = counted
	}
	if charged := p.HeaderRows + p.FooterRows; charged != want {
		t.Errorf("%dx%d: the arranger charges %d chrome rows (%d header + %d footer) into %d rows, want %d",
			width, height, charged, p.HeaderRows, p.FooterRows, p.Rows, want)
	}
	return 1
}

// TestModelPanelPaintsRowsOrNothing is the regression charge on the starving
// bands: heights at which the panel painted its kicker, its column header and
// its total row around an item window of ZERO — no model rows, and no
// "▼ N below" hint either, because a window with nothing visible reports
// nothing hidden.
//
// The bands are the ones the lone-cell scan named: 15-19 at every width (the
// summary has already yielded and the host itself is short), 33-37 at 80
// columns and 25-29 at 120 and 200, where the summary was KEPT because the
// yield rule protected a floor that could not show a row. Sweeping past them on
// both sides keeps this honest about which side of the floor each height is on.
//
// Two claims, and the second is why modelsMinViewport is two rows rather than
// one: a kept panel shows model rows, and a kept panel that is hiding rows says
// so. A window clipped to a single row paints the row and drops the hint that
// composes with it, which hides the rest of the table just as completely as the
// empty viewport did.
func TestModelPanelPaintsRowsOrNothing(t *testing.T) {
	t.Parallel()
	long := sampleSummary()
	for i := 0; i < 5; i++ {
		long.ByModel = append(long.ByModel, long.ByModel[:2]...)
	}
	for _, reading := range []struct {
		name    string
		summary domain.MetricsSummary
	}{{"fits", sampleSummary()}, {"overflows", long}} {
		reading := reading
		t.Run(reading.name, func(t *testing.T) {
			t.Parallel()
			assertStatsPanelSweep(t, statsScreenWith(reading.summary), reading.name == "overflows")
		})
	}
}

// assertStatsPanelSweep walks the bands and states the two claims, then proves
// it reached both sides of the floor.
func assertStatsPanelSweep(t *testing.T, screen Screen, overflows bool) {
	t.Helper()
	kept, dropped, hiding := 0, 0, 0
	for _, width := range []int{200, 120, 80, 60} {
		for height := 12; height <= 40; height++ {
			p, view, placed := statsArrange(t, screen, width, height)
			if !placed {
				dropped++
				continue
			}
			kept++
			hiding += assertStatsPanelShowsItsRows(t, width, height, p, view)
		}
	}
	if kept == 0 || dropped == 0 {
		t.Fatalf("the sweep kept the section %d times and dropped it %d times; it has to cross the floor to assert anything about it", kept, dropped)
	}
	if overflows && hiding == 0 {
		t.Fatal("the overflowing reading never hid a row, so the hint half of the assertion never ran")
	}
	t.Logf("stats model floor: %d geometries kept the panel, %d dropped it, %d hid rows", kept, dropped, hiding)
}

// assertStatsPanelShowsItsRows is the pair of claims at one kept geometry, and
// reports whether this geometry was hiding rows.
func assertStatsPanelShowsItsRows(t *testing.T, width, height int, p screenlayout.Placement, view string) int {
	t.Helper()
	if p.ItemViewport < 1 {
		t.Errorf("%dx%d: the model panel was painted into %d rows with an item viewport of %d — chrome around an empty table",
			width, height, p.Rows, p.ItemViewport)
		return 0
	}
	if p.Above+p.Below == 0 {
		return 0
	}
	if !strings.Contains(view, "▼") && !strings.Contains(view, "▲") {
		t.Errorf("%dx%d: %d model row(s) are hidden and the panel paints no hint:\n%s",
			width, height, p.Above+p.Below, view)
	}
	return 1
}
