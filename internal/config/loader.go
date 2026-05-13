package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadBundle reads the active yaml profile plus the per-entity folders
// rooted at the parent directory of the yaml's parent (i.e. the config
// root that holds both `config/<active>.yaml` and the entity folders as
// siblings).
//
// Validation runs against the merged result; dangling refs and missing files
// fail with an error suitable for wrapping in domain.ErrConfigInvalid.
func LoadBundle(path string) (Bundle, error) {
	rootDir := ConfigRootFromYAMLPath(path)

	wired, err := readWiring(path)
	if err != nil {
		return Bundle{}, err
	}

	skills, skillWarn, err := LoadSkills(filepath.Join(rootDir, EntityKindSkill.Folder()))
	if err != nil {
		return Bundle{}, err
	}
	laws, lawWarn, err := LoadLaws(filepath.Join(rootDir, EntityKindLaw.Folder()))
	if err != nil {
		return Bundle{}, err
	}
	personas, personaWarn, err := LoadPersonas(filepath.Join(rootDir, EntityKindPersona.Folder()))
	if err != nil {
		return Bundle{}, err
	}
	templates, templateWarn, err := LoadTemplates(filepath.Join(rootDir, EntityKindTemplate.Folder()))
	if err != nil {
		return Bundle{}, err
	}
	notifications, notificationWarn, err := LoadNotifications(filepath.Join(rootDir, "notifications"))
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
