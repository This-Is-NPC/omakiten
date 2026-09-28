package operation

import (
	"context"
	"strconv"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func (s *Service) entityReposOrErr() (app.EntityServiceRepos, error) {
	if s.entity.editor == nil || s.entity.files == nil || s.entity.slugger == nil {
		return app.EntityServiceRepos{}, domain.NewError(domain.ErrValidation, "entity authoring is not wired", nil)
	}
	return app.EntityServiceRepos{Editor: s.entity.editor, Files: s.entity.files, Slugger: s.entity.slugger}, nil
}

func (s *Service) skillService() (*app.SkillService, error) {
	repos, err := s.entityReposOrErr()
	if err != nil {
		return nil, err
	}
	return app.NewSkillService(repos, s.snapshot), nil
}

func (s *Service) lawService() (*app.LawService, error) {
	repos, err := s.entityReposOrErr()
	if err != nil {
		return nil, err
	}
	return app.NewLawService(repos, s.snapshot, s.registry), nil
}

func (s *Service) personaService() (*app.PersonaService, error) {
	repos, err := s.entityReposOrErr()
	if err != nil {
		return nil, err
	}
	return app.NewPersonaService(repos, s.snapshot), nil
}

// ReimportBundle reloads the on-disk bundle after an external editor write.
func (s *Service) ReimportBundle(ctx context.Context) error {
	if s.entity.editor == nil {
		return domain.NewError(domain.ErrValidation, "entity authoring is not wired", nil)
	}
	bundle, _, sourceHashes, err := s.entity.editor.LoadPlan()
	if err != nil {
		return err
	}
	_, err = s.entity.editor.Apply(ctx, bundle, sourceHashes, nil)
	return err
}

func (s *Service) AddSkill(ctx context.Context, input domain.SkillInput) (domain.Skill, error) {
	svc, err := s.skillService()
	if err != nil {
		return domain.Skill{}, err
	}
	return svc.Add(ctx, input)
}

func (s *Service) EditSkill(ctx context.Context, ref string, update domain.SkillUpdate) (domain.Skill, error) {
	svc, err := s.skillService()
	if err != nil {
		return domain.Skill{}, err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrSkillNotFound, "skill")
	if err != nil {
		return domain.Skill{}, err
	}
	return svc.Edit(ctx, slug, update)
}

func (s *Service) RemoveSkill(ctx context.Context, ref string) (string, error) {
	svc, err := s.skillService()
	if err != nil {
		return "", err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrSkillNotFound, "skill")
	if err != nil {
		return "", err
	}
	if err := svc.Remove(ctx, slug); err != nil {
		return "", err
	}
	return slug, nil
}

func (s *Service) SkillEntity(ctx context.Context, ref string) (domain.Skill, error) {
	svc, err := s.skillService()
	if err != nil {
		return domain.Skill{}, err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrSkillNotFound, "skill")
	if err != nil {
		return domain.Skill{}, err
	}
	return svc.Show(ctx, slug)
}

func (s *Service) AddLaw(ctx context.Context, input domain.LawInput) (domain.Law, error) {
	svc, err := s.lawService()
	if err != nil {
		return domain.Law{}, err
	}
	return svc.Add(ctx, input)
}

func (s *Service) EditLaw(ctx context.Context, ref string, update domain.LawUpdate) (domain.Law, error) {
	svc, err := s.lawService()
	if err != nil {
		return domain.Law{}, err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrLawNotFound, "law")
	if err != nil {
		return domain.Law{}, err
	}
	return svc.Edit(ctx, slug, update)
}

func (s *Service) RemoveLaw(ctx context.Context, ref string) (string, error) {
	svc, err := s.lawService()
	if err != nil {
		return "", err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrLawNotFound, "law")
	if err != nil {
		return "", err
	}
	if err := svc.Remove(ctx, slug); err != nil {
		return "", err
	}
	return slug, nil
}

func (s *Service) LawEntity(ctx context.Context, ref string) (domain.Law, error) {
	svc, err := s.lawService()
	if err != nil {
		return domain.Law{}, err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrLawNotFound, "law")
	if err != nil {
		return domain.Law{}, err
	}
	return svc.Show(ctx, slug)
}

func (s *Service) AddPersona(ctx context.Context, input domain.PersonaInput) (domain.Persona, error) {
	svc, err := s.personaService()
	if err != nil {
		return domain.Persona{}, err
	}
	return svc.Add(ctx, input)
}

func (s *Service) EditPersona(ctx context.Context, ref string, update domain.PersonaUpdate) (domain.Persona, error) {
	svc, err := s.personaService()
	if err != nil {
		return domain.Persona{}, err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrPersonaNotFound, "persona")
	if err != nil {
		return domain.Persona{}, err
	}
	return svc.Edit(ctx, slug, update)
}

func (s *Service) RemovePersona(ctx context.Context, ref string) (string, error) {
	svc, err := s.personaService()
	if err != nil {
		return "", err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrPersonaNotFound, "persona")
	if err != nil {
		return "", err
	}
	if err := svc.Remove(ctx, slug); err != nil {
		return "", err
	}
	return slug, nil
}

func (s *Service) PersonaEntity(ctx context.Context, ref string) (domain.Persona, error) {
	svc, err := s.personaService()
	if err != nil {
		return domain.Persona{}, err
	}
	slug, err := resolveEntityRef(ctx, ref, svc.List, domain.ErrPersonaNotFound, "persona")
	if err != nil {
		return domain.Persona{}, err
	}
	return svc.Show(ctx, slug)
}

func resolveEntityRef[T any](ctx context.Context, raw string, list func(context.Context) ([]T, error), notFound domain.ErrorCode, kind string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, kind+" slug is required", nil)
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		items, listErr := list(ctx)
		if listErr != nil {
			return "", listErr
		}
		for _, item := range items {
			key, itemID := entityIdentity(item)
			if itemID == id {
				return key, nil
			}
		}
		return "", domain.NewError(notFound, kind+" not found", map[string]any{"id": id})
	}
	if domain.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, kind+" slug must be lowercase, hyphenated", map[string]any{"slug": raw})
	}
	return raw, nil
}

func entityIdentity(item any) (string, int64) {
	switch v := item.(type) {
	case domain.Skill:
		return v.Key, v.ID
	case domain.Law:
		return v.Key, v.ID
	case domain.Persona:
		return v.Key, v.ID
	default:
		return "", 0
	}
}
