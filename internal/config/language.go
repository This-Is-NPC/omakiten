package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// languageFile mirrors the on-disk YAML shape of a Language entity. Kept
// separate from Language so loader concerns (yaml tags, strict decode)
// stay isolated from the Snapshot-facing type defined in catalog.go.
type languageFile struct {
	Code   string            `yaml:"code"`
	Name   string            `yaml:"name"`
	Native string            `yaml:"native"`
	Keys   map[string]string `yaml:"keys,omitempty"`
}

// LoadLanguages reads every <code>.yaml under dir (bundled) and
// dir/custom (user-authored). Custom files override bundled files with
// the same code. Two files declaring the same code inside the same
// scope is rejected as a duplicate. Files with a non-yaml extension are
// ignored. A missing dir returns an empty slice with no error so
// first-run paths can call this safely before materialization.
//
// The loader does not consult any configured language: it just discovers
// what is on disk. The Snapshot picks the active language at build time
// against the validated `languages.cli` / `languages.tui` config fields.
func LoadLanguages(dir string) ([]Language, []SourceWarning, error) {
	files, err := listLanguageFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	byCode := map[string]Language{}
	order := []string{}
	var warnings []SourceWarning
	seen := map[string]entityFile{}
	for _, file := range files {
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file.Path, err)
		}
		var lf languageFile
		if err := decodeLanguageStrict(raw, &lf); err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		code := strings.TrimSpace(lf.Code)
		if code == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("language code is required"))
		}
		if code != strings.ToLower(code) {
			return nil, nil, parseError(file.Path, fmt.Errorf("language code %q must be lowercase", code))
		}
		if strings.TrimSpace(lf.Name) == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("language name is required"))
		}
		if strings.TrimSpace(lf.Native) == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("language native label is required"))
		}
		filenameCode := slugFromFilename(file.Path)
		if filenameCode != code {
			warnings = append(warnings, SourceWarning{
				Slug:    code,
				Path:    file.Path,
				Message: fmt.Sprintf("filename code %q does not match code field %q", filenameCode, code),
			})
		}
		if previous, dup := seen[code]; dup {
			if previous.IsCustom == file.IsCustom {
				return nil, nil, parseError(file.Path, fmt.Errorf("duplicate language code %q (also defined in %s)", code, previous.Path))
			}
		}
		seen[code] = file
		keys := lf.Keys
		if keys == nil {
			keys = map[string]string{}
		}
		if _, exists := byCode[code]; !exists {
			order = append(order, code)
		}
		byCode[code] = Language{
			Code:       code,
			Name:       strings.TrimSpace(lf.Name),
			Native:     strings.TrimSpace(lf.Native),
			Keys:       keys,
			SourcePath: file.Path,
			IsCustom:   file.IsCustom,
		}
	}
	sort.Strings(order)
	out := make([]Language, 0, len(order))
	for _, code := range order {
		out = append(out, byCode[code])
	}
	return out, warnings, nil
}

func listLanguageFiles(dir string) ([]entityFile, error) {
	defaults, err := readLanguageYAMLFiles(dir, false)
	if err != nil {
		return nil, err
	}
	customs, err := readLanguageYAMLFiles(filepath.Join(dir, "custom"), true)
	if err != nil {
		return nil, err
	}
	return append(defaults, customs...), nil
}

func readLanguageYAMLFiles(dir string, isCustom bool) ([]entityFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []entityFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}
		files = append(files, entityFile{Path: filepath.Join(dir, name), IsCustom: isCustom})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func decodeLanguageStrict(raw []byte, target *languageFile) error {
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(target); err != nil {
		return err
	}
	return nil
}
