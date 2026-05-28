package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
)

// renderSettingsGeneral renders the read-only info card surfaced under
// Settings › General. Stacks two bordered tables (Runtime / Project)
// vertically — feedback during T2 review was that the side-by-side
// layout cramped the long path values, so the wide-screen branch is
// gone and the medium / narrow branches are unified.
//
// The data is metadata-only — paths, versions, and the active workflow /
// theme keys. Mutating any of these still goes through the dedicated
// pickers (`t` for theme, `c` for config) which remain reachable from
// every Settings sub.
//
// The body is wrapped in `sliceScrollRows` against `m.settingsGeneralScroll`
// so the user can `j`/`k`/`PgUp`/`PgDn`/`g`/`G` through the tables when the
// terminal is shorter than the rendered content — the sibling sub-tabs
// already scrolled, so General was the only Settings dead-end.
func (m Model) renderSettingsGeneral() string {
	body := m.renderSettingsGeneralBody()
	hint := m.styles.hint.Render(m.t("tui.settings.general_hint"))

	bodyLines := strings.Split(body, "\n")
	viewport := m.settingsGeneralViewportRows()
	visible := m.sliceScrollRows(bodyLines, m.settingsGeneralLines.Scroll(), viewport)
	return "\n" + indentBlock(strings.Join(visible, "\n")+"\n\n"+hint, 2)
}

// renderSettingsGeneralBody builds the Runtime + Project summary tables
// as a single rendered block. Extracted so the key handler can measure
// the body height for scroll clamping without re-running the renderer's
// scroll wrapper.
func (m Model) renderSettingsGeneralBody() string {
	valueOrDash := func(value string) string {
		if value == "" {
			return m.styles.hint.Render("—")
		}
		return value
	}

	bucketKeys := make([]string, 0, len(m.workflow.Buckets))
	for _, bucket := range m.workflow.Buckets {
		bucketKeys = append(bucketKeys, bucket.Key)
	}
	sort.Strings(bucketKeys)

	scope := m.t("tui.settings.runtime.scope_global")
	if m.repos.RepoLocalDir != "" {
		scope = fmt.Sprintf(m.t("tui.settings.runtime.scope_local_fmt"), m.repos.RepoLocalDir)
	}
	runtimeRows := m.summaryRows(m.t("tui.kicker.runtime"),
		[2]string{m.t("tui.settings.runtime.version"), valueOrDash(m.repos.Version)},
		[2]string{m.t("tui.settings.runtime.scope"), valueOrDash(scope)},
		[2]string{m.t("tui.settings.runtime.config"), valueOrDash(m.repos.ConfigPath)},
		[2]string{m.t("tui.settings.runtime.database"), valueOrDash(m.repos.DBPath)},
		[2]string{m.t("tui.settings.runtime.lang_cli"), valueOrDash(m.languages.CLI)},
		[2]string{m.t("tui.settings.runtime.lang_tui"), valueOrDash(m.languages.TUI)},
		[2]string{m.t("tui.settings.runtime.lang_agent_output"), valueOrDash(m.languages.AgentOutput)},
	)
	projectRows := m.summaryRows(m.t("tui.kicker.project"),
		[2]string{m.t("tui.settings.project.workflow"), valueOrDash(m.workflow.Key)},
		[2]string{m.t("tui.settings.project.buckets"), valueOrDash(strings.Join(bucketKeys, ", "))},
		[2]string{m.t("tui.settings.project.theme"), valueOrDash(m.theme.Key)},
	)

	// Compose the effective-config tables after Runtime / Project so
	// the entire Settings › General body packs through a single
	// renderSummaryTables call. The shared call keeps the Auto
	// label-width scan honest across all stacked panels — otherwise
	// runtime/project would size against a 14-col floor while the
	// effective sections would size against 24, and stacked tables of
	// mismatched widths look ragged.
	tables := [][][]string{runtimeRows, projectRows}
	tables = append(tables, m.effectiveConfigSections()...)

	return m.renderSummaryTables(summaryTablesOpts{
		LabelWidth: 24,
		ValueWidth: 46,
		Auto:       true,
	}, tables...)
}

