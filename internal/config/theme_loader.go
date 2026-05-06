package config

import (
	"os"

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
