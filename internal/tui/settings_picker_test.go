package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/paths"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

const minimalThemeYAML = `version: 1
key: %s
name: %s
colors:
  background: "#000000"
  foreground: "#FFFFFF"
  primary: "#FF00FF"
  secondary: "#00FF00"
  border: "#444444"
  highlight: "#222222"
  error: "#FF0000"
`

// newPickerModel materializes a config root that ships two themes (catppuccin
// at root + ocean under custom/) and two yaml profiles (omakiten.yaml + a
// user variant) so the pickers have something to switch between.
func newPickerModel(t *testing.T) (Model, string) {
	t.Helper()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, tuiTestBundle()); err != nil {
		t.Fatalf("SaveFullBundle() error = %v", err)
	}

	// catppuccin theme at the default location (already referenced as active
	// by tuiTestBundle).
	writeThemeFile(t, filepath.Join(tmp, "themes", "catppuccin.yaml"), "catppuccin", "Catppuccin")
	// ocean theme under custom/ — exercises the merge path in discoverThemes.
	writeThemeFile(t, filepath.Join(tmp, "themes", "custom", "ocean.yaml"), "ocean", "Ocean")

	// A second config profile placed under custom/ — exercises the new
	// custom-overrides-default subtree the picker scans alongside the root.
	if err := os.MkdirAll(filepath.Join(tmp, "config", "custom"), 0o755); err != nil {
		t.Fatalf("MkdirAll(config/custom) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "config", "custom", "config-experiment.yaml"), []byte("# placeholder\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config-experiment.yaml) error = %v", err)
	}

	ctx := context.Background()
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	files := configstore.New()
	editor := app.NewBundleEditor(store, files, configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Workflow: app.NewWorkflowServiceFromStore(store), Comments: store, Dependencies: store, Entries: store, Config: store, Editor: editor,
		BundleStore: files, EntityFiles: files, Slugger: files,
	}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	return model, tmp
}

