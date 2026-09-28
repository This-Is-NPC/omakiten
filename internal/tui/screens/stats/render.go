package stats

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
)

// View renders the Stats › General body: optional Totals + Tokens outer chrome
// above a per-model breakdown panel whose item window is owned by screenlayout.
func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if !s.available() {
		return kit.Panel(kit.T("tui.empty.metrics_unavailable"))
	}

	blocks := make([]string, 0, 2)
	if budget := s.outerBudget(kit); budget != "" {
		// Top block: project Totals + Tokens as two bordered tables. Lives
		// outside the model-stats panel so the headline numbers read as the
		// summary while the per-model table reads as the detail beneath.
		blocks = append(blocks, budget)
	}
	blocks = append(blocks, s.modelPanelView(kit))

	return "\n" + screenkit.Indent(strings.Join(blocks, "\n\n"), 2)
}

// Model table column widths. Natural sizes used when the terminal has room;
// FitWidths shrinks them below 80 columns so the panel no longer runs past the
// edge (#2442). Gaps between the six columns are one space each.
const (
	modelW = 26
	countW = 8
	ratioW = 9
	likeW  = 7
)

func modelColumnWidths(available int) []int {
	natural := []int{modelW, countW, countW, ratioW, countW, likeW}
	// Six cells, five single-space gaps.
	budget := available - 5
	if budget < len(natural) {
		budget = len(natural)
	}
	return gridtable.FitWidths(natural, budget, 3)
}

// modelTablePrefix is the unselected CursorMarker + space. Header, body and
// total share it so MODEL sits over the first data cell instead of flush
// against the box — same indent Table/Plans use for ID/SLUG.
func modelTablePrefix(kit screenkit.Kit) string {
	return kit.CursorMarker(false) + " "
}

func modelGrid(kit screenkit.Kit, inner int) (prefix string, widths []int) {
	prefix = modelTablePrefix(kit)
	return prefix, modelColumnWidths(max(1, inner-screenkit.VisibleWidth(prefix)))
}

// modelRow formats one metrics reading into the six-column grid. Shared by the
// per-model rows and the total row so the two can never misalign. widths come
// from modelColumnWidths so header, body and footer share one FitWidths call.
func modelRow(kit screenkit.Kit, label string, am domain.AgentMetrics, widths []int) string {
	searchPct := "—"
	if am.SessionCorrelatedSample > 0 {
		searchPct = fmt.Sprintf("%.0f%%", am.SearchBeforeRecordRatio*100)
	}
	likePct := "—"
	if am.Buckets[domain.MetricBucketSolutionAdded] > 0 {
		likePct = fmt.Sprintf("%.0f%%", am.LikeRate*100)
	}
	return gridtable.FormatRowAligned([]string{
		label,
		fmt.Sprintf("%d", am.Buckets[domain.MetricBucketErrorRecorded]),
		fmt.Sprintf("%d", am.Buckets[domain.MetricBucketErrorsResearched]),
		searchPct,
		fmt.Sprintf("%d", am.Buckets[domain.MetricBucketSolutionAdded]),
		likePct,
	}, widths, []bool{false, true, true, true, true, true})
}

// modelHeaderRows is the rows modelPanelHeader always pins: the kicker with the
// period picker, the column header, and the rule that closes them.
const modelHeaderRows = 4

// modelChromeRows is the rows modelPanelHeader and modelPanelFooter pin around
// the data window, COUNTED rather than composed.
//
// [Screen.modelsWholeShapeRows] reads this on every arrange, and every arrange
// runs on every keystroke; composing a chip strip and two styled table rows to
// learn a number the reading's shape already fixes is work the keystroke budget
// charges for and the layout does not need. Measured: rendering the two
// builders instead of counting them costs 1080 more allocations per j/k round
// trip. The branches below are the branches of modelPanelFooter, and
// TestModelChromeRowsCountsWhatTheBuildersPin holds the two to each other at
// every fixture reading — a count that drifts from the builders fails there
// rather than in a geometry nobody records.
func (s Screen) modelChromeRows() int {
	rows := modelHeaderRows
	rows++ // Bottom is always pinned so the section paints its own box
	if len(s.summary.ByModel) > 0 {
		rows += 2 // the closing join and the total row
	}
	if s.summary.Since != "" {
		rows += 2 // the blank spacer and the since note
	}
	return rows
}

