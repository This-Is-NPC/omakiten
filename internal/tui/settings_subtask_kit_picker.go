package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/picker"
)

// subtaskKitOption is one row in the sub-task kit picker. IsNone marks
// the sentinel "none (inherit root)" entry the picker prepends so users
// can disable the cascade without editing omakiten.yaml by hand;
// Filename + Display + IsCustom mirror configOption for actual kit
// files. Pulled out as a sibling type (not reusing configOption) so the
// sentinel can carry a nil filename without forcing every caller of
// the config picker to branch on an empty string.
type subtaskKitOption struct {
	Filename string
	Display  string
	IsCustom bool
	IsNone   bool
}

// openSubtaskKitPicker scans the active config dir for kit files
// (mirrors the root-kit picker's source) and prepends a sentinel
// "none (inherit root)" entry. Initial cursor lands on the currently
// configured sub-kit (or on the sentinel when no sub-kit is wired).
func (m *Model) openSubtaskKitPicker() {
	options, err := discoverSubtaskKitOptions(m.repos.Editor.ConfigDir())
	if err != nil {
		m.status = err.Error()
		return
	}
	if len(options) == 0 {
		// No kit files visible — the picker would only show the
		// sentinel. Still useful to land here so the user understands
		// the cascade is disabled, but surface a hint via status.
		m.status = m.t("tui.status.no_subtask_kit_profiles")
	}
	active := m.currentSubtaskKitFilename()
	cursor := 0
	for i, opt := range options {
		switch {
		case opt.IsNone && active == "":
			cursor = i
		case opt.Filename == active:
			cursor = i
		}
	}
	m.subtaskKitPickerOptions = options
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{mode: entityScreenSubtaskKitPicker}
	m.entityPicker = picker.New(picker.Single).WithCursor(cursor, len(options), 0)
	m.status = m.t("tui.status.subtask_kit_picker")
}

// discoverSubtaskKitOptions lists every yaml profile the config picker
// would offer + a sentinel "none (inherit root)" entry. Profile order:
// sentinel first (so it stays predictable), then defaults alpha, then
// customs alpha — same precedence the existing root-kit picker uses.
func discoverSubtaskKitOptions(configDir string) ([]subtaskKitOption, error) {
	profiles, err := discoverConfigProfiles(configDir)
	if err != nil {
		return nil, err
	}
	out := make([]subtaskKitOption, 0, len(profiles)+1)
	out = append(out, subtaskKitOption{IsNone: true})
	for _, p := range profiles {
		out = append(out, subtaskKitOption{
			Filename: p.Filename,
			Display:  p.Display,
			IsCustom: p.IsCustom,
		})
	}
	return out, nil
}

// currentSubtaskKitFilename returns the basename of the sub-kit
// currently wired into omakiten.yaml, or "" when no cascade is active.
func (m Model) currentSubtaskKitFilename() string {
	snap := m.repos.activeSnapshot()
	if snap == nil {
		return ""
	}
	rel := snap.SubtaskKitPath()
	if rel == "" {
		return ""
	}
	return filepath.Base(rel)
}

// updateSubtaskKitPicker routes keypresses while the sub-kit picker is
// open. Cancel closes the overlay; Select runs the apply path.
func (m Model) updateSubtaskKitPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.entityPicker, cmd = m.entityPicker.Update(msg, len(m.subtaskKitPickerOptions), scrollDataRows(m.pickerViewportRows()))
	switch m.entityPicker.LastEvent() {
	case picker.EventCancel:
		m.closeEntityScreen(m.t("tui.status.subtask_kit_picker_cancelled"))
	case picker.EventSelect:
		m.applySubtaskKitSelection()
	}
	return m, cmd
}

