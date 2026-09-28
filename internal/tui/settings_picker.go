package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/paths"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/settingspicker"
)

var setActiveConfigInDir = paths.SetActiveConfigInDir

// themeOption is one row in the theme picker. Slug is the basename without
// the `.yaml` extension; IsCustom indicates that the file lives under
// themes/custom/ rather than at themes/ root.
type themeOption struct {
	Slug     string
	Name     string
	IsCustom bool
}

// configOption is one row in the config-profile picker. Filename includes
// the trailing `.yaml`; Display strips the extension for nicer rendering.
// IsCustom marks profiles loaded from <config-dir>/custom — they are
// preserved across default refreshes and rendered with a CUSTOM badge.
type configOption struct {
	Filename string
	Display  string
	IsCustom bool
}

// openThemePicker discovers every theme on disk and seeds the picker state.
// Hot-reload: selecting a theme rewrites the active yaml's theme.active and
// re-imports immediately, so the change is visible without restart.
func (m *Model) openThemePicker() {
	options, err := discoverThemes(m.repos.Editor.RootDir())
	if err != nil {
		m.status = err.Error()
		return
	}
	if len(options) == 0 {
		m.status = m.t("tui.status.no_themes_found")
		return
	}
	m.themePickerScreen = settingspicker.New(settingspicker.Theme).Open(themePickerPayload(options, m.theme.Key))
	m.pushScreen(screenhost.ThemePicker)
	m.status = m.t("tui.status.theme_picker")
}

func (m *Model) openConfigPicker() {
	options, err := discoverConfigProfiles(m.repos.Editor.ConfigDir())
	if err != nil {
		m.status = err.Error()
		return
	}
	if len(options) == 0 {
		m.status = m.t("tui.status.no_config_profiles")
		return
	}
	active := filepath.Base(m.repos.Editor.Path())
	m.configPickerScreen = settingspicker.New(settingspicker.Config).Open(configPickerPayload(options, active))
	m.pushScreen(screenhost.ConfigPicker)
	m.status = m.t("tui.status.config_picker")
}

func themePickerPayload(options []themeOption, active string) settingspicker.Payload {
	rows := make([]settingspicker.Option, len(options))
	for i, option := range options {
		rows[i] = settingspicker.Option{Value: option.Slug, Label: option.Name, Detail: option.Slug, Custom: option.IsCustom, Active: option.Slug == active}
	}
	return settingspicker.Payload{Kind: settingspicker.Theme, Current: active, Options: rows}
}

func configPickerPayload(options []configOption, active string) settingspicker.Payload {
	rows := make([]settingspicker.Option, len(options))
	for i, option := range options {
		rows[i] = settingspicker.Option{Value: option.Filename, Label: option.Display, Detail: option.Filename, Custom: option.IsCustom, Active: option.Filename == active}
	}
	return settingspicker.Payload{Kind: settingspicker.Config, Current: active, Options: rows}
}

// discoverThemes scans <root>/themes (defaults) + <root>/themes/custom for
// *.yaml files, parses just enough metadata to render the picker, and
// returns a stable-sorted list with defaults first.
func discoverThemes(rootDir string) ([]themeOption, error) {
	root := filepath.Join(rootDir, "themes")
	defaults, err := readThemeFiles(root, false)
	if err != nil {
		return nil, err
	}
	customs, err := readThemeFiles(filepath.Join(root, "custom"), true)
	if err != nil {
		return nil, err
	}
	out := append(defaults, customs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsCustom != out[j].IsCustom {
			return !out[i].IsCustom
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

func readThemeFiles(dir string, isCustom bool) ([]themeOption, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []themeOption
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			continue
		}
		slug := strings.TrimSuffix(name, filepath.Ext(name))
		display := slug
		if theme, err := config.LoadTheme(filepath.Join(dir, name)); err == nil && strings.TrimSpace(theme.Name) != "" {
			display = theme.Name
		}
		out = append(out, themeOption{Slug: slug, Name: display, IsCustom: isCustom})
	}
	return out, nil
}

// discoverConfigProfiles lists every yaml profile available to the picker:
// defaults at the config-dir root + user profiles under custom/. Custom
// entries are tagged so the picker can render the CUSTOM badge. Ordering:
// defaults alpha, then customs alpha. No file is privileged by name.
func discoverConfigProfiles(configDir string) ([]configOption, error) {
	defaults, err := readYAMLProfilesIn(configDir, false)
	if err != nil {
		return nil, err
	}
	customs, err := readYAMLProfilesIn(filepath.Join(configDir, "custom"), true)
	if err != nil {
		return nil, err
	}
	out := append(defaults, customs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsCustom != out[j].IsCustom {
			return !out[i].IsCustom
		}
		return out[i].Filename < out[j].Filename
	})
	return out, nil
}

func readYAMLProfilesIn(dir string, isCustom bool) ([]configOption, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []configOption
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == paths.ActiveConfigStateFile {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			continue
		}
		out = append(out, configOption{
			Filename: name,
			Display:  strings.TrimSuffix(name, filepath.Ext(name)),
			IsCustom: isCustom,
		})
	}
	return out, nil
}

