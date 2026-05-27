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
//
// RelativePath is the kit's identity inside the config dir
// (`foo.yaml` for a default entry, `custom/foo.yaml` for a user
// override). Comparing the picker's active row against the snapshot's
// SubtaskKitPath via this field instead of `filepath.Base(Filename)`
// is the locked behaviour from task #301 review §11557 finding B8 —
// a default `foo.yaml` and a `custom/foo.yaml` are distinct kits even
// though they share a basename, and treating them as the same row
// would silently land the active dot on the wrong entry.
type subtaskKitOption struct {
	Filename     string
	RelativePath string
	Display      string
	IsCustom     bool
	IsNone       bool
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
	active := m.currentSubtaskKitRelative()
	cursor := 0
	for i, opt := range options {
		switch {
		case opt.IsNone && active == "":
			cursor = i
		case opt.RelativePath == active:
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
//
// Each non-sentinel option carries both Filename (basename for
// display) and RelativePath (kit identity inside the config dir). The
// active-row picker uses RelativePath so a default `foo.yaml` and a
// `custom/foo.yaml` resolve to distinct rows (#301 review §11557
// finding B8).
func discoverSubtaskKitOptions(configDir string) ([]subtaskKitOption, error) {
	profiles, err := discoverConfigProfiles(configDir)
	if err != nil {
		return nil, err
	}
	out := make([]subtaskKitOption, 0, len(profiles)+1)
	out = append(out, subtaskKitOption{IsNone: true})
	for _, p := range profiles {
		out = append(out, subtaskKitOption{
			Filename:     p.Filename,
			RelativePath: profileRelativePath(p),
			Display:      p.Display,
			IsCustom:     p.IsCustom,
		})
	}
	return out, nil
}

// profileRelativePath returns the kit identity inside the config dir
// (`foo.yaml` for defaults, `custom/foo.yaml` for user overrides).
// Matches the SubtaskKit field shape `omakiten.yaml` carries so the
// active picker option compares cleanly with the snapshot's
// SubtaskKitPath without going through filepath.Base.
func profileRelativePath(p configOption) string {
	if p.IsCustom {
		return filepath.Join("custom", p.Filename)
	}
	return p.Filename
}

// currentSubtaskKitRelative returns the relative path of the sub-kit
// currently wired into omakiten.yaml (`foo.yaml` or
// `custom/foo.yaml`), or "" when no cascade is active. Used by the
// picker to land the active dot on the right row even when a default
// and a custom kit share a basename (#301 review §11557 finding B8).
func (m Model) currentSubtaskKitRelative() string {
	bundle, err := m.repos.Editor.Load()
	if err != nil {
		return ""
	}
	return bundle.SubtaskKit
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
// failure the YAML mutation is rolled back via the transactional
// helper (`applySubtaskKitWithRollback`) so disk AND runtime/cache
// state both return to the prior wiring — #301 review §11557 finding
// B9 closed the regression where the YAML was reverted but the cache
// had already rotated to the candidate snapshot.
func (m *Model) applySubtaskKitSelection() {
	if m.entityPicker.Cursor < 0 || m.entityPicker.Cursor >= len(m.subtaskKitPickerOptions) {
		return
	}
	chosen := m.subtaskKitPickerOptions[m.entityPicker.Cursor]
	relative := ""
	if !chosen.IsNone {
		relative = chosen.RelativePath
	}

	originalRelative, err := m.loadCurrentSubtaskKitRelative()
	if err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), err.Error())
		return
	}
	previousActive := m.currentSubtaskKitRelative()

	if applyErr := m.applySubtaskKitWithRollback(originalRelative, relative); applyErr != nil {
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), applyErr.Error())
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

// applySubtaskKitWithRollback is the transactional write/reload/
// rollback helper for the sub-task kit picker. The candidate write
// always runs first; on reload failure the helper rewrites the
// original `subtask_kit` value AND re-runs `reloadBundle` against the
// restored file so the runtime cache rotates back to the prior
// snapshot. Without the second reload the on-disk YAML would say
// "use the previous kit" while the cache still held the candidate's
// snapshot (#301 review §11557 finding B9).
//
// The second `reloadBundle` suppresses the bundle.swapped emit so the
// user is not bounced through a second orphan-migration prompt for a
// rollback they did not consent to (mirrors `revertConfigSwap`).
func (m *Model) applySubtaskKitWithRollback(originalRelative, candidateRelative string) error {
	if _, err := m.repos.Editor.Apply(m.ctx, func(bundle *config.Bundle) error {
		bundle.SubtaskKit = candidateRelative
		return nil
	}); err != nil {
		return err
	}
	if err := m.reloadBundle(m.repos.Editor.Path()); err != nil {
		if _, rerr := m.repos.Editor.Apply(m.ctx, func(bundle *config.Bundle) error {
			bundle.SubtaskKit = originalRelative
			return nil
		}); rerr != nil {
			return fmt.Errorf("%w (rollback yaml write also failed: %v)", err, rerr)
		}
		m.suppressNextSwapEmit = true
		if rerr := m.reloadBundle(m.repos.Editor.Path()); rerr != nil {
			return fmt.Errorf("%w (rollback reload also failed: %v)", err, rerr)
		}
		return err
	}
	return nil
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
//
// The active dot uses RelativePath identity, not the basename — a
// default `foo.yaml` and a `custom/foo.yaml` are distinct rows and
// only one carries the dot at a time (#301 review §11557 finding B8).
func (m Model) renderSubtaskKitPicker() string {
	active := m.currentSubtaskKitRelative()
	rows := make([]string, 0, len(m.subtaskKitPickerOptions))
	for index, opt := range m.subtaskKitPickerOptions {
		marker := m.cursorMarker(m.entityPicker.Cursor == index)
		dot := " "
		switch {
		case opt.IsNone && active == "":
			dot = "•"
		case !opt.IsNone && opt.RelativePath == active:
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
