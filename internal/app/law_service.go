package app

import (
	"context"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type LawService struct {
	snap     *config.Snapshot
	editor   *BundleEditor
	files    EntityFileWriter
	slugger  Slugifier
	registry *domain.EnumRegistry
}

// NewLawService wires the law orchestration layer. registry must be non-nil:
// the service resolves severity labels and ids exclusively through the
// supplied EnumRegistry so each call site operates against the bundle that
// was actually loaded, free of process-global state.
func NewLawService(snap *config.Snapshot, editor *BundleEditor, files EntityFileWriter, slugger Slugifier, registry *domain.EnumRegistry) *LawService {
	return &LawService{snap: snap, editor: editor, files: files, slugger: slugger, registry: registry}
}

// lawsFromSnapshot projects the snapshot's config.Law slice into the
// domain shape. Ids are positional (1-based) and stable within a
// snapshot; severity labels are resolved to the snapshot's configured
// severity ids so the Severity field carries the same int as the
// legacy SQL adapter produced. Unknown labels resolve to 0 — the
// validator rejects bundles with bad severities at import time, so 0
// only appears for laws that survived a bundle without severity
// validation in tests.
func lawsFromSnapshot(snap *config.Snapshot) []domain.Law {
	laws := snap.Laws()
	out := make([]domain.Law, 0, len(laws))
	for i, l := range laws {
		out = append(out, domain.Law{
			ID:         int64(i + 1),
			Key:        l.Slug,
			Name:       l.Name,
			Severity:   domain.Severity(snap.SeverityIDByLabel(l.Severity)),
			Body:       l.Body,
			Scope:      domain.LawScope(l.Scope),
			ProjectKey: l.ProjectSlug,
			PersonaKey: l.PersonaSlug,
			SourcePath: l.SourcePath,
			IsCustom:   l.IsCustom,
		})
	}
	return out
}

// LawListFilter narrows the laws returned by List. Empty values mean "any".
// Phase 2 features (scope/project/persona) leverage these filters.
type LawListFilter struct {
	Scope   string
	Project string
	Persona string
}

func (s *LawService) List(ctx context.Context) ([]domain.Law, error) {
	return s.ListFiltered(ctx, LawListFilter{})
}

func (s *LawService) ListFiltered(_ context.Context, filter LawListFilter) ([]domain.Law, error) {
	bundle, err := s.editor.Load()
	if err != nil {
		return nil, err
	}
	laws := lawsFromSnapshot(config.BuildSnapshot(bundle))
	bySlug := indexLaws(bundle.Laws)
	warnings := warningIndex(bundle.Warnings)
	out := make([]domain.Law, 0, len(laws))
	for _, law := range laws {
		if file, ok := bySlug[law.Key]; ok {
			law.Body = file.Body
			// Frontmatter carries the severity label; resolve to its
			// id via the active registry so the in-memory shape stays
			// consistent with what the sqlite layer wrote. When the
			// label is unknown (typo), keep whatever the store had so
			// the UI can still render and the validator will surface
			// the error on the next bundle import.
			if id, ok := s.severityFromLabel(file.Severity); ok {
				law.Severity = id
			}
			law.Name = file.Name
			law.Scope = domain.LawScope(file.Scope)
			law.ProjectKey = file.ProjectSlug
			law.PersonaKey = file.PersonaSlug
			law.SourcePath = file.SourcePath
		}
		if w, ok := warnings[law.Key]; ok {
			law.Warning = w
		}
		if !filter.matches(law) {
			continue
		}
		out = append(out, law)
	}
	return out, nil
}

func (f LawListFilter) matches(law domain.Law) bool {
	if f.Scope != "" && string(law.Scope) != f.Scope {
		return false
	}
	if f.Project != "" && law.ProjectKey != f.Project {
		return false
	}
	if f.Persona != "" && law.PersonaKey != f.Persona {
		return false
	}
	return true
}

func (s *LawService) Show(ctx context.Context, slug string) (domain.Law, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Law{}, domain.NewError(domain.ErrValidation, "law slug is required", nil)
	}
	laws, err := s.List(ctx)
	if err != nil {
		return domain.Law{}, err
	}
	for _, law := range laws {
		if law.Key == slug {
			return law, nil
		}
	}
	return domain.Law{}, domain.NewError(domain.ErrLawNotFound, "law not found", map[string]any{"slug": slug})
}

