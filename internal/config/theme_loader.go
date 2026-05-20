package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadTheme keeps the existing themes/<slug>.yaml contract.
func LoadTheme(path string) (Theme, error) {
	file, err := os.Open(path)
	if err != nil {
		return Theme{}, err
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var theme Theme
	if err := decoder.Decode(&theme); err != nil {
		return Theme{}, err
	}
	return theme, ValidateTheme(theme)
}

// resolveActiveTheme walks the themes/ folder of `rootDir` looking for
// the active theme slug. Custom overrides win over the shipped defaults
// — themes/custom/<active>.yaml is preferred when present, themes/
// <active>.yaml is the fallback. Returned `path` is the absolute path
// the loader ended up reading (or the default path when no file is on
// disk, so callers can surface the missing target in a warning). When
// `active` is empty the function returns ("", "", nil) so the caller
// can treat it as "no theme requested" rather than an IO failure.
func resolveActiveTheme(rootDir, active string) (Theme, string, error) {
	if active == "" {
		return Theme{}, "", nil
	}
	customPath := filepath.Join(rootDir, "themes", "custom", active+".yaml")
	defaultPath := filepath.Join(rootDir, "themes", active+".yaml")
	themePath := defaultPath
	if _, err := os.Stat(customPath); err == nil {
		themePath = customPath
	}
	theme, err := LoadTheme(themePath)
	if err != nil {
		return Theme{}, themePath, err
	}
	return theme, themePath, nil
}
