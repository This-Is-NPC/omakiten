package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"omakiten/defaults"
)

// LoadBundle reads omakiten.yaml plus the per-entity folders rooted at the
// directory containing path, and returns a fully resolved Bundle.
//
// Validation runs against the merged result; dangling refs and missing files
// fail with an error suitable for wrapping in domain.ErrConfigInvalid.
func LoadBundle(path string) (Bundle, error) {
	configDir := filepath.Dir(path)

	wired, err := readWiring(path)
	if err != nil {
		return Bundle{}, err
	}

	skills, skillWarn, err := LoadSkills(filepath.Join(configDir, EntityKindSkill.Folder()))
	if err != nil {
		return Bundle{}, err
	}
	laws, lawWarn, err := LoadLaws(filepath.Join(configDir, EntityKindLaw.Folder()))
	if err != nil {
		return Bundle{}, err
	}
	personas, personaWarn, err := LoadPersonas(filepath.Join(configDir, EntityKindPersona.Folder()))
	if err != nil {
		return Bundle{}, err
	}

	bundle := Bundle{
		Version:   wired.Version,
		Kit:       wired.Kit,
		Config:    wired.Config,
		Workflows: wired.Workflows,
	}

	bundle.Warnings = append(bundle.Warnings, skillWarn...)
	bundle.Warnings = append(bundle.Warnings, lawWarn...)
	bundle.Warnings = append(bundle.Warnings, personaWarn...)

	bundle.Skills = pickSkills(skills, wired.Skills)
	bundle.Laws = pickLaws(laws, wired.Laws, wired.Personas, wired.Projects)
	bundle.Personas = pickPersonas(personas, wired.Personas)
	bundle.Projects = pickProjects(wired.Projects)

	if err := assertRefsResolve(wired, skills, laws, personas); err != nil {
		return Bundle{}, err
	}
	if err := ValidateBundle(bundle, skills, laws, personas); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

// assertRefsResolve checks every slug referenced by omakiten.yaml against the
// loaded entity sets and fails fast on a dangling ref.
func assertRefsResolve(w wiring, skills []Skill, laws []Law, personas []Persona) error {
	skillSet := slugSet(loadedSkillSlugs(skills))
	lawSet := slugSet(loadedLawSlugs(laws))
	personaSet := slugSet(loadedPersonaSlugs(personas))

	for _, slug := range w.Skills {
		if _, ok := skillSet[slug]; !ok {
			return fmt.Errorf("skills: ref %q has no matching file", slug)
		}
	}
	for _, slug := range w.Laws {
		if _, ok := lawSet[slug]; !ok {
			return fmt.Errorf("laws: ref %q has no matching file", slug)
		}
	}
	for _, persona := range w.Personas {
		if _, ok := personaSet[persona.Slug]; !ok {
			return fmt.Errorf("personas: ref %q has no matching file", persona.Slug)
		}
		for _, slug := range persona.Laws {
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("personas.%s laws: ref %q has no matching file", persona.Slug, slug)
			}
		}
		for _, slug := range persona.Skills {
			if _, ok := skillSet[slug]; !ok {
				return fmt.Errorf("personas.%s skills: ref %q has no matching file", persona.Slug, slug)
			}
		}
	}
	for _, project := range w.Projects {
		for _, slug := range project.Laws {
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("projects.%s laws: ref %q has no matching file", project.Slug, slug)
			}
		}
	}
	return nil
}

