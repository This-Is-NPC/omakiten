package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// skillFrontmatter mirrors the YAML inside skills/<slug>.md.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type lawFrontmatter struct {
	Name     string `yaml:"name,omitempty"`
	Severity string `yaml:"severity"`
}

type personaFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type templateFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Entity      string `yaml:"entity,omitempty"`
	Default     string `yaml:"default,omitempty"`
	Project     string `yaml:"project,omitempty"`
}

// LoadSkills scans dir for *.md files (defaults at root + customs under
// dir/custom) and parses each one into a Skill. Same-slug pairs are resolved
// with custom winning, so the user's `<entity>/custom/<slug>.md` overrides any
// default with the same slug. Returns empty slice when dir does not exist.
func LoadSkills(dir string) ([]Skill, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	bySlug := map[string]Skill{}
	order := []string{}
	var warnings []SourceWarning
	seen := map[string]entityFile{}
	for _, file := range files {
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file.Path, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		var meta skillFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		if strings.TrimSpace(meta.Name) == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("skill name is required"))
		}
		slug := slugFromFilename(file.Path)
		if previous, dup := seen[slug]; dup {
			// Default + custom collision is allowed (custom overrides). Two files
			// inside the same scope (both default OR both custom) is a real conflict.
			if previous.IsCustom == file.IsCustom {
				return nil, nil, parseError(file.Path, fmt.Errorf("duplicate skill slug %q (also defined in %s)", slug, previous.Path))
			}
		}
		seen[slug] = file
		if w := slugMismatchWarning(slug, meta.Name, file.Path); w != nil {
			warnings = append(warnings, *w)
		}
		if _, exists := bySlug[slug]; !exists {
			order = append(order, slug)
		}
		bySlug[slug] = Skill{
			Slug:        slug,
			Name:        meta.Name,
			Description: meta.Description,
			Body:        string(body),
			SourcePath:  file.Path,
			IsCustom:    file.IsCustom,
		}
	}
	skills := make([]Skill, 0, len(order))
	for _, slug := range order {
		skills = append(skills, bySlug[slug])
	}
	return skills, warnings, nil
}

func LoadLaws(dir string) ([]Law, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	bySlug := map[string]Law{}
	order := []string{}
	var warnings []SourceWarning
	seen := map[string]entityFile{}
	for _, file := range files {
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file.Path, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		var meta lawFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		if strings.TrimSpace(meta.Severity) == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("law severity is required"))
		}
		bodyText := strings.TrimSpace(string(body))
		if bodyText == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("law body is required"))
		}
		slug := slugFromFilename(file.Path)
		if previous, dup := seen[slug]; dup {
			if previous.IsCustom == file.IsCustom {
				return nil, nil, parseError(file.Path, fmt.Errorf("duplicate law slug %q (also defined in %s)", slug, previous.Path))
			}
		}
		seen[slug] = file
		// Laws may use slug as identifier; the optional `name` field is purely
		// human-readable, so a divergent name does not warn.
		if _, exists := bySlug[slug]; !exists {
			order = append(order, slug)
		}
		bySlug[slug] = Law{
			Slug:       slug,
			Name:       meta.Name,
			Severity:   strings.ToLower(strings.TrimSpace(meta.Severity)),
			Body:       string(body),
			SourcePath: file.Path,
			IsCustom:   file.IsCustom,
		}
	}
	laws := make([]Law, 0, len(order))
	for _, slug := range order {
		laws = append(laws, bySlug[slug])
	}
	return laws, warnings, nil
}

func LoadPersonas(dir string) ([]Persona, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	bySlug := map[string]Persona{}
	order := []string{}
	var warnings []SourceWarning
	seen := map[string]entityFile{}
	for _, file := range files {
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file.Path, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		var meta personaFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		if strings.TrimSpace(meta.Name) == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("persona name is required"))
		}
		slug := slugFromFilename(file.Path)
		if previous, dup := seen[slug]; dup {
			if previous.IsCustom == file.IsCustom {
				return nil, nil, parseError(file.Path, fmt.Errorf("duplicate persona slug %q (also defined in %s)", slug, previous.Path))
			}
		}
		seen[slug] = file
		if w := slugMismatchWarning(slug, meta.Name, file.Path); w != nil {
			warnings = append(warnings, *w)
		}
		if _, exists := bySlug[slug]; !exists {
			order = append(order, slug)
		}
		bySlug[slug] = Persona{
			Slug:        slug,
			Name:        meta.Name,
			Description: meta.Description,
			Body:        string(body),
			SourcePath:  file.Path,
			IsCustom:    file.IsCustom,
		}
	}
	personas := make([]Persona, 0, len(order))
	for _, slug := range order {
		personas = append(personas, bySlug[slug])
	}
	return personas, warnings, nil
}