// effectiveConfigSections builds one summary table per top-level
// Settings section reported by the active snapshot's effective-config
// accessor. Returned as a list of pre-built table rows so the General
// renderer can fold them into its single renderSummaryTables call
// alongside the Runtime / Project tables.
//
// Returns an empty slice when no snapshot is bound or no sections are
// populated — the General view still renders Runtime + Project in that
// case, so the caller does not need a fallback hint here. (The footer
// hint on the panel itself, tui.settings.general_hint, already covers
// the read-only contract for both halves of the body.)
//
// Layout: one bordered table per section, rows = `key path` +
// `effective value`. Reuses the same summary-table primitive as
// Runtime/Project per feedback_tui_wrappable_sections — no new layout
// primitive lands here.
//
// Source layer (`default` / `project` / `env`) is reserved on the
// accessor but not yet threaded — the i18n keys exist
// (`tui.settings.effective.source.*`) so a follow-up wave can light up
// a third column without touching this composer's call shape.
func (m Model) effectiveConfigSections() [][][]string {
	snap := m.repos.activeSnapshot()
	if snap == nil {
		return nil
	}
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		return nil
	}

	// Bucket tuples by section. EffectiveSectionKeys returns sections
	// in the accessor's struct-field order, which is convenient for
	// non-TUI consumers but doesn't match the user-relevance ordering
	// the Settings viewer wants. orderEffectiveSections reorders the
	// section list so user-facing prefs (theme, tricks, priorities,
	// …) lead and infra plumbing (mcp, sqlite, backup, hooks) trails;
	// sections not in the table fall through to a stable tail in their
	// original accessor order so future additions stay defensive.
	//
	// hiddenEffectiveSections drops sections whose effective values are
	// already surfaced elsewhere in the TUI (Runtime card, Project line,
	// dedicated sub-tabs) — see the set's doc comment for the per-section
	// rationale. Hidden sections are filtered after ordering so the
	// unknown-section tail-append still runs against the visible set.
	sectionKeys := orderEffectiveSections(snap.EffectiveSectionKeys())
	sectionKeys = filterHiddenEffectiveSections(sectionKeys)
	grouped := make(map[string][]config.EffectiveTuple, len(sectionKeys))
	for _, t := range tuples {
		grouped[t.Section] = append(grouped[t.Section], t)
	}

	// Cap the value column so long literals (long paths, packed
	// transition lists, multi-line YAML scalars) truncate with the
	// `…` glyph instead of wrapping and breaking the grid. The
	// summary-table packer scales the value column up in Auto mode,
	// so this is only a soft ceiling for individual cell values —
	// the table itself still fills the panel width.
	const valueCap = 80

	tables := make([][][]string, 0, len(sectionKeys))
	for _, section := range sectionKeys {
		rows := grouped[section]
		if len(rows) == 0 {
			continue
		}
		label := m.t("tui.settings.effective.section." + section)
		fields := make([][2]string, 0, len(rows))
		for _, r := range rows {
			keyPath := r.Key
			if keyPath == "" {
				// Scalar sections (no nested path) — show the
				// section name as the key so the row still reads
				// as `<section> · <value>` instead of leaving an
				// empty label cell.
				keyPath = section
			}
			fields = append(fields, [2]string{
				keyPath,
				truncateText(r.Value, valueCap),
			})
		}
		tables = append(tables, m.summaryRows(label, fields...))
	}
	return tables
}

// effectiveSectionRenderOrder is the user-relevance ordering applied
// to effective-config sections in Settings › General. Tiers (top →
// bottom): user-facing prefs (theme/tricks/priorities/severities),
// content surface (views/context/tui/output), search & solutions,
// infra integration (mcp/events/sqlite/backup), with hooks last as the
// power-user escape hatch. The accessor itself stays struct-field-order
// so non-TUI consumers (MCP, CLI) keep deciding their own ordering —
// only the renderer picks up this table. Sections absent from the
// table are appended in their original accessor order so new Settings
// fields never silently disappear from the viewer (see task #346
// DoD #2). Sections listed in hiddenEffectiveSections are filtered
// after ordering so the viewer never surfaces them.
var effectiveSectionRenderOrder = []string{
	"theme",
	"tricks",
	"priorities",
	"severities",
	"views",
	"context",
	"tui",
	"output",
	"search",
	"solutions",
	"activity_log",
	"mcp",
	"events",
	"sqlite",
	"backup",
	"hooks",
}

