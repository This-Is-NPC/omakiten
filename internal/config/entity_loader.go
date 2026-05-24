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

// LoadSkills scans dir for *.md files (defaults at root + customs under
// dir/custom) and parses each one into a Skill. Same-slug pairs are resolved
// with custom winning, so the user's `<entity>/custom/<slug>.md` overrides any
// default with the same slug. Returns empty slice when dir does not exist.
func LoadSkills(dir string) ([]Skill, []SourceWarning, error) {
	return LoadFromDir(dir, LoadOptions[Skill]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodeSkillFile,
		SlugOf:       func(s Skill) string { return s.Slug },
		Collision:    CollideOverwrite,
	})
}

func decodeSkillFile(path string, raw []byte, isCustom bool) (Skill, *SourceWarning, error) {
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return Skill{}, nil, parseError(path, err)
	}
	var meta skillFrontmatter
	if err := decodeStrict(fm, &meta); err != nil {
		return Skill{}, nil, parseError(path, err)
	}
	if strings.TrimSpace(meta.Name) == "" {
		return Skill{}, nil, parseError(path, fmt.Errorf("skill name is required"))
	}
	slug := slugFromFilename(path)
	return Skill{
		Slug:        slug,
		Name:        meta.Name,
		Description: meta.Description,
		Body:        string(body),
		SourcePath:  path,
		IsCustom:    isCustom,
	}, slugMismatchWarning(slug, meta.Name, path), nil
}

func LoadLaws(dir string) ([]Law, []SourceWarning, error) {
	return LoadFromDir(dir, LoadOptions[Law]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodeLawFile,
		SlugOf:       func(l Law) string { return l.Slug },
		Collision:    CollideOverwrite,
	})
}

func decodeLawFile(path string, raw []byte, isCustom bool) (Law, *SourceWarning, error) {
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return Law{}, nil, parseError(path, err)
	}
	var meta lawFrontmatter
	if err := decodeStrict(fm, &meta); err != nil {
		return Law{}, nil, parseError(path, err)
	}
	if strings.TrimSpace(meta.Severity) == "" {
		return Law{}, nil, parseError(path, fmt.Errorf("law severity is required"))
	}
	if strings.TrimSpace(string(body)) == "" {
		return Law{}, nil, parseError(path, fmt.Errorf("law body is required"))
	}
	// Laws use slug as identifier; the optional `name` field is purely
	// human-readable, so a divergent name does not warn.
	return Law{
		Slug:       slugFromFilename(path),
		Name:       meta.Name,
		Severity:   strings.ToLower(strings.TrimSpace(meta.Severity)),
		Body:       string(body),
		SourcePath: path,
		IsCustom:   isCustom,
	}, nil, nil
}

func LoadPersonas(dir string) ([]Persona, []SourceWarning, error) {
	return LoadFromDir(dir, LoadOptions[Persona]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodePersonaFile,
		SlugOf:       func(p Persona) string { return p.Slug },
		Collision:    CollideOverwrite,
	})
}

func decodePersonaFile(path string, raw []byte, isCustom bool) (Persona, *SourceWarning, error) {
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return Persona{}, nil, parseError(path, err)
	}
	var meta personaFrontmatter
	if err := decodeStrict(fm, &meta); err != nil {
		return Persona{}, nil, parseError(path, err)
	}
	if strings.TrimSpace(meta.Name) == "" {
		return Persona{}, nil, parseError(path, fmt.Errorf("persona name is required"))
	}
	slug := slugFromFilename(path)
	return Persona{
		Slug:        slug,
		Name:        meta.Name,
		Description: meta.Description,
		Body:        string(body),
		Laws:        append([]string(nil), meta.Laws...),
		SourcePath:  path,
		IsCustom:    isCustom,
	}, slugMismatchWarning(slug, meta.Name, path), nil
}