func (s *LawService) Add(ctx context.Context, input domain.LawInput) (domain.Law, error) {
	slug, name, severity, body, scope, project, persona, err := normalizeLawInput(input, s.slugger, s.normalizeSeverity)
	if err != nil {
		return domain.Law{}, err
	}

	path := s.files.CustomEntityFilePath(s.editor.RootDir(), config.EntityKindLaw, slug)
	bytes, err := s.files.LawFileBytes(config.Law{Slug: slug, Name: name, 			Severity: s.severityLabel(severity), Body: body})
	if err != nil {
		return domain.Law{}, configError(path, err)
	}

	if err := assertNoCollision(path, slug, "law"); err != nil {
		return domain.Law{}, err
	}

	if _, err := s.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		for _, l := range bundle.Laws {
			if l.Slug == slug {
				return domain.NewError(domain.ErrValidation, "law key must be unique", map[string]any{"slug": slug})
			}
		}
		law := config.Law{
			Slug:     slug,
			Name:     name,
			Severity: s.severityLabel(severity),
			Body:     body,
			Scope:    string(scope),
			IsCustom: true,
		}
		switch scope {
		case domain.LawScopeProject:
			if !projectExists(*bundle, project) {
				return domain.NewError(domain.ErrValidation, "project not found", map[string]any{"slug": project})
			}
			law.ProjectSlug = project
		case domain.LawScopePersona:
			if !personaExists(*bundle, persona) {
				return domain.NewError(domain.ErrPersonaNotFound, "persona not found", map[string]any{"slug": persona})
			}
			law.PersonaSlug = persona
		}
		bundle.Laws = append(bundle.Laws, law)
		return nil
	}, []FileOp{{Op: OpWrite, Path: path, Bytes: bytes}}); err != nil {
		return domain.Law{}, err
	}
	return s.Show(ctx, slug)
}

func (s *LawService) Edit(ctx context.Context, slug string, update domain.LawUpdate) (domain.Law, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.Law{}, domain.NewError(domain.ErrValidation, "law slug is required", nil)
	}
	current, err := s.Show(ctx, slug)
	if err != nil {
		return domain.Law{}, err
	}

	law := config.Law{
		Slug:     slug,
		Name:     current.Name,
		// config.Law.Severity is the YAML/frontmatter label string;
		// translate from the in-memory id back to the configured label
		// so the rewritten file reads naturally.
			Severity: s.severityLabel(current.Severity),
		Body:     current.Body,
	}
	changed := false
	if update.Name != nil {
		law.Name = strings.TrimSpace(*update.Name)
		changed = true
	}
	if update.Severity != nil {
		next, err := s.normalizeSeverity(*update.Severity)
		if err != nil {
			return domain.Law{}, err
		}
		law.Severity = s.severityLabel(next)
		changed = true
	}
	if update.Body != nil {
		next := strings.TrimSpace(*update.Body)
		if next == "" {
			return domain.Law{}, domain.NewError(domain.ErrValidation, "law body is required", nil)
		}
		law.Body = next
		changed = true
	}
	if !changed {
		return current, nil
	}

	path := current.SourcePath
	if path == "" {
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindLaw, slug)
	}
	bytes, err := s.files.LawFileBytes(law)
	if err != nil {
		return domain.Law{}, configError(path, err)
	}

	if _, err := s.editor.ApplyWithFiles(ctx, nil, []FileOp{{Op: OpWrite, Path: path, Bytes: bytes}}); err != nil {
		return domain.Law{}, err
	}
	return s.Show(ctx, slug)
}

func (s *LawService) Remove(ctx context.Context, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return domain.NewError(domain.ErrValidation, "law slug is required", nil)
	}
	current, err := s.Show(ctx, slug)
	if err != nil {
		return err
	}
	path := current.SourcePath
	if path == "" {
		path = s.files.EntityFilePath(s.editor.RootDir(), config.EntityKindLaw, slug)
	}
	_, err = s.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		bundle.Laws = filterLawsBySlug(bundle.Laws, slug)
		for i := range bundle.Personas {
			bundle.Personas[i].Laws = filterStrings(bundle.Personas[i].Laws, slug)
		}
		for i := range bundle.Projects {
			bundle.Projects[i].Laws = filterStrings(bundle.Projects[i].Laws, slug)
		}
		return nil
	}, []FileOp{{Op: OpDelete, Path: path}})
	return err
}

