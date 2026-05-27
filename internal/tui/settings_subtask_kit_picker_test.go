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

// TestSubtaskKitPickerDistinguishesDefaultFromCustomSameBasename pins
// task #301 review §11557 finding B8: a default `foo.yaml` and a
// `custom/foo.yaml` are distinct kits even though they share a
// basename. The active-row dot must land on the row whose full
// relative path matches what omakiten.yaml stamps as `subtask_kit:`,
// not on whichever row happens to come first alphabetically.
func TestSubtaskKitPickerDistinguishesDefaultFromCustomSameBasename(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	// Default kit at config-dir root.
	writeSubKitSibling(t, configDir, "izakaya.yaml")
	// Custom override that ships the SAME basename — different kit
	// identity, different RelativePath.
	if err := os.MkdirAll(filepath.Join(configDir, "custom"), 0o755); err != nil {
		t.Fatalf("MkdirAll(custom): %v", err)
	}
	customBundle := tuiTestBundle(t)
	customBundle.Kit.Key = "izakaya-custom"
	customBundle.Kit.Name = "izakaya (custom override)"
	if err := config.SaveFullBundle(filepath.Join(configDir, "custom", "izakaya.yaml"), customBundle); err != nil {
		t.Fatalf("SaveFullBundle(custom/izakaya): %v", err)
	}

	// Lock the active sub-kit to the CUSTOM override.
	model.openSubtaskKitPicker()
	customIdx := -1
	for i, opt := range model.subtaskKitPickerOptions {
		if opt.RelativePath == filepath.Join("custom", "izakaya.yaml") {
			customIdx = i
			break
		}
	}
	if customIdx < 0 {
		t.Fatalf("picker missing custom/izakaya.yaml; got %+v", model.subtaskKitPickerOptions)
	}
	model.entityPicker = model.entityPicker.WithCursor(customIdx, len(model.subtaskKitPickerOptions), 0)
	model.applySubtaskKitSelection()
	if model.status == "" || strings.Contains(strings.ToLower(model.status), "fail") {
		t.Fatalf("seed selection failed: %q", model.status)
	}

	// Re-open the picker — the active dot must land on the CUSTOM row
	// only, not on the default `izakaya.yaml` row.
	model.openSubtaskKitPicker()
	active := model.currentSubtaskKitRelative()
	if active != filepath.Join("custom", "izakaya.yaml") {
		t.Fatalf("currentSubtaskKitRelative = %q, want custom/izakaya.yaml (relative-path identity)", active)
	}
	// Confirm both rows exist with distinct RelativePath values.
	var defaultOpt, customOpt subtaskKitOption
	for _, opt := range model.subtaskKitPickerOptions {
		switch opt.RelativePath {
		case "izakaya.yaml":
			defaultOpt = opt
		case filepath.Join("custom", "izakaya.yaml"):
			customOpt = opt
		}
	}
	if defaultOpt.RelativePath == "" {
		t.Fatalf("picker missing default izakaya.yaml row; got %+v", model.subtaskKitPickerOptions)
	}
	if customOpt.RelativePath == "" {
		t.Fatalf("picker missing custom/izakaya.yaml row; got %+v", model.subtaskKitPickerOptions)
	}
	if defaultOpt.Filename == customOpt.Filename && defaultOpt.RelativePath == customOpt.RelativePath {
		t.Fatalf("default and custom rows collapsed into one identity: default=%+v custom=%+v", defaultOpt, customOpt)
	}
}

// TestSubtaskKitPickerRollbackRestoresRuntimeSnapshot pins task #301
// review §11557 finding B9: when the candidate sub-kit reload fails,
// rollback must restore disk AND runtime — the cache pointer should
// snap back to the prior bundle so the user is not stuck driving a
// runtime that holds a different snapshot than the YAML names. The
// pre-fix code only rewrote the YAML, leaving the cache rotated to
// the bad bundle.
func TestSubtaskKitPickerRollbackRestoresRuntimeSnapshot(t *testing.T) {
	model, root := newPickerModel(t)
	configDir := filepath.Join(root, "config")
	good := writeSubKitSibling(t, configDir, "izakaya.yaml")

	// Land the runtime on `good` first so we have a known prior snapshot.
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
		t.Fatalf("seed selection failed: %q", model.status)
	}
	priorSubPath := ""
	if snap := model.repos.activeSnapshot(); snap != nil {
		priorSubPath = snap.SubtaskKitPath()
	}
	if priorSubPath == "" {
		t.Fatal("runtime snapshot missing sub-kit after seed selection")
	}

	// Build a bad kit (nested cascade — the validator rejects it) so
	// the candidate reload fails.
	badName := "kaiseki.yaml"
	badBundle := tuiTestBundle(t)
	badBundle.Kit.Key = "kaiseki"
	badBundle.Kit.Name = "kaiseki"
	badBundle.SubtaskKit = "izakaya.yaml"
	if err := config.SaveFullBundle(filepath.Join(configDir, badName), badBundle); err != nil {
		t.Fatalf("SaveFullBundle(bad): %v", err)
	}

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
		t.Fatalf("expected failure status after bad selection, got %q", model.status)
	}

	// Runtime assertion: the active snapshot must still resolve the
	// prior sub-kit path. Without the runtime-reload step in the
	// rollback helper, the cache would still hold the bad bundle's
	// snapshot (or a half-rotated one).
	afterSnap := model.repos.activeSnapshot()
	if afterSnap == nil {
		t.Fatal("runtime snapshot is nil after rollback")
	}
	if got := afterSnap.SubtaskKitPath(); got != priorSubPath {
		t.Fatalf("runtime sub-kit path after rollback = %q, want %q (cache did not rotate back)", got, priorSubPath)
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
