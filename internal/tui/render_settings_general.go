package tui

import (
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

	scope := "global"
	if m.repos.RepoLocalDir != "" {
		scope = "local (" + m.repos.RepoLocalDir + ")"
	}
	runtimeRows := m.summaryRows("Runtime",
		[2]string{"okt version", valueOrDash(m.repos.Version)},
		[2]string{"scope", valueOrDash(scope)},
		[2]string{"config", valueOrDash(m.repos.ConfigPath)},
		[2]string{"database", valueOrDash(m.repos.DBPath)},
	)
	projectRows := m.summaryRows("Project",
		[2]string{"workflow", valueOrDash(m.workflow.Key)},
		[2]string{"buckets", valueOrDash(strings.Join(bucketKeys, ", "))},
		[2]string{"theme", valueOrDash(m.theme.Key)},
	)

	body := m.renderSummaryTables(summaryTablesOpts{
		LabelWidth: 14,
		ValueWidth: 46,
	}, runtimeRows, projectRows)

	hint := m.styles.hint.Render("read-only · use t (theme) / c (config) to switch · edit ~/.config/omakiten/omakiten.yaml for the rest")
	return "\n" + indentBlock(body+"\n\n"+hint, 2)
}

// handleSettingsGeneralKey routes keypresses while Settings › General is
// active. The view itself is read-only; only the global theme/config
// pickers remain reachable from here so the user can still hot-swap
// themes and pick a config profile without leaving Settings.
func (m *Model) handleSettingsGeneralKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "t":
		m.openThemePicker()
	case "c":
		m.openConfigPicker()
	}
	return nil
}
