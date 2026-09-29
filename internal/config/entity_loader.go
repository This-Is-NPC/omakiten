package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"omakiten/internal/domain"
)

// skillFrontmatter mirrors the YAML inside skills/<slug>.md.
type skillFrontmatter struct {
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description,omitempty"`
	SchemaVersion int      `yaml:"schema_version"`
	RoleAffinity  []string `yaml:"role_affinity,omitempty"`
}

type lawFrontmatter struct {
	Name     string `yaml:"name,omitempty"`
	Severity string `yaml:"severity"`
}

type personaFrontmatter struct {
	Name            string   `yaml:"name"`
	Description     string   `yaml:"description,omitempty"`
	Laws            []string `yaml:"laws,omitempty"`
	SchemaVersion   int      `yaml:"schema_version"`
	SkillRepertoire []string `yaml:"skill_repertoire,omitempty"`
}

type templateFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Entity      string   `yaml:"entity,omitempty"`
	Default     string   `yaml:"default,omitempty"`
	Project     string   `yaml:"project,omitempty"`
	Laws        []string `yaml:"laws,omitempty"`
}

func loadSkillsReader(dir string, reader bundleSourceReader) ([]Skill, []SourceWarning, error) {
	opts := LoadOptions[Skill]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodeSkillFile,
		SlugOf:       func(s Skill) string { return s.Slug },
		Collision:    CollideOverwrite,
	}
	if reader == nil {
		return LoadFromDir(dir, opts)
	}
	return loadFromDirReader(dir, opts, reader)
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
	if meta.SchemaVersion != CurrentEntitySchemaVersion {
		return Skill{}, nil, parseError(path, fmt.Errorf("skill schema_version must be %d", CurrentEntitySchemaVersion))
	}
	slug := slugFromFilename(path)
	return Skill{
		Slug:          slug,
		Name:          meta.Name,
		Description:   meta.Description,
		Body:          string(body),
		SchemaVersion: meta.SchemaVersion,
		RoleAffinity:  append([]string(nil), meta.RoleAffinity...),
		SourcePath:    path,
		IsCustom:      isCustom,
	}, slugMismatchWarning(slug, meta.Name, path), nil
}

func loadLawsReader(dir string, reader bundleSourceReader) ([]Law, []SourceWarning, error) {
	opts := LoadOptions[Law]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodeLawFile,
		SlugOf:       func(l Law) string { return l.Slug },
		Collision:    CollideOverwrite,
	}
	if reader == nil {
		return LoadFromDir(dir, opts)
	}
	return loadFromDirReader(dir, opts, reader)
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

func loadPersonasReader(dir string, reader bundleSourceReader) ([]Persona, []SourceWarning, error) {
	opts := LoadOptions[Persona]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodePersonaFile,
		SlugOf:       func(p Persona) string { return p.Slug },
		Collision:    CollideOverwrite,
	}
	if reader == nil {
		return LoadFromDir(dir, opts)
	}
	return loadFromDirReader(dir, opts, reader)
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
	if meta.SchemaVersion != CurrentEntitySchemaVersion {
		return Persona{}, nil, parseError(path, fmt.Errorf("persona schema_version must be %d", CurrentEntitySchemaVersion))
	}
	slug := slugFromFilename(path)
	return Persona{
		Slug:            slug,
		Name:            meta.Name,
		Description:     meta.Description,
		Body:            string(body),
		Laws:            append([]string(nil), meta.Laws...),
		SchemaVersion:   meta.SchemaVersion,
		SkillRepertoire: append([]string(nil), meta.SkillRepertoire...),
		SourcePath:      path,
		IsCustom:        isCustom,
	}, slugMismatchWarning(slug, meta.Name, path), nil
}

func loadTemplatesReader(dir string, reader bundleSourceReader) ([]TaskTemplate, []SourceWarning, error) {
	opts := LoadOptions[TaskTemplate]{
		Suffixes:     []string{".md"},
		MaxFileBytes: MaxEntityFileBytes,
		Decode:       decodeTemplateFile,
		SlugOf:       func(t TaskTemplate) string { return t.Slug },
		Collision:    CollideOverwrite,
	}
	if reader == nil {
		return LoadFromDir(dir, opts)
	}
	return loadFromDirReader(dir, opts, reader)
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
	Raw      []byte
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
	expected := domain.Slugify(name)
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

// SourceError identifies the file whose contents failed to decode.
type SourceError struct {
	Path string
	Err  error
}

func (e *SourceError) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }

func (e *SourceError) Unwrap() error { return e.Err }

func parseError(path string, err error) error {
	return &SourceError{Path: path, Err: err}
}

// ReadEntityFile reads one entity source with the loader size and path constraints.
func ReadEntityFile(path string) ([]byte, error) { return readFileBounded(path, MaxEntityFileBytes) }
