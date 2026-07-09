package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

// handleInsightsKey owns the Stats › Insights sub-mode keyboard surface.
// The view is read-only, so the local vocabulary is scroll-only —
// `j`/`k`/`PgUp`/`PgDn`/`ctrl+u`/`ctrl+d`/`g`/`G` nudge the body offset when
// the six-section panel is taller than the terminal, mirroring
// handleSettingsGeneralKey (offset-only, no cursor). `r` (refresh) stays on
// the common-key path via refreshCurrentView. refreshInsightsLines runs
// before every ScrollBy so the linelist clamps against the current body's
// bounds (the realtime tick can grow/shrink the body between keypresses).
func (m *Model) handleInsightsKey(msg tea.KeyMsg) {
	pageStep := taskViewPageStep(m.insightsViewportRows())
	// One rebuild before the switch: every scroll verb needs the linelist
	// synced to the live body, and a rebuild on a non-scroll key is a
	// harmless no-op.
	m.refreshInsightsLines()
	switch msg.String() {
	case "down", "j":
		m.insightsLines = m.insightsLines.ScrollBy(1)
	case "up", "k":
		m.insightsLines = m.insightsLines.ScrollBy(-1)
	case "pgdown", "ctrl+d":
		m.insightsLines = m.insightsLines.ScrollBy(pageStep)
	case "pgup", "ctrl+u":
		m.insightsLines = m.insightsLines.ScrollBy(-pageStep)
	case "home", "g":
		m.insightsLines = m.insightsLines.ScrollBy(-(1 << 20))
	case "end", "G":
		m.insightsLines = m.insightsLines.ScrollBy(1 << 20)
	}
}

// insightsViewportRows is the body-row budget for the Insights panel.
// The panel chrome the body does not own is the bordered panel's top +
// bottom edge (2 rows); `panelViewportRows` already accounts for the
// screen header / status / leading blank / footer.
func (m Model) insightsViewportRows() int {
	return m.panelViewportRows(2)
}

// refreshInsightsLines rebuilds the linelist's lines + viewport from the
// current insights body. Called before every ScrollBy (and by the
// renderer's test seam) so the scroll clamp always operates on the
// live body. WithCursor(-1) forces the no-selection sentinel — the view
// is read-only, and a zero-value cursor would trip the linelist's
// follow branch and pin the offset to line 0 every frame.
func (m *Model) refreshInsightsLines() {
	lines := strings.Split(m.renderInsightsBody(), "\n")
	m.insightsLines = m.insightsLines.
		WithCursor(-1).
		WithLines(lines).
		WithViewport(m.insightsViewportRows())
}

// renderInsights draws the intelligence-layer sub-mode: the six
// today-insights the InsightsService computes on demand (stuck tasks,
// cycle time, WIP, guard hotspots, the error loop, and the per-model
// contrast). It is a pure presenter — it reads the cached m.insights the
// refresh path loaded from InsightsService.Today and renders it; it never
// queries. Each sub-insight carries an explicit HasData flag so the
// renderer can paint a muted empty line instead of a misleading zero.
//
// Layout follows the dev-editorial language: a single panel, a `//`
// kicker + horizontal rule per section, numbered `#N` section heads, and
// the single accent reserved for the headline figure of each insight.
//
// The body wraps in `sliceScrollRows` against `m.insightsLines.Scroll()`
// so the user can `j`/`k`/`PgUp`/`PgDn`/`g`/`G` through the six sections
// when the terminal is shorter than the rendered content. A stale offset
// after a realtime-tick body change is absorbed by `sliceScrollRows`,
// which clamps an out-of-range offset against the current body length.
func (m Model) renderInsights() string {
	if m.repos.Insights == nil {
		return m.renderPanel(m.t("tui.empty.insights_unavailable"))
	}
	if !m.insightsLoaded {
		return m.renderPanel(m.styles.hint.Render(m.t("tui.insights.computing")))
	}

	bodyLines := strings.Split(m.renderInsightsBody(), "\n")
	viewport := m.insightsViewportRows()
	visible := m.sliceScrollRows(bodyLines, m.insightsLines.Scroll(), viewport)
	return m.renderPanel(strings.Join(visible, "\n"))
}

