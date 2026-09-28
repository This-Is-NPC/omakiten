package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const subtaskKitCommandsWarning = "commands: ignored at depth >=1; agent always resolves at project root"

type loadBundleOptions struct {
	subtask bool
}

type bundleSourceReader interface {
	readFile(path string, max int64) ([]byte, error)
	listFiles(dir string, suffixes []string, isCustom bool, max int64) ([]entityFile, error)
}

type bundleEntities struct {
	skills           []Skill
	laws             []Law
	personItems      []Persona
	templateItems    []TaskTemplate
	notifications    map[string]Notification
	languages        []Language
	skillWarn        []SourceWarning
	lawWarn          []SourceWarning
	personaWarn      []SourceWarning
	templateWarn     []SourceWarning
	notificationWarn []SourceWarning
	languageWarn     []SourceWarning
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

// LoadBundlePlan reads every source through one caching reader, parses the
// captured bytes, and returns their hashes. A caller can therefore pair a
// planned value with the exact bytes that produced it without a second read.
func LoadBundlePlan(path string) (Bundle, map[string]string, error) {
	raw, err := readFileBounded(path, MaxWiringFileBytes)
	if err != nil {
		return Bundle{}, nil, err
	}
	reader := newPlanSourceReader()
	reader.capture(path, raw)
	bundle, err := loadBundleFromRawReader(path, raw, loadBundleOptions{}, reader)
	if err != nil {
		return Bundle{}, nil, err
	}
	return bundle, reader.hashes, nil
}

type planSourceReader struct {
	raw    map[string][]byte
	hashes map[string]string
}

func newPlanSourceReader() *planSourceReader {
	return &planSourceReader{raw: make(map[string][]byte), hashes: make(map[string]string)}
}

func (r *planSourceReader) capture(path string, raw []byte) {
	if _, ok := r.raw[path]; ok {
		return
	}
	rawCopy := append([]byte(nil), raw...)
	r.raw[path] = rawCopy
	r.hashes[path] = hashBytes(rawCopy)
}

func (r *planSourceReader) readFile(path string, max int64) ([]byte, error) {
	if raw, ok := r.raw[path]; ok {
		return append([]byte(nil), raw...), nil
	}
	raw, err := readFileBounded(path, max)
	if err != nil {
		return nil, err
	}
	r.capture(path, raw)
	return append([]byte(nil), raw...), nil
}

func (r *planSourceReader) listFiles(dir string, suffixes []string, isCustom bool, max int64) ([]entityFile, error) {
	files, err := listFilesIn(dir, suffixes, isCustom, max)
	if err != nil {
		return nil, err
	}
	for i := range files {
		r.capture(files[i].Path, files[i].Raw)
		files[i].Raw = append([]byte(nil), r.raw[files[i].Path]...)
	}
	return files, nil
}

func loadBundle(path string, opts loadBundleOptions) (Bundle, error) {
	raw, err := readFileBounded(path, MaxWiringFileBytes)
	if err != nil {
		return Bundle{}, err
	}
	return loadBundleFromRaw(path, raw, opts)
}

func loadBundleFromRaw(path string, raw []byte, opts loadBundleOptions) (Bundle, error) {
	return loadBundleFromRawReader(path, raw, opts, nil)
}

func loadBundleFromRawReader(path string, raw []byte, opts loadBundleOptions, reader bundleSourceReader) (Bundle, error) {
	if err := validateCurrentConfigLayout(path); err != nil {
		return Bundle{}, err
	}
	rootDir := ConfigRootFromYAMLPath(path)

	wired, fields, importSources, err := readWiringDetailedRawReader(path, raw, reader)
	if err != nil {
		return Bundle{}, err
	}
	if opts.subtask {
		if err := validateSubtaskWiring(path, wired, fields); err != nil {
			return Bundle{}, err
		}
	}

	return finishBundleLoad(path, rootDir, wired, importSources, opts, reader)
}

func finishBundleLoad(path, rootDir string, wired wiring, importSources []string, opts loadBundleOptions, reader bundleSourceReader) (Bundle, error) {
	entities, err := loadBundleEntities(rootDir, reader)
	if err != nil {
		return Bundle{}, err
	}

	theme, themePath, themeErr := resolveActiveThemeReader(rootDir, wired.Config.Theme.Active, reader)

	bundle := Bundle{
		Version:        wired.Version,
		Kit:            wired.Kit,
		SubtaskKit:     strings.TrimSpace(wired.SubtaskKit),
		Config:         wired.Config,
		Workflows:      wired.Workflows,
		Surfaces:       wired.Surfaces,
		Notifications:  entities.notifications,
		Languages:      entities.languages,
		ActiveTheme:    theme,
		ActiveThemeErr: themeErr,
		// Root profile first, then every file it pulled in via a `from:`
		// import directive (stable first-encounter order from the resolver).
		// Hot reload watches all of these — editing an imported file triggers
		// the same rebuild as editing the root YAML.
		SourcePaths: append([]string{path}, importSources...),
		Sources:     buildSettingsSources(wired.Config, wired.Kit.Key),
	}

	if themeErr != nil {
		bundle.Warnings = append(bundle.Warnings, SourceWarning{
			Path:    themePath,
			Message: fmt.Sprintf("active theme %q not loadable: %v", wired.Config.Theme.Active, themeErr),
		})
	}

	prepareBundle(&bundle, path, wired, entities, opts.subtask)

	if err := ValidateBundle(bundle, entities.skills, entities.laws, entities.personItems, entities.templateItems); err != nil {
		return Bundle{}, err
	}
	return loadSubtaskBundle(path, bundle, opts, reader)
}

func prepareBundle(bundle *Bundle, path string, wired wiring, entities bundleEntities, subtask bool) {
	appendBundleEntityWarnings(bundle, entities)
	bundle.Skills = pickSkills(entities.skills, wired.Skills)
	bundle.Laws = pickLaws(entities.laws, wired.Laws, wired.Personas, wired.Projects)
	bundle.Personas = pickPersonas(entities.personItems, wired.Personas)
	bundle.Templates = pickTemplates(entities.templateItems, wired.Templates)
	populateBundleCatalogs(bundle, entities)
	bundle.Projects = pickProjects(wired.Projects)
	bundle.Commands = wired.Commands
	if subtask && len(bundle.Commands) > 0 {
		bundle.Warnings = append(bundle.Warnings, SourceWarning{Path: path, Message: subtaskKitCommandsWarning})
		bundle.Commands = nil
	}
	bundle.Warnings = append(bundle.Warnings, warnDanglingRefs(wired, entities.skills, entities.laws, entities.personItems, entities.templateItems)...)
	bundle.Warnings = append(bundle.Warnings, appendBundleReferenceWarnings(*bundle, entities)...)
	if kit, kitErr := LoadKitConfigByKey(bundle.Kit.Key); kitErr == nil {
		NormalizeEventsRetention(&bundle.Config, kit)
		NormalizeEventsOrphanSweep(&bundle.Config, kit)
	}
	bundle.Warnings = append(bundle.Warnings, warnLogsWindowExceedsRetention(bundle.Config)...)
}

func loadSubtaskBundle(path string, bundle Bundle, opts loadBundleOptions, reader bundleSourceReader) (Bundle, error) {
	if opts.subtask || bundle.SubtaskKit == "" {
		return bundle, nil
	}
	subtaskPath, resolveErr := resolveSubtaskKitPath(path, bundle.SubtaskKit)
	if resolveErr != nil {
		return Bundle{}, resolveErr
	}
	var subtaskBundle Bundle
	var err error
	if reader == nil {
		subtaskBundle, err = loadBundle(subtaskPath, loadBundleOptions{subtask: true})
	} else {
		var raw []byte
		raw, err = reader.readFile(subtaskPath, MaxWiringFileBytes)
		if err == nil {
			subtaskBundle, err = loadBundleFromRawReader(subtaskPath, raw, loadBundleOptions{subtask: true}, reader)
		}
	}
	if err != nil {
		return Bundle{}, fmt.Errorf("subtask_kit %q (%s): %w", bundle.SubtaskKit, subtaskPath, err)
	}
	bundle.SourcePaths = append(bundle.SourcePaths, subtaskBundle.SourcePaths...)
	bundle.Warnings = append(bundle.Warnings, subtaskBundle.Warnings...)
	bundle.SubtaskBundle = &subtaskBundle
	return bundle, nil
}

func loadBundleEntities(rootDir string, reader bundleSourceReader) (bundleEntities, error) {
	var entities bundleEntities
	var err error
	entities.skills, entities.skillWarn, err = loadSkillsReader(filepath.Join(rootDir, EntityKindSkill.Folder()), reader)
	if err != nil {
		return bundleEntities{}, err
	}
	entities.laws, entities.lawWarn, err = loadLawsReader(filepath.Join(rootDir, EntityKindLaw.Folder()), reader)
	if err != nil {
		return bundleEntities{}, err
	}
	entities.personItems, entities.personaWarn, err = loadPersonasReader(filepath.Join(rootDir, EntityKindPersona.Folder()), reader)
	if err != nil {
		return bundleEntities{}, err
	}
	entities.templateItems, entities.templateWarn, err = loadTemplatesReader(filepath.Join(rootDir, EntityKindTemplate.Folder()), reader)
	if err != nil {
		return bundleEntities{}, err
	}
	entities.notifications, entities.notificationWarn, err = loadNotificationsReader(filepath.Join(rootDir, "notifications"), reader)
	if err != nil {
		return bundleEntities{}, err
	}
	entities.languages, entities.languageWarn, err = loadLanguagesReader(filepath.Join(rootDir, "languages"), reader)
	if err != nil {
		return bundleEntities{}, err
	}
	return entities, nil
}

func appendBundleEntityWarnings(bundle *Bundle, entities bundleEntities) {
	bundle.Warnings = append(bundle.Warnings, entities.skillWarn...)
	bundle.Warnings = append(bundle.Warnings, entities.lawWarn...)
	bundle.Warnings = append(bundle.Warnings, entities.personaWarn...)
	bundle.Warnings = append(bundle.Warnings, entities.templateWarn...)
	bundle.Warnings = append(bundle.Warnings, entities.notificationWarn...)
	bundle.Warnings = append(bundle.Warnings, entities.languageWarn...)
}

func populateBundleCatalogs(bundle *Bundle, entities bundleEntities) {
	// All* expose the full on-disk catalog with the active subset flagged,
	// while runtime resolution keeps using the picked slices.
	bundle.AllSkills = catalogSkills(entities.skills, bundle.Skills)
	bundle.AllLaws = catalogLaws(entities.laws, bundle.Laws)
	bundle.AllPersonas = catalogPersonas(entities.personItems, bundle.Personas)
	bundle.AllTemplates = catalogTemplates(entities.templateItems, bundle.Templates)
}

func appendBundleReferenceWarnings(bundle Bundle, entities bundleEntities) []SourceWarning {
	warnings := warnCommandRefs(
		bundle,
		slugSet(loadedPersonaSlugs(entities.personItems)),
		slugSet(loadedLawSlugs(entities.laws)),
		slugSet(loadedTemplateSlugs(entities.templateItems)),
	)
	return warnings
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
// holds both the yaml and the entity folders as siblings.
//
//   - <root>/config/<file>.yaml          → returns <root>
//   - <root>/config/custom/<file>.yaml   → returns <root>
//
// The custom/ branch matters when `.active` resolves to a user-authored
// profile — without it, entity folders would be searched at
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

const CurrentEntitySchemaVersion = 2

func validateCurrentConfigLayout(path string) error {
	configDir := filepath.Dir(path)
	if filepath.Base(configDir) == "custom" {
		if filepath.Base(filepath.Dir(configDir)) != "config" {
			return fmt.Errorf("config path %q must be under a current config/ directory", path)
		}
	} else if filepath.Base(configDir) != "config" {
		return fmt.Errorf("config path %q is outside the current config/ directory", path)
	}

	rootDir := ConfigRootFromYAMLPath(path)
	for _, kind := range []string{"skills", "laws", "personas", "templates"} {
		legacyDir := filepath.Join(rootDir, "config", kind)
		if _, err := lstatNoFollow(legacyDir); err == nil {
			return fmt.Errorf("legacy entity directory %q is not supported; use %s/%s", legacyDir, rootDir, kind)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func validateCurrentPersonaWiring(personas []PersonaWiring) error {
	for _, persona := range personas {
		if persona.SchemaVersion != CurrentEntitySchemaVersion {
			return fmt.Errorf("personas.%s schema_version must be %d", persona.Slug, CurrentEntitySchemaVersion)
		}
	}
	return nil
}

// readWiringDetailed reads, import-expands, and strictly decodes a profile YAML
// file. It returns the decoded wiring, the set of top-level keys present in the
// RESOLVED document (used by sub-kit required-field validation), the list of
// imported source paths (absolute, de-duplicated, first-encounter order), and
// any error.
//
// Import expansion runs BEFORE topLevelYAMLFields and the strict
// KnownFields(true) decode so both observe the resolved shape: a section pulled
// in via `from:` is decoded and validated exactly like an inline section. Unknown
// imported keys fail the same strict decode that rejects unknown inline keys, and
// required-field checks see the expanded document rather than the raw file that
// still carries the directive.
func readWiringDetailed(path string) (wiring, map[string]struct{}, []string, error) {
	raw, err := readFileBounded(path, MaxWiringFileBytes)
	if err != nil {
		return wiring{}, nil, nil, err
	}
	return readWiringDetailedRawReader(path, raw, nil)
}

func readWiringDetailedRawReader(path string, raw []byte, reader bundleSourceReader) (wiring, map[string]struct{}, []string, error) {
	// Parse once into a node tree, expand value-level `from:` directives, then
	// re-encode so the existing strict-decode path runs unchanged over the
	// resolved YAML.
	var probe yaml.Node
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return wiring{}, nil, nil, err
	}
	resolved, sources, err := resolveImportsWithReader(&probe, path, reader)
	if err != nil {
		return wiring{}, nil, nil, err
	}
	// resolveImports returns the canonical root as sources[0] followed by each
	// imported file. The caller already tracks the (lexical) root path, so drop
	// the leading root here and surface only the imported files.
	var importSources []string
	if len(sources) > 1 {
		importSources = sources[1:]
	}
	resolvedRaw, err := yaml.Marshal(resolved)
	if err != nil {
		return wiring{}, nil, nil, fmt.Errorf("re-encode resolved config %q: %w", path, err)
	}

	fields := topLevelYAMLFields(resolvedRaw)
	decoder := yaml.NewDecoder(bytes.NewReader(resolvedRaw))
	decoder.KnownFields(true)

	var w wiring
	if err := decoder.Decode(&w); err != nil {
		return wiring{}, nil, nil, err
	}
	if err := validateCurrentPersonaWiring(w.Personas); err != nil {
		return wiring{}, nil, nil, err
	}
	return w, fields, importSources, nil
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
	if escapesDir(resolvedRoot, resolvedJoined) {
		return "", fmt.Errorf("subtask_kit %q: resolved path %q escapes config directory %q via symlink", rel, resolvedJoined, resolvedRoot)
	}
	return joined, nil
}

// escapesDir reports whether resolvedChild lies outside resolvedRoot after both
// have been symlink-resolved. A child escapes when its path relative to the root
// is ".." itself or begins with a "../" ascent. Shared by resolveSubtaskKitPath
// and resolveImportPath so the two path-safety routines cannot drift — an earlier
// `strings.HasPrefix(rel, "..")` form here over-rejected sibling directories whose
// name merely starts with ".." (e.g. "..foo"), which the import path resolved
// correctly.
func escapesDir(resolvedRoot, resolvedChild string) bool {
	rel, err := filepath.Rel(resolvedRoot, resolvedChild)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
