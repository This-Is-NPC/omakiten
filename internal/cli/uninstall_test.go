package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/installer"
	"omakiten/internal/lifecycle"
	"omakiten/internal/paths"
)

// TestRunUninstall_DefaultPreservesDataAndConfig walks the AC §2 default:
// no purge flags → binary + wrappers removed, data + config left intact.
func TestRunUninstall_DefaultPreservesDataAndConfig(t *testing.T) {
	home := seedFakeInstall(t)

	res, err := runUninstall(context.Background(), uninstallInputs{})
	if err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "uninstall_completed" {
		t.Fatalf("code: got %v want uninstall_completed", payload["code"])
	}
	if payload["binary_removed"] != true {
		t.Fatalf("binary_removed: got %v want true", payload["binary_removed"])
	}
	wrappers := payload["wrappers"].([]string)
	if len(wrappers) != 1 {
		t.Fatalf("wrappers: got %v want 1 entry", wrappers)
	}
	if _, present := payload["data_dir"]; present {
		t.Fatalf("data_dir should be absent when --purge-data not set")
	}
	if _, present := payload["config_root"]; present {
		t.Fatalf("config_root should be absent when --purge-config not set")
	}

	// Confirm filesystem post-conditions: binary + wrapper gone, data + config intact.
	bin := lifecycle.BinaryPath(home)
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("binary still present at %s: %v", bin, err)
	}
	bashrc, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if bytes.Contains(bashrc, []byte(installer.WrapperBegin)) {
		t.Fatalf("wrapper still in bashrc: %s", bashrc)
	}
	dataDB := filepath.Join(home, ".data", "omakiten", "omakiten.db")
	if _, err := os.Stat(dataDB); err != nil {
		t.Fatalf("data dir should survive default uninstall: %v", err)
	}
	cfgYaml := filepath.Join(home, ".cfg", "omakiten", "config", "omakiten.yaml")
	if _, err := os.Stat(cfgYaml); err != nil {
		t.Fatalf("config root should survive default uninstall: %v", err)
	}
}

// TestRunUninstall_PurgeRemovesEverything walks the --purge convenience
// shorthand: every target including data + config goes.
func TestRunUninstall_PurgeRemovesEverything(t *testing.T) {
	home := seedFakeInstall(t)

	res, err := runUninstall(context.Background(), uninstallInputs{PurgeData: true, PurgeConfig: true})
	if err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["data_removed"] != true {
		t.Fatalf("data_removed: got %v want true", payload["data_removed"])
	}
	if payload["config_removed"] != true {
		t.Fatalf("config_removed: got %v want true", payload["config_removed"])
	}

	if _, err := os.Stat(filepath.Join(home, ".data", "omakiten")); !os.IsNotExist(err) {
		t.Fatalf("data dir survived --purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cfg", "omakiten")); !os.IsNotExist(err) {
		t.Fatalf("config root survived --purge: %v", err)
	}
}

// TestRunUninstall_MissingBinaryStillCompletes covers the bootstrap-
// failure scenario: user invokes uninstall when the installer never
// finished writing the binary. The command should still report
// uninstall_completed and just flag binary_removed=false.
func TestRunUninstall_MissingBinaryStillCompletes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(paths.HomeEnv, "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".cfg"))
	t.Setenv(lifecycle.InstallDirEnv, filepath.Join(home, ".local", "bin"))

	res, err := runUninstall(context.Background(), uninstallInputs{})
	if err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	payload, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("payload type: got %T want map[string]any", res)
	}
	if payload["code"] != "uninstall_completed" {
		t.Fatalf("code: got %v want uninstall_completed", payload["code"])
	}
	if payload["binary_removed"] != false {
		t.Fatalf("binary_removed: expected false on missing binary, got %v", payload["binary_removed"])
	}
}

// TestUninstallPicker_TogglesPurgeData drives the bubbletea model
// directly: cursor down to the data row, toggle, then apply.
func TestUninstallPicker_TogglesPurgeData(t *testing.T) {
	seedFakeInstall(t)

	model, err := newUninstallPickerModel(uninstallInputs{})
	if err != nil {
		t.Fatalf("newUninstallPickerModel: %v", err)
	}
	var current tea.Model = model
	current, _ = current.Update(tea.KeyMsg{Type: tea.KeyDown})
	current, _ = current.Update(tea.KeyMsg{Type: tea.KeyDown})
	current, _ = current.Update(tea.KeyMsg{Type: tea.KeyEnter})
	current, _ = current.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	final, ok := current.(uninstallPickerModel)
	if !ok {
		t.Fatalf("final model type: got %T want uninstallPickerModel", current)
	}
	if !final.done {
		t.Fatalf("picker did not signal done after y")
	}
	if !final.purgeData {
		t.Fatalf("purgeData should be toggled on after enter on data row")
	}
	if final.purgeCfg {
		t.Fatalf("purgeCfg should remain false")
	}
}

