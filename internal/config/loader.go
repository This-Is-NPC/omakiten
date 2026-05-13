package config

import (
	"path/filepath"
)

// LoadBundle reads the active yaml profile plus the per-entity folders
// rooted at the parent directory of the yaml's parent (i.e. the config
// root that holds both `config/<active>.yaml` and the entity folders as
// siblings). Equivalent to LoadBundleWithRepoLocal(path, "").
//
// Validation runs against the merged result; dangling refs and missing files
// fail with an error suitable for wrapping in domain.ErrConfigInvalid.
func LoadBundle(path string) (Bundle, error) {
	return LoadBundleWithRepoLocal(path, "")
}

// LoadBundleWithRepoLocal is LoadBundle plus an optional repo-local
// override layer rooted at repoLocalDir (e.g. the path returned by
// FindRepoLocal). When non-empty:
//
//   - `<repoLocalDir>/omakiten.yaml` (if present) overlays the global
//     wiring via the entry-merge rules in mergeWiringMaps.
//   - `<repoLocalDir>/<entity>/*.md` files participate in the entity
//     loaders as the highest-precedence source layer (overrides custom
//     and default by slug; new slugs are added).
//   - `<repoLocalDir>/notifications/*.yaml` participates the same way
//     for notifications.
//
// repoLocalDir == "" disables the repo-local layer entirely so existing
// callers retain pre-feature behaviour.
func LoadBundleWithRepoLocal(path, repoLocalDir string) (Bundle, error) {
	rootDir := ConfigRootFromYAMLPath(path)

	wired, disables, err := readWiringWithRepoLocal(path, repoLocalDir)
	if err != nil {
		return Bundle{}, err
	}

	skills, skillWarn, err := LoadSkills(
		filepath.Join(rootDir, EntityKindSkill.Folder()),
		repoLocalEntityDir(repoLocalDir, EntityKindSkill),
	)
	if err != nil {
		return Bundle{}, err
	}
	skills = filterSkillsByDisabled(skills, disables.Skills)

	laws, lawWarn, err := LoadLaws(
		filepath.Join(rootDir, EntityKindLaw.Folder()),
		repoLocalEntityDir(repoLocalDir, EntityKindLaw),
	)
	if err != nil {
		return Bundle{}, err
	}
	laws = filterLawsByDisabled(laws, disables.Laws)

	personas, personaWarn, err := LoadPersonas(
		filepath.Join(rootDir, EntityKindPersona.Folder()),
		repoLocalEntityDir(repoLocalDir, EntityKindPersona),
	)
	if err != nil {
		return Bundle{}, err
	}
	personas = filterPersonasByDisabled(personas, disables.Personas)

	templates, templateWarn, err := LoadTemplates(
		filepath.Join(rootDir, EntityKindTemplate.Folder()),
		repoLocalEntityDir(repoLocalDir, EntityKindTemplate),
	)
	if err != nil {
		return Bundle{}, err
	}
	templates = filterTemplatesByDisabled(templates, disables.Templates)

	notifications, notificationWarn, err := LoadNotifications(
		filepath.Join(rootDir, "notifications"),
		repoLocalSubdir(repoLocalDir, "notifications"),
	)
	if err != nil {
		return Bundle{}, err
	}

	bundle := Bundle{
		Version:   wired.Version,
		Kit:       wired.Kit,
		Config:    wired.Config,
		Workflows: wired.Workflows,
		Notifications:   notifications,
	}

	bundle.Warnings = append(bundle.Warnings, skillWarn...)
	bundle.Warnings = append(bundle.Warnings, lawWarn...)
	bundle.Warnings = append(bundle.Warnings, personaWarn...)
	bundle.Warnings = append(bundle.Warnings, templateWarn...)
	bundle.Warnings = append(bundle.Warnings, notificationWarn...)

	bundle.Skills = pickSkills(skills, wired.Skills)
	bundle.Laws = pickLaws(laws, wired.Laws, wired.Personas, wired.Projects)
	bundle.Personas = pickPersonas(personas, wired.Personas)
	bundle.Templates = pickTemplates(templates, wired.Templates)
	bundle.Projects = pickProjects(wired.Projects)
	bundle.MCPCommands = wired.MCPCommands

	if err := assertRefsResolve(wired, skills, laws, personas, templates); err != nil {
		return Bundle{}, err
	}
	if err := ValidateBundle(bundle, skills, laws, personas, templates); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

// ConfigRootFromYAMLPath strips the trailing `config/<file>.yaml` (or
// `config/custom/<file>.yaml`) from path and returns the layout root that
// holds both the yaml and the entity folders as siblings. Three shapes are
// recognized:
//
//   - <root>/config/<file>.yaml          → returns <root>
//   - <root>/config/custom/<file>.yaml   → returns <root>
//   - <root>/<file>.yaml (legacy flat)   → returns <root>
//
// The custom/ branch matters when `.active` resolves to a user-authored
// profile (or a kit that was migrated into custom/ during a layout
// migration) — without it, entity folders would be searched at
// <root>/config/custom/<entity> instead of <root>/<entity>.
func ConfigRootFromYAMLPath(path string) string {
	configDir := filepath.Dir(path)
	base := filepath.Base(configDir)
	if base == "custom" {
		parent := filepath.Dir(configDir)
		if filepath.Base(parent) == "config" {
			return filepath.Dir(parent)
		}
	}
	if base == "config" {
		return filepath.Dir(configDir)
	}
	return configDir
}

// repoLocalEntityDir returns the per-kind subfolder inside a discovered
// `.omakiten/` directory, or "" when repoLocalDir is empty so entity
// loaders skip the third source layer.
func repoLocalEntityDir(repoLocalDir string, kind EntityKind) string {
	return repoLocalSubdir(repoLocalDir, kind.Folder())
}

func repoLocalSubdir(repoLocalDir, name string) string {
	if repoLocalDir == "" {
		return ""
	}
	return filepath.Join(repoLocalDir, name)
}

// filterSkillsByDisabled drops skills whose slug appears in disabled.
// Applied after the layered LoadSkills pass so the removal subtracts
// from the union of default + custom + repo-local — keeps the
// `*_disabled` semantic working in auto-load mode (when no `skills:`
// wiring slot exists). Same shape as the law/persona/template
// counterparts.
func filterSkillsByDisabled(loaded []Skill, disabled []string) []Skill {
	if len(disabled) == 0 || len(loaded) == 0 {
		return loaded
	}
	drop := make(map[string]struct{}, len(disabled))
	for _, slug := range disabled {
		drop[slug] = struct{}{}
	}
	out := make([]Skill, 0, len(loaded))
	for _, s := range loaded {
		if _, gone := drop[s.Slug]; gone {
			continue
		}
		out = append(out, s)
	}
	return out
}

func filterLawsByDisabled(loaded []Law, disabled []string) []Law {
	if len(disabled) == 0 || len(loaded) == 0 {
		return loaded
	}
	drop := make(map[string]struct{}, len(disabled))
	for _, slug := range disabled {
		drop[slug] = struct{}{}
	}
	out := make([]Law, 0, len(loaded))
	for _, l := range loaded {
		if _, gone := drop[l.Slug]; gone {
			continue
		}
		out = append(out, l)
	}
	return out
}

func filterPersonasByDisabled(loaded []Persona, disabled []string) []Persona {
	if len(disabled) == 0 || len(loaded) == 0 {
		return loaded
	}
	drop := make(map[string]struct{}, len(disabled))
	for _, slug := range disabled {
		drop[slug] = struct{}{}
	}
	out := make([]Persona, 0, len(loaded))
	for _, p := range loaded {
		if _, gone := drop[p.Slug]; gone {
			continue
		}
		out = append(out, p)
	}
	return out
}

func filterTemplatesByDisabled(loaded []TaskTemplate, disabled []string) []TaskTemplate {
	if len(disabled) == 0 || len(loaded) == 0 {
		return loaded
	}
	drop := make(map[string]struct{}, len(disabled))
	for _, slug := range disabled {
		drop[slug] = struct{}{}
	}
	out := make([]TaskTemplate, 0, len(loaded))
	for _, tm := range loaded {
		if _, gone := drop[tm.Slug]; gone {
			continue
		}
		out = append(out, tm)
	}
	return out
}