// hiddenEffectiveSections names the top-level Settings sections that
// Settings › General intentionally suppresses because their effective
// values are already surfaced elsewhere in the TUI:
//
//   - languages: Runtime card already prints CLI / TUI / AgentOutput
//   - workflow: Project line prints the active workflow key, and the
//     Guards sub-tab owns transitions/guards
//   - template_defaults: Templates sub-tab is the dedicated surface
//   - tag_synonyms: Tags sub-tab is the dedicated surface
//
// The i18n labels (`tui.settings.effective.section.<n>`) stay in the
// locale packs so a future re-enable does not have to round-trip
// through the parity test — see i18n parity rule in MEMORY.
var hiddenEffectiveSections = map[string]bool{
	"languages":         true,
	"workflow":          true,
	"template_defaults": true,
	"tag_synonyms":      true,
}

// orderEffectiveSections returns sections reordered per
// effectiveSectionRenderOrder. Known sections lead in tier order;
// unknown sections follow in their original input order. Only sections
// present in the input slice are returned — the ordering table is a
// preference, not a presence guarantee. Hidden sections are still
// returned here; filterHiddenEffectiveSections is the dedicated drop
// step so unknown-section tail-append stays uncoupled from the hide
// policy.
func orderEffectiveSections(sections []string) []string {
	if len(sections) == 0 {
		return sections
	}
	present := make(map[string]bool, len(sections))
	for _, s := range sections {
		present[s] = true
	}
	out := make([]string, 0, len(sections))
	known := make(map[string]bool, len(effectiveSectionRenderOrder))
	for _, s := range effectiveSectionRenderOrder {
		known[s] = true
		if present[s] {
			out = append(out, s)
		}
	}
	for _, s := range sections {
		if !known[s] {
			out = append(out, s)
		}
	}
	return out
}