// TestUninstallPicker_CtrlCAborts pins the abort path so the JSON
// envelope reports a validation_error instead of running the lifecycle
// helpers on a half-confirmed picker.
func TestUninstallPicker_CtrlCAborts(t *testing.T) {
	seedFakeInstall(t)

	model, err := newUninstallPickerModel(uninstallInputs{})
	if err != nil {
		t.Fatalf("newUninstallPickerModel: %v", err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	final, ok := updated.(uninstallPickerModel)
	if !ok {
		t.Fatalf("final model type: got %T want uninstallPickerModel", updated)
	}
	if !final.aborted {
		t.Fatalf("expected aborted=true after ctrl+c")
	}
	if final.done {
		t.Fatalf("done should stay false on abort")
	}
}

// TestResolveUninstallInputs_NoTTYNoFlags pins the headless-without-
// flags failure: when nothing on the command-line resolves the purge
// intent and stdin is not a TTY, the JSON envelope must surface a
// validation_error instead of opening a bubbletea picker that would
// hang on the closed input.
func TestResolveUninstallInputs_NoTTYNoFlags(t *testing.T) {
	_, err := resolveUninstallInputs(context.Background(), uninstallInputs{}, false)
	if err == nil {
		t.Fatalf("expected validation_error when no flags + no TTY")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("error code: got %v want ErrValidation", err)
	}
}

// TestResolveUninstallInputs_ExplicitBypassesPicker covers the
// happy headless path: any explicit flag (including --yes) returns
// the inputs verbatim without touching the picker.
func TestResolveUninstallInputs_ExplicitBypassesPicker(t *testing.T) {
	got, err := resolveUninstallInputs(context.Background(), uninstallInputs{PurgeData: true}, true)
	if err != nil {
		t.Fatalf("resolveUninstallInputs: %v", err)
	}
	if !got.PurgeData || got.PurgeConfig {
		t.Fatalf("inputs: got %+v want PurgeData=true PurgeConfig=false", got)
	}
}

// TestRunUninstall_PurgeDataFailureSurfacesCodedError simulates a
// read-only data directory: PurgeDataDir hits an os.RemoveAll error
// and the failure must be reported as ErrUninstallFailed with the
// data_dir payload populated.
func TestRunUninstall_PurgeDataFailureSurfacesCodedError(t *testing.T) {
	if goruntime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based read-only directories don't gate root or windows ACLs")
	}
	home := seedFakeInstall(t)

	dataDir := filepath.Join(home, ".data", "omakiten")
	// 0o500 on the parent prevents removal of children; restore on
	// cleanup so t.TempDir's own cleanup can finish.
	parent := filepath.Dir(dataDir)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	_, err := runUninstall(context.Background(), uninstallInputs{PurgeData: true})
	if err == nil {
		t.Fatalf("expected error from read-only data dir")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrUninstallFailed {
		t.Fatalf("error: got %v want ErrUninstallFailed", err)
	}
}

// seedFakeInstall stands up a HOME-scoped fake install: binary at
// $HOME/.local/bin/okt, wrapper block in $HOME/.bashrc, data dir
// under XDG_DATA_HOME, config root under XDG_CONFIG_HOME. Returns
// the HOME path so callers can assert filesystem post-conditions.
func seedFakeInstall(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(paths.HomeEnv, "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".cfg"))
	t.Setenv(lifecycle.InstallDirEnv, filepath.Join(home, ".local", "bin"))

	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, lifecycle.BinaryName()), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# user content\n"), 0o644); err != nil {
		t.Fatalf("seed bashrc: %v", err)
	}
	if err := installer.InstallWrapper(bashrc); err != nil {
		t.Fatalf("install wrapper: %v", err)
	}

	dataDir := filepath.Join(home, ".data", "omakiten")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "omakiten.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	cfgDir := filepath.Join(home, ".cfg", "omakiten", "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir cfg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "omakiten.yaml"), []byte("name: x\n"), 0o644); err != nil {
		t.Fatalf("seed yaml: %v", err)
	}

	return home
}
