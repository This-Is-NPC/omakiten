package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
)

// writeSubKitSibling materializes a second kit file under the config dir
// the picker can select. Returns its basename.
func writeSubKitSibling(t *testing.T, configDir, name string) string {
	t.Helper()
	bundle := tuiTestBundle(t)
	bundle.Kit.Key = strings.TrimSuffix(name, filepath.Ext(name))
	bundle.Kit.Name = bundle.Kit.Key
	path := filepath.Join(configDir, name)
	if err := config.SaveFullBundle(path, bundle); err != nil {
		t.Fatalf("SaveFullBundle(%s): %v", path, err)
	}
	return name
}

// TestSubtaskKitPickerListsProfilesPlusNoneSentinel pins AC §2: the
// picker lists installed kit files plus a "none (inherit root)"
// sentinel option so the user can disable the cascade without editing
// the YAML by hand.
func TestSubtaskKitPickerListsProfilesPlusNoneSentinel(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	writeSubKitSibling(t, configDir, "izakaya.yaml")

	model.openSubtaskKitPicker()
	if model.entityForm.mode != entityScreenSubtaskKitPicker {
		t.Fatalf("picker mode = %v, want sub-task kit picker", model.entityForm.mode)
	}

	if len(model.subtaskKitPickerOptions) < 2 {
		t.Fatalf("picker options = %d, want ≥ 2 (at least one kit + the none sentinel); got %+v", len(model.subtaskKitPickerOptions), model.subtaskKitPickerOptions)
	}
	var sawNone, sawIzakaya bool
	for _, opt := range model.subtaskKitPickerOptions {
		if opt.IsNone {
			sawNone = true
		}
		if opt.Filename == "izakaya.yaml" {
			sawIzakaya = true
		}
	}
	if !sawNone {
		t.Fatalf("picker missing 'none (inherit root)' sentinel; got %+v", model.subtaskKitPickerOptions)
	}
	if !sawIzakaya {
		t.Fatalf("picker missing izakaya.yaml from config dir; got %+v", model.subtaskKitPickerOptions)
	}
}

// TestSubtaskKitPickerWritesYAMLOnSelect pins AC §3: selecting a kit
// writes `subtask_kit: <relative-path>` into the active omakiten.yaml
// via the shared BundleEditor. Asserts the on-disk YAML round-trips
// through the loader so the cache rebuild picks the value up.
func TestSubtaskKitPickerWritesYAMLOnSelect(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	subKit := writeSubKitSibling(t, configDir, "izakaya.yaml")

	model.openSubtaskKitPicker()
	cursor := -1
	for i, opt := range model.subtaskKitPickerOptions {
		if opt.Filename == subKit {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		t.Fatalf("sub-kit picker missing izakaya.yaml; got %+v", model.subtaskKitPickerOptions)
	}
	model.entityPicker = model.entityPicker.WithCursor(cursor, len(model.subtaskKitPickerOptions), 0)
	model.applySubtaskKitSelection()

	if model.status == "" || strings.Contains(strings.ToLower(model.status), "fail") {
		t.Fatalf("apply selection surfaced failure status: %q", model.status)
	}

	configPath := model.repos.Editor.Path()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", configPath, err)
	}
	if !strings.Contains(string(raw), "subtask_kit: "+subKit) {
		t.Fatalf("omakiten.yaml missing `subtask_kit: %s`; got:\n%s", subKit, raw)
	}
}

