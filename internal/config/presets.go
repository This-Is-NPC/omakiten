package config

import (
	"errors"
	"strings"
)

var (
	ErrPresetNotFound = errors.New("preset not found")
)

// Preset describes one official workflow starter file bundled with Omakiten.
// Per task #82 §13, the human-facing title and description are resolved
// through `Snapshot.Catalog(CLI)` keys `cli.preset.<name>.{title,description}`
// at render time. The struct stays language-free so the config package
// never carries English literals.
type Preset struct {
	Name string `json:"name"`
}

var officialPresets = []Preset{
	{Name: "omakase"},
	{Name: "izakaya"},
	{Name: "kaiseki"},
	{Name: "shokunin"},
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
