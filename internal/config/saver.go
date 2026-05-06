package config

import (
	"bytes"
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveBundle writes only the wiring file (omakiten.yaml). Per-entity files are
// written separately via the SkillFile / LawFile / PersonaFile helpers below or
// through the BundleEditor's transactional Apply.
func SaveBundle(path string, bundle Bundle) error {
	w, err := bundleToWiring(bundle)
	if err != nil {
		return err
	}
	data, err := marshalWiring(w)
	if err != nil {
		return err
	}
	return WriteAtomic(path, data)
}

// SaveFullBundle writes the wiring file plus every entity file present in the
// bundle. Tests and migrations use this to materialize a fresh config root
// from a Bundle literal in one call. Entities flagged IsCustom are placed
// under <root>/<kind>/custom/; the rest go to the default location.
func SaveFullBundle(configPath string, bundle Bundle) error {
	rootDir := ConfigRootFromYAMLPath(configPath)
	for _, skill := range bundle.Skills {
		bytes, err := SkillFileBytes(skill)
		if err != nil {
			return err
		}
		if err := WriteAtomic(entityWritePath(rootDir, EntityKindSkill, skill.Slug, skill.IsCustom), bytes); err != nil {
			return err
		}
	}
	for _, law := range bundle.Laws {
		bytes, err := LawFileBytes(law)
		if err != nil {
			return err
		}
		if err := WriteAtomic(entityWritePath(rootDir, EntityKindLaw, law.Slug, law.IsCustom), bytes); err != nil {
			return err
		}
	}
	for _, persona := range bundle.Personas {
		bytes, err := PersonaFileBytes(persona)
		if err != nil {
			return err
		}
		if err := WriteAtomic(entityWritePath(rootDir, EntityKindPersona, persona.Slug, persona.IsCustom), bytes); err != nil {
			return err
		}
	}
	return SaveBundle(configPath, bundle)
}

func entityWritePath(rootDir string, kind EntityKind, slug string, isCustom bool) string {
	if isCustom {
		return CustomEntityFilePath(rootDir, kind, slug)
	}
	return EntityFilePath(rootDir, kind, slug)
}

func bundleToWiring(bundle Bundle) (wiring, error) {
	w := wiring{
		Version:   bundle.Version,
		Kit:       bundle.Kit,
		Config:    bundle.Config,
		Workflows: bundle.Workflows,
	}
	for _, skill := range bundle.Skills {
		w.Skills = append(w.Skills, skill.Slug)
	}
	for _, law := range bundle.Laws {
		if law.Scope == "" || law.Scope == "global" {
			w.Laws = append(w.Laws, law.Slug)
		}
	}
	personaWiring := map[string]int{}
	for _, persona := range bundle.Personas {
		w.Personas = append(w.Personas, PersonaWiring{
			Slug:   persona.Slug,
			Skills: append([]string(nil), persona.Skills...),
			Laws:   append([]string(nil), persona.Laws...),
		})
		personaWiring[persona.Slug] = len(w.Personas) - 1
	}
	for _, law := range bundle.Laws {
		if law.Scope == "persona" && law.PersonaSlug != "" {
			idx, ok := personaWiring[law.PersonaSlug]
			if !ok {
				return wiring{}, fmt.Errorf("law %q references unknown persona %q", law.Slug, law.PersonaSlug)
			}
			if !contains(w.Personas[idx].Laws, law.Slug) {
				w.Personas[idx].Laws = append(w.Personas[idx].Laws, law.Slug)
			}
		}
	}
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
	for _, law := range bundle.Laws {
		if law.Scope == "project" && law.ProjectSlug != "" {
			idx, ok := projectWiring[law.ProjectSlug]
			if !ok {
				return wiring{}, fmt.Errorf("law %q references unknown project %q", law.Slug, law.ProjectSlug)
			}
			if !contains(w.Projects[idx].Laws, law.Slug) {
				w.Projects[idx].Laws = append(w.Projects[idx].Laws, law.Slug)
			}
		}
	}
	return w, nil
}

func marshalWiring(w wiring) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(w); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SkillFileBytes renders skills/<slug>.md content.
func SkillFileBytes(skill Skill) ([]byte, error) {
	fm, err := yaml.Marshal(skillFrontmatter{Name: skill.Name, Description: skill.Description})
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
	fm, err := yaml.Marshal(personaFrontmatter{Name: persona.Name, Description: persona.Description})
	if err != nil {
		return nil, err
	}
	return JoinFrontmatter(fm, []byte(persona.Body)), nil
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
