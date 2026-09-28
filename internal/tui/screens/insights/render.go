package insights

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

// View draws the intelligence-layer sub-mode: the six today-insights the host
// computes on demand. It is a pure presenter — it reads the
// cached reading the refresh path loaded and renders it; it never queries. Each
// sub-insight carries an explicit HasData flag so the renderer paints a muted
// empty line instead of a misleading zero.
//
// Layout follows the dev-editorial language: a single panel, a `//` kicker + a
// horizontal rule per section, numbered `#N` section heads, and the single
// accent reserved for the headline figure of each insight.
//
// The body cell windows the reading against the grid-owned offset inside the
// panel viewport. A stale offset after a realtime-tick body change is absorbed
// by the grid's arranger, which clamps it against the current body length.
func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	id := screenstate.For(kit.T("tui.kicker.insights"))
	if state, ok := screenstate.Resolve(kit, kit.PanelContentWidth(),
		id.Unavailable(!s.available(), kit.T("tui.empty.insights_unavailable")),
		id.Loading(!s.loaded, kit.T("tui.insights.computing")),
	); ok {
		return kit.Panel(state)
	}
	return "\n" + screenkit.Indent(screengrid.Render(kit, s.grid, s.panelBox(kit), s.bodyRoot(kit)).View, 2)
}

// renderBody builds the header + six numbered sections as a single rendered
// block. Extracted so the key handler can measure the body height for scroll
// clamping without re-running the renderer's scroll wrapper.
func (s Screen) renderBody(kit screenkit.Kit, width int) string {
	if width < 20 {
		width = 20
	}

	ins := s.insights
	var sections []string

	sections = append(sections, s.section(kit, 1, kit.T("tui.insights.stuck.kicker"), width, s.stuckBody(kit, ins.Stuck, ins.StuckDays)))
	sections = append(sections, s.section(kit, 2, kit.T("tui.insights.cycle.kicker"), width, s.cycleBody(kit, ins.CycleTime)))
	sections = append(sections, s.section(kit, 3, kit.T("tui.insights.wip.kicker"), width, s.wipBody(kit, ins.WIP)))
	sections = append(sections, s.section(kit, 4, kit.T("tui.insights.guards.kicker"), width, s.guardsBody(kit, ins.Guards)))
	sections = append(sections, s.section(kit, 5, kit.T("tui.insights.errors.kicker"), width, s.errorBody(kit, ins.ErrorLoop)))
	sections = append(sections, s.section(kit, 6, kit.T("tui.insights.models.kicker"), width, s.modelsBody(kit, ins.PerModel)))

	header := kit.Styles.FocusKicker(kit.T("tui.kicker.insights")) + kit.Styles.Hint.Render("  // "+kit.T("tui.insights.subtitle"))
	return header + "\n\n" + strings.Join(sections, "\n\n")
}

// section wraps one insight's body in the shared section chrome: a numbered
// `#N // KICKER` head, a horizontal rule, then the body lines. Centralising it
// keeps every section visually identical so the six read as one editorial index
// rather than six bespoke blocks.
func (s Screen) section(kit screenkit.Kit, n int, kicker string, width int, body []string) string {
	head := kit.Styles.Info.Render(fmt.Sprintf("#%d ", n)) + kit.Styles.Kicker(kicker)
	rows := []string{head, kit.HRule(width)}
	rows = append(rows, body...)
	return strings.Join(rows, "\n")
}

// emptyLine renders the canonical "no data yet" placeholder — a muted hint,
// never a zero — used wherever a sub-insight's HasData is false.
func emptyLine(kit screenkit.Kit) string {
	return kit.Styles.Hint.Render(kit.T("tui.insights.empty"))
}

// accentNum paints a headline figure in the single accent colour so each
// insight has exactly one visually loud number (the dev-editorial "single
// accent" rule); everything else stays in the structural info tone.
func accentNum(kit screenkit.Kit, v string) string {
	return kit.Styles.HintAccent.Render(v)
}

func info(kit screenkit.Kit, value string) string {
	return kit.Styles.Info.Render(value)
}

func hint(kit screenkit.Kit, value string) string {
	return kit.Styles.Hint.Render(value)
}

func warning(kit screenkit.Kit, value string) string {
	return kit.Styles.Warning.Render(value)
}

func (s Screen) stuckBody(kit screenkit.Kit, stuck domain.StuckInsight, stuckDays int) []string {
	if !stuck.HasData {
		return []string{emptyLine(kit)}
	}
	lines := []string{hint(kit, fmt.Sprintf(kit.T("tui.insights.stuck.threshold_fmt"), stuckDays))}
	marker := accentNum(kit, "›")
	for _, t := range stuck.Tasks {
		line := fmt.Sprintf("%s %s  %s  %s",
			marker,
			info(kit, fmt.Sprintf("#%d", t.TaskID)),
			accentNum(kit, fmt.Sprintf("%dd", t.DaysStuck)),
			screenkit.Truncate(screenkit.Sanitize(t.Title), 40),
		)
		line += hint(kit, fmt.Sprintf(kit.T("tui.insights.stuck.in_bucket_fmt"), s.bucketLabel(t.BucketID)))
		lines = append(lines, line)
	}
	return lines
}

