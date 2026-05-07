package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderSettingsGeneral renders the read-only info card surfaced under
// Settings › General. Splits into two side-by-side panels (Runtime /
// Project) on wide terminals and stacks them on narrow ones, mirroring
// the runtime / tokens layout that lived on the old Config view in T1.
//
// The data is metadata-only — paths, versions, and the active workflow /
// theme keys. Mutating any of these still goes through the dedicated
// pickers (`t` for theme, `c` for config) which remain reachable from
// every Settings sub.
func (m Model) renderSettingsGeneral() string {
	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}
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

	runtimeRows := [][]string{
		{labelCell("Runtime"), ""},
		{labelCell("okt version"), valueOrDash(m.repos.Version)},
		{labelCell("config"), valueOrDash(m.repos.ConfigPath)},
		{labelCell("database"), valueOrDash(m.repos.DBPath)},
	}
	projectRows := [][]string{
		{labelCell("Project"), ""},
		{labelCell("workflow"), valueOrDash(m.workflow.Key)},
		{labelCell("buckets"), valueOrDash(strings.Join(bucketKeys, ", "))},
		{labelCell("theme"), valueOrDash(m.theme.Key)},
	}

	const (
		labelWidth = 14
		valueWidth = 46
		tableWidth = 1 + labelWidth + 1 + valueWidth + 1
		gap        = 2
	)
	widths := []int{labelWidth, valueWidth}

	var body string
	switch {
	case m.availableWidth() >= tableWidth*2+gap:
		left := renderGridTable(runtimeRows, widths, m.styles.border)
		right := renderGridTable(projectRows, widths, m.styles.border)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
	case m.availableWidth() >= tableWidth:
		left := renderGridTable(runtimeRows, widths, m.styles.border)
		right := renderGridTable(projectRows, widths, m.styles.border)
		body = left + "\n\n" + right
	default:
		valueW := clampInt(m.availableWidth()-labelWidth-3, 8, valueWidth)
		narrowWidths := []int{labelWidth, valueW}
		all := append(append([][]string{}, runtimeRows...), projectRows...)
		body = renderGridTable(all, narrowWidths, m.styles.border)
	}

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