// modelPanelHeader is the chrome the panel draws ABOVE its data window: the
// kicker with the period picker inlined, the column header, and the rule that
// closes them.
//
// The period strip is components/chipstrip — same leaf Logs uses for its
// filter chips — so overflow cuts share one policy. The picker is owned here
// rather than by the summary block because it is bound to THIS dataset.
func (s Screen) modelPanelHeader(kit screenkit.Kit, inner int) []string {
	period := s.Period()
	chips := make([]tokenstrip.Chip, 0, len(Periods))
	for _, p := range Periods {
		chips = append(chips, tokenstrip.Chip{Label: p, Active: p == period})
	}
	kicker := kit.Styles.FocusKicker(kit.T("tui.kicker.stats"))
	if inner < 1 {
		inner = 1
	}
	// Room left on the kicker row after the label and the two-space gap.
	stripBudget := inner - screenkit.VisibleWidth(kicker) - 2
	strip := tokenstrip.Chips(chips, tokenstrip.ChipStyles{
		Active:   kit.Styles.ActiveNav,
		Inactive: kit.Styles.Nav,
		Sep:      kit.Styles.Hint,
	}, tokenstrip.ChipOptions{
		Sep:           " · ",
		BracketActive: false,
		Width:         stripBudget,
	})
	prefix, widths := modelGrid(kit, inner)
	colhdr := prefix + gridtable.FormatRowAligned([]string{
		kit.T("tui.stat.column.model"),
		kit.T("tui.stat.column.errors"),
		kit.T("tui.stat.column.searches"),
		kit.T("tui.stat.column.search_pct"),
		kit.T("tui.stat.column.sol"),
		kit.T("tui.stat.column.like_pct"),
	}, widths, []bool{false, true, true, true, true, true})
	block := framed.List(kit.Styles.Border, inner+panel.Borders, kicker+"  "+strip, []string{kit.Styles.Info.Render(colhdr)}, nil)
	return block.Header
}

// modelPanelFooter is the chrome the panel draws BELOW its data window: the rule
// and column-sum row that close the table, and the date the reading starts from.
//
// Both are omitted on an empty dataset: there is no total to sum and an empty
// summary must not date itself, so the empty panel is a different shape rather
// than a shorter version of the same one. [Screen.modelsBlock] leaves the whole
// footer unpinned for the same reason on a terminal too short to hold it and
// the rows it captions.
func (s Screen) modelPanelFooter(kit screenkit.Kit, inner int) []string {
	if inner < 1 {
		inner = 1
	}
	box := framed.Box(kit.Styles.Border, inner+panel.Borders, "", nil)
	wrap := box.Chrome
	rows := make([]string, 0, 5)
	if len(s.summary.ByModel) > 0 {
		prefix, widths := modelGrid(kit, inner)
		rows = append(rows,
			panel.Join(kit.Styles.Border, inner),
			wrap(kit.Styles.Info.Render(prefix+modelRow(kit, kit.T("tui.stat.total_row_label"), s.summary.Total, widths))),
		)
	}
	if s.summary.Since != "" {
		rows = append(rows, wrap(""), wrap(kit.Styles.Hint.Render(fmt.Sprintf(kit.T("tui.stat.since_fmt"), s.summary.Since))))
	}
	return append(rows, box.Footer...)
}

// modelDataRows is the scrollable window's content: one row per AI model, or the
// empty-state hint when the reading holds none.
func (s Screen) modelDataRows(kit screenkit.Kit, inner int) []string {
	if inner < 1 {
		inner = 1
	}
	if len(s.summary.ByModel) == 0 {
		return []string{kit.Styles.Hint.Render(kit.T("tui.empty.stats"))}
	}
	prefix, widths := modelGrid(kit, inner)
	rows := make([]string, 0, len(s.summary.ByModel))
	for _, am := range s.summary.ByModel {
		label := gridtable.Truncate(screenkit.Sanitize(am.AgentModel), widths[0])
		rows = append(rows, prefix+modelRow(kit, label, am, widths))
	}
	return rows
}

// renderBudgetTables renders the Totals (tasks / comments / tags) and Tokens
// (estimated, plus max + a `[BUDGET EXCEEDED]` badge when a budget ceiling is
// configured) blocks as two bordered grid tables. The max row is omitted when
// no token budget is set (`MaxTokens == 0`) so the panel never advertises a
// misleading "max: 0" ceiling. Side-by-side when the panel is wide enough;
// otherwise stacked, with a single combined table as the narrow-terminal
// fallback.
func (s Screen) renderBudgetTables(kit screenkit.Kit) string {
	totals := s.deps.Totals
	totalsRows := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.kicker.totals"),
		[2]string{kit.T("tui.stat.tasks"), fmt.Sprintf("%d", totals.Tasks)},
		[2]string{kit.T("tui.stat.comments"), fmt.Sprintf("%d", totals.Comments)},
		[2]string{kit.T("tui.stat.tags"), fmt.Sprintf("%d", totals.Tags)},
	)
	tokensFields := []([2]string){
		{kit.T("tui.stat.estimated"), fmt.Sprintf("%d", totals.Tokens.EstimatedTotal)},
	}
	if totals.Tokens.MaxTokens > 0 {
		tokensFields = append(tokensFields, [2]string{kit.T("tui.stat.max"), fmt.Sprintf("%d", totals.Tokens.MaxTokens)})
	}
	tokensRows := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.kicker.tokens"), tokensFields...)
	if totals.Tokens.Truncated {
		tokensRows = append(tokensRows, []gridtable.Cell{
			gridtable.Styled(kit.Styles.Error.Render(kit.T("tui.stat.error_badge"))),
			gridtable.Styled(kit.Styles.Error.Render(kit.T("tui.stat.budget_exceeded"))),
		})
	}
	// Summary tables are outer chrome, not the model Cell body. Keep their
	// side-by-side decision on the host's available width; the Cell receives
	// panel content width through its Canvas and classifies that geometry itself.
	return gridtable.Summaries(kit.AvailableWidth(), kit.Styles.Border, gridtable.Options{
		LabelWidth:  13,
		ValueWidth:  27,
		SideBySide:  true,
		MergeNarrow: true,
	}, totalsRows, tokensRows)
}