func normalizeLawInput(input domain.LawInput, slugger Slugifier, severityValidator func(domain.Severity) (domain.Severity, error)) (string, string, domain.Severity, string, domain.LawScope, string, string, error) {
	if severityValidator == nil {
		return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation,
			"law severity validator is required; supply LawService.normalizeSeverity",
			nil)
	}
	severity, err := severityValidator(input.Severity)
	if err != nil {
		return "", "", domain.SeverityZero, "", "", "", "", err
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation, "law body is required", nil)
	}
	slug := strings.TrimSpace(input.Key)
	if slug == "" {
		return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation, "law key is required", nil)
	}
	if slugger.Slugify(slug) != slug {
		return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation, "law key must be lowercase, hyphenated", map[string]any{"slug": slug})
	}
	name := strings.TrimSpace(input.Name)
	scope := input.Scope
	if scope == "" {
		scope = domain.LawScopeGlobal
	}
	switch scope {
	case domain.LawScopeGlobal:
		// no owner needed
	case domain.LawScopeProject:
		if strings.TrimSpace(input.Project) == "" {
			return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation, "project slug is required for project-scoped laws", nil)
		}
	case domain.LawScopePersona:
		if strings.TrimSpace(input.Persona) == "" {
			return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation, "persona slug is required for persona-scoped laws", nil)
		}
	default:
		return "", "", domain.SeverityZero, "", "", "", "", domain.NewError(domain.ErrValidation, "law scope must be global, project, or persona", map[string]any{"scope": string(scope)})
	}
	return slug, name, severity, body, scope, strings.TrimSpace(input.Project), strings.TrimSpace(input.Persona), nil
}

// normalizeSeverity validates that the supplied severity id is in the
// active config.severities table. Callers (CLI, MCP) translate user
// input from label to id via the bundle-scoped registry before reaching
// this point; this function only accepts ids and is the second-line
// guard against stale ids (e.g. caller cached an id whose entry was
// removed since).
func (s *LawService) normalizeSeverity(value domain.Severity) (domain.Severity, error) {
	if value == domain.SeverityZero {
		return domain.SeverityZero, domain.NewError(domain.ErrValidation, "law severity is required", nil)
	}
	if !s.isSeverityRegistered(value) {
		return domain.SeverityZero, domain.NewError(domain.ErrValidation,
			"law severity id is not in config.severities",
			map[string]any{"severity": int(value)})
	}
	return value, nil
}


func indexLaws(items []config.Law) map[string]config.Law {
	out := make(map[string]config.Law, len(items))
	for _, item := range items {
		out[item.Slug] = item
	}
	return out
}

func filterLawsBySlug(items []config.Law, slug string) []config.Law {
	out := items[:0]
	for _, item := range items {
		if item.Slug == slug {
			continue
		}
		out = append(out, item)
	}
	return out
}

func projectExists(bundle config.Bundle, slug string) bool {
	for _, project := range bundle.Projects {
		if project.Slug == slug {
			return true
		}
	}
	return false
}

func personaExists(bundle config.Bundle, slug string) bool {
	for _, persona := range bundle.Personas {
		if persona.Slug == slug {
			return true
		}
	}
	return false
}

// severityFromLabel resolves a severity label to its id via the bundle-scoped
// registry.
func (s *LawService) severityFromLabel(label string) (domain.Severity, bool) {
	return s.registry.SeverityFromLabel(label)
}

// severityLabel returns the label for a severity id via the bundle-scoped
// registry.
func (s *LawService) severityLabel(sev domain.Severity) string {
	return s.registry.SeverityLabel(sev)
}

// isSeverityRegistered reports whether the given severity id is known in
// the active table.
func (s *LawService) isSeverityRegistered(sev domain.Severity) bool {
	return s.registry.IsSeverityRegistered(sev)
}
