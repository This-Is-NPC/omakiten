package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

	return m.renderSummaryTables(summaryTablesOpts{
		LabelWidth: 14,
		ValueWidth: 46,
		Auto:       true,
	}, runtimeRows, projectRows)
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

// handleSettingsGeneralKey routes keypresses while Settings › General is
// active. The view itself is read-only; only the global theme/config
// pickers remain reachable from here so the user can still hot-swap
// themes and pick a config profile without leaving Settings. `e` shells
// out to $EDITOR against the active omakiten.yaml so the user can edit
// the wiring file directly; on return the bundle is re-imported through
// the same handleEditorFinished path the entity edits use.
//
// `j`/`k`/`PgUp`/`PgDn`/`g`/`G` scroll the body when the rendered
// summary block is taller than the available viewport — the sibling
// Settings sub-tabs already scrolled, so this binding closes the
// general-only dead-end. Scroll is offset-only (no cursor) since the
// view is read-only.
func (m *Model) handleSettingsGeneralKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "t":
		m.openThemePicker()
	case "c":
		m.openConfigPicker()
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
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(taskViewPageStep(m.settingsGeneralViewportRows()))
	case "pgup", "ctrl+u":
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(-taskViewPageStep(m.settingsGeneralViewportRows()))
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
// viewport from the current Settings › General render. Called from
// every *Model handler that touches the scroll state so the
// linelist always has accurate inputs before its ScrollBy fires.
func (m *Model) refreshSettingsGeneralLines() {
	body := m.renderSettingsGeneralBody()
	lines := strings.Split(body, "\n")
	// WithCursor(-1) forces the no-selection sentinel even when the
	// parent Model was constructed via a bare struct literal (test
	// path) — the zero-value linelist.Model has cursor=0 which would
	// trip Resync's Follow branch and clamp scroll to line 0 every
	// frame. Settings › General is read-only, so cursor=-1 is always
	// correct here.
	m.settingsGeneralLines = m.settingsGeneralLines.
		WithCursor(-1).
		WithLines(lines).
		WithViewport(m.settingsGeneralViewportRows())
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