// renderInsightsBody builds the header + six numbered sections as a single
// rendered block. Extracted so the key handler can measure the body height
// for scroll clamping (refreshInsightsLines) without re-running the
// renderer's scroll wrapper — `renderSettingsGeneral` follows the same split.
func (m Model) renderInsightsBody() string {
	width := m.availableWidth() - 4
	if width < 20 {
		width = 20
	}

	ins := m.insights
	var sections []string

	sections = append(sections, m.insightSection(1, m.t("tui.insights.stuck.kicker"), width, m.insightStuckBody(ins.Stuck, ins.StuckDays)))
	sections = append(sections, m.insightSection(2, m.t("tui.insights.cycle.kicker"), width, m.insightCycleBody(ins.CycleTime)))
	sections = append(sections, m.insightSection(3, m.t("tui.insights.wip.kicker"), width, m.insightWIPBody(ins.WIP)))
	sections = append(sections, m.insightSection(4, m.t("tui.insights.guards.kicker"), width, m.insightGuardsBody(ins.Guards)))
	sections = append(sections, m.insightSection(5, m.t("tui.insights.errors.kicker"), width, m.insightErrorBody(ins.ErrorLoop)))
	sections = append(sections, m.insightSection(6, m.t("tui.insights.models.kicker"), width, m.insightModelsBody(ins.PerModel)))

	header := m.styles.kicker(m.t("tui.kicker.insights")) + m.styles.hint.Render("  // "+m.t("tui.insights.subtitle"))
	return header + "\n\n" + strings.Join(sections, "\n\n")
}

// insightSection wraps one insight's body in the shared section chrome: a
// numbered `#N // KICKER` head, a horizontal rule, then the body lines.
// Centralising it keeps every section visually identical so the six read
// as one editorial index rather than six bespoke blocks.
func (m Model) insightSection(n int, kicker string, width int, body []string) string {
	head := m.styles.info.Render(fmt.Sprintf("#%d ", n)) + m.styles.kicker(kicker)
	rows := []string{head, m.hRule(width)}
	rows = append(rows, body...)
	return strings.Join(rows, "\n")
}

// emptyLine renders the canonical "no data yet" placeholder — a muted
// hint, never a zero — used wherever a sub-insight's HasData is false.
func (m Model) emptyLine() string {
	return m.styles.hint.Render(m.t("tui.insights.empty"))
}

// accentNum paints a headline figure in the single accent color so each
// insight has exactly one visually loud number (the dev-editorial "single
// accent" rule); everything else stays in the structural info tone.
func (m Model) accentNum(v string) string {
	return m.styles.hintAccent.Render(v)
}

func (m Model) insightStuckBody(s domain.StuckInsight, stuckDays int) []string {
	if !s.HasData {
		return []string{m.emptyLine()}
	}
	lines := []string{
		m.styles.hint.Render(fmt.Sprintf(m.t("tui.insights.stuck.threshold_fmt"), stuckDays)),
	}
	for _, t := range s.Tasks {
		// `› #id  <Nd>  title — sat in <bucket>`
		line := fmt.Sprintf("%s %s  %s  %s",
			m.styles.hintAccent.Render("›"),
			m.styles.info.Render(fmt.Sprintf("#%d", t.TaskID)),
			m.accentNum(fmt.Sprintf("%dd", t.DaysStuck)),
			truncateText(sanitizeTerminalText(t.Title), 40),
		)
		bucket := m.bucketLabel(t.BucketID)
		line += m.styles.hint.Render(fmt.Sprintf(m.t("tui.insights.stuck.in_bucket_fmt"), bucket))
		lines = append(lines, line)
	}
	return lines
}

func (m Model) insightCycleBody(c domain.CycleTimeInsight) []string {
	if !c.HasData {
		return []string{m.emptyLine()}
	}
	lines := make([]string, 0, len(c.Buckets)+1)
	for _, b := range c.Buckets {
		line := fmt.Sprintf("%s %-12s %s  %s",
			m.styles.info.Render("·"),
			truncateText(sanitizeTerminalText(b.FromBucket), 12),
			m.accentNum(fmt.Sprintf("%.1fd", b.AvgDwellDays)),
			m.styles.hint.Render(fmt.Sprintf(m.t("tui.insights.cycle.samples_fmt"), b.Samples)),
		)
		lines = append(lines, line)
	}
	if c.Bottleneck != "" {
		lines = append(lines, m.styles.warning.Render(fmt.Sprintf(m.t("tui.insights.cycle.bottleneck_fmt"), sanitizeTerminalText(c.Bottleneck))))
	}
	return lines
}

func (m Model) insightWIPBody(w domain.WIPInsight) []string {
	if !w.HasData {
		return []string{m.emptyLine()}
	}
	lines := make([]string, 0, len(w.Buckets))
	for _, b := range w.Buckets {
		line := fmt.Sprintf("%s %-12s %s",
			m.styles.info.Render("·"),
			truncateText(m.bucketLabel(b.BucketID), 12),
			m.accentNum(fmt.Sprintf("%d", b.Count)),
		)
		lines = append(lines, line)
	}
	return lines
}

