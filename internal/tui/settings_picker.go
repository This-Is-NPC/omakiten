package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/paths"
	"omakiten/internal/tui/components/picker"
)

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
	cursor := 0
	for i, opt := range options {
		if opt.Slug == m.theme.Key {
			cursor = i
			break
		}
	}
	m.themePickerOptions = options
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{mode: entityScreenThemePicker}
	// picker.WithCursor routes the open-time seed cursor through one
	// typed mutator that clamps + follow-scrolls; the prior raw field
	// write left scroll at whatever stale value the prior open
	// happened to land on.
	m.entityPicker = picker.New(picker.Single).WithCursor(cursor, len(options), 0)
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
	cursor := 0
	for i, opt := range options {
		if opt.Filename == active {
			cursor = i
			break
		}
	}
	m.configPickerOptions = options
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{mode: entityScreenConfigPicker}
	m.entityPicker = picker.New(picker.Single).WithCursor(cursor, len(options), 0)
	m.status = m.t("tui.status.config_picker")
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

// updateThemePicker handles input while the theme picker is open. Returns
// the model + any tea.Cmd to dispatch (the editorFinishedMsg post-write
// reuses the same enrichment path as the entity flows).
func (m Model) updateThemePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.entityPicker, cmd = m.entityPicker.Update(msg, len(m.themePickerOptions), scrollDataRows(m.pickerViewportRows()))
	switch m.entityPicker.LastEvent() {
	case picker.EventCancel:
		m.closeEntityScreen(m.t("tui.status.theme_picker_cancelled"))
	case picker.EventSelect:
		// Evaluate the side-effecting call before reading m for the return
		// tuple — Go does not specify the order of non-function operands
		// against intervening function calls, and the pointer-receiver method
		// must run before m is captured for the returned tea.Model.
		applied := m.applyThemeSelection()
		return m, applied
	}
	return m, cmd
}

// applyThemeSelection writes the chosen theme slug into the active yaml,
// re-imports the bundle, and reloads the theme + styles in place.
func (m *Model) applyThemeSelection() tea.Cmd {
	if m.entityPicker.Cursor < 0 || m.entityPicker.Cursor >= len(m.themePickerOptions) {
		return nil
	}
	chosen := m.themePickerOptions[m.entityPicker.Cursor].Slug
	if _, err := m.repos.Editor.Apply(m.ctx, func(bundle *config.Bundle) error {
		bundle.Config.Theme.Active = chosen
		return nil
	}); err != nil {
		m.status = err.Error()
		return nil
	}
	if err := m.reloadTheme(); err != nil {
		m.status = err.Error()
		return nil
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return nil
	}
	m.closeEntityScreen(fmt.Sprintf(m.t("tui.status.theme_switched_fmt"), chosen))
	return nil
}

func (m Model) updateConfigPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.entityPicker, cmd = m.entityPicker.Update(msg, len(m.configPickerOptions), scrollDataRows(m.pickerViewportRows()))
	switch m.entityPicker.LastEvent() {
	case picker.EventCancel:
		m.closeEntityScreen(m.t("tui.status.config_picker_cancelled"))
	case picker.EventSelect:
		m.applyConfigSelection()
	}
	return m, cmd
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
	// Rebuild the markdown renderer so cached body renders pick up the
	// new palette on the next View() — the cache lives inside the
	// renderer, so swapping the pointer is enough to invalidate it.
	m.markdown = newMarkdownRenderer(tokensFromTheme(theme))
	return nil
}

// applyConfigSelection imports the chosen workflow preset in place: it
// re-imports the new bundle into SQLite, repoints the editor at the new yaml,
// refreshes every bundle-derived field on the Model (theme, styles, markdown,
// priorities/severities, registry, notifications, token badge, workflow
// service), and re-queries the task snapshot. On any failure the DB and the
// .active state file stay untouched and the error surfaces in m.status so
// the user can retry without leaving the TUI in a half-applied state.
func (m *Model) applyConfigSelection() {
	if m.entityPicker.Cursor < 0 || m.entityPicker.Cursor >= len(m.configPickerOptions) {
		return
	}
	chosen := m.configPickerOptions[m.entityPicker.Cursor].Filename
	newPath := m.resolveConfigPath(chosen)

	if err := m.reloadBundle(newPath); err != nil {
		m.status = fmt.Sprintf(m.t("tui.status.config_switch_failed_fmt"), chosen, err.Error())
		return
	}
	if err := paths.SetActiveConfig(chosen); err != nil {
		m.status = err.Error()
		return
	}
	display := strings.TrimSuffix(chosen, filepath.Ext(chosen))
	m.closeEntityScreen(fmt.Sprintf(m.t("tui.status.config_switched_fmt"), display))
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

func (m Model) renderThemePicker() string {
	rows := make([]string, 0, len(m.themePickerOptions))
	for index, opt := range m.themePickerOptions {
		marker := m.cursorMarker(m.entityPicker.Cursor == index)
		active := " "
		if opt.Slug == m.theme.Key {
			active = "•"
		}
		label := opt.Name
		if label == "" {
			label = opt.Slug
		}
		row := fmt.Sprintf("%s %s %s", marker, active, label)
		if opt.Slug != label {
			row += "  " + m.styles.hint.Render(opt.Slug)
		}
		if opt.IsCustom {
			row += " " + m.styles.badgeInfo.Render(m.t("tui.badge.custom"))
		}
		rows = append(rows, row)
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.theme_current_fmt"), m.theme.Key)),
		m.styles.hint.Render(m.t("tui.picker.hint.theme")),
		"",
	}
	return m.renderPickerPanel(header, rows, m.entityPicker.Scroll, m.pickerViewportRows())
}

func (m Model) renderConfigPicker() string {
	active := filepath.Base(m.repos.Editor.Path())
	rows := make([]string, 0, len(m.configPickerOptions))
	for index, opt := range m.configPickerOptions {
		marker := m.cursorMarker(m.entityPicker.Cursor == index)
		dot := " "
		if opt.Filename == active {
			dot = "•"
		}
		row := fmt.Sprintf("%s %s %s  %s", marker, dot, opt.Display, m.styles.hint.Render(opt.Filename))
		if opt.IsCustom {
			row += " " + m.styles.badgeInfo.Render(m.t("tui.badge.custom"))
		}
		rows = append(rows, row)
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.config_active_fmt"), active)),
		m.styles.hint.Render(m.t("tui.picker.hint.theme")),
		"",
	}
	return m.renderPickerPanel(header, rows, m.entityPicker.Scroll, m.pickerViewportRows())
}