// LoadTemplates scans dir (defaults at root + customs in dir/custom) for *.md
// files and parses each into a TaskTemplate. Custom files override defaults
// with the same slug. Returns empty slice when dir does not exist.
//
// Templates are not validated structurally — the body is free-form markdown
// that the agent uses as a scaffold. Frontmatter requires `name`; `description`
// and `entity` are optional metadata for humans browsing the kit.
func LoadTemplates(dir string) ([]TaskTemplate, []SourceWarning, error) {
	return LoadFromDir(dir, LoadOptions[TaskTemplate]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodeTemplateFile,
		SlugOf:       func(t TaskTemplate) string { return t.Slug },
		Collision:    CollideOverwrite,
	})
}

func decodeTemplateFile(path string, raw []byte, isCustom bool) (TaskTemplate, *SourceWarning, error) {
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return TaskTemplate{}, nil, parseError(path, err)
	}
	var meta templateFrontmatter
	if err := decodeStrict(fm, &meta); err != nil {
		return TaskTemplate{}, nil, parseError(path, err)
	}
	if strings.TrimSpace(meta.Name) == "" {
		return TaskTemplate{}, nil, parseError(path, fmt.Errorf("template name is required"))
	}
	slug := slugFromFilename(path)
	return TaskTemplate{
		Slug:        slug,
		Name:        meta.Name,
		Description: meta.Description,
		Entity:      strings.TrimSpace(meta.Entity),
		Default:     strings.TrimSpace(meta.Default),
		ProjectSlug: strings.TrimSpace(meta.Project),
		Laws:        append([]string(nil), meta.Laws...),
		Body:        string(body),
		SourcePath:  path,
		IsCustom:    isCustom,
	}, slugMismatchWarning(slug, meta.Name, path), nil
}

// entityFile pairs a discovered file path with whether it lives under
// the `custom/` subtree. Defaults at the folder root carry
// IsCustom=false; files inside <folder>/custom/ carry IsCustom=true.
// Slug collisions across scopes are resolved by LoadFromDir per the
// CollisionPolicy on the per-domain LoadOptions.
type entityFile struct {
	Path     string
	IsCustom bool
}

// listFilesIn returns every file directly under dir whose lowercase
// extension matches one of `exts` (".md", ".yaml", ".yml", …),
// sorted by absolute path so the caller's merge-by-slug step is
// deterministic. The isCustom flag is stamped on every returned
// entry — callers pass false for the defaults walk and true for
// the matching custom/ subtree walk. A non-existent dir returns
// (nil, nil) so the surrounding loader path can short-circuit
// without distinguishing "no custom subtree" from "no defaults".
//
// Used exclusively by LoadFromDir, which feeds the entity loader
// (.md), the language pack loader (.yaml/.yml), and the notification
// loader (.yaml/.yml) — all three previously inlined the same
// os.ReadDir + suffix-filter + sort sequence with the same edge
// cases.
func listFilesIn(dir string, exts []string, isCustom bool) ([]entityFile, error) {
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
		if !hasAnySuffix(lower, exts) {
			continue
		}
		files = append(files, entityFile{Path: filepath.Join(dir, name), IsCustom: isCustom})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// hasAnySuffix reports whether name ends in any of the supplied
// suffixes. Lifted out of listFilesIn so the call stays readable
// when a future loader needs three or four candidate extensions.
func hasAnySuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
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
	return decodeYAMLStrict(data, target)
}

// decodeYAMLStrict is the shared yaml.NewDecoder + KnownFields(true)
// wrapper every config loader pipes its bytes through. Extracted from
// the per-loader inline decoders so a future tweak (e.g. line/column
// in error envelopes, document-mode toggle) lands in one place.
// Empty-input guards stay at the caller because frontmatter and
// language-pack semantics differ: a 0-byte frontmatter is an error
// (the file claimed to be an entity but carried no fields); a 0-byte
// language pack is acceptable (an empty placeholder pack inherits
// every key from the en baseline).
func decodeYAMLStrict(data []byte, target any) error {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	return dec.Decode(target)
}

func parseError(path string, err error) error {
	return fmt.Errorf("%s: %w", path, err)
}