func writeThemeFile(t *testing.T, path, key, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	body := fmt.Sprintf(minimalThemeYAML, key, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func TestThemePickerListsDefaultsAndCustom(t *testing.T) {
	model, _ := newPickerModel(t)
	model.openThemePicker()

	if model.entityForm.mode != entityScreenThemePicker {
		t.Fatalf("picker mode = %v, want theme picker", model.entityForm.mode)
	}

	slugs := make([]string, len(model.themePickerOptions))
	customByIndex := map[int]bool{}
	for i, opt := range model.themePickerOptions {
		slugs[i] = opt.Slug
		customByIndex[i] = opt.IsCustom
	}
	if len(slugs) != 2 {
		t.Fatalf("themePickerOptions = %v, want [catppuccin ocean]", slugs)
	}
	if slugs[0] != "catppuccin" || customByIndex[0] {
		t.Fatalf("first option = %s (custom=%v), want catppuccin (default)", slugs[0], customByIndex[0])
	}
	if slugs[1] != "ocean" || !customByIndex[1] {
		t.Fatalf("second option = %s (custom=%v), want ocean (custom)", slugs[1], customByIndex[1])
	}
}

func TestThemePickerHotReloadsOnEnter(t *testing.T) {
	model, _ := newPickerModel(t)
	model = pressRune(t, model, '3') // switch to config view
	model = pressRune(t, model, 't')
	if model.entityForm.mode != entityScreenThemePicker {
		t.Fatalf("expected theme picker open, got %v", model.entityForm.mode)
	}

	// Move to ocean (second row) and apply.
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if model.entityForm.mode != entityScreenClosed {
		t.Fatalf("picker should close after selection, mode = %v", model.entityForm.mode)
	}
	if model.theme.Key != "ocean" {
		t.Fatalf("model.theme.Key = %q, want ocean (hot-reload failed)", model.theme.Key)
	}
	bundle, err := model.repos.Editor.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if bundle.Config.Theme.Active != "ocean" {
		t.Fatalf("bundle.Config.Theme.Active = %q, want ocean (yaml not persisted)", bundle.Config.Theme.Active)
	}
}

func TestConfigPickerListsProfilesExcludingStateFile(t *testing.T) {
	model, root := newPickerModel(t)
	// Touch the state file directly so we can confirm the picker filters it.
	if err := os.WriteFile(filepath.Join(root, "config", paths.ActiveConfigStateFile), []byte("omakiten.yaml\n"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	model.openConfigPicker()
	if model.entityForm.mode != entityScreenConfigPicker {
		t.Fatalf("picker mode = %v, want config picker", model.entityForm.mode)
	}

	files := make([]string, len(model.configPickerOptions))
	for i, opt := range model.configPickerOptions {
		files[i] = opt.Filename
	}
	if len(files) != 2 {
		t.Fatalf("configPickerOptions = %v, want 2 entries (.active filtered)", files)
	}
	if files[0] != "omakiten.yaml" {
		t.Fatalf("first option = %q, want omakiten.yaml (default first)", files[0])
	}
	if files[1] != "config-experiment.yaml" {
		t.Fatalf("second option = %q, want config-experiment.yaml", files[1])
	}
	if model.configPickerOptions[0].IsCustom {
		t.Fatalf("default profile incorrectly tagged as custom")
	}
	if !model.configPickerOptions[1].IsCustom {
		t.Fatalf("user profile under custom/ not tagged as custom")
	}
}

func TestThemePickerEscRestoresNavTabs(t *testing.T) {
	model, _ := newPickerModel(t)
	model.width = 200
	model.height = 60
	model = pressRune(t, model, '3')

	before := model.View()
	if !strings.Contains(before, "// SETTINGS") {
		t.Fatalf("nav bar missing before opening picker:\n%s", before)
	}

	model = pressRune(t, model, 't')
	model = pressKey(t, model, tea.KeyEsc)

	after := model.View()
	if !strings.Contains(after, "// SETTINGS") {
		t.Fatalf("nav bar missing after esc — bug repro:\n%s", after)
	}
	// Ensure every top-zone label is present so a degraded narrow-fallback
	// rendering does not pass as a successful restore.
	for _, want := range []string{"// TASKS", "// STATS", "// SETTINGS"} {
		if !strings.Contains(after, want) {
			t.Fatalf("nav zone %q missing after esc — visual degradation:\n%s", want, after)
		}
	}
}

func TestViewIsClampedToTerminalHeightPreservingHeaderAndFooter(t *testing.T) {
	model, _ := newPickerModel(t)
	// Force the Settings › General info card into a short terminal so the
	// renderer would otherwise scroll the header off the top.
	model.width = 200
	model.height = 18
	model = pressRune(t, model, '3')

	out := model.View()
	lines := strings.Split(out, "\n")
	if len(lines) > model.height {
		t.Fatalf("View() returned %d lines for height=%d — clamp failed", len(lines), model.height)
	}
	// Header (project breadcrumb) must remain at the top.
	if !strings.Contains(lines[1], "omakiten") {
		t.Fatalf("first content line missing project breadcrumb:\n%s", lines[1])
	}
	// Footer (keybinding hints) must remain at the bottom — Settings ›
	// General advertises `tab zones`, `,// subs`, and the theme/config
	// pickers, so any of those tokens proves the footer survived clamp.
	footer := strings.Join(lines[len(lines)-3:], "\n")
	if !strings.Contains(footer, "tab zones") && !strings.Contains(footer, "theme") {
		t.Fatalf("footer not anchored at bottom after clamp:\n%s", footer)
	}
}

func TestThemePickerEnterRestoresNavTabs(t *testing.T) {
	model, _ := newPickerModel(t)
	model.width = 200
	model.height = 60
	model = pressRune(t, model, '3')
	model = pressRune(t, model, 't')
	// Move to the second option then apply via enter.
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if model.entityScreen != entityScreenClosed {
		t.Fatalf("entityScreen = %v after enter, want closed", model.entityScreen)
	}
	out := model.View()
	for _, want := range []string{"// TASKS", "// STATS", "// SETTINGS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nav zone %q missing after apply:\n%s", want, out)
		}
	}
}

func TestThemePickerEscRestoresEntityScreenClosed(t *testing.T) {
	model, _ := newPickerModel(t)
	model = pressRune(t, model, '3')
	model = pressRune(t, model, 't')
	if model.entityScreen == entityScreenClosed {
		t.Fatalf("expected entityScreen != closed after opening picker")
	}
	if model.entityForm.mode != entityScreenThemePicker {
		t.Fatalf("expected theme picker mode, got %v", model.entityForm.mode)
	}

	model = pressKey(t, model, tea.KeyEsc)
	if model.entityScreen != entityScreenClosed {
		t.Fatalf("entityScreen = %v after esc, want closed (header would hide nav)", model.entityScreen)
	}
	if model.entityForm.mode != entityScreenClosed {
		t.Fatalf("entityForm.mode = %v after esc, want closed", model.entityForm.mode)
	}
}

func TestConfigPickerEscRestoresEntityScreenClosed(t *testing.T) {
	model, _ := newPickerModel(t)
	model = pressRune(t, model, '3')
	model = pressRune(t, model, 'c')
	if model.entityScreen == entityScreenClosed {
		t.Fatalf("expected entityScreen != closed after opening picker")
	}

	model = pressKey(t, model, tea.KeyEsc)
	if model.entityScreen != entityScreenClosed {
		t.Fatalf("entityScreen = %v after esc, want closed (header would hide nav)", model.entityScreen)
	}
}

func TestConfigPickerPersistsSelectionAndShowsRestartHint(t *testing.T) {
	model, root := newPickerModel(t)
	t.Setenv(paths.HomeEnv, root)
	t.Setenv("XDG_CONFIG_HOME", "")

	model = pressRune(t, model, '3')
	model = pressRune(t, model, 'c')
	if model.entityForm.mode != entityScreenConfigPicker {
		t.Fatalf("expected config picker open, got %v", model.entityForm.mode)
	}

	// Move to the experiment profile and apply.
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if model.entityForm.mode != entityScreenClosed {
		t.Fatalf("picker should close after selection, mode = %v", model.entityForm.mode)
	}
	if !strings.Contains(strings.ToLower(model.status), "restart") {
		t.Fatalf("status should mention restart, got %q", model.status)
	}
	got, err := paths.ActiveConfigFile()
	if err != nil {
		t.Fatalf("ActiveConfigFile() error = %v", err)
	}
	// User profile lives under custom/, so ActiveConfigFile must resolve there.
	want := filepath.Join(root, "config", "custom", "config-experiment.yaml")
	if got != want {
		t.Fatalf("ActiveConfigFile() = %q, want %q", got, want)
	}
}