// applySubtaskKitSelection writes the chosen sub-kit path into
// omakiten.yaml (or clears it for the "none" sentinel), then triggers
// the hot-reload path so #285's migration handler and #282's
// transparency notice fire through the cache rebuild. On reload
// failure the YAML mutation is rolled back via a second Apply so the
// on-disk wiring never diverges from the runtime snapshot.
func (m *Model) applySubtaskKitSelection() {
	if m.entityPicker.Cursor < 0 || m.entityPicker.Cursor >= len(m.subtaskKitPickerOptions) {
		return
	}
	chosen := m.subtaskKitPickerOptions[m.entityPicker.Cursor]
	relative := ""
	if !chosen.IsNone {
		if chosen.IsCustom {
			relative = filepath.Join("custom", chosen.Filename)
		} else {
			relative = chosen.Filename
		}
	}

	originalRelative, err := m.loadCurrentSubtaskKitRelative()
	if err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), err.Error())
		return
	}
	previousActive := m.currentSubtaskKitFilename()

	if _, err := m.repos.Editor.Apply(m.ctx, func(bundle *config.Bundle) error {
		bundle.SubtaskKit = relative
		return nil
	}); err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), err.Error())
		return
	}

	if err := m.reloadBundle(m.repos.Editor.Path()); err != nil {
		// Reload failed against the candidate YAML — restore the prior
		// subtask_kit value via a second Apply so the on-disk state
		// matches the runtime again. The rollback Apply failure is
		// folded into the surfaced error so operators see both.
		if _, rerr := m.repos.Editor.Apply(m.ctx, func(bundle *config.Bundle) error {
			bundle.SubtaskKit = originalRelative
			return nil
		}); rerr != nil {
			m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), fmt.Sprintf("%v (rollback also failed: %v)", err, rerr))
			return
		}
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), err.Error())
		return
	}
	switch {
	case chosen.IsNone:
		if previousActive == "" {
			m.closeEntityScreen(m.t("tui.status.subtask_kit_already_none"))
		} else {
			m.closeEntityScreen(m.t("tui.status.subtask_kit_cleared"))
		}
	default:
		display := strings.TrimSuffix(chosen.Filename, filepath.Ext(chosen.Filename))
		m.closeEntityScreen(fmt.Sprintf(m.t("tui.status.subtask_kit_switched_fmt"), display))
	}
}

// loadCurrentSubtaskKitRelative reads the active wiring file and
// returns the SubtaskKit field as written on disk. Used by the
// rollback path so the YAML can be restored byte-for-byte after a
// failed reload — the in-memory snapshot's SubtaskKitPath returns a
// resolved (absolute) path, which would not round-trip through the
// saver. Errors propagate so the caller surfaces them via status.
func (m Model) loadCurrentSubtaskKitRelative() (string, error) {
	bundle, err := m.repos.Editor.Load()
	if err != nil {
		return "", err
	}
	return bundle.SubtaskKit, nil
}

// renderSubtaskKitPicker draws the picker panel matching the root-kit
// picker's kicker + hint + rows shape (single accent per the design
// language) so the surface reads identically when the user toggles
// between the two pickers.
func (m Model) renderSubtaskKitPicker() string {
	active := m.currentSubtaskKitFilename()
	rows := make([]string, 0, len(m.subtaskKitPickerOptions))
	for index, opt := range m.subtaskKitPickerOptions {
		marker := m.cursorMarker(m.entityPicker.Cursor == index)
		dot := " "
		switch {
		case opt.IsNone && active == "":
			dot = "•"
		case !opt.IsNone && opt.Filename == active:
			dot = "•"
		}
		var row string
		if opt.IsNone {
			row = fmt.Sprintf("%s %s %s", marker, dot, m.styles.hint.Render(m.t("tui.picker.subtask_kit_none")))
		} else {
			row = fmt.Sprintf("%s %s %s  %s", marker, dot, opt.Display, m.styles.hint.Render(opt.Filename))
			if opt.IsCustom {
				row += " " + m.styles.badgeInfo.Render(m.t("tui.badge.custom"))
			}
		}
		rows = append(rows, row)
	}
	displayActive := active
	if displayActive == "" {
		displayActive = m.t("tui.picker.subtask_kit_none")
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.subtask_kit_active_fmt"), displayActive)),
		m.styles.hint.Render(m.t("tui.picker.hint.subtask_kit")),
		"",
	}
	return m.renderPickerPanel(header, rows, m.entityPicker.Scroll, m.pickerViewportRows())
}
