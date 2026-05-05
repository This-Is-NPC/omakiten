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
		m.status = "No themes found"
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
	m.entityForm = entityForm{
		mode:         entityScreenThemePicker,
		pickerCursor: cursor,
	}
	m.pickerScroll = 0
	m.status = "Theme picker"
}

func (m *Model) openConfigPicker() {
	options, err := discoverConfigProfiles(m.repos.Editor.ConfigDir())
	if err != nil {
		m.status = err.Error()
		return
	}
	if len(options) == 0 {
		m.status = "No config profiles found"
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
	m.entityForm = entityForm{
		mode:         entityScreenConfigPicker,
		pickerCursor: cursor,
	}
	m.pickerScroll = 0
	m.status = "Config picker"
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
// the canonical default first, then defaults alpha, then customs alpha.
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
		if out[i].Filename == paths.DefaultConfigFilename {
			return true
		}
		if out[j].Filename == paths.DefaultConfigFilename {
			return false
		}
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
	rowCount := len(m.themePickerOptions)
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.closeEntityScreen("Theme picker cancelled")
	case "up", "k":
		if m.entityForm.pickerCursor > 0 {
			m.entityForm.pickerCursor--
			m.syncPickerScroll(rowCount)
		}
	case "down", "j":
		if m.entityForm.pickerCursor < rowCount-1 {
			m.entityForm.pickerCursor++
			m.syncPickerScroll(rowCount)
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.pickerViewportRows())
		m.entityForm.pickerCursor -= step
		if m.entityForm.pickerCursor < 0 {
			m.entityForm.pickerCursor = 0
		}
		m.syncPickerScroll(rowCount)
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.pickerViewportRows())
		m.entityForm.pickerCursor += step
		if m.entityForm.pickerCursor > rowCount-1 {
			m.entityForm.pickerCursor = rowCount - 1
		}
		m.syncPickerScroll(rowCount)
	case "home", "g":
		m.entityForm.pickerCursor = 0
		m.syncPickerScroll(rowCount)
	case "end", "G":
		m.entityForm.pickerCursor = rowCount - 1
		m.syncPickerScroll(rowCount)
	case "enter":
		return m, m.applyThemeSelection()
	}
	return m, nil
}

// applyThemeSelection writes the chosen theme slug into the active yaml,
// re-imports the bundle, and reloads the theme + styles in place.
func (m *Model) applyThemeSelection() tea.Cmd {
	if m.entityForm.pickerCursor < 0 || m.entityForm.pickerCursor >= len(m.themePickerOptions) {
		return nil
	}
	chosen := m.themePickerOptions[m.entityForm.pickerCursor].Slug
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
	m.closeEntityScreen(fmt.Sprintf("Theme switched to %s", chosen))
	return nil
}

func (m Model) updateConfigPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rowCount := len(m.configPickerOptions)
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.closeEntityScreen("Config picker cancelled")
	case "up", "k":
		if m.entityForm.pickerCursor > 0 {
			m.entityForm.pickerCursor--
			m.syncPickerScroll(rowCount)
		}
	case "down", "j":
		if m.entityForm.pickerCursor < rowCount-1 {
			m.entityForm.pickerCursor++
			m.syncPickerScroll(rowCount)
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.pickerViewportRows())
		m.entityForm.pickerCursor -= step
		if m.entityForm.pickerCursor < 0 {
			m.entityForm.pickerCursor = 0
		}
		m.syncPickerScroll(rowCount)
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.pickerViewportRows())
		m.entityForm.pickerCursor += step
		if m.entityForm.pickerCursor > rowCount-1 {
			m.entityForm.pickerCursor = rowCount - 1
		}
		m.syncPickerScroll(rowCount)
	case "home", "g":
		m.entityForm.pickerCursor = 0
		m.syncPickerScroll(rowCount)
	case "end", "G":
		m.entityForm.pickerCursor = rowCount - 1
		m.syncPickerScroll(rowCount)
	case "enter":
		m.applyConfigSelection()
	}
	return m, nil
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
	return nil
}

// applyConfigSelection persists the user's config-profile choice and shows
// a restart-required hint. Hot-swapping the active config in place would
// require rebuilding repos and re-importing the bundle without restarting
// — out of scope for the current iteration.
func (m *Model) applyConfigSelection() {
	if m.entityForm.pickerCursor < 0 || m.entityForm.pickerCursor >= len(m.configPickerOptions) {
		return
	}
	chosen := m.configPickerOptions[m.entityForm.pickerCursor].Filename
	if err := paths.SetActiveConfig(chosen); err != nil {
		m.status = err.Error()
		return
	}
	display := strings.TrimSuffix(chosen, filepath.Ext(chosen))
	m.closeEntityScreen(fmt.Sprintf("Config switched to %s — restart TUI to apply", display))
}

func (m Model) renderThemePicker() string {
	contentWidth := m.availableWidth() - 4
	rows := make([]string, 0, len(m.themePickerOptions))
	for index, opt := range m.themePickerOptions {
		marker := normalMarker
		if m.entityForm.pickerCursor == index {
			marker = m.styles.marker.Render(selectionMarker)
		}
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
			row += " " + m.styles.badgeInfo.Render("CUSTOM")
		}
		rows = append(rows, row)
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf("Theme · current: %s", m.theme.Key)),
		m.styles.hint.Render("up/down: move · enter: apply (hot-reload) · esc: cancel"),
		"",
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	header = append(header, m.sliceScrollRows(rows, m.pickerScroll, m.pickerViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(header, "\n")), 2)
}

func (m Model) renderConfigPicker() string {
	contentWidth := m.availableWidth() - 4
	active := filepath.Base(m.repos.Editor.Path())
	rows := make([]string, 0, len(m.configPickerOptions))
	for index, opt := range m.configPickerOptions {
		marker := normalMarker
		if m.entityForm.pickerCursor == index {
			marker = m.styles.marker.Render(selectionMarker)
		}
		dot := " "
		if opt.Filename == active {
			dot = "•"
		}
		row := fmt.Sprintf("%s %s %s  %s", marker, dot, opt.Display, m.styles.hint.Render(opt.Filename))
		if opt.IsCustom {
			row += " " + m.styles.badgeInfo.Render("CUSTOM")
		}
		rows = append(rows, row)
	}
	header := []string{
		m.styles.kicker(fmt.Sprintf("Config profile · active: %s", active)),
		m.styles.hint.Render("up/down: move · enter: select (restart required) · esc: cancel"),
		"",
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	header = append(header, m.sliceScrollRows(rows, m.pickerScroll, m.pickerViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(header, "\n")), 2)
}