func readWiring(path string) (wiring, error) {
	file, err := os.Open(path)
	if err != nil {
		return wiring{}, err
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var w wiring
	if err := decoder.Decode(&w); err != nil {
		return wiring{}, err
	}
	return w, nil
}

func pickSkills(loaded []Skill, refs []string) []Skill {
	bySlug := map[string]Skill{}
	for _, s := range loaded {
		bySlug[s.Slug] = s
	}
	out := make([]Skill, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		if s, ok := bySlug[ref]; ok {
			out = append(out, s)
		}
	}
	return out
}

// pickLaws stamps scope/owner metadata on the loaded law records based on where
// the slug is referenced inside the wiring file. A law referenced from
// `personas[i].laws` is scope=persona; from `projects[i].laws` is scope=project;
// otherwise scope=global.
func pickLaws(loaded []Law, global []string, personas []PersonaWiring, projects []ProjectWiring) []Law {
	bySlug := map[string]Law{}
	for _, l := range loaded {
		bySlug[l.Slug] = l
	}

	type scoped struct {
		scope, owner string
	}
	scope := map[string]scoped{}
	for _, slug := range global {
		if _, present := scope[slug]; !present {
			scope[slug] = scoped{scope: "global"}
		}
	}
	for _, persona := range personas {
		for _, slug := range persona.Laws {
			if _, present := scope[slug]; !present {
				scope[slug] = scoped{scope: "persona", owner: persona.Slug}
			}
		}
	}
	for _, project := range projects {
		for _, slug := range project.Laws {
			if _, present := scope[slug]; !present {
				scope[slug] = scoped{scope: "project", owner: project.Slug}
			}
		}
	}

	out := make([]Law, 0, len(scope))
	// Preserve insertion order (global first, then personas, then projects) by
	// re-walking the same lists.
	emit := func(slug, scopeName, owner string) {
		if l, ok := bySlug[slug]; ok {
			l.Scope = scopeName
			switch scopeName {
			case "project":
				l.ProjectSlug = owner
			case "persona":
				l.PersonaSlug = owner
			}
			out = append(out, l)
		}
	}
	emitted := map[string]struct{}{}
	for _, slug := range global {
		if _, dup := emitted[slug]; dup {
			continue
		}
		emitted[slug] = struct{}{}
		emit(slug, "global", "")
	}
	for _, persona := range personas {
		for _, slug := range persona.Laws {
			if _, dup := emitted[slug]; dup {
				continue
			}
			emitted[slug] = struct{}{}
			emit(slug, "persona", persona.Slug)
		}
	}
	for _, project := range projects {
		for _, slug := range project.Laws {
			if _, dup := emitted[slug]; dup {
				continue
			}
			emitted[slug] = struct{}{}
			emit(slug, "project", project.Slug)
		}
	}
	return out
}

func pickPersonas(loaded []Persona, refs []PersonaWiring) []Persona {
	bySlug := map[string]Persona{}
	for _, p := range loaded {
		bySlug[p.Slug] = p
	}
	out := make([]Persona, 0, len(refs))
	for _, ref := range refs {
		if p, ok := bySlug[ref.Slug]; ok {
			p.Skills = append([]string(nil), ref.Skills...)
			p.Laws = append([]string(nil), ref.Laws...)
			out = append(out, p)
		}
	}
	return out
}

func pickProjects(refs []ProjectWiring) []Project {
	out := make([]Project, 0, len(refs))
	for _, ref := range refs {
		out = append(out, Project{
			Slug:        ref.Slug,
			Name:        ref.Name,
			Description: ref.Description,
			Laws:        append([]string(nil), ref.Laws...),
		})
	}
	return out
}

// LoadTheme keeps the existing themes/<slug>.yaml contract — themes are not
// part of this restructure.
func LoadTheme(path string) (Theme, error) {
	file, err := os.Open(path)
	if err != nil {
		return Theme{}, err
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var theme Theme
	if err := decoder.Decode(&theme); err != nil {
		return Theme{}, err
	}
	return theme, ValidateTheme(theme)
}

// HashFile returns the sha256 hex digest of the file at path.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// EnsureDefaultFiles materializes the embedded default kit into configDir on
// first run. Existing files are never overwritten.
func EnsureDefaultFiles(configDir string) error {
	if err := copyDefaultIfMissing(filepath.Join(configDir, "omakiten.yaml"), "omakiten.yaml"); err != nil {
		return err
	}
	if err := copyDefaultDir(configDir, "skills"); err != nil {
		return err
	}
	if err := copyDefaultDir(configDir, "laws"); err != nil {
		return err
	}
	if err := copyDefaultDir(configDir, "personas"); err != nil {
		return err
	}
	if err := copyDefaultIfMissing(filepath.Join(configDir, "themes", "catppuccin-macchiato.yaml"), "themes/catppuccin-macchiato.yaml"); err != nil {
		return err
	}
	return nil
}

func copyDefaultDir(configDir, sub string) error {
	entries, err := defaults.FS.ReadDir(sub)
	if err != nil {
		// A subfolder absent from the embed FS just means there are no
		// defaults of that kind, which is allowed.
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return nil
		}
		return fmt.Errorf("read embedded %s: %w", sub, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := sub + "/" + entry.Name()
		dst := filepath.Join(configDir, sub, entry.Name())
		if err := copyDefaultIfMissing(dst, src); err != nil {
			return err
		}
	}
	return nil
}

func copyDefaultIfMissing(dstPath, srcPath string) error {
	if _, err := os.Stat(dstPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := defaults.FS.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read default %s: %w", srcPath, err)
	}
	return WriteAtomic(dstPath, data)
}

// WriteAtomic writes data to path through a temp file + rename, ensuring no
// reader observes a half-written file.
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