func (s Screen) cycleBody(kit screenkit.Kit, c domain.CycleTimeInsight) []string {
	if !c.HasData {
		return []string{emptyLine(kit)}
	}
	lines := make([]string, 0, len(c.Buckets)+1)
	bullet := info(kit, "·")
	for _, b := range c.Buckets {
		line := fmt.Sprintf("%s %-12s %s  %s",
			bullet,
			screenkit.Truncate(screenkit.Sanitize(b.FromBucket), 12),
			accentNum(kit, fmt.Sprintf("%.1fd", b.AvgDwellDays)),
			hint(kit, fmt.Sprintf(kit.T("tui.insights.cycle.samples_fmt"), b.Samples)),
		)
		lines = append(lines, line)
	}
	if c.Bottleneck != "" {
		lines = append(lines, warning(kit, fmt.Sprintf(kit.T("tui.insights.cycle.bottleneck_fmt"), screenkit.Sanitize(c.Bottleneck))))
	}
	return lines
}

func (s Screen) wipBody(kit screenkit.Kit, w domain.WIPInsight) []string {
	if !w.HasData {
		return []string{emptyLine(kit)}
	}
	lines := make([]string, 0, len(w.Buckets))
	bullet := info(kit, "·")
	for _, b := range w.Buckets {
		line := fmt.Sprintf("%s %-12s %s",
			bullet,
			screenkit.Truncate(s.bucketLabel(b.BucketID), 12),
			accentNum(kit, fmt.Sprintf("%d", b.Count)),
		)
		lines = append(lines, line)
	}
	return lines
}

func (s Screen) guardsBody(kit screenkit.Kit, g domain.GuardInsight) []string {
	if !g.HasData {
		return []string{emptyLine(kit)}
	}
	lines := make([]string, 0, len(g.Hotspots))
	bullet := info(kit, "·")
	for _, h := range g.Hotspots {
		label := h.Rule
		if h.Tag != "" {
			label = h.Rule + "/" + h.Tag
		}
		line := fmt.Sprintf("%s %-24s %s  %s",
			bullet,
			screenkit.Truncate(screenkit.Sanitize(label), 24),
			accentNum(kit, fmt.Sprintf("%dx", h.Hits)),
			hint(kit, fmt.Sprintf(kit.T("tui.insights.guards.recent_fmt"), h.Recent7d)),
		)
		lines = append(lines, line)
	}
	return lines
}

func (s Screen) errorBody(kit screenkit.Kit, e domain.ErrorLoopInsight) []string {
	if !e.HasData {
		return []string{emptyLine(kit)}
	}
	// The open count travels INSIDE the format string (%s) so a locale can
	// reorder it with indexed verbs (%[2]d etc.) instead of the word order
	// being frozen around a prefix concatenation. The accent is applied by
	// splitting on a sentinel AFTER formatting — nesting the pre-styled accent
	// inside hint.Render would let its ANSI reset strip the hint style from
	// everything after the number.
	const marker = "\x00"
	line := fmt.Sprintf(kit.T("tui.insights.errors.summary_fmt"), marker, e.Total, e.Resolved)
	open := accentNum(kit, fmt.Sprintf("%d", e.Open))
	before, after, found := strings.Cut(line, marker)
	if !found {
		// Translator dropped the %s placeholder — degrade to prefixing the
		// accented count rather than losing it.
		return []string{open + " " + kit.Styles.Hint.Render(line)}
	}
	return []string{kit.Styles.Hint.Render(before) + open + kit.Styles.Hint.Render(after)}
}

func (s Screen) modelsBody(kit screenkit.Kit, p domain.PerModelInsight) []string {
	if !p.HasData {
		return []string{emptyLine(kit)}
	}
	lines := make([]string, 0, len(p.Models))
	bullet := info(kit, "·")
	for _, mc := range p.Models {
		name := screenkit.Truncate(screenkit.Sanitize(mc.AgentModel), 24)
		if mc.Partial {
			line := fmt.Sprintf("%s %-24s %s",
				bullet,
				name,
				warning(kit, fmt.Sprintf(kit.T("tui.insights.models.partial_fmt"), ShortStampDate(screenkit.Sanitize(mc.FirstStampedAt)), mc.SampleSize)),
			)
			lines = append(lines, line)
			continue
		}
		dwell := hint(kit, kit.T("tui.insights.models.no_dwell"))
		if mc.DwellSamples > 0 {
			dwell = accentNum(kit, fmt.Sprintf("%.1fd", mc.AvgDwellDays))
		}
		guards := hint(kit, fmt.Sprintf(kit.T("tui.insights.models.guards_fmt"), mc.GuardViolations))
		if mc.GuardsPerTask > 0 {
			guards += hint(kit, fmt.Sprintf(kit.T("tui.insights.models.guards_per_task_fmt"), mc.GuardsPerTask))
		}
		line := fmt.Sprintf("%s %-24s %s  %s", bullet, name, dwell, guards)
		lines = append(lines, line)
	}
	return lines
}

// ShortStampDate trims a SQLite "YYYY-MM-DD HH:MM:SS" timestamp to its date
// part for the partial-state "sample since <date>" label. An empty or
// unexpected value passes through unchanged so the label degrades gracefully.
func ShortStampDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// bucketLabel resolves a workflow bucket id to its human name from the resolved
// workflow, falling back to `#<id>` when the id is not in the active kit (e.g. a
// historical bucket that no longer exists). Keeps the stuck / WIP insights
// readable without the view ever querying.
func (s Screen) bucketLabel(id int64) string {
	for _, bucket := range s.buckets {
		if bucket.ID == id {
			return screenkit.Sanitize(bucket.Name)
		}
	}
	return fmt.Sprintf("#%d", id)
}