// filterHiddenEffectiveSections drops sections listed in
// hiddenEffectiveSections while preserving the input order. Run after
// orderEffectiveSections so the unknown-section tail-append still
// works against the full accessor list — the hide policy only affects
// what reaches the renderer, not whether future Settings fields stay
// defensive.
func filterHiddenEffectiveSections(sections []string) []string {
	if len(sections) == 0 {
		return sections
	}
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		if hiddenEffectiveSections[s] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// settingsGeneralViewportRows is the data-row budget for the General
// body. The panel chrome below the body is two lines (blank separator +
// hint row); `panelViewportRows` already accounts for screen header /
// status / leading blank / footer, so passing 2 here yields the rows
// `sliceScrollRows` may spend on body + indicator hints.
func (m Model) settingsGeneralViewportRows() int {
	return m.panelViewportRows(2)
}

// clampSettingsGeneralScroll keeps `offset` inside the body bounds given
// the viewport budget. `sliceScrollRows` reserves up to two rows for the
// "▲ N above" / "▼ N below" hints, so the data window is `viewport - 2`;
// the max useful offset is `total - dataRows` (further would strand the
// user past the last data line). Returns 0 when the entire body fits.
func clampSettingsGeneralScroll(offset, total, viewport int) int {
	if viewport <= 0 {
		return 0
	}
	dataRows := scrollDataRows(viewport)
	if total <= dataRows {
		return 0
	}
	max := total - dataRows
	if offset > max {
		return max
	}
	if offset < 0 {
		return 0
	}
	return offset
}

// handleSettingsGeneralKey routes keypresses while Settings › General
// OR Settings › Guards is active. Both subs are read-only scroll-only
// surfaces, so the handler is shared — `refreshSettingsGeneralLines`
// picks the right body source from `m.sub` before each ScrollBy so the
// linelist always knows the current body's bounds. Only the global
// theme/config/subtask-kit/editor pickers remain reachable from here so
// the user can still hot-swap themes and pick a config profile without
// leaving Settings. `e` shells out to $EDITOR against the active
// omakiten.yaml so the user can edit the wiring file directly; on
// return the bundle is re-imported through the same
// handleEditorFinished path the entity edits use.
//
// `j`/`k`/`PgUp`/`PgDn`/`g`/`G` scroll the body when the rendered block
// is taller than the available viewport — the sibling Settings sub-tabs
// already scrolled, so this binding closes the general/guards
// dead-ends. Scroll is offset-only (no cursor) since the view is
// read-only.
func (m *Model) handleSettingsGeneralKey(msg tea.KeyMsg) tea.Cmd {
	pageStep := func() int {
		if m.sub == subSettingsGuards {
			return taskViewPageStep(m.settingsGuardsViewportRows())
		}
		return taskViewPageStep(m.settingsGeneralViewportRows())
	}
	switch msg.String() {
	case "t":
		m.openThemePicker()
	case "c":
		m.openConfigPicker()
	case "s":
		m.openSubtaskKitPicker()
	case "e":
		if m.repos.Editor == nil {
			m.status = m.t("tui.status.editor_unavailable")
			return nil
		}
		return runExternalEditor(m.repos.Editor.Path())
	case "down", "j":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(1)
	case "up", "k":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(-1)
	case "pgdown", "ctrl+d":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(pageStep())
	case "pgup", "ctrl+u":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(-pageStep())
	case "home", "g":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(-(1 << 20))
	case "end", "G":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(1 << 20)
	}
	return nil
}

// refreshSettingsGeneralLines rebuilds the linelist's body lines +
// viewport from the active Settings sub-tab's render. Called from
// every *Model handler that touches the scroll state so the
// linelist always has accurate inputs before its ScrollBy fires.
//
// The linelist is shared between Settings › General and Settings ›
// Guards — both subs are read-only scroll-only surfaces, so a single
// offset/cursor pair is enough. The body source switches on `m.sub`
// so j/k/pgup/pgdn/g/G operate on the visible body's bounds in each
// sub. The viewport also switches because the Guards body packs a
// slightly different chrome budget (mirror helper
// `settingsGuardsViewportRows`).
func (m *Model) refreshSettingsGeneralLines() {
	var (
		body     string
		viewport int
	)
	if m.sub == subSettingsGuards {
		body = m.renderSettingsGuardsBody()
		viewport = m.settingsGuardsViewportRows()
	} else {
		body = m.renderSettingsGeneralBody()
		viewport = m.settingsGeneralViewportRows()
	}
	lines := strings.Split(body, "\n")
	// WithCursor(-1) forces the no-selection sentinel even when the
	// parent Model was constructed via a bare struct literal (test
	// path) — the zero-value linelist.Model has cursor=0 which would
	// trip Resync's Follow branch and clamp scroll to line 0 every
	// frame. Settings › General and Settings › Guards are both
	// read-only, so cursor=-1 is always correct here.
	m.settingsGeneralLines = m.settingsGeneralLines.
		WithCursor(-1).
		WithLines(lines).
		WithViewport(viewport)
}

// maxSettingsGeneralScroll returns the offset that puts the last body
// row at the bottom of the viewport. Surfaces the bound the linelist
// component clamps to internally — at the bottom, only the
// "▲ N above" hint reserves a row (no below-hint), so the formula
// is total - viewport + AboveHintRows(HintsSplit) = total - viewport
// + 1. Test harness reads this to assert `G` lands on the right
// offset.
func (m Model) maxSettingsGeneralScroll() int {
	body := m.renderSettingsGeneralBody()
	total := strings.Count(body, "\n") + 1
	viewport := m.settingsGeneralViewportRows()
	if viewport <= 0 {
		return 0
	}
	bound := total - viewport + 1
	if bound < 0 {
		return 0
	}
	return bound
}

// maxSettingsGeneralScroll returns the offset that puts the last body
