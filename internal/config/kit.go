package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var kitOnce sync.Once
var kitSettings Settings
var kitError error

// LoadKitConfig returns the immutable settings baseline from the Omakase fixture.
func LoadKitConfig() (Settings, error) {
	kitOnce.Do(func() { kitSettings, kitError = loadKitSettings() })
	return kitSettings, kitError
}

func loadKitSettings() (Settings, error) {
	tmp, err := os.MkdirTemp("", "okt-kit-")
	if err != nil {
		return Settings{}, err
	}
	defer os.RemoveAll(tmp)
	if err := copyEmbeddedDirRecursive("config", filepath.Join(tmp, "config"), false); err != nil {
		return Settings{}, err
	}
	w, _, _, err := readWiringDetailed(filepath.Join(tmp, "config", "omakase.yaml"))
	return w.Config, err
}

// MustLoadKitConfig returns the fixture baseline or panics on invalid embedded data.
func MustLoadKitConfig() Settings {
	cfg, err := LoadKitConfig()
	if err != nil {
		panic(fmt.Errorf("fixture YAML unparseable: %w", err))
	}
	return cfg
}
