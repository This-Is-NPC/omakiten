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
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Laws        []string `yaml:"laws,omitempty"`
}

type templateFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Entity      string   `yaml:"entity,omitempty"`
	Default     string   `yaml:"default,omitempty"`
	Project     string   `yaml:"project,omitempty"`
	Laws        []string `yaml:"laws,omitempty"`
}

// LoadSkills scans defaultDir for *.md files (defaults at root + customs
// under defaultDir/custom) plus an optional repoLocalDir (flat *.md files
// authored under the repo's `.omakiten/skills/`). Same-slug collisions
// resolve by layer precedence: repo-local > custom > default. Returns an
// empty slice when no source contributes files.
func LoadSkills(defaultDir, repoLocalDir string) ([]Skill, []SourceWarning, error) {
	files, err := listEntityFiles(defaultDir, repoLocalDir)
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
			if previous.Layer == file.Layer {
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
			Slug:         slug,
			Name:         meta.Name,
			Description:  meta.Description,
			Body:         string(body),
			SourcePath:   file.Path,
			IsCustom:     file.Layer == layerCustom,
			IsRepoLocal:  file.Layer == layerRepoLocal,
		}
	}
	skills := make([]Skill, 0, len(order))
	for _, slug := range order {
		skills = append(skills, bySlug[slug])
	}
	return skills, warnings, nil
}

func LoadLaws(defaultDir, repoLocalDir string) ([]Law, []SourceWarning, error) {
	files, err := listEntityFiles(defaultDir, repoLocalDir)
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
			if previous.Layer == file.Layer {
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
			Slug:         slug,
			Name:         meta.Name,
			Severity:     strings.ToLower(strings.TrimSpace(meta.Severity)),
			Body:         string(body),
			SourcePath:   file.Path,
			IsCustom:     file.Layer == layerCustom,
			IsRepoLocal:  file.Layer == layerRepoLocal,
		}
	}
	laws := make([]Law, 0, len(order))
	for _, slug := range order {
		laws = append(laws, bySlug[slug])
	}
	return laws, warnings, nil
}

func LoadPersonas(defaultDir, repoLocalDir string) ([]Persona, []SourceWarning, error) {
	files, err := listEntityFiles(defaultDir, repoLocalDir)
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
			if previous.Layer == file.Layer {
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
			Slug:         slug,
			Name:         meta.Name,
			Description:  meta.Description,
			Body:         string(body),
			Laws:         append([]string(nil), meta.Laws...),
			SourcePath:   file.Path,
			IsCustom:     file.Layer == layerCustom,
			IsRepoLocal:  file.Layer == layerRepoLocal,
		}
	}
	personas := make([]Persona, 0, len(order))
	for _, slug := range order {
		personas = append(personas, bySlug[slug])
	}
	return personas, warnings, nil
}

// LoadTemplates scans defaultDir (defaults at root + customs in
// defaultDir/custom) for *.md files, plus an optional repoLocalDir flat
// folder. Layer precedence resolves slug collisions; repo-local > custom >
// default. Returns an empty slice when no source contributes files.
//
// Templates are not validated structurally — the body is free-form markdown
// that the agent uses as a scaffold. Frontmatter requires `name`; `description`
// and `entity` are optional metadata for humans browsing the kit.
func LoadTemplates(defaultDir, repoLocalDir string) ([]TaskTemplate, []SourceWarning, error) {
	files, err := listEntityFiles(defaultDir, repoLocalDir)
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
			if previous.Layer == file.Layer {
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
			Slug:         slug,
			Name:         meta.Name,
			Description:  meta.Description,
			Entity:       strings.TrimSpace(meta.Entity),
			Default:      strings.TrimSpace(meta.Default),
			ProjectSlug:  strings.TrimSpace(meta.Project),
			Laws:         append([]string(nil), meta.Laws...),
			Body:         string(body),
			SourcePath:   file.Path,
			IsCustom:     file.Layer == layerCustom,
			IsRepoLocal:  file.Layer == layerRepoLocal,
		}
	}
	templates := make([]TaskTemplate, 0, len(order))
	for _, slug := range order {
		templates = append(templates, bySlug[slug])
	}
	return templates, warnings, nil
}

// entityLayer identifies which source folder a discovered .md came from.
// Layer ordering is also precedence ordering: a higher-layer file overrides
// a lower-layer file with the same slug.
type entityLayer int

const (
	layerDefault entityLayer = iota
	layerCustom
	layerRepoLocal
)

// entityFile pairs a discovered .md path with the source layer it came
// from. Defaults at the entity-folder root use layerDefault; files inside
// <entity>/custom/ use layerCustom; files inside the repo-local override
// dir (<repo>/.omakiten/<entity>/) use layerRepoLocal. Slug collisions
// resolve highest-layer-wins at the loader level.
type entityFile struct {
	Path  string
	Layer entityLayer
}

// listEntityFiles returns one entityFile per `.md` discovered under three
// optional source dirs, in precedence order: defaultDir (root) →
// defaultDir/custom → repoLocalDir (flat). repoLocalDir == "" disables the
// third source. The returned slice is sorted within each layer; downstream
// merging treats later entries as overrides.
func listEntityFiles(defaultDir, repoLocalDir string) ([]entityFile, error) {
	defaults, err := readMDFilesIn(defaultDir, layerDefault)
	if err != nil {
		return nil, err
	}
	customs, err := readMDFilesIn(filepath.Join(defaultDir, "custom"), layerCustom)
	if err != nil {
		return nil, err
	}
	files := append(defaults, customs...)
	if repoLocalDir != "" {
		repoLocal, err := readMDFilesIn(repoLocalDir, layerRepoLocal)
		if err != nil {
			return nil, err
		}
		files = append(files, repoLocal...)
	}
	return files, nil
}

func readMDFilesIn(dir string, layer entityLayer) ([]entityFile, error) {
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
		files = append(files, entityFile{Path: filepath.Join(dir, name), Layer: layer})
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
