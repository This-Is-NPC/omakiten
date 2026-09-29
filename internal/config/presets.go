package config

import (
	"errors"
	"strings"
)

var (
	ErrPresetNotFound = errors.New("preset not found")
)

// Preset identifies an official workflow repository. Delivery surfaces resolve
// its title and description through the language catalog.
type Preset struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
}

var officialPresets = []Preset{
	{Name: "omakase", Repository: "https://github.com/This-Is-NPC/okt-workflow-omakase.git"},
	{Name: "izakaya", Repository: "https://github.com/This-Is-NPC/okt-workflow-izakaya.git"},
	{Name: "kaiseki", Repository: "https://github.com/This-Is-NPC/okt-workflow-kaiseki.git"},
	{Name: "shokunin", Repository: "https://github.com/This-Is-NPC/okt-workflow-shokunin.git"},
}

// ListPresets returns the official presets in menu order.
func ListPresets() []Preset {
	out := make([]Preset, len(officialPresets))
	copy(out, officialPresets)
	return out
}

// PresetByName resolves an official repository by name.
func PresetByName(name string) (Preset, bool) {
	name = strings.TrimSpace(name)
	for _, preset := range officialPresets {
		if preset.Name == name {
			return preset, true
		}
	}
	return Preset{}, false
}
