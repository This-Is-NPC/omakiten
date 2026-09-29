package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SaveBundle writes configuration separately from entity files. Repository
// packages retain their imports and update the modules that own changed values.
// BundleEditor stages package writes before publishing the selection atomically.
func SaveBundle(path string, bundle Bundle) error {
	if _, selected, err := ResolvePresetSelection(path); err != nil {
		return err
	} else if selected {
		return EditBundle(path, func(working string) error { return SaveBundle(working, bundle) })
	}
	if err := validateEditablePresetConfig(path); err != nil {
		return err
	}
	if err := validateCurrentConfigLayout(path); err != nil {
		return err
	}
	w, err := bundleToWiring(bundle)
	if err != nil {
		return err
	}
	if isPresetRoot(ConfigRootFromYAMLPath(path)) {
		return savePresetWiring(path, w)
	}
	data, err := encodeYAML(w)
	if err != nil {
		return err
	}
	header, err := readHeaderComments(path)
	if err != nil {
		// Size-cap overflow is the one surfaced failure — silently
		// stamping a saved file on top of an oversize on-disk wiring
		// would mask the operator's pathological config. Other read
		// errors (perm denied, IO etc.) still silently degrade inside
		// readHeaderComments so the save proceeds.
		return err
	}
	if len(header) > 0 {
		data = append(header, data...)
	}
	return WriteAtomic(path, data)
}

// readHeaderComments returns the leading comment block (lines that are
// either blank or start with `#`, up to the first content line) from
// the file at path. Returns (nil, nil) when the file does not exist
// yet (a fresh wiring is being seeded) or when an opaque read failure
// hits the preservation path. The only error surfaced is the bounded
// reader's tooLargeError — a wiring file that already exceeds
// MaxWiringFileBytes is a pathological state the saver should refuse
// to compound, since the load path (W5 #220) already rejects bundles
// past that cap.
func readHeaderComments(path string) ([]byte, error) {
	existing, err := readFileBounded(path, MaxWiringFileBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if IsConfigTooLarge(err) {
			return nil, err
		}
		// Other read failures (perm denied, mid-read IO) skip the
		// preservation path rather than blocking the write. The atomic
		// WriteAtomic below still surfaces real permission errors on
		// the destination.
		return nil, nil
	}
	return leadingHeaderComments(existing)
}

func leadingHeaderComments(existing []byte) ([]byte, error) {
	lines := strings.SplitAfter(string(existing), "\n")
	var header []byte
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			header = append(header, []byte(line)...)
			continue
		}
		break
	}
	return header, nil
}

// SaveFullBundle writes the wiring file plus every entity file present in the
// bundle. Tests use this to materialize a fresh config root
// from a Bundle literal in one call. Entities flagged IsCustom are placed
// under <root>/<kind>/custom/; the rest go to the default location. Files are
// published independently, so a later failure does not roll back earlier
// writes.
func SaveFullBundle(configPath string, bundle Bundle) error {
	if err := validateCurrentConfigLayout(configPath); err != nil {
		return err
	}
	rootDir := ConfigRootFromYAMLPath(configPath)
	if err := writeEntityFiles(rootDir, EntityKindSkill, bundle.Skills,
		func(skill Skill) string { return skill.Slug },
		func(skill Skill) bool { return skill.IsCustom }, SkillFileBytes); err != nil {
		return err
	}
	if err := writeEntityFiles(rootDir, EntityKindLaw, bundle.Laws,
		func(law Law) string { return law.Slug },
		func(law Law) bool { return law.IsCustom }, LawFileBytes); err != nil {
		return err
	}
	if err := writeEntityFiles(rootDir, EntityKindPersona, bundle.Personas,
		func(persona Persona) string { return persona.Slug },
		func(persona Persona) bool { return persona.IsCustom }, PersonaFileBytes); err != nil {
		return err
	}
	return SaveBundle(configPath, bundle)
}

func writeEntityFiles[T any](rootDir string, kind EntityKind, entities []T, slug func(T) string, isCustom func(T) bool, render func(T) ([]byte, error)) error {
	for _, entity := range entities {
		bytes, err := render(entity)
		if err != nil {
			return err
		}
		if err := WriteAtomic(entityWritePath(rootDir, kind, slug(entity), isCustom(entity)), bytes); err != nil {
			return err
		}
	}
	return nil
}

func entityWritePath(rootDir string, kind EntityKind, slug string, isCustom bool) string {
	if isCustom {
		return CustomEntityFilePath(rootDir, kind, slug)
	}
	return EntityFilePath(rootDir, kind, slug)
}

func bundleToWiring(bundle Bundle) (wiring, error) {
	w := wiring{
		Version:    bundle.Version,
		Kit:        bundle.Kit,
		SubtaskKit: bundle.SubtaskKit,
		Config:     bundle.Config,
		Workflows:  bundle.Workflows,
		Surfaces:   bundle.Surfaces,
		Commands:   bundle.Commands,
	}
	appendGlobalWiring(&w, bundle)
	personaWiring := appendPersonaWiring(&w, bundle)
	if err := appendPersonaLaws(&w, bundle, personaWiring); err != nil {
		return wiring{}, err
	}
	projectWiring := appendProjectWiring(&w, bundle)
	if err := appendProjectLaws(&w, bundle, projectWiring); err != nil {
		return wiring{}, err
	}
	return w, nil
}

