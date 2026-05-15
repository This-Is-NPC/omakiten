package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"omakiten/defaults"
	"omakiten/internal/paths"
)

// Scope names the configuration layer a preset is being seeded into.
// The runtime resolution order in LoadBundle is fixed (kit -> user-global ->
// user-custom -> repo-local) and is not tunable by Scope; Scope only picks
// the destination directory for SeedPreset writes.
type Scope string

const (
	ScopeGlobal Scope = "global"
	ScopeLocal  Scope = "local"
)

// SeedOptions parameterises SeedPreset for both supported scopes.
//
// GlobalRoot is the user-global ConfigRoot (parent of <root>/config/) and is
// required for ScopeGlobal. LocalRoot is the directory the .omakiten/ overlay
// lives at — typically the CWD or a discovered repo root — and is required
// for ScopeLocal.
type SeedOptions struct {
	GlobalRoot string
	LocalRoot  string
}

// SeedResult describes the outcome of a SeedPreset call so callers can
// surface "wrote", "overwrote", or "no-op" to the user.
type SeedResult struct {
	Preset      Preset
	Path        string
	NoOp        bool
	Overwritten bool
}

// SeedPreset writes the named preset into the directory implied by scope
// and sets the .active marker so subsequent runtime loads pick it up.
//
// Both scopes share the same library layout: <root>/config/<name>.yaml +
// <root>/config/.active. ScopeGlobal targets ConfigRoot; ScopeLocal targets
// <repoRoot>/.omakiten/. The library file is invoked at runtime via
// --config <path>; auto-discovery of the repo-local overlay
// (<repoRoot>/.omakiten/omakiten.yaml) is a separate code path that this
// function deliberately does not touch — wholesale-preset overlays would
// collide with the global wiring on workflow ids, and fixing that needs a
// merge-rule change in config/wiring_merge.go.
//
// Idempotence: when the destination already exists and its bytes match the
// embedded preset, SeedPreset returns NoOp=true without touching the file.
// When the bytes diverge and force is false, returns ErrPresetTargetExists
// so the caller can prompt or fail; when force is true, the file is
// overwritten atomically.
func SeedPreset(scope Scope, name string, force bool, opts SeedOptions) (SeedResult, error) {
	target, err := scopeTarget(scope, opts, name)
	if err != nil {
		return SeedResult{}, err
	}

	preset, ok := PresetByName(name)
	if !ok || name != filepath.Base(name) {
		return SeedResult{}, fmt.Errorf("%w: %s", ErrPresetNotFound, name)
	}

	srcPath := filepath.ToSlash(filepath.Join("config", preset.Name+".yaml"))
	srcData, err := defaults.FS.ReadFile(srcPath)
	if err != nil {
		return SeedResult{}, fmt.Errorf("%w: %s", ErrPresetNotFound, name)
	}

	existing, statErr := os.Stat(target.path)
	switch {
	case statErr == nil && !existing.IsDir():
		current, err := os.ReadFile(target.path)
		if err != nil {
			return SeedResult{}, fmt.Errorf("read existing preset %s: %w", target.path, err)
		}
		if bytes.Equal(current, srcData) {
			if err := target.markActive(preset.Name); err != nil {
				return SeedResult{}, err
			}
			return SeedResult{Preset: preset, Path: target.path, NoOp: true}, nil
		}
		if !force {
			return SeedResult{}, fmt.Errorf("%w: %s", ErrPresetTargetExists, target.path)
		}
		if err := WriteAtomic(target.path, srcData); err != nil {
			return SeedResult{}, fmt.Errorf("write preset file %s: %w", target.path, err)
		}
		if err := target.markActive(preset.Name); err != nil {
			return SeedResult{}, err
		}
		return SeedResult{Preset: preset, Path: target.path, Overwritten: true}, nil
	case statErr != nil && !os.IsNotExist(statErr):
		return SeedResult{}, fmt.Errorf("stat preset target %s: %w", target.path, statErr)
	}

	if err := WriteAtomic(target.path, srcData); err != nil {
		return SeedResult{}, fmt.Errorf("write preset file %s: %w", target.path, err)
	}
	if err := target.markActive(preset.Name); err != nil {
		return SeedResult{}, err
	}
	return SeedResult{Preset: preset, Path: target.path}, nil
}

// seedTarget is the absolute path SeedPreset writes plus an optional
// .active-marker writer; ScopeLocal returns a no-op writer because the
// overlay layout has no library to select from.
type seedTarget struct {
	path       string
	markActive func(presetName string) error
}

func scopeTarget(scope Scope, opts SeedOptions, name string) (seedTarget, error) {
	switch scope {
	case ScopeGlobal:
		if opts.GlobalRoot == "" {
			return seedTarget{}, fmt.Errorf("seed preset: GlobalRoot required for scope %q", scope)
		}
		configDir := filepath.Join(opts.GlobalRoot, "config")
		return seedTarget{
			path: filepath.Join(configDir, name+".yaml"),
			markActive: func(presetName string) error {
				return paths.SetActiveConfigInDir(configDir, presetName+".yaml")
			},
		}, nil
	case ScopeLocal:
		if opts.LocalRoot == "" {
			return seedTarget{}, fmt.Errorf("seed preset: LocalRoot required for scope %q", scope)
		}
		configDir := filepath.Join(opts.LocalRoot, RepoLocalDirName, "config")
		return seedTarget{
			path: filepath.Join(configDir, name+".yaml"),
			markActive: func(presetName string) error {
				return paths.SetActiveConfigInDir(configDir, presetName+".yaml")
			},
		}, nil
	default:
		return seedTarget{}, fmt.Errorf("seed preset: unknown scope %q", scope)
	}
}
