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

// LoadSkills scans dir for *.md files and parses each one into a Skill.
// Returns an empty slice (not error) when dir does not exist, since a fresh
// install materializes defaults later.
func LoadSkills(dir string) ([]Skill, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	skills := make([]Skill, 0, len(files))
	var warnings []SourceWarning
	seen := map[string]string{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file, err)
		}
		var meta skillFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file, err)
		}
		if strings.TrimSpace(meta.Name) == "" {
			return nil, nil, parseError(file, fmt.Errorf("skill name is required"))
		}
		slug := slugFromFilename(file)
		if other, dup := seen[slug]; dup {
			return nil, nil, parseError(file, fmt.Errorf("duplicate skill slug %q (also defined in %s)", slug, other))
		}
		seen[slug] = file
		if w := slugMismatchWarning(slug, meta.Name, file); w != nil {
			warnings = append(warnings, *w)
		}
		skills = append(skills, Skill{
			Slug:        slug,
			Name:        meta.Name,
			Description: meta.Description,
			Body:        string(body),
			SourcePath:  file,
		})
	}
	return skills, warnings, nil
}

func LoadLaws(dir string) ([]Law, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	laws := make([]Law, 0, len(files))
	var warnings []SourceWarning
	seen := map[string]string{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file, err)
		}
		var meta lawFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file, err)
		}
		if strings.TrimSpace(meta.Severity) == "" {
			return nil, nil, parseError(file, fmt.Errorf("law severity is required"))
		}
		bodyText := strings.TrimSpace(string(body))
		if bodyText == "" {
			return nil, nil, parseError(file, fmt.Errorf("law body is required"))
		}
		slug := slugFromFilename(file)
		if other, dup := seen[slug]; dup {
			return nil, nil, parseError(file, fmt.Errorf("duplicate law slug %q (also defined in %s)", slug, other))
		}
		seen[slug] = file
		// Laws may use slug as identifier; the optional `name` field is purely
		// human-readable, so a divergent name does not warn.
		laws = append(laws, Law{
			Slug:       slug,
			Name:       meta.Name,
			Severity:   strings.ToLower(strings.TrimSpace(meta.Severity)),
			Body:       string(body),
			SourcePath: file,
		})
	}
	return laws, warnings, nil
}

func LoadPersonas(dir string) ([]Persona, []SourceWarning, error) {
	files, err := listEntityFiles(dir)
	if err != nil {
		return nil, nil, err
	}

	personas := make([]Persona, 0, len(files))
	var warnings []SourceWarning
	seen := map[string]string{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}
		fm, body, err := SplitFrontmatter(raw)
		if err != nil {
			return nil, nil, parseError(file, err)
		}
		var meta personaFrontmatter
		if err := decodeStrict(fm, &meta); err != nil {
			return nil, nil, parseError(file, err)
		}
		if strings.TrimSpace(meta.Name) == "" {
			return nil, nil, parseError(file, fmt.Errorf("persona name is required"))
		}
		slug := slugFromFilename(file)
		if other, dup := seen[slug]; dup {
			return nil, nil, parseError(file, fmt.Errorf("duplicate persona slug %q (also defined in %s)", slug, other))
		}
		seen[slug] = file
		if w := slugMismatchWarning(slug, meta.Name, file); w != nil {
			warnings = append(warnings, *w)
		}
		personas = append(personas, Persona{
			Slug:        slug,
			Name:        meta.Name,
			Description: meta.Description,
			Body:        string(body),
			SourcePath:  file,
		})
	}
	return personas, warnings, nil
}

func listEntityFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
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
