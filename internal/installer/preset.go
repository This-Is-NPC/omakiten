package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"omakiten/internal/config"
)

// ReadPreset captures a local directory, Git URL or official catalog entry.
func ReadPreset(ctx context.Context, source string) (config.PresetPackage, error) {
	if preset, ok := config.PresetByName(source); ok {
		source = preset.Repository
	}
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		path, err := filepath.Abs(source)
		if err != nil {
			return config.PresetPackage{}, err
		}
		return readPresetSource(path, path)
	}
	if !strings.Contains(source, "://") && !strings.HasPrefix(source, "git@") {
		return config.PresetPackage{}, fmt.Errorf("%w: source %q is not a directory, Git URL or catalog name; run `okt preset catalog` or pass a local directory", config.ErrPresetNotFound, source)
	}
	stage, err := os.MkdirTemp("", "okt-preset-")
	if err != nil {
		return config.PresetPackage{}, err
	}
	defer os.RemoveAll(stage)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	checkout := filepath.Join(stage, "repository")
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--", source, checkout)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		return config.PresetPackage{}, fmt.Errorf("fetch preset %q: %w: %s; check the repository URL and Git access, or install a local checkout with `okt preset add <directory>`", source, err, strings.TrimSpace(string(output)))
	}
	return readPresetSource(checkout, source)
}

func readPresetSource(path, source string) (config.PresetPackage, error) {
	p, err := config.ReadPresetDirectory(path)
	if err == nil && p.Manifest.Origin.Source == "" {
		p.Manifest.Origin.Source = source
	}
	return p, err
}

// InstallPreset selects an installed preset or captures its source repository.
// Updates retain an active modified preset and publish pristine revisions separately.
func InstallPreset(ctx context.Context, root, source string, update bool) (config.PresetResult, error) {
	items, err := config.InstalledPresets(root)
	if err != nil {
		return config.PresetResult{}, err
	}
	active := activePresetSource(items, source)
	if active != nil && (!update || active.Dirty) {
		return presetInstallResult(root, *active, true, false), nil
	}
	if active != nil && active.Manifest.Origin.Source != "" {
		source = active.Manifest.Origin.Source
	}
	if !update {
		if selected, found, err := selectInstalledPreset(root, items, source); found || err != nil {
			return presetInstallResult(root, selected, false, false), err
		}
	}
	p, err := ReadPreset(ctx, source)
	if err != nil {
		return config.PresetResult{}, err
	}
	item, err := config.InstallPreset(root, p)
	if err != nil {
		return config.PresetResult{}, err
	}
	if _, err := config.ActivatePreset(root, item.ID); err != nil {
		return config.PresetResult{}, err
	}
	return presetInstallResult(root, item, active != nil && active.ID == item.ID, update && len(items) > 0), nil
}

func activePresetSource(items []config.PresetInstallation, source string) *config.PresetInstallation {
	for i := range items {
		item := &items[i]
		if item.Active && (item.ID == source || item.Manifest.Name == source || strings.TrimSuffix(item.Manifest.Name, "-local") == source || item.Manifest.Origin.Source == source) {
			return item
		}
	}
	return nil
}

func selectInstalledPreset(root string, items []config.PresetInstallation, source string) (config.PresetInstallation, bool, error) {
	for _, item := range items {
		if item.Manifest.Name == source || item.ID == source {
			selected, err := config.ActivatePreset(root, source)
			return selected, true, err
		}
	}
	return config.PresetInstallation{}, false, nil
}

func presetInstallResult(root string, item config.PresetInstallation, noOp, refreshed bool) config.PresetResult {
	return config.PresetResult{Path: filepath.Join(root, config.PresetSelectionFile), PresetName: item.Manifest.Name, NoOp: noOp, Refreshed: refreshed}
}
