package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
		return "\n" + indentBlock(m.styles.panel.Render("Metrics repository not available."), 2)
	}

	period := m.statsPeriod
	if period == "" {
		period = "30d"
	}

	// Period picker inline with the kicker.
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
		modelW  = 26
		countW  = 8
		ratioW  = 9
		likeW   = 7
	)

	sepLine := m.styles.separator.Render(strings.Repeat("─", contentWidth))
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

	rows = append(rows, "", m.renderStatsTotalsBlock(), "", m.renderStatsTokensBlock())

	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

// renderStatsTotalsBlock renders the project headline counts (tasks /
// comments / context entries / tags) that previously lived on the Config
// view's runtime header. Sourced from the slices loaded by `refresh()`,
// so the block stays cheap even though it is rendered every tick.
func (m Model) renderStatsTotalsBlock() string {
	row := func(label string, count int) string {
		return m.styles.info.Render(fmt.Sprintf("// %-10s", strings.ToUpper(label))) + " " + fmt.Sprintf("%d", count)
	}
	parts := []string{
		m.styles.kicker("Totals"),
		row("tasks", len(m.tasks)),
		row("comments", len(m.comments)),
		row("context", len(m.entries)),
		row("tags", len(m.tags)),
	}
	return strings.Join(parts, "\n")
}

// renderStatsTokensBlock renders the token-budget summary (estimated /
// max + a colored "[BUDGET EXCEEDED]" badge when m.metrics.Truncated).
// The data is the same domain.TokenMetrics aggregated by computeMetrics
// — moving it here keeps Stats as the single observability surface.
func (m Model) renderStatsTokensBlock() string {
	row := func(label, value string) string {
		return m.styles.info.Render(fmt.Sprintf("// %-10s", strings.ToUpper(label))) + " " + value
	}
	parts := []string{
		m.styles.kicker("Tokens"),
		row("estimated", fmt.Sprintf("%d", m.metrics.EstimatedTotal)),
		row("max", fmt.Sprintf("%d", m.metrics.MaxTokens)),
	}
	if m.metrics.Truncated {
		parts = append(parts, m.styles.error.Render("[BUDGET EXCEEDED] estimated > max"))
	}
	return strings.Join(parts, "\n")
}
