package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

var statsPeriods = []string{"7d", "30d", "all"}

func (m *Model) handleStatsKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		m.cycleStatsPeriod(-1)
	case "right", "l":
		m.cycleStatsPeriod(1)
	}
}

func (m *Model) cycleStatsPeriod(dir int) {
	idx := m.statsPeriodIdx()
	m.statsPeriod = statsPeriods[(idx+dir+len(statsPeriods))%len(statsPeriods)]
	if err := m.refreshStats(); err != nil {
		m.status = err.Error()
	}
}

func (m Model) statsPeriodIdx() int {
	for i, p := range statsPeriods {
		if p == m.statsPeriod {
			return i
		}
	}
	return 1 // default: 30d
}

func (m Model) renderStats() string {
	if m.repos.Metrics == nil {
		return m.renderPanel(m.t("tui.empty.metrics_unavailable"))
	}

	// Top block: project Totals + Tokens as two bordered tables. Lives
	// outside the model-stats panel so the headline numbers read as the
	// summary while the per-model table reads as the detail beneath.
	budget := m.renderStatsBudgetTables()

	// Bottom block: per-model breakdown panel with the period picker
	// inlined into the kicker.
	model := m.renderStatsModelPanel()

	return "\n" + indentBlock(budget+"\n\n"+model, 2)
}

// renderStatsModelPanel renders the per-AI-model breakdown table (errors
// recorded / searched / search-before-record %, solutions added, like %).
// Owns the period picker (`7d / 30d / all`) inlined into the kicker —
// the picker is bound to this dataset, not the project totals.
func (m Model) renderStatsModelPanel() string {
	period := m.statsPeriod
	if period == "" {
		period = "30d"
	}

	pickerParts := make([]string, len(statsPeriods))
	for i, p := range statsPeriods {
		if p == period {
			pickerParts[i] = m.styles.activeNav.Render(p)
		} else {
			pickerParts[i] = m.styles.nav.Render(p)
		}
	}
	periodPicker := strings.Join(pickerParts, m.styles.hint.Render(" · "))

	summary := m.statsSummary
	contentWidth := m.availableWidth() - 4

	const (
		modelW = 26
		countW = 8
		ratioW = 9
		likeW  = 7
	)

	sepLine := m.hRule(contentWidth)
	header := fmt.Sprintf("%-*s %*s %*s %*s %*s %*s",
		modelW, m.t("tui.stat.column.model"),
		countW, m.t("tui.stat.column.errors"),
		countW, m.t("tui.stat.column.searches"),
		ratioW, m.t("tui.stat.column.search_pct"),
		countW, m.t("tui.stat.column.sol"),
		likeW, m.t("tui.stat.column.like_pct"),
	)

	rows := []string{
		m.styles.kicker(m.t("tui.kicker.stats")) + "  " + periodPicker,
		m.styles.info.Render(header),
		sepLine,
	}

	for _, am := range summary.ByModel {
		searchPct := "—"
		if am.SessionCorrelatedSample > 0 {
			searchPct = fmt.Sprintf("%.0f%%", am.SearchBeforeRecordRatio*100)
		}
		likePct := "—"
		if am.Buckets[domain.MetricBucketSolutionAdded] > 0 {
			likePct = fmt.Sprintf("%.0f%%", am.LikeRate*100)
		}
		row := fmt.Sprintf("%-*s %*d %*d %*s %*d %*s",
			modelW, truncateText(am.AgentModel, modelW),
			countW, am.Buckets[domain.MetricBucketErrorRecorded],
			countW, am.Buckets[domain.MetricBucketErrorsResearched],
			ratioW, searchPct,
			countW, am.Buckets[domain.MetricBucketSolutionAdded],
			likeW, likePct,
		)
		rows = append(rows, row)
	}

	if len(summary.ByModel) == 0 {
		rows = append(rows, m.styles.hint.Render(m.t("tui.empty.stats")))
	} else {
		t := summary.Total
		searchPct := "—"
		if t.SessionCorrelatedSample > 0 {
			searchPct = fmt.Sprintf("%.0f%%", t.SearchBeforeRecordRatio*100)
		}
		likePct := "—"
		if t.Buckets[domain.MetricBucketSolutionAdded] > 0 {
			likePct = fmt.Sprintf("%.0f%%", t.LikeRate*100)
		}
		totalRow := fmt.Sprintf("%-*s %*d %*d %*s %*d %*s",
			modelW, m.t("tui.stat.total_row_label"),
			countW, t.Buckets[domain.MetricBucketErrorRecorded],
			countW, t.Buckets[domain.MetricBucketErrorsResearched],
			ratioW, searchPct,
			countW, t.Buckets[domain.MetricBucketSolutionAdded],
			likeW, likePct,
		)
		rows = append(rows, sepLine, m.styles.info.Render(totalRow))
	}

	if summary.Since != "" {
		rows = append(rows, "", m.styles.hint.Render(fmt.Sprintf(m.t("tui.stat.since_fmt"), summary.Since)))
	}

	return m.styles.panel.Render(strings.Join(rows, "\n"))
}

// renderStatsBudgetTables renders the Totals (tasks / comments / tags) and
// Tokens (estimated, plus max + a `[BUDGET EXCEEDED]` badge when a budget
// ceiling is configured) blocks as two bordered grid tables. The max row is
// omitted when no token budget is set (`MaxTokens == 0`) so the panel never
// advertises a misleading "max: 0" ceiling. Visually
// matches the old Config runtime header from pre-T2 — the user feedback
// is explicit that text-row layouts read as "loose" next to the model
// breakdown table immediately above. Side-by-side when the panel is
// wide enough; otherwise stacked, with a single combined table as the
// narrow-terminal fallback.
func (m Model) renderStatsBudgetTables() string {
	totalsRows := m.summaryRows(m.t("tui.kicker.totals"),
		[2]string{m.t("tui.stat.tasks"), fmt.Sprintf("%d", len(m.tasks))},
		[2]string{m.t("tui.stat.comments"), fmt.Sprintf("%d", len(m.comments))},
		[2]string{m.t("tui.stat.tags"), fmt.Sprintf("%d", len(m.tags))},
	)
	tokensFields := []([2]string){
		{m.t("tui.stat.estimated"), fmt.Sprintf("%d", m.metrics.EstimatedTotal)},
	}
	if m.metrics.MaxTokens > 0 {
		tokensFields = append(tokensFields, [2]string{m.t("tui.stat.max"), fmt.Sprintf("%d", m.metrics.MaxTokens)})
	}
	tokensRows := m.summaryRows(m.t("tui.kicker.tokens"), tokensFields...)
	if m.metrics.Truncated {
		tokensRows = append(tokensRows, []string{m.styles.error.Render(m.t("tui.stat.error_badge")), m.styles.error.Render(m.t("tui.stat.budget_exceeded"))})
	}

	return m.renderSummaryTables(summaryTablesOpts{
		LabelWidth:  13,
		ValueWidth:  27,
		SideBySide:  true,
		MergeNarrow: true,
	}, totalsRows, tokensRows)
}
