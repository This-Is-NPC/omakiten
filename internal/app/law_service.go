package app

import (
	"context"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type LawService struct {
	repo   ConfigRepository
	editor *BundleEditor
}

func NewLawService(repo ConfigRepository, editor *BundleEditor) *LawService {
	return &LawService{repo: repo, editor: editor}
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

func (s *LawService) ListFiltered(ctx context.Context, filter LawListFilter) ([]domain.Law, error) {
	laws, err := s.repo.ListActiveLaws(ctx)
	if err != nil {
		return nil, err
	}
	bundle, err := s.editor.Load()
	if err != nil {
		return nil, err
	}
	bySlug := indexLaws(bundle.Laws)
	warnings := warningIndex(bundle.Warnings)
	out := make([]domain.Law, 0, len(laws))
	for _, law := range laws {
		if file, ok := bySlug[law.Key]; ok {
			law.Body = file.Body
			law.Severity = file.Severity
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
	slug, name, severity, body, scope, project, persona, err := normalizeLawInput(input)
	if err != nil {
		return domain.Law{}, err
	}

	path := config.CustomEntityFilePath(s.editor.RootDir(), config.EntityKindLaw, slug)
	bytes, err := config.LawFileBytes(config.Law{Slug: slug, Name: name, Severity: string(severity), Body: body})
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
			Severity: string(severity),
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
		Severity: current.Severity,
		Body:     current.Body,
	}
	changed := false
	if update.Name != nil {
		law.Name = strings.TrimSpace(*update.Name)
		changed = true
	}
	if update.Severity != nil {
		next, err := normalizeSeverity(*update.Severity)
		if err != nil {
			return domain.Law{}, err
		}
		law.Severity = string(next)
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
		path = config.EntityFilePath(s.editor.RootDir(), config.EntityKindLaw, slug)
	}
	bytes, err := config.LawFileBytes(law)
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
		path = config.EntityFilePath(s.editor.RootDir(), config.EntityKindLaw, slug)
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

func normalizeLawInput(input domain.LawInput) (string, string, domain.LawSeverity, string, domain.LawScope, string, string, error) {
	severity, err := normalizeSeverity(input.Severity)
	if err != nil {
		return "", "", "", "", "", "", "", err
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return "", "", "", "", "", "", "", domain.NewError(domain.ErrValidation, "law body is required", nil)
	}
	slug := strings.TrimSpace(input.Key)
	if slug == "" {
		return "", "", "", "", "", "", "", domain.NewError(domain.ErrValidation, "law key is required", nil)
	}
	if config.Slugify(slug) != slug {
		return "", "", "", "", "", "", "", domain.NewError(domain.ErrValidation, "law key must be lowercase, hyphenated", map[string]any{"slug": slug})
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
			return "", "", "", "", "", "", "", domain.NewError(domain.ErrValidation, "project slug is required for project-scoped laws", nil)
		}
	case domain.LawScopePersona:
		if strings.TrimSpace(input.Persona) == "" {
			return "", "", "", "", "", "", "", domain.NewError(domain.ErrValidation, "persona slug is required for persona-scoped laws", nil)
		}
	default:
		return "", "", "", "", "", "", "", domain.NewError(domain.ErrValidation, "law scope must be global, project, or persona", map[string]any{"scope": string(scope)})
	}
	return slug, name, severity, body, scope, strings.TrimSpace(input.Project), strings.TrimSpace(input.Persona), nil
}

func normalizeSeverity(value domain.LawSeverity) (domain.LawSeverity, error) {
	severity := domain.LawSeverity(strings.TrimSpace(strings.ToLower(string(value))))
	switch severity {
	case domain.LawSeverityInfo, domain.LawSeverityWarning, domain.LawSeverityError:
		return severity, nil
	}
	return "", domain.NewError(domain.ErrValidation, "law severity must be info, warning, or error", map[string]any{"severity": string(value)})
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
