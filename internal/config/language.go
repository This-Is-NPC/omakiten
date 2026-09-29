package config

import (
	"fmt"
	"maps"
	"omakiten/defaults"
	"strings"
	"sync"
)

type languageFile struct {
	Code   string            `yaml:"code"`
	Name   string            `yaml:"name"`
	Native string            `yaml:"native"`
	Keys   map[string]string `yaml:"keys,omitempty"`
}

var bundledLanguageOnce sync.Once
var bundledLanguages []Language
var bundledLanguageError error

func readBundledLanguages() {
	entries, err := defaults.FS.ReadDir("languages")
	if err != nil {
		bundledLanguageError = err
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := "languages/" + entry.Name()
		raw, err := defaults.FS.ReadFile(path)
		if err != nil {
			bundledLanguageError = err
			return
		}
		var file languageFile
		if err := decodeYAMLStrict(raw, &file); err != nil {
			bundledLanguageError = parseError(path, err)
			return
		}
		code := strings.TrimSuffix(entry.Name(), ".yaml")
		if file.Code != code || strings.ToLower(code) != code || strings.TrimSpace(file.Name) == "" || strings.TrimSpace(file.Native) == "" {
			bundledLanguageError = fmt.Errorf("invalid bundled language metadata: %s", path)
			return
		}
		bundledLanguages = append(bundledLanguages, Language{Code: code, Name: file.Name, Native: file.Native, Keys: file.Keys})
	}
}

// LoadBundledLanguages returns independent copies of the application's locales.
func LoadBundledLanguages() ([]Language, error) {
	bundledLanguageOnce.Do(readBundledLanguages)
	if bundledLanguageError != nil {
		return nil, bundledLanguageError
	}
	result := make([]Language, len(bundledLanguages))
	for i, language := range bundledLanguages {
		result[i] = language
		result[i].Keys = maps.Clone(language.Keys)
	}
	return result, nil
}

// LoadBundledLanguage returns an independent copy of a bundled locale.
func LoadBundledLanguage(code string) (Language, error) {
	bundledLanguageOnce.Do(readBundledLanguages)
	if bundledLanguageError != nil {
		return Language{}, bundledLanguageError
	}
	for _, language := range bundledLanguages {
		if language.Code == code {
			language.Keys = maps.Clone(language.Keys)
			return language, nil
		}
	}
	return Language{}, fmt.Errorf("unknown application language %q; run `okt config language show`", code)
}