// applyThemeSelection writes the chosen theme slug into the active yaml,
// re-imports the bundle, and reloads the theme + styles in place.
func (m *Model) applyThemeSelection(chosen string) {
	if chosen == "" {
		return
	}
	if _, err := applyBundleEditor(m.ctx, m.repos.Editor, func(bundle *config.Bundle) error {
		bundle.Config.Theme.Active = chosen
		return nil
	}); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.reloadTheme(); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.popScreen()
	m.status = fmt.Sprintf(m.t("tui.status.theme_switched_fmt"), chosen)
}

// reloadTheme re-reads the theme yaml referenced by the (just-saved) active
// bundle and rebuilds the lipgloss style set in place. Looks up custom/
// override first, then defaults — same resolution order as the CLI's
// loadActiveTheme helper.
func (m *Model) reloadTheme() error {
	bundle, err := m.repos.Editor.Load()
	if err != nil {
		return err
	}
	root := m.repos.Editor.RootDir()
	active := bundle.Config.Theme.Active
	customPath := filepath.Join(root, "themes", "custom", active+".yaml")
	defaultPath := filepath.Join(root, "themes", active+".yaml")
	themePath := defaultPath
	if _, err := os.Stat(customPath); err == nil {
		themePath = customPath
	}
	theme, err := config.LoadTheme(themePath)
	if err != nil {
		return err
	}
	m.theme = theme
	m.styles = newStyles(theme)
	// Kit.Markdown tokens rotate with the theme; each screen Reloads its
	// Renderer on the next paint so the StyleConfig follows without a
	// process-wide cache.
	return nil
}

// applyConfigSelection imports the chosen workflow preset in place: it
// stages the new bundle, repoints the editor at the new yaml,
// refreshes every bundle-derived field on the Model (theme, styles, markdown,
// priorities/severities, registry, notifications, token badge, workflow
// service), and re-queries the task snapshot. On any failure the DB and the
// .active state file stay untouched and the error surfaces in m.status so
// the user can retry without leaving the TUI in a half-applied state.
func (m *Model) applyConfigSelection(chosen string) {
	if chosen == "" {
		return
	}
	newPath := m.resolveConfigPath(chosen)
	configDir := m.repos.Editor.ConfigDir()
	if err := setActiveConfigInDir(configDir, chosen); err != nil {
		m.status = err.Error()
		return
	}

	if err := m.reloadBundle(newPath); err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.config_switch_failed_fmt"), chosen, err.Error())
		return
	}
	display := strings.TrimSuffix(chosen, filepath.Ext(chosen))
	m.popScreen()
	m.status = fmt.Sprintf(m.t("tui.status.config_switched_fmt"), display)
}

// resolveConfigPath mirrors paths.ActiveConfigFile's custom/<name> →
// root/<name> resolution without writing `.active`. Used by the swap path so
// the new bundle can be validated before the on-disk pointer is moved.
func (m *Model) resolveConfigPath(filename string) string {
	dir := m.repos.Editor.ConfigDir()
	customPath := filepath.Join(dir, "custom", filename)
	if _, err := os.Stat(customPath); err == nil {
		return customPath
	}
	return filepath.Join(dir, filename)
}

func (m *Model) applySettingsPicker(action screenhost.Action) {
	kind := settingspicker.Kind(action.EntityKind)
	if !m.settingsPickerAllows(kind, action.Value) {
		m.status = fmt.Sprintf("invalid %s picker selection", kind)
		return
	}
	switch kind {
	case settingspicker.Theme:
		m.applyThemeSelection(action.Value)
	case settingspicker.Config:
		m.applyConfigSelection(action.Value)
	case settingspicker.SubtaskKit:
		m.applySubtaskKitSelection(action.Value)
	}
}

func (m Model) settingsPickerAllows(kind settingspicker.Kind, value string) bool {
	var screen settingspicker.Screen
	switch kind {
	case settingspicker.Theme:
		screen = m.themePickerScreen
	case settingspicker.Config:
		screen = m.configPickerScreen
	case settingspicker.SubtaskKit:
		screen = m.subtaskKitPickerScreen
	default:
		return false
	}
	if !pickerRouteIDOpen(m, kind.ID()) {
		return false
	}
	for _, option := range screen.Payload().Options {
		if option.Value == value && (value != "" || option.None) {
			return true
		}
	}
	return false
}

func pickerRouteIDOpen(m Model, id screenhost.ID) bool {
	return len(m.screenStack) > 0 && m.screenStack[len(m.screenStack)-1] == id
}

func (m *Model) refreshSettingsPicker(screen settingspicker.Screen) {
	switch screen.Payload().Kind {
	case settingspicker.Theme:
		options, err := discoverThemes(m.repos.Editor.RootDir())
		if err != nil {
			m.status = err.Error()
			return
		}
		m.themePickerScreen = screen.Refresh(themePickerPayload(options, m.theme.Key), nil)
	case settingspicker.Config:
		active := filepath.Base(m.repos.Editor.Path())
		options, err := discoverConfigProfiles(m.repos.Editor.ConfigDir())
		if err != nil {
			m.status = err.Error()
			return
		}
		m.configPickerScreen = screen.Refresh(configPickerPayload(options, active), nil)
	case settingspicker.SubtaskKit:
		options, err := discoverSubtaskKitOptions(m.repos.Editor.ConfigDir())
		if err != nil {
			m.status = err.Error()
			return
		}
		m.subtaskKitPickerScreen = screen.Refresh(subtaskKitPickerPayload(options, m.currentSubtaskKitRelative()), nil)
	}
	m.status = m.t("tui.status.refreshed")
}
