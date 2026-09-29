package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omakiten/internal/paths"
)

// PresetResult reports the selected config and whether installation changed it.
type PresetResult struct {
	Path       string
	PresetName string
	NoOp       bool
	Refreshed  bool
}

// SeedFixture materializes the embedded Omakase fixture for tests and development.
func SeedFixture(rootDir string, force bool) (PresetResult, error) {
	if err := paths.ValidateNoSymlinkComponents(rootDir); err != nil {
		return PresetResult{}, fmt.Errorf("refusing config install root %s: %w", rootDir, err)
	}

	configDir := filepath.Join(rootDir, "config")
	activeName := "omakase.yaml"
	activePath := filepath.Join(configDir, activeName)

	existedBefore, err := pathExists(activePath)
	if err != nil {
		return PresetResult{}, err
	}
	previousActive, _ := readActiveMarker(configDir)

	if force {
		if err := RefreshDefaultFiles(rootDir); err != nil {
			return PresetResult{}, err
		}
	} else {
		if err := EnsureDefaultFiles(rootDir); err != nil {
			return PresetResult{}, err
		}
	}

	if err := paths.SetActiveConfigInDir(configDir, activeName); err != nil {
		return PresetResult{}, err
	}

	res := PresetResult{
		Path:       activePath,
		PresetName: "omakase",
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
	data, err := readFileBounded(filepath.Join(configDir, paths.ActiveConfigStateFile), MaxWiringFileBytes)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
