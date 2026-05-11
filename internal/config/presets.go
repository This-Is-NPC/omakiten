package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"omakiten/defaults"
)

var (
	ErrPresetNotFound     = errors.New("preset not found")
	ErrPresetTargetExists = errors.New("preset target exists")
)

// Preset describes one official workflow starter file bundled with Omakiten.
type Preset struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

var officialPresets = []Preset{
	{Name: "omakase", Title: "Chef's choice", Description: "Balanced workflow with self-branch, resume, and documentation guards."},
	{Name: "izakaya", Title: "Casual", Description: "Backlog -> dev -> done with no guards. Good for spikes and personal projects."},
	{Name: "kaiseki", Title: "Multi-course", Description: "Adds requirements, planning, and docs columns with strict transition guards."},
	{Name: "shokunin", Title: "Artisan", Description: "Kaiseki plus tests-passing and peer-review checkpoints for maximum traceability."},
}

// ListPresets returns the official presets in menu order.
func ListPresets() []Preset {
	out := make([]Preset, len(officialPresets))
	copy(out, officialPresets)
	return out
}

// PresetByName resolves an official preset by its stable filename stem.
func PresetByName(name string) (Preset, bool) {
	name = strings.TrimSpace(name)
	for _, preset := range officialPresets {
		if preset.Name == name {
			return preset, true
		}
	}
	return Preset{}, false
}

// CopyPreset writes defaults/config/<name>.yaml as <dstRoot>/config/<name>.yaml
// and returns the preset metadata together with the absolute destination path.
func CopyPreset(name, dstRoot string, overwrite bool) (Preset, string, error) {
	preset, ok := PresetByName(name)
	if !ok || name != filepath.Base(name) {
		return Preset{}, "", fmt.Errorf("%w: %s", ErrPresetNotFound, name)
	}

	srcPath := filepath.ToSlash(filepath.Join("config", preset.Name+".yaml"))
	data, err := defaults.FS.ReadFile(srcPath)
	if err != nil {
		return Preset{}, "", fmt.Errorf("%w: %s", ErrPresetNotFound, name)
	}

	dstPath := filepath.Join(dstRoot, "config", preset.Name+".yaml")
	if _, err := os.Stat(dstPath); err == nil {
		if !overwrite {
			return Preset{}, "", fmt.Errorf("%w: %s", ErrPresetTargetExists, dstPath)
		}
	} else if !os.IsNotExist(err) {
		return Preset{}, "", fmt.Errorf("stat preset target %s: %w", dstPath, err)
	}

	if err := WriteAtomic(dstPath, data); err != nil {
		return Preset{}, "", fmt.Errorf("write preset file %s: %w", dstPath, err)
	}
	return preset, dstPath, nil
}
