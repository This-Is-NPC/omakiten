package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/settingspicker"
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
	m.subtaskKitPickerScreen = settingspicker.New(settingspicker.SubtaskKit).Open(subtaskKitPickerPayload(options, active))
	m.pushScreen(screenhost.SubtaskKitPicker)
	m.status = m.t("tui.status.subtask_kit_picker")
}

func subtaskKitPickerPayload(options []subtaskKitOption, active string) settingspicker.Payload {
	rows := make([]settingspicker.Option, len(options))
	for i, option := range options {
		rows[i] = settingspicker.Option{
			Value: option.RelativePath, Label: option.Display, Detail: option.Filename,
			Custom: option.IsCustom, None: option.IsNone,
			Active: option.IsNone && active == "" || option.RelativePath == active,
		}
	}
	return settingspicker.Payload{Kind: settingspicker.SubtaskKit, Current: active, Options: rows}
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

// applySubtaskKitSelection writes the chosen sub-kit path into
// omakiten.yaml (or clears it for the "none" sentinel), then triggers
// the hot-reload path so #285's migration handler and #282's
// transparency notice fire through the cache rebuild. If reload fails,
// the current-path write remains published and the error is surfaced.
func (m *Model) applySubtaskKitSelection(relative string) {
	previousActive := m.currentSubtaskKitRelative()

	if _, err := bundledraft.ApplyPlanned(m.ctx, m.repos.Editor, func(bundle *config.Bundle) error {
		bundle.SubtaskKit = relative
		return nil
	}); err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), err.Error())
		return
	}
	if err := m.reloadBundle(m.repos.Editor.Path()); err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switch_failed_fmt"), err.Error())
		return
	}
	switch relative {
	case "":
		if previousActive == "" {
			m.popScreen()
			m.status = m.t("tui.status.subtask_kit_already_none")
		} else {
			m.popScreen()
			m.status = m.t("tui.status.subtask_kit_cleared")
		}
	default:
		display := strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative))
		m.popScreen()
		m.status = fmt.Sprintf(m.t("tui.status.subtask_kit_switched_fmt"), display)
	}
}