// TestSubtaskKitPickerNoneClearsKey pins AC §3 (clear path): selecting
// the "none" sentinel removes `subtask_kit:` from omakiten.yaml so
// the cascade disables cleanly without editing the YAML by hand.
func TestSubtaskKitPickerNoneClearsKey(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	subKit := writeSubKitSibling(t, configDir, "izakaya.yaml")

	model.openSubtaskKitPicker()
	cursor := -1
	for i, opt := range model.subtaskKitPickerOptions {
		if opt.Filename == subKit {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		t.Fatalf("sub-kit picker missing %s; got %+v", subKit, model.subtaskKitPickerOptions)
	}
	model.entityPicker = model.entityPicker.WithCursor(cursor, len(model.subtaskKitPickerOptions), 0)
	model.applySubtaskKitSelection()

	model.openSubtaskKitPicker()
	noneIdx := -1
	for i, opt := range model.subtaskKitPickerOptions {
		if opt.IsNone {
			noneIdx = i
			break
		}
	}
	if noneIdx < 0 {
		t.Fatalf("sub-kit picker missing none sentinel; got %+v", model.subtaskKitPickerOptions)
	}
	model.entityPicker = model.entityPicker.WithCursor(noneIdx, len(model.subtaskKitPickerOptions), 0)
	model.applySubtaskKitSelection()

	raw, err := os.ReadFile(model.repos.Editor.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "subtask_kit:") {
		t.Fatalf("omakiten.yaml still carries `subtask_kit:` after selecting none; got:\n%s", raw)
	}
}

// TestSubtaskKitPickerRollsBackOnReloadFailure pins the rollback
// contract introduced after the #281 review: selecting a sub-kit
// file that fails to load must restore the prior subtask_kit value
// in omakiten.yaml so on-disk wiring never diverges from the
// runtime snapshot. Picker stays open so the operator can retry.
func TestSubtaskKitPickerRollsBackOnReloadFailure(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	// Materialize a sub-kit file that load-validates fine to seed the
	// "previously configured" state.
	good := writeSubKitSibling(t, configDir, "izakaya.yaml")
	// Materialize a sub-kit file whose own subtask_kit key is set so
	// the validator rejects it (nested cascade is not allowed) — this
	// drives the reload failure path without standing up a malformed
	// YAML by hand.
	badName := "kaiseki.yaml"
	badBundle := tuiTestBundle(t)
	badBundle.Kit.Key = "kaiseki"
	badBundle.Kit.Name = "kaiseki"
	badBundle.SubtaskKit = "izakaya.yaml"
	if err := config.SaveFullBundle(filepath.Join(configDir, badName), badBundle); err != nil {
		t.Fatalf("SaveFullBundle(bad): %v", err)
	}

	// First selection: lock the prior sub-kit to "good".
	model.openSubtaskKitPicker()
	cursor := -1
	for i, opt := range model.subtaskKitPickerOptions {
		if opt.Filename == good {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		t.Fatalf("picker missing %s", good)
	}
	model.entityPicker = model.entityPicker.WithCursor(cursor, len(model.subtaskKitPickerOptions), 0)
	model.applySubtaskKitSelection()
	if model.status == "" || strings.Contains(strings.ToLower(model.status), "fail") {
		t.Fatalf("seed selection unexpectedly failed: %q", model.status)
	}

	// Second selection: pick the malformed sub-kit. Reload rejects it.
	model.openSubtaskKitPicker()
	cursor = -1
	for i, opt := range model.subtaskKitPickerOptions {
		if opt.Filename == badName {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		t.Fatalf("picker missing %s", badName)
	}
	model.entityPicker = model.entityPicker.WithCursor(cursor, len(model.subtaskKitPickerOptions), 0)
	model.applySubtaskKitSelection()
	if !strings.Contains(strings.ToLower(model.status), "fail") {
		t.Fatalf("expected failure status after malformed selection, got %q", model.status)
	}

	// Rollback assertion: the YAML still references the prior good
	// sub-kit, not the malformed one.
	raw, err := os.ReadFile(model.repos.Editor.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "subtask_kit: "+good) {
		t.Fatalf("omakiten.yaml missing prior `subtask_kit: %s` after rollback; got:\n%s", good, raw)
	}
	if strings.Contains(string(raw), "subtask_kit: "+badName) {
		t.Fatalf("rollback failed: omakiten.yaml still carries `subtask_kit: %s`; got:\n%s", badName, raw)
	}
}

// TestSubtaskKitPickerEscClosesPicker mirrors the root-config picker's
// cancel path so muscle memory stays consistent across the settings
// menu.
func TestSubtaskKitPickerEscClosesPicker(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	writeSubKitSibling(t, configDir, "izakaya.yaml")

	model.openSubtaskKitPicker()
	if model.entityForm.mode != entityScreenSubtaskKitPicker {
		t.Fatalf("open failed: mode = %v", model.entityForm.mode)
	}

	model = pressKey(t, model, tea.KeyEsc)
	if model.entityScreen != entityScreenClosed {
		t.Fatalf("entityScreen after esc = %v, want closed", model.entityScreen)
	}
}