func (m Model) insightGuardsBody(g domain.GuardInsight) []string {
	if !g.HasData {
		return []string{m.emptyLine()}
	}
	lines := make([]string, 0, len(g.Hotspots))
	for _, h := range g.Hotspots {
		label := h.Rule
		if h.Tag != "" {
			label = h.Rule + "/" + h.Tag
		}
		line := fmt.Sprintf("%s %-24s %s  %s",
			m.styles.info.Render("·"),
			truncateText(sanitizeTerminalText(label), 24),
			m.accentNum(fmt.Sprintf("%dx", h.Hits)),
			m.styles.hint.Render(fmt.Sprintf(m.t("tui.insights.guards.recent_fmt"), h.Recent7d)),
		)
		lines = append(lines, line)
	}
	return lines
}

func (m Model) insightErrorBody(e domain.ErrorLoopInsight) []string {
	if !e.HasData {
		return []string{m.emptyLine()}
	}
	// The open count travels INSIDE the format string (%s) so a locale can
	// reorder it with indexed verbs (%[2]d etc.) instead of the word order
	// being frozen around a prefix concatenation. The accent is applied by
	// splitting on a sentinel AFTER formatting — nesting the pre-styled
	// accent inside hint.Render would let its ANSI reset strip the hint
	// style from everything after the number.
	const marker = "\x00"
	line := fmt.Sprintf(m.t("tui.insights.errors.summary_fmt"), marker, e.Total, e.Resolved)
	open := m.accentNum(fmt.Sprintf("%d", e.Open))
	before, after, found := strings.Cut(line, marker)
	if !found {
		// Translator dropped the %s placeholder — degrade to prefixing the
		// accented count rather than losing it.
		return []string{open + " " + m.styles.hint.Render(line)}
	}
	return []string{m.styles.hint.Render(before) + open + m.styles.hint.Render(after)}
}

func (m Model) insightModelsBody(p domain.PerModelInsight) []string {
	if !p.HasData {
		return []string{m.emptyLine()}
	}
	lines := make([]string, 0, len(p.Models))
	for _, mc := range p.Models {
		name := truncateText(sanitizeTerminalText(mc.AgentModel), 24)
		// Below-gate rows render as partial-state: the model name followed by
		// "sample since <date>, N rows" — never a confident average or a
		// misleading zero on a tiny n.
		if mc.Partial {
			line := fmt.Sprintf("%s %-24s %s",
				m.styles.info.Render("·"),
				name,
				m.styles.warning.Render(fmt.Sprintf(m.t("tui.insights.models.partial_fmt"), m.shortStampDate(mc.FirstStampedAt), mc.SampleSize)),
			)
			lines = append(lines, line)
			continue
		}
		dwell := m.styles.hint.Render(m.t("tui.insights.models.no_dwell"))
		if mc.DwellSamples > 0 {
			dwell = m.accentNum(fmt.Sprintf("%.1fd", mc.AvgDwellDays))
		}
		guards := m.styles.hint.Render(fmt.Sprintf(m.t("tui.insights.models.guards_fmt"), mc.GuardViolations))
		// Append the per-task rate so a high-volume model is not read as worse
		// purely for doing more work; suffixed (not folded into guards_fmt) to
		// keep the translated count string free of a float ordinal.
		if mc.GuardsPerTask > 0 {
			guards += m.styles.hint.Render(fmt.Sprintf(m.t("tui.insights.models.guards_per_task_fmt"), mc.GuardsPerTask))
		}
		line := fmt.Sprintf("%s %-24s %s  %s",
			m.styles.info.Render("·"),
			name,
			dwell,
			guards,
		)
		lines = append(lines, line)
	}
	return lines
}

// shortStampDate trims a SQLite "YYYY-MM-DD HH:MM:SS" timestamp to its date
// part for the partial-state "sample since <date>" label. An empty or
// unexpected value passes through unchanged so the label degrades gracefully.
func (m Model) shortStampDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// bucketLabel resolves a workflow bucket id to its human name from the
// resolved workflow, falling back to `#<id>` when the id is not in the
// active kit (e.g. a historical bucket that no longer exists). Keeps the
// stuck / WIP insights readable without the view ever querying.
func (m Model) bucketLabel(id int64) string {
	for _, b := range m.workflow.Buckets {
		if b.ID == id {
			return b.Name
		}
	}
	return fmt.Sprintf("#%d", id)
}
