package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		return m.renderPanel("Metrics repository not available.")
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
		modelW, "MODEL",
		countW, "ERRORS",
		countW, "SEARCHES",
		ratioW, "SEARCH%",
		countW, "SOL",
		likeW, "LIKE%",
	)

	rows := []string{
		m.styles.kicker("Stats") + "  " + periodPicker,
		m.styles.info.Render(header),
		sepLine,
	}

	for _, am := range summary.ByModel {
		searchPct := "—"
		if am.SessionCorrelatedSample > 0 {
			searchPct = fmt.Sprintf("%.0f%%", am.SearchBeforeRecordRatio*100)
		}
		likePct := "—"
		if am.SolutionsAdded > 0 {
			likePct = fmt.Sprintf("%.0f%%", am.LikeRate*100)
		}
		row := fmt.Sprintf("%-*s %*d %*d %*s %*d %*s",
			modelW, truncateText(am.AgentModel, modelW),
			countW, am.ErrorsRecorded,
			countW, am.ErrorsSearched,
			ratioW, searchPct,
			countW, am.SolutionsAdded,
			likeW, likePct,
		)
		rows = append(rows, row)
	}

	if len(summary.ByModel) == 0 {
		rows = append(rows, m.styles.hint.Render("No agent activity recorded yet for this period."))
	} else {
		t := summary.Total
		searchPct := "—"
		if t.SessionCorrelatedSample > 0 {
			searchPct = fmt.Sprintf("%.0f%%", t.SearchBeforeRecordRatio*100)
		}
		likePct := "—"
		if t.SolutionsAdded > 0 {
			likePct = fmt.Sprintf("%.0f%%", t.LikeRate*100)
		}
		totalRow := fmt.Sprintf("%-*s %*d %*d %*s %*d %*s",
			modelW, "total",
			countW, t.ErrorsRecorded,
			countW, t.ErrorsSearched,
			ratioW, searchPct,
			countW, t.SolutionsAdded,
			likeW, likePct,
		)
		rows = append(rows, sepLine, m.styles.info.Render(totalRow))
	}

	if summary.Since != "" {
		rows = append(rows, "", m.styles.hint.Render("since "+summary.Since))
	}

	return m.styles.panel.Render(strings.Join(rows, "\n"))
}

// renderStatsBudgetTables renders the Totals (tasks / comments / context
// entries / tags) and Tokens (estimated / max + a `[BUDGET EXCEEDED]`
// badge when truncated) blocks as two bordered grid tables. Visually
// matches the old Config runtime header from pre-T2 — the user feedback
// is explicit that text-row layouts read as "loose" next to the model
// breakdown table immediately above. Side-by-side when the panel is
// wide enough; otherwise stacked, with a single combined table as the
// narrow-terminal fallback.
func (m Model) renderStatsBudgetTables() string {
	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}
	totalsRows := [][]string{
		{labelCell("Totals"), ""},
		{labelCell("tasks"), fmt.Sprintf("%d", len(m.tasks))},
		{labelCell("comments"), fmt.Sprintf("%d", len(m.comments))},
		{labelCell("context"), fmt.Sprintf("%d", len(m.entries))},
		{labelCell("tags"), fmt.Sprintf("%d", len(m.tags))},
	}
	tokensRows := [][]string{
		{labelCell("Tokens"), ""},
		{labelCell("estimated"), fmt.Sprintf("%d", m.metrics.EstimatedTotal)},
		{labelCell("max"), fmt.Sprintf("%d", m.metrics.MaxTokens)},
	}
	if m.metrics.Truncated {
		tokensRows = append(tokensRows, []string{m.styles.error.Render("[ERROR]"), m.styles.error.Render("budget exceeded")})
	}

	const (
		labelWidth = 13
		valueWidth = 27
		tableWidth = 1 + labelWidth + 1 + valueWidth + 1
		gap        = 2
	)
	widths := []int{labelWidth, valueWidth}

	switch {
	case m.availableWidth() >= tableWidth*2+gap:
		left := renderGridTable(totalsRows, widths, m.styles.border)
		right := renderGridTable(tokensRows, widths, m.styles.border)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
	case m.availableWidth() >= tableWidth:
		left := renderGridTable(totalsRows, widths, m.styles.border)
		right := renderGridTable(tokensRows, widths, m.styles.border)
		return left + "\n\n" + right
	default:
		valueW := clampInt(m.availableWidth()-labelWidth-3, 8, valueWidth)
		narrowWidths := []int{labelWidth, valueW}
		all := append(append([][]string{}, totalsRows...), tokensRows...)
		return renderGridTable(all, narrowWidths, m.styles.border)
	}
}
