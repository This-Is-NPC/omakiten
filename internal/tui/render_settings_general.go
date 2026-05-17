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
func (m Model) renderSettingsGeneral() string {
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

	body := m.renderSummaryTables(summaryTablesOpts{
		LabelWidth: 14,
		ValueWidth: 46,
	}, runtimeRows, projectRows)

	hint := m.styles.hint.Render(m.t("tui.settings.general_hint"))
	return "\n" + indentBlock(body+"\n\n"+hint, 2)
}

// handleSettingsGeneralKey routes keypresses while Settings › General is
// active. The view itself is read-only; only the global theme/config
// pickers remain reachable from here so the user can still hot-swap
// themes and pick a config profile without leaving Settings. `e` shells
// out to $EDITOR against the active omakiten.yaml so the user can edit
// the wiring file directly; on return the bundle is re-imported through
// the same handleEditorFinished path the entity edits use.
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
	}
	return nil
}
