package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const subtaskKitMCPCommandsWarning = "mcp_commands: ignored at depth >=1; MCP always resolves at project root"

type loadBundleOptions struct {
	subtask bool
}

// LoadBundle reads the active yaml profile plus the per-entity folders
// rooted at the parent directory of the yaml's parent (i.e. the config
// root that holds both `config/<active>.yaml` and the entity folders as
// siblings).
//
// Validation runs against the merged result; dangling refs and missing files
// fail with an error suitable for wrapping in domain.ErrConfigInvalid.
func LoadBundle(path string) (Bundle, error) {
	return loadBundle(path, loadBundleOptions{})
}

func loadBundle(path string, opts loadBundleOptions) (Bundle, error) {
	rootDir := ConfigRootFromYAMLPath(path)

	wired, fields, err := readWiringDetailed(path)
	if err != nil {
		return Bundle{}, err
	}
	if opts.subtask {
		if err := validateSubtaskWiring(path, wired, fields); err != nil {
			return Bundle{}, err
		}
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
	languages, languageWarn, err := LoadLanguages(filepath.Join(rootDir, "languages"))
	if err != nil {
		return Bundle{}, err
	}

	theme, themePath, themeErr := resolveActiveTheme(rootDir, wired.Config.Theme.Active)

	bundle := Bundle{
		Version:        wired.Version,
		Kit:            wired.Kit,
		SubtaskKit:     strings.TrimSpace(wired.SubtaskKit),
		Config:         wired.Config,
		Workflows:      wired.Workflows,
		Notifications:  notifications,
		Languages:      languages,
		ActiveTheme:    theme,
		ActiveThemeErr: themeErr,
		SourcePaths:    []string{path},
		Sources:        buildSettingsSources(wired.Config, wired.Kit.Key),
	}

	if themeErr != nil {
		bundle.Warnings = append(bundle.Warnings, SourceWarning{
			Path:    themePath,
			Message: fmt.Sprintf("active theme %q not loadable: %v", wired.Config.Theme.Active, themeErr),
		})
	}

	bundle.Warnings = append(bundle.Warnings, skillWarn...)
	bundle.Warnings = append(bundle.Warnings, lawWarn...)
	bundle.Warnings = append(bundle.Warnings, personaWarn...)
	bundle.Warnings = append(bundle.Warnings, templateWarn...)
	bundle.Warnings = append(bundle.Warnings, notificationWarn...)
	bundle.Warnings = append(bundle.Warnings, languageWarn...)

	bundle.Skills = pickSkills(skills, wired.Skills)
	bundle.Laws = pickLaws(laws, wired.Laws, wired.Personas, wired.Projects)
	bundle.Personas = pickPersonas(personas, wired.Personas)
	bundle.Templates = pickTemplates(templates, wired.Templates)

	// All* expose the full on-disk catalog with the active subset flagged,
	// so the Settings view lists every preset's entities (not just the
	// active wiring) while runtime resolution keeps using the picked slices.
	bundle.AllSkills = buildCatalog(skills, bundle.Skills,
		func(s Skill) string { return s.Slug },
		func(s Skill, a bool) Skill { s.Active = a; return s })
	bundle.AllLaws = buildCatalog(laws, bundle.Laws,
		func(l Law) string { return l.Slug },
		func(l Law, a bool) Law { l.Active = a; return l })
	bundle.AllPersonas = buildCatalog(personas, bundle.Personas,
		func(p Persona) string { return p.Slug },
		func(p Persona, a bool) Persona { p.Active = a; return p })
	bundle.AllTemplates = buildCatalog(templates, bundle.Templates,
		func(t TaskTemplate) string { return t.Slug },
		func(t TaskTemplate, a bool) TaskTemplate { t.Active = a; return t })

	bundle.Projects = pickProjects(wired.Projects)
	bundle.MCPCommands = wired.MCPCommands
	if opts.subtask && len(bundle.MCPCommands) > 0 {
		bundle.Warnings = append(bundle.Warnings, SourceWarning{Path: path, Message: subtaskKitMCPCommandsWarning})
		bundle.MCPCommands = nil
	}

	bundle.Warnings = append(bundle.Warnings, warnDanglingRefs(wired, skills, laws, personas, templates)...)
	bundle.Warnings = append(bundle.Warnings, warnMCPCommandRefs(
		bundle,
		slugSet(loadedPersonaSlugs(personas)),
		slugSet(loadedLawSlugs(laws)),
		slugSet(loadedTemplateSlugs(templates)),
	)...)
	if err := ValidateBundle(bundle, skills, laws, personas, templates); err != nil {
		return Bundle{}, err
	}

	if !opts.subtask && bundle.SubtaskKit != "" {
		subtaskPath, err := resolveSubtaskKitPath(path, bundle.SubtaskKit)
		if err != nil {
			return Bundle{}, err
		}
		subtaskBundle, err := loadBundle(subtaskPath, loadBundleOptions{subtask: true})
		if err != nil {
			return Bundle{}, fmt.Errorf("subtask_kit %q (%s): %w", bundle.SubtaskKit, subtaskPath, err)
		}
		bundle.SourcePaths = append(bundle.SourcePaths, subtaskBundle.SourcePaths...)
		bundle.Warnings = append(bundle.Warnings, subtaskBundle.Warnings...)
		bundle.SubtaskBundle = &subtaskBundle
	}
	return bundle, nil
}

// buildSettingsSources computes the per-leaf-path origin map for the
// loaded Settings against the embedded kit baseline matching `kitKey`
// (falls back to omakase when the binary does not ship a baseline for
// the bundle's custom kit). The env-overlay hook fires after the diff so
// a future loader-level env binding promotes its path to SourceEnv
// without disturbing default/project classification.
//
// Returns nil when the kit baseline is unreadable; consumers fall back
// to SourceDefault via Bundle.SourceFor, so a missing baseline degrades
// to "show every leaf as default" rather than aborting the load.
func buildSettingsSources(user Settings, kitKey string) map[string]string {
	kit, err := LoadKitConfigByKey(kitKey)
	if err != nil {
		return nil
	}
	sources := computeSettingsSources(user, kit)
	if sources == nil {
		return nil
	}
	ApplyEnvOverlay(sources, os.LookupEnv)
	return sources
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

func readWiringDetailed(path string) (wiring, map[string]struct{}, error) {
	raw, err := readFileBounded(path, MaxWiringFileBytes)
	if err != nil {
		return wiring{}, nil, err
	}
	fields := topLevelYAMLFields(raw)
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)

	var w wiring
	if err := decoder.Decode(&w); err != nil {
		return wiring{}, nil, err
	}
	return w, fields, nil
}

func topLevelYAMLFields(raw []byte) map[string]struct{} {
	fields := map[string]struct{}{}
	var doc yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(raw)).Decode(&doc); err != nil {
		return fields
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fields
	}
	for i := 0; i+1 < len(doc.Content[0].Content); i += 2 {
		fields[doc.Content[0].Content[i].Value] = struct{}{}
	}
	return fields
}

