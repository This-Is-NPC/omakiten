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
// Idempotence: when the destination already exists and its bytes match the
// embedded preset, SeedPreset returns NoOp=true without touching the file.
// When the bytes diverge and force is false, returns ErrPresetTargetExists
// so the caller can prompt or fail; when force is true, the file is
// overwritten atomically.
func SeedPreset(scope Scope, name string, force bool, opts SeedOptions) (SeedResult, error) {
	dstRoot, configDir, err := scopeRoots(scope, opts)
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

	dstPath := filepath.Join(dstRoot, "config", preset.Name+".yaml")
	existing, statErr := os.Stat(dstPath)
	switch {
	case statErr == nil && !existing.IsDir():
		current, err := os.ReadFile(dstPath)
		if err != nil {
			return SeedResult{}, fmt.Errorf("read existing preset %s: %w", dstPath, err)
		}
		if bytes.Equal(current, srcData) {
			if err := paths.SetActiveConfigInDir(configDir, preset.Name+".yaml"); err != nil {
				return SeedResult{}, err
			}
			return SeedResult{Preset: preset, Path: dstPath, NoOp: true}, nil
		}
		if !force {
			return SeedResult{}, fmt.Errorf("%w: %s", ErrPresetTargetExists, dstPath)
		}
		if err := WriteAtomic(dstPath, srcData); err != nil {
			return SeedResult{}, fmt.Errorf("write preset file %s: %w", dstPath, err)
		}
		if err := paths.SetActiveConfigInDir(configDir, preset.Name+".yaml"); err != nil {
			return SeedResult{}, err
		}
		return SeedResult{Preset: preset, Path: dstPath, Overwritten: true}, nil
	case statErr != nil && !os.IsNotExist(statErr):
		return SeedResult{}, fmt.Errorf("stat preset target %s: %w", dstPath, statErr)
	}

	if err := WriteAtomic(dstPath, srcData); err != nil {
		return SeedResult{}, fmt.Errorf("write preset file %s: %w", dstPath, err)
	}
	if err := paths.SetActiveConfigInDir(configDir, preset.Name+".yaml"); err != nil {
		return SeedResult{}, err
	}
	return SeedResult{Preset: preset, Path: dstPath}, nil
}

// scopeRoots resolves (dstRoot, configDir) for a given scope. dstRoot is the
// directory CopyPreset / SeedPreset writes <root>/config/<preset>.yaml under;
// configDir is the absolute path of that config/ subdir, used for .active.
func scopeRoots(scope Scope, opts SeedOptions) (string, string, error) {
	switch scope {
	case ScopeGlobal:
		if opts.GlobalRoot == "" {
			return "", "", fmt.Errorf("seed preset: GlobalRoot required for scope %q", scope)
		}
		root := opts.GlobalRoot
		return root, filepath.Join(root, "config"), nil
	case ScopeLocal:
		if opts.LocalRoot == "" {
			return "", "", fmt.Errorf("seed preset: LocalRoot required for scope %q", scope)
		}
		root := filepath.Join(opts.LocalRoot, RepoLocalDirName)
		return root, filepath.Join(root, "config"), nil
	default:
		return "", "", fmt.Errorf("seed preset: unknown scope %q", scope)
	}
}
