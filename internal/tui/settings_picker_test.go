package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/events"
	"omakiten/internal/paths"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/settingspicker"
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
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, tuiTestBundle(t)); err != nil {
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
	store := snapstore.Open(t, dbPath)

	files := configstore.New()
	editor := bundleeditor.New(files, configPath)
	if _, err := applyBundleEditor(ctx, editor, nil); err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	bus := events.NewInProcessBus(config.EventsSettings{})
	cache := agentruntime.NewBundleCache(store.Store, bus, files)
	if _, err := cache.Resolve(ctx, project.ID, configPath); err != nil {
		t.Fatalf("cache.Resolve: %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:    store,
		Comments: store, Dependencies: store, Editor: editor,
		BundleStore: files,
		Events:      store,
		Orphans:     store,
		Cache:       cache,
		ProjectID:   project.ID,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

func TestSettingsPickersOpenAsRegisteredChildScreens(t *testing.T) {
	model, _ := newPickerModel(t)
	for _, tc := range []struct {
		open func(*Model)
		id   screenhost.ID
		kind settingspicker.Kind
	}{
		{(*Model).openThemePicker, screenhost.ThemePicker, settingspicker.Theme},
		{(*Model).openConfigPicker, screenhost.ConfigPicker, settingspicker.Config},
		{(*Model).openSubtaskKitPicker, screenhost.SubtaskKitPicker, settingspicker.SubtaskKit},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			candidate := model
			tc.open(&candidate)
			if len(candidate.screenStack) != 1 || candidate.screenStack[0] != tc.id {
				t.Fatalf("screen stack = %v, want [%s]", candidate.screenStack, tc.id)
			}
			hosted, ok := candidate.hostedScreen(tc.id)
			pickerScreen, typed := hosted.(settingspicker.Screen)
			if !ok || !typed || pickerScreen.Payload().Kind != tc.kind || len(pickerScreen.Payload().Options) == 0 {
				t.Fatalf("hosted picker = %T/%+v ok=%v", hosted, pickerScreen.Payload(), ok)
			}
		})
	}
}

func TestThemePickerListsDefaultsAndCustom(t *testing.T) {
	model, _ := newPickerModel(t)
	model.openThemePicker()

	options := model.themePickerScreen.Payload().Options
	slugs := make([]string, len(options))
	customByIndex := map[int]bool{}
	for i, opt := range options {
		slugs[i] = opt.Value
		customByIndex[i] = opt.Custom
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
	model = pressRune(t, model, '4') // switch to config view
	model = pressRune(t, model, 't')
	if !pickerRouteOpen(model, screenhost.ThemePicker) {
		t.Fatalf("expected theme picker open, stack=%v", model.screenStack)
	}

	// Move to ocean (second row) and apply.
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if pickerRouteOpen(model, screenhost.ThemePicker) {
		t.Fatalf("picker should close after selection, stack=%v", model.screenStack)
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
	if err := os.WriteFile(filepath.Join(root, "config", paths.ActiveConfigStateFile), []byte("omakase.yaml\n"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	// newPickerModel already wrote omakase.yaml at config/; add a second
	// default preset so the picker has two root-scope options to list
	// alongside the custom/ entry seeded by the helper.
	if err := os.WriteFile(filepath.Join(root, "config", "izakaya.yaml"), []byte("# official preset\n"), 0o644); err != nil {
		t.Fatalf("write izakaya profile: %v", err)
	}

	model.openConfigPicker()
	options := model.configPickerScreen.Payload().Options
	files := make([]string, len(options))
	for i, opt := range options {
		files[i] = opt.Value
	}
	if len(files) != 3 {
		t.Fatalf("configPickerOptions = %v, want 3 entries (.active filtered)", files)
	}
	// Alphabetical order: defaults first, then custom. No special casing.
	if files[0] != "izakaya.yaml" {
		t.Fatalf("first option = %q, want izakaya.yaml", files[0])
	}
	if files[1] != "omakase.yaml" {
		t.Fatalf("second option = %q, want omakase.yaml", files[1])
	}
	if files[2] != "config-experiment.yaml" {
		t.Fatalf("third option = %q, want config-experiment.yaml", files[2])
	}
	if options[0].Custom {
		t.Fatalf("default profile incorrectly tagged as custom")
	}
	if options[1].Custom {
		t.Fatalf("default profile incorrectly tagged as custom")
	}
	if !options[2].Custom {
		t.Fatalf("user profile under custom/ not tagged as custom")
	}
}

func TestThemePickerEscRestoresNavTabs(t *testing.T) {
	model, _ := newPickerModel(t)
	model.width = 200
	model.height = 60
	model = pressRune(t, model, '4')

	before := model.View()
	if !strings.Contains(before, "SETTINGS") {
		t.Fatalf("nav bar missing before opening picker:\n%s", before)
	}

	model = pressRune(t, model, 't')
	model = pressKey(t, model, tea.KeyEsc)

	after := model.View()
	if !strings.Contains(after, "SETTINGS") {
		t.Fatalf("nav bar missing after esc — bug repro:\n%s", after)
	}
	// Ensure every top-zone label is present so a degraded narrow-fallback
	// rendering does not pass as a successful restore.
	for _, want := range []string{"TASKS", "STATS", "SETTINGS"} {
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
	model = pressRune(t, model, '4')

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
	model = pressRune(t, model, '4')
	model = pressRune(t, model, 't')
	// Move to the second option then apply via enter.
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if pickerRouteOpen(model, screenhost.ThemePicker) {
		t.Fatalf("theme picker remained open after enter: %v", model.screenStack)
	}
	out := model.View()
	for _, want := range []string{"TASKS", "STATS", "SETTINGS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("nav zone %q missing after apply:\n%s", want, out)
		}
	}
}

func TestThemePickerEscRestoresEntityScreenClosed(t *testing.T) {
	model, _ := newPickerModel(t)
	model = pressRune(t, model, '4')
	model = pressRune(t, model, 't')
	if !pickerRouteOpen(model, screenhost.ThemePicker) {
		t.Fatalf("expected theme picker route, stack=%v", model.screenStack)
	}

	model = pressKey(t, model, tea.KeyEsc)
	if pickerRouteOpen(model, screenhost.ThemePicker) {
		t.Fatalf("theme picker remained open after esc: %v", model.screenStack)
	}
	if !strings.Contains(strings.ToLower(model.status), "cancel") {
		t.Fatalf("cancel status = %q", model.status)
	}
}

func TestThemePickerRefreshReprojectsCandidatesWithoutIOInScreen(t *testing.T) {
	model, root := newPickerModel(t)
	model.openThemePicker()
	writeThemeFile(t, filepath.Join(root, "themes", "new-theme.yaml"), "new-theme", "New Theme")
	model = pressRune(t, model, 'r')
	var found bool
	for _, option := range model.themePickerScreen.Payload().Options {
		found = found || option.Value == "new-theme"
	}
	if !found || !pickerRouteOpen(model, screenhost.ThemePicker) {
		t.Fatalf("refresh payload/route = %+v / %v", model.themePickerScreen.Payload(), model.screenStack)
	}
	if !strings.Contains(strings.ToLower(model.status), "refresh") {
		t.Fatalf("refresh status = %q", model.status)
	}
}

func TestThemePickerInvalidThemeKeepsPublishedSelection(t *testing.T) {
	model, root := newPickerModel(t)
	brokenPath := filepath.Join(root, "themes", "broken.yaml")
	if err := os.WriteFile(brokenPath, []byte("version: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	model.openThemePicker()
	model.applySettingsPicker(screenhost.Action{EntityKind: string(settingspicker.Theme), Value: "broken"})
	bundle, err := model.repos.Editor.Load()
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Config.Theme.Active != "broken" || model.theme.Key != "catppuccin" {
		t.Fatalf("invalid theme state: yaml=%q runtime=%q", bundle.Config.Theme.Active, model.theme.Key)
	}
	if !pickerRouteOpen(model, screenhost.ThemePicker) || model.status == "" {
		t.Fatalf("invalid theme should stay open with status: stack=%v status=%q", model.screenStack, model.status)
	}
}

func TestConfigPickerEscRestoresEntityScreenClosed(t *testing.T) {
	model, _ := newPickerModel(t)
	model = pressRune(t, model, '4')
	model = pressRune(t, model, 'c')
	if !pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("expected config picker route, stack=%v", model.screenStack)
	}

	model = pressKey(t, model, tea.KeyEsc)
	if pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("config picker remained open after esc: %v", model.screenStack)
	}
}

func TestConfigPickerHotReloadsOnEnter(t *testing.T) {
	model, root := newPickerModel(t)
	t.Setenv(paths.HomeEnv, root)
	t.Setenv("XDG_CONFIG_HOME", "")

	// Overwrite the placeholder experiment file with a real bundle that
	// flips the active theme, so the hot-reload's visible effect is the
	// theme key change on the Model.
	experimentBundle := tuiTestBundle(t)
	experimentBundle.Kit.Key = "experiment"
	experimentBundle.Kit.Name = "Experiment"
	experimentBundle.Config.Theme.Active = "ocean"
	experimentPath := filepath.Join(root, "config", "custom", "config-experiment.yaml")
	if err := config.SaveFullBundle(experimentPath, experimentBundle); err != nil {
		t.Fatalf("SaveFullBundle(experiment) error = %v", err)
	}

	model = pressRune(t, model, '4')
	model = pressRune(t, model, 'c')
	if !pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("expected config picker open, stack=%v", model.screenStack)
	}

	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("picker should close after successful selection, stack=%v; status=%q", model.screenStack, model.status)
	}
	if strings.Contains(strings.ToLower(model.status), "restart") {
		t.Fatalf("status must not mention restart after hot-reload, got %q", model.status)
	}
	if model.theme.Key != "ocean" {
		t.Fatalf("model.theme.Key = %q, want ocean (theme not refreshed from new bundle)", model.theme.Key)
	}
	if got := filepath.Base(model.repos.Editor.Path()); got != "config-experiment.yaml" {
		t.Fatalf("editor.Path basename = %q, want config-experiment.yaml", got)
	}
	got, err := paths.ActiveConfigFile()
	if err != nil {
		t.Fatalf("ActiveConfigFile() error = %v", err)
	}
	want := filepath.Join(root, "config", "custom", "config-experiment.yaml")
	if got != want {
		t.Fatalf("ActiveConfigFile() = %q, want %q", got, want)
	}
	local, err := paths.ActiveConfigFileInDir(filepath.Join(root, "config"))
	if err != nil {
		t.Fatalf("ActiveConfigFileInDir() error: %v", err)
	}
	if local != want {
		t.Fatalf("repo-local active config = %q, want %q after restart resolution", local, want)
	}
}

func TestConfigPickerMarkerFailureKeepsOldRuntimeAndRestartMarker(t *testing.T) {
	model, root := newPickerModel(t)
	t.Setenv(paths.HomeEnv, root)
	t.Setenv("XDG_CONFIG_HOME", "")

	experiment := tuiTestBundle(t)
	experiment.Kit.Key = "experiment"
	experiment.Config.Theme.Active = "ocean"
	experimentPath := filepath.Join(root, "config", "custom", "config-experiment.yaml")
	if err := config.SaveFullBundle(experimentPath, experiment); err != nil {
		t.Fatalf("SaveFullBundle(experiment): %v", err)
	}
	if err := paths.SetActiveConfigInDir(filepath.Join(root, "config"), "omakase.yaml"); err != nil {
		t.Fatalf("SetActiveConfigInDir(old): %v", err)
	}
	oldMarker, err := os.ReadFile(filepath.Join(root, "config", paths.ActiveConfigStateFile))
	if err != nil {
		t.Fatalf("Read old marker: %v", err)
	}
	oldRuntime := model.repos.Cache.Get(model.project.ID)
	oldPath := model.repos.Editor.Path()
	oldTheme := model.theme.Key

	previousSetter := setActiveConfigInDir
	setActiveConfigInDir = func(string, string) error { return errors.New("marker is unwritable") }
	t.Cleanup(func() { setActiveConfigInDir = previousSetter })

	model.openConfigPicker()
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if model.repos.Cache.Get(model.project.ID) != oldRuntime {
		t.Fatal("marker failure changed the live runtime")
	}
	if model.repos.Editor.Path() != oldPath || model.theme.Key != oldTheme {
		t.Fatalf("marker failure changed live model: path=%q theme=%q", model.repos.Editor.Path(), model.theme.Key)
	}
	gotMarker, err := os.ReadFile(filepath.Join(root, "config", paths.ActiveConfigStateFile))
	if err != nil {
		t.Fatalf("Read restored marker: %v", err)
	}
	if string(gotMarker) != string(oldMarker) {
		t.Fatalf("marker after failure = %q, want %q", gotMarker, oldMarker)
	}
	if !pickerRouteOpen(model, screenhost.ConfigPicker) || model.status == "" {
		t.Fatalf("marker failure should keep picker open with status: stack=%v status=%q", model.screenStack, model.status)
	}
}

func TestConfigPickerKeepsStateOnInvalidBundle(t *testing.T) {
	model, root := newPickerModel(t)
	t.Setenv(paths.HomeEnv, root)
	t.Setenv("XDG_CONFIG_HOME", "")

	originalPath := model.repos.Editor.Path()
	originalThemeKey := model.theme.Key

	model = pressRune(t, model, '4')
	model = pressRune(t, model, 'c')
	if !pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("expected config picker open, stack=%v", model.screenStack)
	}

	// The experiment file from newPickerModel is "# placeholder\n" — an
	// invalid bundle. Hot-reload rejects it while the current path remains
	// published.
	model = pressStringKey(t, model, "down")
	model = pressKey(t, model, tea.KeyEnter)

	if !pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("picker must stay open on invalid bundle, stack=%v", model.screenStack)
	}
	if !strings.Contains(strings.ToLower(model.status), "config switch failed") {
		t.Fatalf("status should describe the failure, got %q", model.status)
	}
	if model.repos.Editor.Path() != originalPath {
		t.Fatalf("editor.Path changed despite import failure: %q != %q", model.repos.Editor.Path(), originalPath)
	}
	if model.theme.Key != originalThemeKey {
		t.Fatalf("theme.Key changed despite import failure: %q != %q", model.theme.Key, originalThemeKey)
	}
	marker, err := os.ReadFile(filepath.Join(root, "config", paths.ActiveConfigStateFile))
	if err != nil || !strings.Contains(string(marker), "experiment") {
		t.Fatalf(".active was not published for the selected path: err=%v marker=%q", err, marker)
	}
}

func TestConfigPickerRejectsValueOutsideDiscoveredCandidates(t *testing.T) {
	model, _ := newPickerModel(t)
	original := model.repos.Editor.Path()
	model.openConfigPicker()
	model.applySettingsPicker(screenhost.Action{EntityKind: string(settingspicker.Config), Value: "../../outside.yaml"})
	if model.repos.Editor.Path() != original || !pickerRouteOpen(model, screenhost.ConfigPicker) {
		t.Fatalf("forged candidate changed host state: path=%q stack=%v", model.repos.Editor.Path(), model.screenStack)
	}
	if model.status != "invalid config picker selection" {
		t.Fatalf("forged candidate reached host I/O, status = %q", model.status)
	}
}

func pickerRouteOpen(model Model, id screenhost.ID) bool {
	return len(model.screenStack) > 0 && model.screenStack[len(model.screenStack)-1] == id
}