func validateSubtaskWiring(path string, wired wiring, fields map[string]struct{}) error {
	required := []string{"kit", "config", "workflows"}
	for _, field := range required {
		if _, ok := fields[field]; !ok {
			return fmt.Errorf("sub-kit %q: %s block is required", path, field)
		}
	}
	if strings.TrimSpace(wired.SubtaskKit) != "" {
		return fmt.Errorf("sub-kit %q: nested subtask_kit is not supported", path)
	}
	return nil
}

func resolveSubtaskKitPath(rootPath, rel string) (string, error) {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return "", nil
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("subtask_kit %q: path must be relative to %s", rel, filepath.Dir(rootPath))
	}
	for _, part := range strings.Split(filepath.ToSlash(trimmed), "/") {
		if part == ".." {
			return "", fmt.Errorf("subtask_kit %q: path must not contain parent directory segments", rel)
		}
	}
	configDir := filepath.Dir(rootPath)
	joined := filepath.Join(configDir, trimmed)
	// Reject symlinks that point outside the config dir. The earlier guards
	// catch lexical `..` and absolute paths, but a symlink at any segment can
	// still escape: e.g. `config/sub.yaml -> ../../../etc/passwd`. Resolve
	// the canonical path and assert it stays rooted under configDir. Missing
	// files surface a distinct error at the os.Open call site downstream;
	// EvalSymlinks errors for paths that do not exist yet, so the not-exist
	// branch is treated as "no symlink escape" and the downstream open
	// produces the friendlier diagnostic.
	resolvedRoot, err := filepath.EvalSymlinks(configDir)
	if err != nil {
		// configDir itself unreadable is a separate failure surfaced via the
		// downstream open path; do not double-report here.
		return joined, nil
	}
	resolvedJoined, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// File does not exist yet (or unreadable) — let the downstream open
		// produce the canonical error.
		return joined, nil
	}
	rel2, err := filepath.Rel(resolvedRoot, resolvedJoined)
	if err != nil || strings.HasPrefix(rel2, "..") || rel2 == ".." {
		return "", fmt.Errorf("subtask_kit %q: resolved path %q escapes config directory %q via symlink", rel, resolvedJoined, resolvedRoot)
	}
	return joined, nil
}
