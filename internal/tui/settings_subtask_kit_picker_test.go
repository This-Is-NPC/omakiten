package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/tui/screenhost"
)

func writeSubKitSibling(t *testing.T, configDir, name string) string {
	t.Helper()
	bundle := tuiTestBundle(t)
	bundle.Kit.Key = strings.TrimSuffix(name, filepath.Ext(name))
	bundle.Kit.Name = bundle.Kit.Key
	if err := config.SaveFullBundle(filepath.Join(configDir, name), bundle); err != nil {
		t.Fatalf("SaveFullBundle(%s): %v", name, err)
	}
	return name
}

func selectSubtaskKit(t *testing.T, model *Model, value string) {
	t.Helper()
	model.subtaskKitPickerScreen = model.subtaskKitPickerScreen.Select(value)
	if selected := model.subtaskKitPickerScreen.Selected(); selected.Value != value {
		t.Fatalf("selected = %+v, want %q", selected, value)
	}
	model.applySubtaskKitSelection(value)
}

func TestSubtaskKitPickerListsProfilesPlusNoneSentinel(t *testing.T) {
	model, root := newPickerModel(t)
	writeSubKitSibling(t, filepath.Join(root, "config"), "izakaya.yaml")
	model.openSubtaskKitPicker()
	if !pickerRouteOpen(model, screenhost.SubtaskKitPicker) {
		t.Fatalf("picker stack = %v", model.screenStack)
	}
	var sawNone, sawIzakaya bool
	for _, option := range model.subtaskKitPickerScreen.Payload().Options {
		sawNone = sawNone || option.None
		sawIzakaya = sawIzakaya || option.Value == "izakaya.yaml"
	}
	if !sawNone || !sawIzakaya {
		t.Fatalf("options = %+v, want none and izakaya", model.subtaskKitPickerScreen.Payload().Options)
	}
}

func TestSubtaskKitPickerWritesAndClearsYAML(t *testing.T) {
	model, root := newPickerModel(t)
	kit := writeSubKitSibling(t, filepath.Join(root, "config"), "izakaya.yaml")
	model.openSubtaskKitPicker()
	selectSubtaskKit(t, &model, kit)
	raw, err := os.ReadFile(model.repos.Editor.Path())
	if err != nil || !strings.Contains(string(raw), "subtask_kit: "+kit) {
		t.Fatalf("selected kit not persisted: err=%v\n%s", err, raw)
	}
	model.openSubtaskKitPicker()
	selectSubtaskKit(t, &model, "")
	raw, err = os.ReadFile(model.repos.Editor.Path())
	if err != nil || strings.Contains(string(raw), "subtask_kit:") {
		t.Fatalf("none did not clear kit: err=%v\n%s", err, raw)
	}
}

func TestSubtaskKitPickerKeepsRuntimeOnInvalidReload(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	good := writeSubKitSibling(t, configDir, "izakaya.yaml")
	bad := "kaiseki.yaml"
	badBundle := tuiTestBundle(t)
	badBundle.Kit.Key, badBundle.Kit.Name = "kaiseki", "kaiseki"
	badBundle.SubtaskKit = good
	if err := config.SaveFullBundle(filepath.Join(configDir, bad), badBundle); err != nil {
		t.Fatal(err)
	}

	model.openSubtaskKitPicker()
	selectSubtaskKit(t, &model, good)
	priorPath := model.repos.activeSnapshot().SubtaskKitPath()
	model.openSubtaskKitPicker()
	selectSubtaskKit(t, &model, bad)
	if !strings.Contains(strings.ToLower(model.status), "fail") || !pickerRouteOpen(model, screenhost.SubtaskKitPicker) {
		t.Fatalf("invalid apply status/route = %q / %v", model.status, model.screenStack)
	}
	raw, err := os.ReadFile(model.repos.Editor.Path())
	if err != nil || !strings.Contains(string(raw), "subtask_kit: "+bad) {
		t.Fatalf("published invalid config missing: err=%v\n%s", err, raw)
	}
	if got := model.repos.activeSnapshot().SubtaskKitPath(); got != priorPath {
		t.Fatalf("runtime changed after failed reload = %q, want %q", got, priorPath)
	}
}

func TestSubtaskKitPickerDistinguishesDefaultAndCustomSameBasename(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	writeSubKitSibling(t, configDir, "izakaya.yaml")
	if err := os.MkdirAll(filepath.Join(configDir, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := tuiTestBundle(t)
	custom.Kit.Key, custom.Kit.Name = "izakaya-custom", "Izakaya custom"
	if err := config.SaveFullBundle(filepath.Join(configDir, "custom", "izakaya.yaml"), custom); err != nil {
		t.Fatal(err)
	}
	model.openSubtaskKitPicker()
	customPath := filepath.Join("custom", "izakaya.yaml")
	selectSubtaskKit(t, &model, customPath)
	model.openSubtaskKitPicker()
	if model.currentSubtaskKitRelative() != customPath || model.subtaskKitPickerScreen.Selected().Value != customPath {
		t.Fatalf("custom identity lost: current=%q selected=%+v", model.currentSubtaskKitRelative(), model.subtaskKitPickerScreen.Selected())
	}
}

func TestSubtaskKitPickerEscClosesChildScreen(t *testing.T) {
	model, root := newPickerModel(t)
	writeSubKitSibling(t, filepath.Join(root, "config"), "izakaya.yaml")
	model.openSubtaskKitPicker()
	model = pressKey(t, model, tea.KeyEsc)
	if pickerRouteOpen(model, screenhost.SubtaskKitPicker) {
		t.Fatalf("picker remained open: %v", model.screenStack)
	}
}