func appendGlobalWiring(w *wiring, bundle Bundle) {
	for _, skill := range bundle.Skills {
		w.Skills = append(w.Skills, skill.Slug)
	}
	for _, law := range bundle.Laws {
		if law.Scope == "" || law.Scope == "global" {
			w.Laws = append(w.Laws, law.Slug)
		}
	}
}

func appendPersonaWiring(w *wiring, bundle Bundle) map[string]int {
	personaWiring := map[string]int{}
	for _, persona := range bundle.Personas {
		w.Personas = append(w.Personas, PersonaWiring{
			Slug:            persona.Slug,
			SchemaVersion:   currentSchemaVersion(persona.SchemaVersion),
			SkillRepertoire: append([]string(nil), persona.SkillRepertoire...),
			Laws:            append([]string(nil), persona.Laws...),
		})
		personaWiring[persona.Slug] = len(w.Personas) - 1
	}
	return personaWiring
}

func appendPersonaLaws(w *wiring, bundle Bundle, personaWiring map[string]int) error {
	for _, law := range bundle.Laws {
		if law.Scope == "persona" && law.PersonaSlug != "" {
			idx, ok := personaWiring[law.PersonaSlug]
			if !ok {
				return fmt.Errorf("law %q references unknown persona %q", law.Slug, law.PersonaSlug)
			}
			if !contains(w.Personas[idx].Laws, law.Slug) {
				w.Personas[idx].Laws = append(w.Personas[idx].Laws, law.Slug)
			}
		}
	}
	return nil
}

func appendProjectWiring(w *wiring, bundle Bundle) map[string]int {
	projectWiring := map[string]int{}
	for _, project := range bundle.Projects {
		w.Projects = append(w.Projects, ProjectWiring{
			Slug:        project.Slug,
			Name:        project.Name,
			Description: project.Description,
			Laws:        append([]string(nil), project.Laws...),
		})
		projectWiring[project.Slug] = len(w.Projects) - 1
	}
	return projectWiring
}

func appendProjectLaws(w *wiring, bundle Bundle, projectWiring map[string]int) error {
	for _, law := range bundle.Laws {
		if law.Scope == "project" && law.ProjectSlug != "" {
			idx, ok := projectWiring[law.ProjectSlug]
			if !ok {
				return fmt.Errorf("law %q references unknown project %q", law.Slug, law.ProjectSlug)
			}
			if !contains(w.Projects[idx].Laws, law.Slug) {
				w.Projects[idx].Laws = append(w.Projects[idx].Laws, law.Slug)
			}
		}
	}
	return nil
}

// SkillFileBytes renders skills/<slug>.md content.
func SkillFileBytes(skill Skill) ([]byte, error) {
	fm, err := yaml.Marshal(skillFrontmatter{
		Name:          skill.Name,
		Description:   skill.Description,
		SchemaVersion: currentSchemaVersion(skill.SchemaVersion),
		RoleAffinity:  skill.RoleAffinity,
	})
	if err != nil {
		return nil, err
	}
	return JoinFrontmatter(fm, []byte(skill.Body)), nil
}

// LawFileBytes renders laws/<slug>.md content.
func LawFileBytes(law Law) ([]byte, error) {
	fm, err := yaml.Marshal(lawFrontmatter{Name: law.Name, Severity: law.Severity})
	if err != nil {
		return nil, err
	}
	body := law.Body
	if body == "" {
		body = " "
	}
	return JoinFrontmatter(fm, []byte(body)), nil
}

// PersonaFileBytes renders personas/<slug>.md content.
func PersonaFileBytes(persona Persona) ([]byte, error) {
	fm, err := yaml.Marshal(personaFrontmatter{
		Name:            persona.Name,
		Description:     persona.Description,
		SchemaVersion:   currentSchemaVersion(persona.SchemaVersion),
		SkillRepertoire: persona.SkillRepertoire,
	})
	if err != nil {
		return nil, err
	}
	return JoinFrontmatter(fm, []byte(persona.Body)), nil
}

func currentSchemaVersion(version int) int {
	if version == 0 {
		return CurrentEntitySchemaVersion
	}
	return version
}

// EntityFilePath returns the canonical filesystem path for a default entity
// file: <root>/<kind>/<slug>.md. Use CustomEntityFilePath for user-created
// entries that should land under the custom/ subtree.
func EntityFilePath(rootDir string, kind EntityKind, slug string) string {
	return filepath.Join(rootDir, kind.Folder(), slug+".md")
}

// CustomEntityFilePath returns <root>/<kind>/custom/<slug>.md — the canonical
// destination for entities the user creates themselves (CLI add, TUI 'n').
// These files are preserved across default refreshes and override same-slug
// defaults at load time.
func CustomEntityFilePath(rootDir string, kind EntityKind, slug string) string {
	if isPresetRoot(rootDir) {
		return EntityFilePath(rootDir, kind, slug)
	}
	return filepath.Join(rootDir, kind.Folder(), "custom", slug+".md")
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
