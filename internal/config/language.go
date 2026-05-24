package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"omakiten/defaults"
)

// LoadBundledLanguage reads defaults/languages/<code>.yaml from the
// embed FS. Used by the early-boot CLI bootstrap path that needs a
// usable Catalog before the on-disk install is materialized; the
// returned Language carries SourcePath="" because the bytes never
// touch the filesystem. Strict YAML decoding still applies, so an
// embed corruption surfaces during init rather than at first call.
func LoadBundledLanguage(code string) (Language, error) {
	raw, err := defaults.FS.ReadFile(filepath.ToSlash(filepath.Join("languages", code+".yaml")))
	if err != nil {
		return Language{}, fmt.Errorf("read bundled languages/%s.yaml: %w", code, err)
	}
	var lf languageFile
	if err := decodeLanguageStrict(raw, &lf); err != nil {
		return Language{}, parseError(filepath.Join("languages", code+".yaml"), err)
	}
	keys := lf.Keys
	if keys == nil {
		keys = map[string]string{}
	}
	return Language{
		Code:   strings.TrimSpace(lf.Code),
		Name:   strings.TrimSpace(lf.Name),
		Native: strings.TrimSpace(lf.Native),
		Keys:   keys,
	}, nil
}

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
	return LoadFromDir(dir, LoadOptions[Language]{
		Suffixes:     []string{".yaml", ".yml"},
		MaxFileBytes: MaxLanguagePackBytes,
		Decode:       decodeLanguagePack,
		SlugOf:       func(l Language) string { return l.Code },
		Collision:    CollideOverwrite,
	})
}

// decodeLanguagePack parses a single language YAML file into a Language,
// returning a filename↔code mismatch as a non-fatal warning so the
// loader can keep loading the pack. Validation rules: code required,
// lowercase; name required; native required. Any of these missing or
// malformed fails the load.
func decodeLanguagePack(path string, raw []byte, isCustom bool) (Language, *SourceWarning, error) {
	var lf languageFile
	if err := decodeLanguageStrict(raw, &lf); err != nil {
		return Language{}, nil, parseError(path, err)
	}
	code := strings.TrimSpace(lf.Code)
	if code == "" {
		return Language{}, nil, parseError(path, fmt.Errorf("language code is required"))
	}
	if code != strings.ToLower(code) {
		return Language{}, nil, parseError(path, fmt.Errorf("language code %q must be lowercase", code))
	}
	if strings.TrimSpace(lf.Name) == "" {
		return Language{}, nil, parseError(path, fmt.Errorf("language name is required"))
	}
	if strings.TrimSpace(lf.Native) == "" {
		return Language{}, nil, parseError(path, fmt.Errorf("language native label is required"))
	}
	var warning *SourceWarning
	filenameCode := slugFromFilename(path)
	if filenameCode != code {
		warning = &SourceWarning{
			Slug:    code,
			Path:    path,
			Message: fmt.Sprintf("filename code %q does not match code field %q", filenameCode, code),
		}
	}
	keys := lf.Keys
	if keys == nil {
		keys = map[string]string{}
	}
	return Language{
		Code:       code,
		Name:       strings.TrimSpace(lf.Name),
		Native:     strings.TrimSpace(lf.Native),
		Keys:       keys,
		SourcePath: path,
		IsCustom:   isCustom,
	}, warning, nil
}

func decodeLanguageStrict(raw []byte, target *languageFile) error {
	return decodeYAMLStrict(raw, target)
}
