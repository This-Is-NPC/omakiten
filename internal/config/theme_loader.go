package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadTheme keeps the existing themes/<slug>.yaml contract.
func LoadTheme(path string) (Theme, error) {
	raw, err := readFileBounded(path, MaxEntityFileBytes)
	if err != nil {
		return Theme{}, err
	}
	return decodeThemeBytes(raw)
}

func decodeThemeBytes(raw []byte) (Theme, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
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
// can treat it as "no theme requested" rather than an IO failure. On
// failure the returned error names both candidate paths so operators
// can see the custom override path was considered, with the underlying
// loader error wrapped via %w (errors.Is/As-friendly).
func resolveActiveThemeReader(rootDir, active string, reader bundleSourceReader) (Theme, string, error) {
	if active == "" {
		return Theme{}, "", nil
	}
	customPath := filepath.Join(rootDir, "themes", "custom", active+".yaml")
	defaultPath := filepath.Join(rootDir, "themes", active+".yaml")
	themePath := defaultPath
	if reader == nil {
		if _, err := lstatNoFollow(customPath); err == nil {
			themePath = customPath
		} else if !os.IsNotExist(err) {
			return Theme{}, customPath, fmt.Errorf("resolve theme (custom=%s default=%s): %w", customPath, defaultPath, err)
		}
	} else {
		if _, err := reader.readFile(customPath, MaxEntityFileBytes); err == nil {
			themePath = customPath
		} else if !os.IsNotExist(err) {
			return Theme{}, customPath, fmt.Errorf("resolve theme (custom=%s default=%s): %w", customPath, defaultPath, err)
		}
	}
	var theme Theme
	var err error
	if reader == nil {
		theme, err = LoadTheme(themePath)
	} else {
		var raw []byte
		raw, err = reader.readFile(themePath, MaxEntityFileBytes)
		if err == nil {
			theme, err = decodeThemeBytes(raw)
		}
	}
	if err != nil {
		return Theme{}, themePath, fmt.Errorf("resolve theme (custom=%s default=%s): %w", customPath, defaultPath, err)
	}
	return theme, themePath, nil
}
