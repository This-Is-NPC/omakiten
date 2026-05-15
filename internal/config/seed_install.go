package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omakiten/internal/paths"
)

// SeedResult describes the outcome of a SeedInstall call so callers can
// distinguish first-write / no-op / preset-switch / forced-refresh.
type SeedResult struct {
	// Path is the absolute path of the active yaml after the seed.
	Path string
	// PresetName is the preset whose yaml is now active.
	PresetName string
	// NoOp is true when the requested preset was already the active one
	// and no shipped file diverged enough to require a refresh — the call
	// changed nothing observable on disk.
	NoOp bool
	// Refreshed is true when force = true caused embedded shipped files to
	// be overwritten (the install previously existed).
	Refreshed bool
}

// SeedInstall materialises a complete config install under rootDir and points
// .active at the chosen preset. Layout produced (mirrors the global ConfigRoot
// shape) is:
//
//	rootDir/config/<preset>.yaml + .active
//	rootDir/<entity>/<slug>.md + custom/   (skills, laws, personas, templates)
//	rootDir/themes/<slug>.yaml + custom/
//	rootDir/notifications/<slug>.yaml + custom/
//
// Idempotence:
//   - First call on an empty rootDir materialises everything.
//   - Repeat call with the same preset and force = false is a no-op (NoOp = true).
//   - Repeat call with a different preset flips .active and returns the new
//     path (NoOp = false). Shipped files are not re-copied because they were
//     already present from the first call.
//   - force = true re-copies every embedded shipped file (skills, laws,
//     personas, templates, themes, notifications, and every preset yaml),
//     leaving any user-owned <kind>/custom/ subtree untouched. Refreshed = true.
//
// rootDir is created if missing. The caller is responsible for picking the
// right rootDir per scope: ConfigRoot() for global, <repo>/.omakiten for
// repo-local installs.
func SeedInstall(rootDir, presetName string, force bool) (SeedResult, error) {
	preset, ok := PresetByName(presetName)
	if !ok || presetName != filepath.Base(presetName) {
		return SeedResult{}, fmt.Errorf("%w: %s", ErrPresetNotFound, presetName)
	}

	configDir := filepath.Join(rootDir, "config")
	activeName := preset.Name + ".yaml"
	activePath := filepath.Join(configDir, activeName)

	existedBefore, err := pathExists(activePath)
	if err != nil {
		return SeedResult{}, err
	}
	previousActive, _ := readActiveMarker(configDir)

	if force {
		if err := RefreshDefaultFiles(rootDir); err != nil {
			return SeedResult{}, err
		}
	} else {
		if err := EnsureDefaultFiles(rootDir); err != nil {
			return SeedResult{}, err
		}
	}

	if err := paths.SetActiveConfigInDir(configDir, activeName); err != nil {
		return SeedResult{}, err
	}

	res := SeedResult{
		Path:       activePath,
		PresetName: preset.Name,
		Refreshed:  force && existedBefore,
		NoOp:       !force && existedBefore && previousActive == activeName,
	}
	return res, nil
}

func pathExists(p string) (bool, error) {
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func readActiveMarker(configDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(configDir, paths.ActiveConfigStateFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