// LoadTemplates scans dir (defaults at root + customs in dir/custom) for *.md
// files and parses each into a TaskTemplate. Custom files override defaults
// with the same slug. Returns empty slice when dir does not exist.
//
// Templates are not validated structurally — the body is free-form markdown
// that the agent uses as a scaffold. Frontmatter requires `name`; `description`
// and `entity` are optional metadata for humans browsing the kit.
func LoadTemplates(dir string) ([]TaskTemplate, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	bySlug := map[string]TaskTemplate{}
	order := []string{}
	var warnings []SourceWarning
	seen := map[string]entityFile{}
	for _, file := range files {
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file.Path, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		var meta templateFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file.Path, err)
		}
		if strings.TrimSpace(meta.Name) == "" {
			return nil, nil, parseError(file.Path, fmt.Errorf("template name is required"))
		}
		slug := slugFromFilename(file.Path)
		if previous, dup := seen[slug]; dup {
			if previous.IsCustom == file.IsCustom {
				return nil, nil, parseError(file.Path, fmt.Errorf("duplicate template slug %q (also defined in %s)", slug, previous.Path))
			}
		}
		seen[slug] = file
		if w := slugMismatchWarning(slug, meta.Name, file.Path); w != nil {
			warnings = append(warnings, *w)
		}
		if _, exists := bySlug[slug]; !exists {
			order = append(order, slug)
		}
		bySlug[slug] = TaskTemplate{
			Slug:        slug,
			Name:        meta.Name,
			Description: meta.Description,
			Entity:      strings.TrimSpace(meta.Entity),
			Default:     strings.TrimSpace(meta.Default),
			ProjectSlug: strings.TrimSpace(meta.Project),
			Body:        string(body),
			SourcePath:  file.Path,
			IsCustom:    file.IsCustom,
		}
	}
	templates := make([]TaskTemplate, 0, len(order))
	for _, slug := range order {
		templates = append(templates, bySlug[slug])
	}
	return templates, warnings, nil
}

// entityFile pairs a discovered .md path with whether it lives under the
// `custom/` subtree. Defaults at the entity-folder root carry IsCustom=false;
// files inside <entity>/custom/ carry IsCustom=true. Slug collisions between
// defaults and customs are resolved at the loader level, with custom winning.
type entityFile struct {
	Path     string
	IsCustom bool
}

// listEntityFiles returns one entityFile per `.md` discovered under dir and
// dir/custom. Defaults are emitted first (sorted), then customs (sorted), so
// downstream merging can treat the second pass as a slug-keyed override.
func listEntityFiles(dir string) ([]entityFile, error) {
	defaults, err := readMDFilesIn(dir, false)
	if err != nil {
		return nil, err
	}
	customs, err := readMDFilesIn(filepath.Join(dir, "custom"), true)
	if err != nil {
		return nil, err
	}
	return append(defaults, customs...), nil
}

func readMDFilesIn(dir string, isCustom bool) ([]entityFile, error) {
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
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		files = append(files, entityFile{Path: filepath.Join(dir, name), IsCustom: isCustom})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func slugFromFilename(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func slugMismatchWarning(slug, name, path string) *SourceWarning {
	expected := Slugify(name)
	if expected == "" || expected == slug {
		return nil
	}
	return &SourceWarning{
		Slug:    slug,
		Path:    path,
		Message: fmt.Sprintf("filename slug %q does not match slugify(name) %q", slug, expected),
	}
}

func decodeStrict(data []byte, target any) error {
	if len(data) == 0 {
		return fmt.Errorf("frontmatter is empty")
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(target); err != nil {
		return err
	}
	return nil
}

func parseError(path string, err error) error {
	return fmt.Errorf("%s: %w", path, err)
}
